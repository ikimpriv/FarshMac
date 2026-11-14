package kube_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"

	"farshmac/internal/kube"
	"farshmac/internal/kube/localexec"
)

func executorWith(t *testing.T, extra ...string) *localexec.Executor {
	t.Helper()
	return localexec.New(t, "", append(strings.Fields(localexec.BaseTools), extra...)...)
}

func openFS(t *testing.T, x *localexec.Executor) *kube.ContainerFS {
	t.Helper()
	ctx := context.Background()
	probe, err := kube.ProbeContainer(ctx, x, kube.ContainerRef{})
	if err != nil {
		t.Fatal(err)
	}
	fs, err := kube.OpenContainerFS(ctx, x, kube.ContainerRef{}, probe)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestProbeContainerDetectsTools(t *testing.T) {
	x := executorWith(t, "tar", "find", "stat")
	p, err := kube.ProbeContainer(context.Background(), x, kube.ContainerRef{})
	if err != nil || p.Shell != "/bin/bash" && p.Shell != "/bin/sh" || !p.Tar || !p.Find || !p.Stat || !p.Ls {
		t.Fatalf("probe %+v %v", p, err)
	}
	x = executorWith(t)
	if p, err = kube.ProbeContainer(context.Background(), x, kube.ContainerRef{}); err != nil || p.Tar || p.Find || p.Stat || !p.Ls {
		t.Fatalf("probe without tar/find/stat %+v %v", p, err)
	}
	x.Missing = map[string]bool{"/bin/bash": true}
	if p, err = kube.ProbeContainer(context.Background(), x, kube.ContainerRef{}); err != nil || p.Shell != "/bin/sh" {
		t.Fatalf("fallback to sh %+v %v", p, err)
	}
}

func TestProbeContainerWithoutShell(t *testing.T) {
	x := executorWith(t)
	x.Missing = map[string]bool{"/bin/bash": true, "/bin/sh": true}
	_, err := kube.ProbeContainer(context.Background(), x, kube.ContainerRef{})
	if !errors.Is(err, kube.ErrNoShell) || !strings.Contains(err.Error(), "no such file") {
		t.Fatal("expected a no-shell error, got", err)
	}
	if _, err = kube.OpenContainerFS(context.Background(), x, kube.ContainerRef{}, kube.Probe{}); !errors.Is(err, kube.ErrNoShell) {
		t.Fatal("open without shell", err)
	}
	if fs, err := kube.OpenContainerFS(context.Background(), x, kube.ContainerRef{}, kube.Probe{Shell: "/bin/sh"}); err != nil || fs.ListMode() != "builtin" {
		t.Fatal("a shell alone lists with its builtins", fs, err)
	}
	if fs := openFS(t, executorWith(t)); fs.HasTar() {
		t.Fatal("tar reported without tar")
	}
}

func TestReadDirInAllListingModes(t *testing.T) {
	root := localexec.Fixture(t)
	for _, mode := range []struct {
		name  string
		tools []string
	}{{"find", []string{"find", "stat"}}, {"stat", []string{"stat"}}, {"ls", nil}} {
		t.Run(mode.name, func(t *testing.T) {
			fs := openFS(t, executorWith(t, mode.tools...))
			if (fs.Probe().Find && mode.name != "find") || (!fs.Probe().Find && fs.Probe().Stat && mode.name != "stat") {
				t.Fatalf("wrong listing mode selected: %+v", fs.Probe())
			}
			entries, err := fs.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]os.FileInfo{}
			for _, e := range entries {
				got[e.Name()] = e
			}
			for _, name := range []string{"plain.txt", "it's \"quoted\".txt", "файл №1", "sub dir", ".hidden", "link-to-dir", "broken"} {
				if _, ok := got[name]; !ok {
					t.Fatalf("%s missing from %v", name, keys(got))
				}
			}
			if len(got) != 7 {
				t.Fatal("unexpected entries", keys(got))
			}
			if e := got["plain.txt"]; e.Size() != 5 || e.IsDir() || e.Mode().Perm() != 0640 {
				t.Fatalf("plain.txt %v %o", e.Size(), e.Mode())
			}
			if e := got["sub dir"]; !e.IsDir() || e.Mode().Perm() != 0755 {
				t.Fatalf("sub dir %v", e.Mode())
			}
			if e := got["link-to-dir"]; e.Mode()&os.ModeSymlink == 0 || e.IsDir() {
				t.Fatalf("link-to-dir lstat %v", e.Mode())
			}
			if e := got["broken"]; e.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("broken %v", e.Mode())
			}
			local, _ := os.Lstat(filepath.Join(root, "plain.txt"))
			if d := got["plain.txt"].ModTime().Sub(local.ModTime()); d < -time.Minute || d > time.Minute {
				t.Fatal("mtime off", got["plain.txt"].ModTime(), local.ModTime())
			}
			// Stat follows the link to the directory; the broken one fails.
			if info, err := fs.Stat(filepath.Join(root, "link-to-dir")); err != nil || !info.IsDir() || info.Name() != "link-to-dir" {
				t.Fatal("Stat through link", info, err)
			}
			if _, err := fs.Stat(filepath.Join(root, "broken")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("broken link Stat", err)
			}
			if info, err := fs.Lstat(filepath.Join(root, "it's \"quoted\".txt")); err != nil || info.Size() != 1 || info.Name() != "it's \"quoted\".txt" {
				t.Fatal("Lstat quoted", info, err)
			}
			if _, err := fs.Lstat(filepath.Join(root, "nope")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("Lstat missing", err)
			}
			if _, err := fs.ReadDir(filepath.Join(root, "nope")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("ReadDir missing", err)
			}
		})
	}
}

func keys(m map[string]os.FileInfo) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestContainerFSFileOperations(t *testing.T) {
	root := localexec.Fixture(t)
	fs := openFS(t, executorWith(t))
	// Open / Create round trip of binary data.
	data := make([]byte, 70000)
	for i := range data {
		data[i] = byte(i * 7)
	}
	target := filepath.Join(root, "new dir", "bin's.dat")
	if err := fs.Mkdir(filepath.Dir(target)); err != nil {
		t.Fatal(err)
	}
	w, err := fs.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := fs.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	back, err := io.ReadAll(r)
	r.Close()
	if err != nil || !bytes.Equal(back, data) {
		t.Fatal("round trip", len(back), err)
	}
	// Create refuses an existing file, like sftp's O_EXCL.
	if w, err = fs.Create(target); err == nil {
		w.Write([]byte("x"))
		if err = w.Close(); !errors.Is(err, os.ErrExist) {
			t.Fatal("overwrite must fail with ErrExist, got", err)
		}
	}
	if _, err = fs.Open(filepath.Join(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("open missing", err)
	}
	if _, err = fs.Open(root); err == nil {
		t.Fatal("open directory must fail")
	}
	// Rename, Chmod, Chtimes, Readlink, Symlink, Remove.
	moved := filepath.Join(root, "moved.dat")
	if err = fs.Rename(target, moved); err != nil {
		t.Fatal(err)
	}
	if err = fs.Chmod(moved, 0600); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC)
	if err = fs.Chtimes(moved, when); err != nil {
		t.Fatal(err)
	}
	local, err := os.Lstat(moved)
	if err != nil || local.Mode().Perm() != 0600 || !local.ModTime().Equal(when) {
		t.Fatal("chmod/chtimes", local, err)
	}
	if err = fs.Symlink("moved.dat", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if tgt, err := fs.Readlink(filepath.Join(root, "alias")); err != nil || tgt != "moved.dat" {
		t.Fatal("readlink", tgt, err)
	}
	if _, err = fs.Readlink(moved); err == nil {
		t.Fatal("readlink of a file must fail")
	}
	if err = fs.Remove(filepath.Join(root, "new dir"), true); err != nil {
		t.Fatal(err)
	}
	if err = fs.Remove(moved, false); err != nil {
		t.Fatal(err)
	}
	if err = fs.Remove(moved, false); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("remove missing", err)
	}
	if err = fs.Remove(filepath.Join(root, "sub dir"), true); err == nil {
		t.Fatal("rmdir of a non-empty directory must fail")
	}
	if err = fs.Mkdir(filepath.Join(root, "sub dir")); !errors.Is(err, os.ErrExist) {
		t.Fatal("mkdir existing", err)
	}
	if err = fs.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestContainerFSHonoursCancellation(t *testing.T) {
	x := executorWith(t)
	ctx, cancel := context.WithCancel(context.Background())
	fs, err := kube.OpenContainerFS(ctx, x, kube.ContainerRef{}, kube.Probe{Shell: "/bin/sh", Ls: true})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err = fs.ReadDir(t.TempDir()); err == nil {
		t.Fatal("cancelled context must fail the command")
	}
}

func TestExecStreamInteractive(t *testing.T) {
	x := executorWith(t, "stty")
	ctx := context.Background()
	s, err := kube.StartExec(ctx, x, kube.ContainerRef{}, []string{"/bin/sh", "-c", "stty size; read x; echo \"got:$x\"; stty size; exit 3"}, 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	collect := func(until string) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for !strings.Contains(out.String(), until) {
			select {
			case chunk, ok := <-s.Output():
				if !ok {
					t.Fatalf("stream ended before %q: %q", until, out.String())
				}
				out.Write(chunk)
			case <-deadline:
				t.Fatalf("timeout waiting for %q: %q", until, out.String())
			}
		}
	}
	collect("30 100")
	if err = s.Resize(50, 20); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if err = s.Write([]byte("hello\r")); err != nil {
		t.Fatal(err)
	}
	collect("got:hello")
	collect("20 50")
	for range s.Output() {
	}
	<-s.Done()
	if code, ok := kube.ExitCode(s.Err()); !ok || code != 3 {
		t.Fatal("exit code", s.Err())
	}
	if err = s.Write([]byte("x")); err == nil {
		t.Fatal("write after exit must fail")
	}
	s.Close()
}

func TestExecStreamCloseCancels(t *testing.T) {
	x := executorWith(t, "sleep")
	s, err := kube.StartExec(context.Background(), x, kube.ContainerRef{}, []string{"/bin/sh", "-c", "sleep 30"}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("close did not end the stream")
	}
	if s.Err() == nil {
		t.Fatal("a cancelled stream must report an error")
	}
	if _, ok := s.Err().(utilexec.CodeExitError); ok && !errors.Is(s.Err(), context.Canceled) {
		// Either a context error or the shell's exit after SIGHUP is acceptable.
	}
	s.Close() // idempotent
}

func TestExitCode(t *testing.T) {
	if code, ok := kube.ExitCode(utilexec.CodeExitError{Err: errors.New("x"), Code: 7}); !ok || code != 7 {
		t.Fatal(code, ok)
	}
	if code, ok := kube.ExitCode(&utilexec.CodeExitError{Err: errors.New("x"), Code: 8}); !ok || code != 8 {
		t.Fatal(code, ok)
	}
	if _, ok := kube.ExitCode(errors.New("other")); ok {
		t.Fatal("plain error has no code")
	}
	var zero remotecommand.TerminalSize
	_ = zero
}

// failingExecutor answers every exec with one error, like a runtime that
// cannot reach the container at all.
type failingExecutor struct{ err error }

func (f failingExecutor) Stream(context.Context, kube.ContainerRef, kube.ExecOptions) error {
	return f.err
}

func TestProbeReportsExecFailuresUnwrapped(t *testing.T) {
	err := errors.New(`Internal error occurred: unable to upgrade connection: container not found ("main")`)
	_, got := kube.ProbeContainer(context.Background(), failingExecutor{err}, kube.ContainerRef{})
	if got == nil || errors.Is(got, kube.ErrNoShell) || !strings.Contains(got.Error(), "unable to upgrade connection") {
		t.Fatal("exec failure must not become a missing shell:", got)
	}
	// A runtime that cannot find the shell still means no shell.
	missing := utilexec.CodeExitError{Err: errors.New("exec failed"), Code: 127}
	if _, got = kube.ProbeContainer(context.Background(), failingExecutor{missing}, kube.ContainerRef{}); !errors.Is(got, kube.ErrNoShell) {
		t.Fatal("exit 127 means no shell:", got)
	}
}

func TestBusyboxStatListsWithRealSeparator(t *testing.T) {
	// BusyBox stat prints a \001 escape literally; the format carries a real
	// 0x01 byte, so busybox and GNU stat both produce parsable records.
	root := localexec.Fixture(t)
	x := executorWith(t, "busybox-stat")
	probe, err := kube.ProbeContainer(context.Background(), x, kube.ContainerRef{})
	if err != nil || !probe.Stat {
		t.Fatalf("busybox stat must pass the probe with a real separator byte: %+v %v", probe, err)
	}
	fs, err := kube.OpenContainerFS(context.Background(), x, kube.ContainerRef{}, probe)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(root)
	if err != nil || len(entries) != 7 || fs.ListMode() != "stat" {
		t.Fatalf("busybox stat listing: %d entries, mode %s, err %v", len(entries), fs.ListMode(), err)
	}
}

func TestUnparsableStatFallsBackToLs(t *testing.T) {
	root := localexec.Fixture(t)
	x := executorWith(t, "terse-stat")
	probe, err := kube.ProbeContainer(context.Background(), x, kube.ContainerRef{})
	if err != nil || probe.Stat {
		t.Fatalf("a stat ignoring the format must not pass the probe: %+v %v", probe, err)
	}
	// Even when a stale probe claims stat, listing notices and moves on.
	probe.Stat = true
	fs, err := kube.OpenContainerFS(context.Background(), x, kube.ContainerRef{}, probe)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(root)
	if err != nil || len(entries) != 7 || fs.ListMode() != "ls" {
		t.Fatalf("fallback listing: %d entries, mode %s, err %v", len(entries), fs.ListMode(), err)
	}
	if info, err := fs.Lstat(filepath.Join(root, "plain.txt")); err != nil || info.Size() != 5 {
		t.Fatal("lstat after fallback", info, err)
	}
}

func TestBadFindFallsBack(t *testing.T) {
	root := localexec.Fixture(t)
	x := executorWith(t, "bad-find", "stat")
	probe, err := kube.ProbeContainer(context.Background(), x, kube.ContainerRef{})
	if err != nil || probe.Find || !probe.Stat {
		t.Fatalf("a find rejecting %%y must not pass the probe: %+v %v", probe, err)
	}
	probe.Find = true
	fs, err := kube.OpenContainerFS(context.Background(), x, kube.ContainerRef{}, probe)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(root)
	if err != nil || len(entries) != 7 || fs.ListMode() != "stat" {
		t.Fatalf("fallback from find: %d entries, mode %s, err %v", len(entries), fs.ListMode(), err)
	}
	if _, err := fs.ReadDir(filepath.Join(root, "nope")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing directory after fallback", err)
	}
	// An empty directory is empty, not a parse failure.
	empty := filepath.Join(root, "empty")
	os.Mkdir(empty, 0755)
	if entries, err := fs.ReadDir(empty); err != nil || len(entries) != 0 || fs.ListMode() != "stat" {
		t.Fatal("empty directory", entries, err, fs.ListMode())
	}
}

func TestShellOnlyContainer(t *testing.T) {
	root := localexec.Fixture(t)
	x := localexec.New(t, "")
	probe, err := kube.ProbeContainer(context.Background(), x, kube.ContainerRef{})
	if err != nil || probe.Cat || probe.Ls || probe.Stat || probe.Find || probe.Shell == "" {
		t.Fatalf("shell-only probe %+v %v", probe, err)
	}
	fs, err := kube.OpenContainerFS(context.Background(), x, kube.ContainerRef{}, probe)
	if err != nil || fs.ListMode() != "builtin" {
		t.Fatal("open shell-only", err)
	}
	entries, err := fs.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]os.FileInfo{}
	for _, e := range entries {
		got[e.Name()] = e
	}
	if len(got) != 7 || !got["sub dir"].IsDir() || got["link-to-dir"].Mode()&os.ModeSymlink == 0 || got["plain.txt"].IsDir() {
		t.Fatalf("builtin listing %v", keys(got))
	}
	if _, err := fs.ReadDir(filepath.Join(root, "nope")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing directory", err)
	}
	if _, err := fs.Lstat(filepath.Join(root, "nope")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing path", err)
	}
	if info, err := fs.Stat(filepath.Join(root, "link-to-dir")); err != nil || !info.IsDir() {
		t.Fatal("Stat through a link with builtins", info, err)
	}
	// Reading uses the shell's read loop; writing is refused with the reason.
	r, err := fs.Open(filepath.Join(root, "plain.txt"))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(r)
	r.Close()
	if string(data) != "hello\n" {
		t.Fatalf("read loop %q", data)
	}
	if _, err := fs.Create(filepath.Join(root, "new.txt")); err == nil || !strings.Contains(err.Error(), "cat") {
		t.Fatal("create without cat", err)
	}
}
