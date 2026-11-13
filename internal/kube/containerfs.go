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
// Stat and Ls select the directory listing mode (GNU find -printf, GNU
// stat -c, or ls -lan which BSD and busybox also provide); Tar reports
// whether whole trees can be streamed later.
type Probe struct {
	Shell string
	Tar   bool
	Find  bool
	Stat  bool
	Ls    bool
}

// ErrNoShell is wrapped by ProbeContainer when neither /bin/bash nor /bin/sh
// can be started, for example in a distroless image.
var ErrNoShell = errors.New("no shell in container")

// ErrNoLister is wrapped when a shell exists but none of find, stat or ls
// does, so directories cannot be listed.
var ErrNoLister = errors.New("no find, stat or ls in container")

// Lines of the probe script, one `name=0|1` per tool. The find test needs
// GNU -printf; the stat test needs GNU -c; ls -lan exists everywhere.
const probeScript = `command -v tar >/dev/null 2>&1 && echo tar=1 || echo tar=0
find / -maxdepth 0 -printf '' >/dev/null 2>&1 && echo find=1 || echo find=0
stat -c '%n' / >/dev/null 2>&1 && echo stat=1 || echo stat=0
ls -lan / >/dev/null 2>&1 && echo ls=1 || echo ls=0
`

var shellCandidates = []string{"/bin/bash", "/bin/sh"}

// ProbeContainer finds a shell and the listing tools with at most two execs.
func ProbeContainer(ctx context.Context, x Executor, ref ContainerRef) (Probe, error) {
	var lastErr error
	for _, shell := range shellCandidates {
		var out, errOut bytes.Buffer
		err := x.Stream(ctx, ref, ExecOptions{Command: []string{shell, "-c", probeScript}, Stdout: &out, Stderr: &errOut})
		if ctx.Err() != nil {
			return Probe{}, ctx.Err()
		}
		if err != nil && out.Len() == 0 {
			lastErr = fmt.Errorf("%s: %s", shell, strings.TrimSpace(firstLine(errOut.String()+" "+err.Error())))
			continue
		}
		p := Probe{Shell: shell}
		for _, line := range strings.Split(out.String(), "\n") {
			name, value, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok || value != "1" {
				continue
			}
			switch name {
			case "tar":
				p.Tar = true
			case "find":
				p.Find = true
			case "stat":
				p.Stat = true
			case "ls":
				p.Ls = true
			}
		}
		return p, nil
	}
	return Probe{}, fmt.Errorf("%w: %v", ErrNoShell, lastErr)
}

// ContainerFS is a remotefs.FS over exec in one container. Every operation
// is one command; there is no persistent process. Output from the container
// is parsed, never run.
type ContainerFS struct {
	ctx   context.Context
	x     Executor
	ref   ContainerRef
	probe Probe
}

// OpenContainerFS wraps a probed container. The context bounds every later
// command. A probe without a shell or without any lister is refused.
func OpenContainerFS(ctx context.Context, x Executor, ref ContainerRef, probe Probe) (*ContainerFS, error) {
	if probe.Shell == "" {
		return nil, ErrNoShell
	}
	if !probe.Find && !probe.Stat && !probe.Ls {
		return nil, ErrNoLister
	}
	return &ContainerFS{ctx: ctx, x: x, ref: ref, probe: probe}, nil
}

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
	case strings.Contains(msg, "No such file or directory"), strings.Contains(msg, "not found"):
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
	target  string // symbolic link target, when known
}

func (i *fileInfo) Name() string       { return i.name }
func (i *fileInfo) Size() int64        { return i.size }
func (i *fileInfo) Mode() os.FileMode  { return i.mode }
func (i *fileInfo) ModTime() time.Time { return i.modTime }
func (i *fileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i *fileInfo) Sys() any           { return nil }

// Listing formats. Fields are separated by \001 and records by newline, so
// names with spaces and quotes survive; names containing a newline do not
// (they are rare and would need NUL-terminated records that stat lacks).
//
//	find:  find DIR -mindepth 1 -maxdepth 1 -printf '%y\001%m\001%s\001%T@\001%f\001%l\n'
//	       (Lstat: find PATH -maxdepth 0 -printf ...)
//	stat:  for f in .[!.]* ..?* *; do [ -e "$f" ] || [ -L "$f" ] && printf '%s\001' "$(stat -c '%F\001%a\001%s\001%Y\001%n' -- "$f")" && readlink -- "$f" 2>/dev/null; echo; done
//	       (run with cd DIR; Lstat: the same on one path)
//	ls:    ls -lanA -- DIR     (Lstat: ls -land -- PATH)
const (
	findFormat = `%y\001%m\001%s\001%T@\001%f\001%l\n`
	statFormat = `%F\001%a\001%s\001%Y\001%n`
)

func (f *ContainerFS) ReadDir(dir string) ([]os.FileInfo, error) {
	var infos []*fileInfo
	var err error
	switch {
	case f.probe.Find:
		var out []byte
		out, err = f.run("readdir", dir, "find "+shQuote(dir)+" -mindepth 1 -maxdepth 1 -printf '"+findFormat+"'", nil)
		if err == nil {
			infos, err = parseFindOutput(out)
		}
	case f.probe.Stat:
		script := "cd " + shQuote(dir) + " || exit 1\nfor f in .[!.]* ..?* *; do if [ -e \"$f\" ] || [ -L \"$f\" ]; then printf '%s\\001' \"$(stat -c '" + statFormat + "' -- \"$f\")\"; readlink -- \"$f\" 2>/dev/null; echo; fi; done"
		var out []byte
		out, err = f.run("readdir", dir, script, nil)
		if err == nil {
			infos, err = parseStatOutput(out)
		}
	default:
		var out []byte
		out, err = f.run("readdir", dir, "ls -lanA -- "+shQuote(dir), nil)
		if err == nil {
			infos, err = parseLsOutput(out, time.Now())
		}
	}
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
	var infos []*fileInfo
	var err error
	var out []byte
	switch {
	case f.probe.Find:
		out, err = f.run("lstat", p, "find "+shQuote(p)+" -maxdepth 0 -printf '"+findFormat+"'", nil)
		if err == nil {
			infos, err = parseFindOutput(out)
		}
	case f.probe.Stat:
		script := "f=" + shQuote(p) + "; s=$(stat -c '" + statFormat + "' -- \"$f\") || exit $?; printf '%s\\001' \"$s\"; readlink -- \"$f\" 2>/dev/null; echo"
		out, err = f.run("lstat", p, script, nil)
		if err == nil {
			infos, err = parseStatOutput(out)
		}
	default:
		out, err = f.run("lstat", p, "ls -land -- "+shQuote(p), nil)
		if err == nil {
			infos, err = parseLsOutput(out, time.Now())
		}
	}
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

// Open streams `cat -- PATH`.
func (f *ContainerFS) Open(p string) (io.ReadCloser, error) {
	if info, err := f.lstat(p); err != nil {
		return nil, err
	} else if info.IsDir() {
		return nil, &os.PathError{Op: "open", Path: p, Err: errors.New("is a directory")}
	}
	ctx, cancel := context.WithCancel(f.ctx)
	pr, pw := io.Pipe()
	go func() {
		var errOut bytes.Buffer
		err := f.x.Stream(ctx, f.ref, ExecOptions{Command: []string{f.probe.Shell, "-c", "cat -- " + shQuote(p)}, Stdout: pw, Stderr: &limitedWriter{w: &errOut, limit: 4096}})
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
		fields := strings.Split(string(line), "\001")
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
		fields := strings.Split(string(line), "\001")
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
