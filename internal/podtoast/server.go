package podtoast

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// Server runs after entering the target root in cmd/podtoast. Root is only
// overridden by isolated tests. Production is chrooted, so absolute symlinks
// resolve exactly as they do in the target container, including /proc mounts.
type Owner struct{ UID, GID int }

type Server struct {
	metricsMu    sync.Mutex
	LightMetrics bool
	History      func() (MetricsHistory, error)
	DefaultOwner *Owner
	Root         string
	TargetPID    int
	CheckTarget  func() error
}

func (s *Server) path(p string) string { return filepath.Join(s.Root, filepath.Clean("/"+p)) }
func entry(i os.FileInfo) Entry {
	e := Entry{Filename: i.Name(), Bytes: i.Size(), FileMode: i.Mode(), Modified: i.ModTime()}
	if !utf8.ValidString(i.Name()) {
		e.RawName = []byte(i.Name())
	}
	if st, ok := i.Sys().(*syscall.Stat_t); ok {
		e.UID, e.GID = st.Uid, st.Gid
	}
	return e
}
func reply(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	switch {
	case errors.Is(err, os.ErrNotExist):
		code = 404
	case errors.Is(err, os.ErrPermission):
		code = 403
	case errors.Is(err, os.ErrExist):
		code = 409
	case errors.Is(err, context.Canceled):
		code = 499
	}
	http.Error(w, err.Error(), code)
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.CheckTarget != nil {
		if err := s.CheckTarget(); err != nil {
			http.Error(w, err.Error(), http.StatusGone)
			return
		}
	}
	if r.URL.Path == "/metrics" || r.URL.Path == "/metrics/history" {
		s.MetricsHandler().ServeHTTP(w, r)
		return
	}
	ctx := r.Context()
	p := s.path(r.URL.Query().Get("path"))
	switch {
	case r.Method == "GET" && r.URL.Path == "/v1/hello":
		reply(w, Capability{Version: Version, TargetPID: s.TargetPID, Files: true, DirectorySummary: true, NetworkScope: "pod"})
	case r.Method == "GET" && r.URL.Path == "/v1/directory-summary":
		s.directorySummary(w, r)
	case r.Method == "GET" && r.URL.Path == "/v1/list":
		f, err := os.Open(p)
		if err != nil {
			fail(w, err)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/x-ndjson")
		// A final record distinguishes successful EOF from a truncated transport.
		buffer := bufio.NewWriterSize(w, 64<<10)
		defer buffer.Flush()
		enc := json.NewEncoder(buffer)
		mounts := s.listingMounts()
		directory := s.listingPath(p)
		parentMount := mountAt(mounts, directory)
		mountPoints := map[string]*MountLocation{}
		for i := range mounts {
			mountPoints[mounts[i].Path] = &mounts[i]
		}
		for {
			if ctx.Err() != nil {
				return
			}
			batch, err := f.Readdir(256)
			if err != nil && err != io.EOF {
				_ = enc.Encode(map[string]string{"error": err.Error()})
				return
			}
			for _, info := range batch {
				e := entry(info)
				e.Mount = parentMount
				if mounted := mountPoints[filepath.Clean(filepath.Join(directory, info.Name()))]; mounted != nil {
					e.Mount = mounted
				}
				if e.Mount != nil {
					e.MountPoint = e.Mount.Path != "/" && e.Mount.Path == filepath.Clean(filepath.Join(directory, info.Name()))
				}
				if r.URL.Query().Get("links") == "1" && info.Mode()&os.ModeSymlink != 0 {
					target, statErr := os.Stat(filepath.Join(p, info.Name()))
					dir := statErr == nil && target.IsDir()
					e.LinkDirectory = &dir
				}
				if enc.Encode(Result{Entry: &e}) != nil {
					return
				}
			}
			if err := buffer.Flush(); err != nil {
				return
			}
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			if err == io.EOF {
				_ = enc.Encode(Result{Text: "done"})
				return
			}
		}
	case r.Method == "GET" && r.URL.Path == "/v1/read":
		f, err := openRegular(p, os.O_RDONLY, 0)
		if err != nil {
			fail(w, err)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		// The trailer propagates errors after headers (including partial reads).
		w.Header().Set("Trailer", "Podtoast-Error")
		if _, err := copyContext(ctx, w, f); err != nil {
			w.Header().Set("Podtoast-Error", "read failed: "+err.Error())
		}
	case r.Method == "PUT" && r.URL.Path == "/v1/write":
		f, err := openRegular(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			fail(w, err)
			return
		}
		if s.DefaultOwner != nil {
			err = f.Chown(s.DefaultOwner.UID, s.DefaultOwner.GID)
		}
		if err == nil {
			_, err = copyContext(ctx, f, r.Body)
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(p)
			fail(w, err)
			return
		}
		reply(w, Result{})
	case r.Method == "POST" && r.URL.Path == "/v1/op":
		var op Operation
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&op); err != nil {
			fail(w, err)
			return
		}
		out, err := s.operation(ctx, op)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, out)
	case r.Method == "GET" && r.URL.Path == "/v1/snapshot":
		reply(w, s.snapshot(ctx))
	case r.Method == "GET" && r.URL.Path == "/v1/monitor":
		w.Header().Set("Content-Type", "application/x-ndjson")
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			if json.NewEncoder(w).Encode(s.snapshot(ctx)) != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if s.CheckTarget != nil {
				if s.CheckTarget() != nil {
					return
				}
			}
		}
	case r.Method == "POST" && r.URL.Path == "/v1/probe":
		var p Probe
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&p); err != nil {
			fail(w, err)
			return
		}
		result, err := runProbe(ctx, p)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, result)
	default:
		http.NotFound(w, r)
	}
}
func openRegular(p string, flags int, mode os.FileMode) (*os.File, error) {
	f, err := os.OpenFile(p, flags|syscall.O_NONBLOCK, mode)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err == nil && !st.Mode().IsRegular() {
		err = fmt.Errorf("not a regular file: %s", p)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func copyContext(ctx context.Context, w io.Writer, r io.Reader) (int64, error) {
	buf := make([]byte, 128<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := r.Read(buf)
		if n > 0 {
			m, e := w.Write(buf[:n])
			total += int64(m)
			if e != nil {
				return total, e
			}
			if m != n {
				return total, io.ErrShortWrite
			}
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}
func (s *Server) operation(ctx context.Context, o Operation) (Result, error) {
	if o.RawPath != nil {
		o.Path = string(o.RawPath)
	}
	if o.RawTo != nil {
		o.To = string(o.RawTo)
	}
	if o.RawPrevious != nil {
		o.Previous = string(o.RawPrevious)
	}
	p, to := s.path(o.Path), s.path(o.To)
	var out Result
	var err error
	switch o.Op {
	case "stat", "lstat":
		var info os.FileInfo
		if o.Op == "stat" {
			info, err = os.Stat(p)
		} else {
			info, err = os.Lstat(p)
		}
		if err == nil {
			e := entry(info)
			out.Entry = &e
		}
	case "mkdir":
		err = os.Mkdir(p, 0755)
		if err == nil && s.DefaultOwner != nil {
			err = os.Chown(p, s.DefaultOwner.UID, s.DefaultOwner.GID)
			if err != nil {
				_ = os.Remove(p)
			}
		}
	case "remove":
		var st os.FileInfo
		st, err = os.Lstat(p)
		if err == nil && st.IsDir() != o.Directory {
			err = fmt.Errorf("file type changed: %s", o.Path)
		}
		if err == nil {
			err = os.Remove(p)
		}
	case "rename":
		err = os.Rename(p, to)
	case "chmod":
		err = os.Chmod(p, o.Mode)
	case "chtimes":
		err = os.Chtimes(p, o.Time, o.Time)
	case "symlink":
		err = os.Symlink(o.Path, to) // Target is literal, including relative links.
		if err == nil && s.DefaultOwner != nil {
			err = os.Lchown(to, s.DefaultOwner.UID, s.DefaultOwner.GID)
			if err != nil {
				_ = os.Remove(to)
			}
		}
	case "readlink":
		out.Text, err = os.Readlink(p)
	case "canonical":
		out.Text, err = filepath.EvalSymlinks(p)
		if err == nil && s.Root != "/" {
			out.Text = "/" + strings.TrimPrefix(strings.TrimPrefix(out.Text, s.Root), "/")
		}
	case "copy":
		var dst *os.File
		dst, err = openRegular(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			break
		}
		if s.DefaultOwner != nil {
			err = dst.Chown(s.DefaultOwner.UID, s.DefaultOwner.GID)
			if err != nil {
				dst.Close()
				_ = os.Remove(to)
				break
			}
		}
		sources := []string{p}
		if o.Previous != "" {
			sources = append([]string{s.path(o.Previous)}, sources...)
		}
		for _, src := range sources {
			var f *os.File
			f, err = openRegular(src, os.O_RDONLY, 0)
			if err != nil {
				break
			}
			_, err = copyContext(ctx, dst, f)
			f.Close()
			if err != nil {
				break
			}
		}
		ce := dst.Close()
		if err == nil {
			err = ce
		}
		if err != nil {
			_ = os.Remove(to)
		}
	default:
		err = fmt.Errorf("unknown operation %q", o.Op)
	}
	out.RawText = []byte(out.Text)
	return out, err
}
