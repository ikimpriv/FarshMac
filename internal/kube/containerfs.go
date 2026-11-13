package kube

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Probe records which tools a container offers. Shell is the first of
// /bin/bash, /bin/sh that runs; "" means commands cannot run at all. Find,
// Stat and Ls select the directory listing mode (GNU find -printf, stat -c
// with the full format, or ls -lan which BSD and busybox also provide);
// without any of them the shell's own builtins list names and types. Cat
// reports whether files can be read with cat (and written at all); Tar
// whether whole trees can be streamed later.
type Probe struct {
	Shell string
	Tar   bool
	Cat   bool
	Find  bool
	Stat  bool
	Ls    bool
}

// ErrNoShell is wrapped by ProbeContainer when neither /bin/bash nor /bin/sh
// exists, for example in a distroless image. Exec failures of other kinds
// (the container is not running, the connection cannot be upgraded, RBAC)
// are returned as they are.
var ErrNoShell = errors.New("no shell in container")

// ErrNoLister is kept for callers that test for it; since the shell's
// builtins can always list a directory it is no longer returned.
var ErrNoLister = errors.New("no find, stat or ls in container")

// sep separates the fields of a listing record: a real 0x01 byte, which
// shell single quotes pass through unchanged. GNU stat would also expand a
// \001 escape in its format, busybox stat prints it literally.
const sep = "\x01"

// probeScript prints one `name=…` line per tool. find and stat are tested
// with the exact listing formats, because a find may accept -printf ” yet
// reject %y, and a stat may print the separator escape literally; the
// probe counts the separators in the answer.
var probeScript = "command -v tar >/dev/null 2>&1 && echo tar=1 || echo tar=0\n" +
	"command -v cat >/dev/null 2>&1 && echo cat=1 || echo cat=0\n" +
	"f=$(find / -maxdepth 0 -printf '" + findFormat + "' 2>/dev/null) && echo \"find=$f\" || echo find=0\n" +
	"s=$(stat -c '" + statFormat + "' / 2>/dev/null) && echo \"stat=$s\" || echo stat=0\n" +
	"ls -lan / >/dev/null 2>&1 && echo ls=1 || echo ls=0\n"

var shellCandidates = []string{"/bin/bash", "/bin/sh"}

// ProbeContainer finds a shell and the listing tools with at most two execs.
func ProbeContainer(ctx context.Context, x Executor, ref ContainerRef) (Probe, error) {
	var lastErr error
	for _, shell := range shellCandidates {
		var out, errOut bytes.Buffer
		err := x.Stream(ctx, ref, ExecOptions{Command: []string{shell, "-c", probeScript}, Stdout: &out, Stderr: &limitedWriter{w: &errOut, limit: 4096}})
		if ctx.Err() != nil {
			return Probe{}, ctx.Err()
		}
		if err != nil && out.Len() == 0 {
			msg := strings.TrimSpace(firstLine(errOut.String()))
			if msg == "" {
				msg = err.Error()
			}
			if !shellMissing(err, msg) {
				// The runtime could not run anything: report that, not a
				// missing shell.
				return Probe{}, errors.New(msg)
			}
			lastErr = fmt.Errorf("%s: %s", shell, msg)
			continue
		}
		p := Probe{Shell: shell}
		for _, line := range strings.Split(out.String(), "\n") {
			name, value, ok := strings.Cut(strings.TrimRight(line, "\r"), "=")
			if !ok {
				continue
			}
			switch name {
			case "tar":
				p.Tar = value == "1"
			case "cat":
				p.Cat = value == "1"
			case "find":
				p.Find = strings.Count(value, sep) == 5
			case "stat":
				p.Stat = strings.Count(value, sep) == 4
			case "ls":
				p.Ls = value == "1"
			}
		}
		return p, nil
	}
	return Probe{}, fmt.Errorf("%w: %v", ErrNoShell, lastErr)
}

// shellMissing tells a shell that does not exist from an exec that could not
// run at all: runtimes answer the former with "executable file … not found"
// or "no such file or directory" and exit codes 126/127.
func shellMissing(err error, msg string) bool {
	if code, ok := ExitCode(err); ok && (code == 126 || code == 127) {
		return true
	}
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "executable file") && strings.Contains(lower, "not found") ||
		strings.Contains(lower, "no such file or directory")
}

// listMode is how directories are listed, in order of preference. A mode
// whose command fails or whose output does not parse is given up for the
// rest of the session (some finds accept -printf ” but not %y; some stats
// print the separator escape literally).
type listMode int

const (
	listFind listMode = iota
	listStat
	listLs
	listBuiltin // the shell's own [ -d ] / [ -L ] tests: names and types only
)

func (m listMode) String() string {
	return [...]string{"find", "stat", "ls", "builtin"}[m]
}

// ContainerFS is a remotefs.FS over exec in one container. Every operation
// is one command; there is no persistent process. Output from the container
// is parsed, never run.
type ContainerFS struct {
	ctx   context.Context
	x     Executor
	ref   ContainerRef
	probe Probe
	mode  listMode
}

// OpenContainerFS wraps a probed container. The context bounds every later
// command. Only a probe without a shell is refused: without find, stat and
// ls the shell's builtins still list names and types.
func OpenContainerFS(ctx context.Context, x Executor, ref ContainerRef, probe Probe) (*ContainerFS, error) {
	if probe.Shell == "" {
		return nil, ErrNoShell
	}
	f := &ContainerFS{ctx: ctx, x: x, ref: ref, probe: probe, mode: listBuiltin}
	switch {
	case probe.Find:
		f.mode = listFind
	case probe.Stat:
		f.mode = listStat
	case probe.Ls:
		f.mode = listLs
	}
	return f, nil
}

// ListMode reports how directories are currently listed.
func (f *ContainerFS) ListMode() string { return f.mode.String() }

func (f *ContainerFS) HasTar() bool { return f.probe.Tar }
func (f *ContainerFS) Probe() Probe { return f.probe }
func (f *ContainerFS) Close() error { return nil }

// shQuote makes s one literal shell word.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

// run executes script with the probed shell and returns its stdout. A failed
// command becomes an *os.PathError whose Err is os.ErrNotExist,
// os.ErrPermission or os.ErrExist when stderr says so, so callers can use
// errors.Is as with a local file system.
func (f *ContainerFS) run(op, p, script string, stdin io.Reader) ([]byte, error) {
	var out, errOut bytes.Buffer
	err := f.x.Stream(f.ctx, f.ref, ExecOptions{Command: []string{f.probe.Shell, "-c", script}, Stdin: stdin, Stdout: &out, Stderr: &limitedWriter{w: &errOut, limit: 4096}})
	if err != nil {
		return out.Bytes(), execError(op, p, errOut.String(), err)
	}
	return out.Bytes(), nil
}

func execError(op, p, stderr string, err error) error {
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(firstLine(stderr))
	if msg == "" {
		msg = err.Error()
	}
	var cause error
	switch {
	case strings.Contains(msg, "No such file or directory"), strings.Contains(msg, "not found"), strings.Contains(msg, "can't cd to"):
		cause = os.ErrNotExist
	case strings.Contains(msg, "Permission denied"), strings.Contains(msg, "Operation not permitted"):
		cause = os.ErrPermission
	case strings.Contains(msg, "File exists"), strings.Contains(msg, "cannot overwrite existing file"):
		cause = os.ErrExist
	default:
		cause = errors.New(msg)
	}
	return &os.PathError{Op: op, Path: p, Err: cause}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

type limitedWriter struct {
	w     io.Writer
	limit int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.limit > 0 {
		n := min(len(p), l.limit)
		l.limit -= n
		_, _ = l.w.Write(p[:n])
	}
	return len(p), nil
}

// fileInfo is what the listing commands produce.
type fileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	target  string      // symbolic link target, when known
	follow  os.FileMode // builtin mode: the type behind a link (readlink may be absent)
	known   bool        // follow is set; broken links have known with followBroken
	broken  bool
}

func (i *fileInfo) Name() string       { return i.name }
func (i *fileInfo) Size() int64        { return i.size }
func (i *fileInfo) Mode() os.FileMode  { return i.mode }
func (i *fileInfo) ModTime() time.Time { return i.modTime }
func (i *fileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i *fileInfo) Sys() any           { return nil }

// Listing formats. Fields are separated by a 0x01 byte (sep) and records by
// newline, so names with spaces and quotes survive; names containing a
// newline do not (they are rare and would need NUL-terminated records that
// stat lacks). The command lines, with <s> standing for the separator:
//
//	find:    find DIR -mindepth 1 -maxdepth 1 -printf '%y<s>%m<s>%s<s>%T@<s>%f<s>%l\n'
//	         (Lstat: find PATH -maxdepth 0 -printf ...)
//	stat:    cd DIR; for f in .[!.]* ..?* *; do [ -e "$f" ] || [ -L "$f" ] && printf '%s<s>' "$(stat -c '%F<s>%a<s>%s<s>%Y<s>%n' -- "$f")"; readlink -- "$f" 2>/dev/null; echo; done
//	         (Lstat: the same on one path)
//	ls:      ls -lanA -- DIR     (Lstat: ls -land -- PATH)
//	builtin: cd DIR; for f in .[!.]* ..?* *; do [ -e "$f" ] || [ -L "$f" ] || continue; t=f; [ -d "$f" ] && t=d; [ -L "$f" ] && t=l; printf '%s<s>%s<s>%s<s>%s\n' "$t" "$f" "$(readlink -- "$f")" "<d|f|x: what a link points at>"; done
//	         (Lstat: the same tests on one path; names and types only, no size or time)
const (
	findFormat = "%y" + sep + "%m" + sep + "%s" + sep + "%T@" + sep + "%f" + sep + "%l\n"
	statFormat = "%F" + sep + "%a" + sep + "%s" + sep + "%Y" + sep + "%n"
)

// statLoop lists the entries of the current directory with stat.
const statLoop = "for f in .[!.]* ..?* *; do if [ -e \"$f\" ] || [ -L \"$f\" ]; then printf '%s" + sep + "' \"$(stat -c '" + statFormat + "' -- \"$f\")\"; readlink -- \"$f\" 2>/dev/null; echo; fi; done"

// builtinEntry prints one record for $f with the shell's own tests.
const builtinEntry = "t=f; [ -d \"$f\" ] && t=d; [ -L \"$f\" ] && t=l; l=''; ft=''; if [ \"$t\" = l ]; then l=$(readlink -- \"$f\" 2>/dev/null); ft=x; [ -e \"$f\" ] && ft=f; [ -d \"$f\" ] && ft=d; fi; printf '%s" + sep + "%s" + sep + "%s" + sep + "%s\\n' \"$t\" \"$f\" \"$l\" \"$ft\""

// checkDir fails like the real tools when DIR is missing or not a directory.
func checkDir(dir string) string {
	q := shQuote(dir)
	return "[ -e " + q + " ] || [ -L " + q + " ] || { echo " + q + ": No such file or directory >&2; exit 1; }; [ -d " + q + " ] || { echo " + q + ": Not a directory >&2; exit 1; }; cd " + q + " || exit 1\n"
}

// list runs one listing mode for dir (or one path) and parses the output.
func (f *ContainerFS) list(mode listMode, op, p string, single bool) ([]*fileInfo, error) {
	var out []byte
	var err error
	switch mode {
	case listFind:
		depth := " -mindepth 1 -maxdepth 1"
		if single {
			depth = " -maxdepth 0"
		}
		out, err = f.run(op, p, "find "+shQuote(p)+depth+" -printf '"+findFormat+"'", nil)
		if err == nil {
			return parseFindOutput(out)
		}
	case listStat:
		script := checkDir(p) + statLoop
		if single {
			script = "f=" + shQuote(p) + "; s=$(stat -c '" + statFormat + "' -- \"$f\") || exit $?; printf '%s" + sep + "' \"$s\"; readlink -- \"$f\" 2>/dev/null; echo"
		}
		out, err = f.run(op, p, script, nil)
		if err == nil {
			return parseStatOutput(out)
		}
	case listLs:
		flags := " -lanA -- "
		if single {
			flags = " -land -- "
		}
		out, err = f.run(op, p, "ls"+flags+shQuote(p), nil)
		if err == nil {
			return parseLsOutput(out, time.Now())
		}
	default:
		script := checkDir(p) + "for f in .[!.]* ..?* *; do if [ -e \"$f\" ] || [ -L \"$f\" ]; then " + builtinEntry + "; fi; done"
		if single {
			script = "f=" + shQuote(p) + "; if [ -e \"$f\" ] || [ -L \"$f\" ]; then " + builtinEntry + "; else echo \"$f: No such file or directory\" >&2; exit 1; fi"
		}
		out, err = f.run(op, p, script, nil)
		if err == nil {
			return parseBuiltinOutput(out)
		}
	}
	return nil, err
}

// listWith tries the current mode and falls back to the next ones when the
// tool fails or its output does not parse, remembering the working mode.
// Errors about the path itself (missing, forbidden) end the attempt.
func (f *ContainerFS) listWith(op, p string, single bool) ([]*fileInfo, error) {
	var lastErr error
	for mode := f.mode; mode <= listBuiltin; mode++ {
		infos, err := f.list(mode, op, p, single)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
				return nil, err
			}
			lastErr = err
		} else if len(infos) > 0 || mode == listBuiltin || f.emptyListing(mode, p, single) {
			f.mode = mode
			return infos, nil
		} else {
			lastErr = &os.PathError{Op: op, Path: p, Err: fmt.Errorf("%s listing could not be parsed", mode)}
		}
		if mode == listBuiltin {
			break
		}
		f.mode = mode + 1
	}
	return nil, lastErr
}

// emptyListing tells an empty directory (nothing to parse) from unparsable
// output: the mode produced no records, so ask the builtins whether the
// directory really has no entries.
func (f *ContainerFS) emptyListing(mode listMode, p string, single bool) bool {
	if single {
		return false
	}
	infos, err := f.list(listBuiltin, "readdir", p, false)
	return err == nil && len(infos) == 0
}

func (f *ContainerFS) ReadDir(dir string) ([]os.FileInfo, error) {
	infos, err := f.listWith("readdir", dir, false)
	if err != nil {
		return nil, err
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].name < infos[j].name })
	result := make([]os.FileInfo, 0, len(infos))
	for _, i := range infos {
		if i.name == "." || i.name == ".." {
			continue
		}
		result = append(result, i)
	}
	return result, nil
}

func (f *ContainerFS) Lstat(p string) (os.FileInfo, error) {
	info, err := f.lstat(p)
	if err != nil {
		return nil, err
	}
	return info, nil
}

func (f *ContainerFS) lstat(p string) (*fileInfo, error) {
	infos, err := f.listWith("lstat", p, true)
	if err != nil {
		return nil, err
	}
	if len(infos) != 1 {
		return nil, &os.PathError{Op: "lstat", Path: p, Err: os.ErrNotExist}
	}
	info := infos[0]
	info.name = path.Base(p)
	return info, nil
}

// Stat follows a final symbolic link (and links behind it, up to 16).
func (f *ContainerFS) Stat(p string) (os.FileInfo, error) {
	current := p
	for i := 0; i < 16; i++ {
		info, err := f.lstat(current)
		if err != nil {
			return nil, err
		}
		if info.mode&os.ModeSymlink == 0 {
			info.name = path.Base(p)
			return info, nil
		}
		target := info.target
		if target == "" && info.known {
			// No readlink in the container: the builtin listing already
			// looked through the link.
			if info.broken {
				return nil, &os.PathError{Op: "stat", Path: p, Err: os.ErrNotExist}
			}
			return &fileInfo{name: path.Base(p), mode: info.follow}, nil
		}
		if target == "" {
			if target, err = f.Readlink(current); err != nil {
				return nil, err
			}
		}
		if !path.IsAbs(target) {
			target = path.Join(path.Dir(current), target)
		}
		current = target
	}
	return nil, &os.PathError{Op: "stat", Path: p, Err: errors.New("too many levels of symbolic links")}
}

// Open streams `cat -- PATH`. Without cat the shell reads the file line by
// line (`while IFS= read -r l || [ -n "$l" ]; do printf '%s\n' "$l"; done <
// PATH`), which suits text files only: NUL bytes are dropped and a missing
// final newline is added.
func (f *ContainerFS) Open(p string) (io.ReadCloser, error) {
	if info, err := f.lstat(p); err != nil {
		return nil, err
	} else if info.IsDir() {
		return nil, &os.PathError{Op: "open", Path: p, Err: errors.New("is a directory")}
	}
	script := "cat -- " + shQuote(p)
	if !f.probe.Cat {
		script = "while IFS= read -r l || [ -n \"$l\" ]; do printf '%s\\n' \"$l\"; done < " + shQuote(p)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	pr, pw := io.Pipe()
	go func() {
		var errOut bytes.Buffer
		err := f.x.Stream(ctx, f.ref, ExecOptions{Command: []string{f.probe.Shell, "-c", script}, Stdout: pw, Stderr: &limitedWriter{w: &errOut, limit: 4096}})
		pw.CloseWithError(execError("read", p, errOut.String(), err))
	}()
	return &cancelReader{ReadCloser: pr, cancel: cancel}, nil
}

type cancelReader struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *cancelReader) Close() error {
	err := r.ReadCloser.Close()
	r.cancel()
	return err
}

// Create writes stdin to a new file with `set -C; cat > PATH` (noclobber, so
// an existing file is refused like sftp's O_EXCL). Close finishes the write
// and returns the command's error.
func (f *ContainerFS) Create(p string) (io.WriteCloser, error) {
	if !f.probe.Cat {
		return nil, &os.PathError{Op: "create", Path: p, Err: errors.New("cat is not available in the container, files cannot be written")}
	}
	ctx, cancel := context.WithCancel(f.ctx)
	pr, pw := io.Pipe()
	w := &execWriter{WriteCloser: pw, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(w.done)
		var errOut bytes.Buffer
		err := f.x.Stream(ctx, f.ref, ExecOptions{Command: []string{f.probe.Shell, "-c", "set -C; cat > " + shQuote(p)}, Stdin: pr, Stderr: &limitedWriter{w: &errOut, limit: 4096}})
		w.err = execError("create", p, errOut.String(), err)
		pr.CloseWithError(w.err)
	}()
	return w, nil
}

type execWriter struct {
	io.WriteCloser
	cancel context.CancelFunc
	done   chan struct{}
	err    error
	closed bool
}

func (w *execWriter) Close() error {
	if w.closed {
		return w.err
	}
	w.closed = true
	_ = w.WriteCloser.Close()
	<-w.done
	w.cancel()
	return w.err
}

func (f *ContainerFS) Mkdir(p string) error {
	_, err := f.run("mkdir", p, "mkdir -- "+shQuote(p), nil)
	return err
}

// Remove deletes a file, or an empty directory when dir is set, matching
// the other remotefs implementations (trees are removed by remotefs.RemoveTree).
func (f *ContainerFS) Remove(p string, dir bool) error {
	cmd := "rm -- "
	if dir {
		cmd = "rmdir -- "
	}
	_, err := f.run("remove", p, cmd+shQuote(p), nil)
	return err
}

func (f *ContainerFS) Rename(from, to string) error {
	_, err := f.run("rename", from, "mv -- "+shQuote(from)+" "+shQuote(to), nil)
	return err
}

func (f *ContainerFS) Chmod(p string, mode os.FileMode) error {
	_, err := f.run("chmod", p, "chmod -- "+strconv.FormatUint(uint64(mode.Perm()|setBits(mode)), 8)+" "+shQuote(p), nil)
	return err
}

func setBits(mode os.FileMode) os.FileMode {
	var bits os.FileMode
	if mode&os.ModeSetuid != 0 {
		bits |= 04000
	}
	if mode&os.ModeSetgid != 0 {
		bits |= 02000
	}
	if mode&os.ModeSticky != 0 {
		bits |= 01000
	}
	return bits
}

// Chtimes sets the modification time with `TZ=UTC touch -m -t CCYYMMDDhhmm.SS`,
// the one form GNU, BSD and busybox touch all accept.
func (f *ContainerFS) Chtimes(p string, t time.Time) error {
	stamp := t.UTC().Format("200601021504.05")
	_, err := f.run("chtimes", p, "TZ=UTC touch -m -t "+stamp+" -- "+shQuote(p), nil)
	return err
}

func (f *ContainerFS) Readlink(p string) (string, error) {
	out, err := f.run("readlink", p, "readlink -- "+shQuote(p), nil)
	if err != nil {
		return "", err
	}
	target := strings.TrimSuffix(string(out), "\n")
	if target == "" {
		return "", &os.PathError{Op: "readlink", Path: p, Err: errors.New("not a symbolic link")}
	}
	return target, nil
}

func (f *ContainerFS) Symlink(target, p string) error {
	_, err := f.run("symlink", p, "ln -s -- "+shQuote(target)+" "+shQuote(p), nil)
	return err
}

// --- output parsers ---

func typeMode(c byte) (os.FileMode, bool) {
	switch c {
	case 'd':
		return os.ModeDir, true
	case 'l':
		return os.ModeSymlink, true
	case 'f', '-':
		return 0, true
	case 'p':
		return os.ModeNamedPipe, true
	case 's':
		return os.ModeSocket, true
	case 'c':
		return os.ModeCharDevice | os.ModeDevice, true
	case 'b':
		return os.ModeDevice, true
	}
	return 0, false
}

func permMode(octal string) os.FileMode {
	v, err := strconv.ParseUint(octal, 8, 32)
	if err != nil {
		return 0
	}
	mode := os.FileMode(v & 0777)
	if v&04000 != 0 {
		mode |= os.ModeSetuid
	}
	if v&02000 != 0 {
		mode |= os.ModeSetgid
	}
	if v&01000 != 0 {
		mode |= os.ModeSticky
	}
	return mode
}

// parseFindOutput reads records of findFormat: type letter, octal perms,
// size, epoch seconds with fraction, name, link target.
func parseFindOutput(out []byte) ([]*fileInfo, error) {
	var infos []*fileInfo
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		fields := strings.Split(string(line), sep)
		if len(fields) < 6 || fields[0] == "" {
			continue
		}
		mode, ok := typeMode(fields[0][0])
		if !ok {
			continue
		}
		size, _ := strconv.ParseInt(fields[2], 10, 64)
		infos = append(infos, &fileInfo{name: fields[4], size: size, mode: mode | permMode(fields[1]), modTime: epochTime(fields[3]), target: fields[5]})
	}
	return infos, nil
}

// parseBuiltinOutput reads records of the builtin mode: type letter (f, d,
// l), name, link target. Sizes and times are unknown; modes are nominal.
func parseBuiltinOutput(out []byte) ([]*fileInfo, error) {
	var infos []*fileInfo
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		fields := strings.Split(string(line), sep)
		if len(fields) < 2 || fields[0] == "" || fields[1] == "" {
			continue
		}
		info := &fileInfo{name: fields[1]}
		switch fields[0] {
		case "d":
			info.mode = os.ModeDir | 0755
		case "l":
			info.mode = os.ModeSymlink | 0777
		default:
			info.mode = 0644
		}
		if len(fields) > 2 {
			info.target = fields[2]
		}
		if len(fields) > 3 && fields[3] != "" {
			info.known = true
			switch fields[3] {
			case "d":
				info.follow = os.ModeDir | 0755
			case "f":
				info.follow = 0644
			default:
				info.broken = true
			}
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func epochTime(s string) time.Time {
	secs, frac, _ := strings.Cut(s, ".")
	sec, err := strconv.ParseInt(secs, 10, 64)
	if err != nil {
		return time.Time{}
	}
	var nsec int64
	if frac != "" {
		frac = (frac + "000000000")[:9]
		nsec, _ = strconv.ParseInt(frac, 10, 64)
	}
	return time.Unix(sec, nsec)
}

// parseStatOutput reads records of statFormat plus the readlink field:
// GNU %F text type, octal perms, size, epoch seconds, name, target.
func parseStatOutput(out []byte) ([]*fileInfo, error) {
	var infos []*fileInfo
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		fields := strings.Split(string(line), sep)
		if len(fields) < 5 {
			continue
		}
		var mode os.FileMode
		switch t := fields[0]; {
		case t == "directory":
			mode = os.ModeDir
		case t == "symbolic link":
			mode = os.ModeSymlink
		case strings.HasPrefix(t, "regular"):
			mode = 0
		case t == "fifo":
			mode = os.ModeNamedPipe
		case t == "socket":
			mode = os.ModeSocket
		case strings.HasPrefix(t, "character"):
			mode = os.ModeCharDevice | os.ModeDevice
		case strings.HasPrefix(t, "block"):
			mode = os.ModeDevice
		default:
			continue
		}
		size, _ := strconv.ParseInt(fields[2], 10, 64)
		target := ""
		if len(fields) > 5 {
			target = fields[5]
		}
		infos = append(infos, &fileInfo{name: fields[4], size: size, mode: mode | permMode(fields[1]), modTime: epochTime(fields[3]), target: target})
	}
	return infos, nil
}

var months = map[string]time.Month{"Jan": 1, "Feb": 2, "Mar": 3, "Apr": 4, "May": 5, "Jun": 6, "Jul": 7, "Aug": 8, "Sep": 9, "Oct": 10, "Nov": 11, "Dec": 12}

// parseLsOutput reads `ls -lan` lines from GNU, BSD and busybox:
//
//	drwxr-xr-x  2 0 0 4096 Jan  5 12:00 name with spaces
//	lrwxrwxrwx  1 0 0    3 Jan  5  2023 link -> target
//
// Device files (size "1, 3") and lines that do not fit are skipped. Times
// without a year are this year, or last year when that would be in the future.
func parseLsOutput(out []byte, now time.Time) ([]*fileInfo, error) {
	var infos []*fileInfo
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" || strings.HasPrefix(line, "total ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 9 || len(fields[0]) < 10 {
			continue
		}
		typ, ok := typeMode(fields[0][0])
		if !ok {
			continue
		}
		mode := typ | lsPerms(fields[0][1:10])
		size, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			continue
		}
		month, ok := months[fields[5]]
		if !ok {
			continue
		}
		day, err := strconv.Atoi(fields[6])
		if err != nil {
			continue
		}
		var when time.Time
		if hh, mm, found := strings.Cut(fields[7], ":"); found {
			h, _ := strconv.Atoi(hh)
			m, _ := strconv.Atoi(mm)
			when = time.Date(now.Year(), month, day, h, m, 0, 0, time.Local)
			if when.After(now.Add(24 * time.Hour)) {
				when = when.AddDate(-1, 0, 0)
			}
		} else {
			year, err := strconv.Atoi(fields[7])
			if err != nil {
				continue
			}
			when = time.Date(year, month, day, 0, 0, 0, 0, time.Local)
		}
		// The name is everything after the eighth field, spaces included.
		rest := line
		for i := 0; i < 8; i++ {
			rest = strings.TrimLeft(rest, " \t")
			j := strings.IndexAny(rest, " \t")
			if j < 0 {
				rest = ""
				break
			}
			rest = rest[j:]
		}
		name := strings.TrimLeft(rest, " \t")
		target := ""
		if typ == os.ModeSymlink {
			if n, t, found := strings.Cut(name, " -> "); found {
				name, target = n, t
			}
		}
		if name == "" {
			continue
		}
		infos = append(infos, &fileInfo{name: name, size: size, mode: mode, modTime: when, target: target})
	}
	return infos, nil
}

func lsPerms(s string) os.FileMode {
	var mode os.FileMode
	for i, c := range []byte(s) {
		switch c {
		case 'r', 'w', 'x':
			mode |= 1 << uint(8-i)
		case 's', 'S':
			if i == 2 {
				mode |= os.ModeSetuid
			} else {
				mode |= os.ModeSetgid
			}
			if c == 's' {
				mode |= 1 << uint(8-i)
			}
		case 't', 'T':
			mode |= os.ModeSticky
			if c == 't' {
				mode |= 1 << uint(8-i)
			}
		}
	}
	return mode
}
