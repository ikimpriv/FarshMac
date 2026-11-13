// Package localexec is a kube.Executor for tests: it runs container
// commands with the local /bin/sh against the local file system, with a
// PATH restricted to the tools a test allows.
package localexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/creack/pty"
	utilexec "k8s.io/client-go/util/exec"

	"farshmac/internal/kube"
)

// Executor runs container commands with the local /bin/sh and a PATH
// holding only the tools a test allows, so a container without tar, find or
// stat can be simulated. Paths are absolute, as in a container. Command[0]
// is the shell the caller chose (/bin/bash or /bin/sh); it is replaced by
// the local /bin/sh unless it is listed in Missing, and the remaining
// arguments are passed through, so `sh -c script` runs the script locally.
type Executor struct {
	bin     string          // the only PATH entry
	root    string          // working directory of every command ("" = inherited)
	Missing map[string]bool // shells reported as absent by the "runtime" (exit 126)
}

func (x *Executor) Stream(ctx context.Context, _ kube.ContainerRef, opts kube.ExecOptions) error {
	if len(opts.Command) == 0 {
		return errors.New("empty command")
	}
	shell := opts.Command[0]
	if x.Missing[shell] {
		return utilexec.CodeExitError{Err: fmt.Errorf("OCI runtime exec failed: exec failed: unable to start container process: exec: %q: stat %s: no such file or directory", shell, shell), Code: 126}
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", opts.Command[1:]...)
	cmd.Env = []string{"PATH=" + x.bin, "HOME=/", "LANG=C", "LC_ALL=C"}
	cmd.Dir = x.root
	if opts.TTY {
		return x.streamTTY(ctx, cmd, opts)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = opts.Stdin, opts.Stdout, opts.Stderr
	return exitError(cmd.Run())
}

func (x *Executor) streamTTY(ctx context.Context, cmd *exec.Cmd, opts kube.ExecOptions) error {
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
func toolbox(t testing.TB, tools ...string) string {
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

// BaseTools are the commands a minimal container file system needs; tests
// add "tar", "find", "stat" (GNU-style stubs) or others on top.
const BaseTools = "cat ls mkdir rm rmdir mv chmod touch readlink ln basename printf"

// New builds an executor whose PATH holds only tools: real binaries found
// with exec.LookPath (the test is skipped when one is missing), except
// "find" and "stat", which are shell stubs producing GNU output from BSD
// stat, so the find and stat listing modes can be proved on macOS. The test
// is skipped off darwin for that reason. root, when not empty, is the
// working directory of every command; container paths are absolute anyway.
func New(t testing.TB, root string, tools ...string) *Executor {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the find/stat stubs rely on BSD stat")
	}
	return &Executor{bin: toolbox(t, tools...), root: root}
}

// Fixture creates a directory tree with awkward names: spaces, quotes,
// non-ASCII, a hidden folder, a link to a folder and a broken link.
func Fixture(t testing.TB) string {
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
