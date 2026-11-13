package kube

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

// localExecutor runs container commands with the local /bin/sh and a PATH
// holding only the tools a test allows, so a container without tar, find or
// stat can be simulated. Paths are absolute, as in a container.
type localExecutor struct {
	bin     string          // the only PATH entry
	missing map[string]bool // shells reported as absent by the "runtime"
}

func (x *localExecutor) Stream(ctx context.Context, _ ContainerRef, opts ExecOptions) error {
	if len(opts.Command) == 0 {
		return errors.New("empty command")
	}
	shell := opts.Command[0]
	if x.missing[shell] {
		return utilexec.CodeExitError{Err: fmt.Errorf("OCI runtime exec failed: exec failed: unable to start container process: exec: %q: stat %s: no such file or directory", shell, shell), Code: 126}
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", opts.Command[1:]...)
	cmd.Env = []string{"PATH=" + x.bin, "HOME=/", "LANG=C", "LC_ALL=C"}
	if opts.TTY {
		return x.streamTTY(ctx, cmd, opts)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = opts.Stdin, opts.Stdout, opts.Stderr
	return exitError(cmd.Run())
}

func (x *localExecutor) streamTTY(ctx context.Context, cmd *exec.Cmd, opts ExecOptions) error {
	size := &pty.Winsize{Cols: 80, Rows: 24}
	if opts.Resize != nil {
		select {
		case s := <-opts.Resize:
			size = &pty.Winsize{Cols: s.Width, Rows: s.Height}
		default:
		}
	}
	ptmx, err := pty.StartWithSize(cmd, size)
	if err != nil {
		return err
	}
	if opts.Stdin != nil {
		go io.Copy(ptmx, opts.Stdin)
	}
	stop := make(chan struct{})
	resized := make(chan struct{})
	go func() {
		defer close(resized)
		if opts.Resize == nil {
			return
		}
		for {
			select {
			case s, ok := <-opts.Resize:
				if !ok {
					return
				}
				_ = pty.Setsize(ptmx, &pty.Winsize{Cols: s.Width, Rows: s.Height})
			case <-ctx.Done():
				return
			case <-stop:
				return
			}
		}
	}()
	copied := make(chan struct{})
	go func() {
		if opts.Stdout != nil {
			io.Copy(opts.Stdout, ptmx) // ends with EIO when the child exits
		}
		close(copied)
	}()
	err = cmd.Wait()
	<-copied
	close(stop)
	<-resized
	ptmx.Close()
	return exitError(err)
}

func exitError(err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return utilexec.CodeExitError{Err: err, Code: exit.ExitCode()}
	}
	return err
}

// findStub emulates the GNU find calls containerfs.go makes, on BSD stat.
const findStub = `#!/bin/sh
path=""; maxdepth=1
while [ $# -gt 0 ]; do
  case "$1" in
    -maxdepth) maxdepth=$2; shift;;
    -mindepth|-printf) shift;;
    -*) ;;
    *) path=$1;;
  esac
  shift
done
emit() {
  m=$(/usr/bin/stat -f '%p' "$1" 2>/dev/null) || return 1
  case "$m" in
    10????) t=f;; 12????) t=l;; 14????) t=s;; 4????) t=d;; 6????) t=b;; 2????) t=c;; 1????) t=p;; *) t=f;;
  esac
  perms=$(printf '%o' $(( 0$m & 07777 )))
  size=$(/usr/bin/stat -f '%z' "$1")
  mtime=$(/usr/bin/stat -f '%m' "$1")
  target=""
  [ -L "$1" ] && target=$(/usr/bin/readlink "$1")
  printf '%s\001%s\001%s\001%s.0000000000\001%s\001%s\n' "$t" "$perms" "$size" "$mtime" "$(/usr/bin/basename "$1")" "$target"
}
if [ "$maxdepth" = 0 ]; then
  emit "$path" || { echo "find: '$path': No such file or directory" >&2; exit 1; }
  exit 0
fi
[ -d "$path" ] || { echo "find: '$path': No such file or directory" >&2; exit 1; }
for f in "$path"/.[!.]* "$path"/..?* "$path"/*; do
  if [ -e "$f" ] || [ -L "$f" ]; then emit "$f"; fi
done
exit 0
`

// statStub emulates GNU `stat -c FMT -- FILE...` for the format in
// containerfs.go (any format is answered with that one).
const statStub = `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in
    -c) shift;;
    --) shift; break;;
    -*) ;;
    *) break;;
  esac
  shift
done
rc=0
for f in "$@"; do
  m=$(/usr/bin/stat -f '%p' "$f" 2>/dev/null) || { echo "stat: cannot statx '$f': No such file or directory" >&2; rc=1; continue; }
  case "$m" in
    10????) t="regular file";; 12????) t="symbolic link";; 14????) t="socket";; 4????) t="directory";; 6????) t="block special file";; 2????) t="character special file";; 1????) t="fifo";; *) t="regular file";;
  esac
  perms=$(printf '%o' $(( 0$m & 07777 )))
  printf '%s\001%s\001%s\001%s\001%s\n' "$t" "$perms" "$(/usr/bin/stat -f '%z' "$f")" "$(/usr/bin/stat -f '%m' "$f")" "$f"
done
exit $rc
`

// toolbox builds a PATH directory with the named real tools and stubs.
func toolbox(t *testing.T, tools ...string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(bin, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range tools {
		switch name {
		case "find":
			os.WriteFile(filepath.Join(bin, "find"), []byte(findStub), 0755)
		case "stat":
			os.WriteFile(filepath.Join(bin, "stat"), []byte(statStub), 0755)
		default:
			real, err := exec.LookPath(name)
			if err != nil {
				t.Skipf("%s not installed", name)
			}
			if err := os.Symlink(real, filepath.Join(bin, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return bin
}

const baseTools = "cat ls mkdir rm rmdir mv chmod touch readlink ln basename printf"

func executorWith(t *testing.T, extra ...string) *localExecutor {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the find/stat stubs rely on BSD stat")
	}
	return &localExecutor{bin: toolbox(t, append(strings.Fields(baseTools), extra...)...)}
}

// fixture creates a directory tree with awkward names.
func fixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	for _, dir := range []string{"sub dir", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{"plain.txt": "hello", "it's \"quoted\".txt": "q", "файл №1": "unicode", "sub dir/inner": "x"}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0640); err != nil {
			t.Fatal(err)
		}
	}
	os.Symlink("sub dir", filepath.Join(root, "link-to-dir"))
	os.Symlink("missing", filepath.Join(root, "broken"))
	return root
}

func openFS(t *testing.T, x *localExecutor) *ContainerFS {
	t.Helper()
	ctx := context.Background()
	probe, err := ProbeContainer(ctx, x, ContainerRef{})
	if err != nil {
		t.Fatal(err)
	}
	fs, err := OpenContainerFS(ctx, x, ContainerRef{}, probe)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestProbeContainerDetectsTools(t *testing.T) {
	x := executorWith(t, "tar", "find", "stat")
	p, err := ProbeContainer(context.Background(), x, ContainerRef{})
	if err != nil || p.Shell != "/bin/bash" && p.Shell != "/bin/sh" || !p.Tar || !p.Find || !p.Stat || !p.Ls {
		t.Fatalf("probe %+v %v", p, err)
	}
	x = executorWith(t)
	if p, err = ProbeContainer(context.Background(), x, ContainerRef{}); err != nil || p.Tar || p.Find || p.Stat || !p.Ls {
		t.Fatalf("probe without tar/find/stat %+v %v", p, err)
	}
	x.missing = map[string]bool{"/bin/bash": true}
	if p, err = ProbeContainer(context.Background(), x, ContainerRef{}); err != nil || p.Shell != "/bin/sh" {
		t.Fatalf("fallback to sh %+v %v", p, err)
	}
}

func TestProbeContainerWithoutShell(t *testing.T) {
	x := executorWith(t)
	x.missing = map[string]bool{"/bin/bash": true, "/bin/sh": true}
	_, err := ProbeContainer(context.Background(), x, ContainerRef{})
	if !errors.Is(err, ErrNoShell) || !strings.Contains(err.Error(), "no such file") {
		t.Fatal("expected a no-shell error, got", err)
	}
	if _, err = OpenContainerFS(context.Background(), x, ContainerRef{}, Probe{}); !errors.Is(err, ErrNoShell) {
		t.Fatal("open without shell", err)
	}
	if _, err = OpenContainerFS(context.Background(), x, ContainerRef{}, Probe{Shell: "/bin/sh"}); !errors.Is(err, ErrNoLister) {
		t.Fatal("open without lister", err)
	}
	if fs := openFS(t, executorWith(t)); fs.HasTar() {
		t.Fatal("tar reported without tar")
	}
}

func TestReadDirInAllListingModes(t *testing.T) {
	root := fixture(t)
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
	root := fixture(t)
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
	fs, err := OpenContainerFS(ctx, x, ContainerRef{}, Probe{Shell: "/bin/sh", Ls: true})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err = fs.ReadDir(t.TempDir()); err == nil {
		t.Fatal("cancelled context must fail the command")
	}
}

func TestParseLsOutputVariants(t *testing.T) {
	now := time.Date(2026, 2, 1, 0, 0, 0, 0, time.Local)
	out := "total 8\n" +
		"drwxr-xr-x   2 0 0 4096 Jan  5 12:00 name with spaces\n" +
		"lrwxrwxrwx   1 0 0    3 Dec 31  2023 link -> some target\n" +
		"-rwsr-xr-x@  1 0 0  120 Nov 30 09:15 setuid\n" +
		"crw-rw-rw-   1 0 0   1,  3 Jan  1 00:00 null\n" +
		"-rw-r--r--   1 0 0    0 Jan  5 12:00 a -> b.txt\n"
	infos, err := parseLsOutput([]byte(out), now)
	if err != nil || len(infos) != 4 {
		t.Fatalf("%d entries %v", len(infos), err)
	}
	if infos[0].name != "name with spaces" || !infos[0].IsDir() || infos[0].modTime.Year() != 2026 || infos[0].modTime.Month() != 1 {
		t.Fatalf("%+v", infos[0])
	}
	if infos[1].name != "link" || infos[1].target != "some target" || infos[1].mode&os.ModeSymlink == 0 || infos[1].modTime.Year() != 2023 {
		t.Fatalf("%+v", infos[1])
	}
	if infos[2].name != "setuid" || infos[2].mode&os.ModeSetuid == 0 || infos[2].mode.Perm() != 0755 || infos[2].modTime.Year() != 2025 {
		t.Fatalf("%+v", infos[2])
	}
	if infos[3].name != "a -> b.txt" || infos[3].target != "" {
		t.Fatalf("%+v", infos[3])
	}
}

func TestPodSummary(t *testing.T) {
	pod := samplePod()
	s := PodSummaryOf(pod)
	if s.Phase != "Running" || s.Node != "node-1" || len(s.Containers) != 3 {
		t.Fatalf("%+v", s)
	}
	if s.Containers[0].Kind != "init" || s.Containers[0].State != "terminated:0" {
		t.Fatalf("init %+v", s.Containers[0])
	}
	if s.Containers[1].Kind != "" || s.Containers[1].State != "running" || !s.Containers[1].Ready || s.Containers[1].Started.IsZero() {
		t.Fatalf("app %+v", s.Containers[1])
	}
	if s.Containers[2].Kind != "ephemeral" || s.Containers[2].State != "waiting:CrashLoopBackOff" {
		t.Fatalf("debug %+v", s.Containers[2])
	}
	if len(s.Regular()) != 1 || s.Regular()[0].Name != "app" {
		t.Fatal("regular", s.Regular())
	}
	if _, ok := s.SingleContainer(); ok {
		t.Fatal("init containers must prevent the single-container shortcut")
	}
	pod.Spec.InitContainers, pod.Spec.EphemeralContainers = nil, nil
	if name, ok := PodSummaryOf(pod).SingleContainer(); !ok || name != "app" {
		t.Fatal("single container", name, ok)
	}
	if got := s.Names(); strings.Join(got, ",") != "app,debug,setup" {
		t.Fatal("names", got)
	}
}

func TestExecStreamInteractive(t *testing.T) {
	x := executorWith(t, "stty")
	ctx := context.Background()
	s, err := StartExec(ctx, x, ContainerRef{}, []string{"/bin/sh", "-c", "stty size; read x; echo \"got:$x\"; stty size; exit 3"}, 100, 30)
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
	if code, ok := ExitCode(s.Err()); !ok || code != 3 {
		t.Fatal("exit code", s.Err())
	}
	if err = s.Write([]byte("x")); err == nil {
		t.Fatal("write after exit must fail")
	}
	s.Close()
}

func TestExecStreamCloseCancels(t *testing.T) {
	x := executorWith(t, "sleep")
	s, err := StartExec(context.Background(), x, ContainerRef{}, []string{"/bin/sh", "-c", "sleep 30"}, 80, 24)
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
	if code, ok := ExitCode(utilexec.CodeExitError{Err: errors.New("x"), Code: 7}); !ok || code != 7 {
		t.Fatal(code, ok)
	}
	if code, ok := ExitCode(&utilexec.CodeExitError{Err: errors.New("x"), Code: 8}); !ok || code != 8 {
		t.Fatal(code, ok)
	}
	if _, ok := ExitCode(errors.New("other")); ok {
		t.Fatal("plain error has no code")
	}
	var zero remotecommand.TerminalSize
	_ = zero
}
