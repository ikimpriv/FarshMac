package remotefs

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"farshmac/internal/fsops"
	"github.com/pkg/sftp"
)

// ConnectionLost reports errors after which the session itself is unusable, so
// a Retry has to dial again instead of repeating the call on a dead connection.
func ConnectionLost(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, fsops.ErrCancelled) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.ErrClosedPipe) {
		return true
	}
	if errors.Is(err, sftp.ErrSSHFxConnectionLost) || errors.Is(err, sftp.ErrSSHFxNoConnection) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNABORTED) {
		return true
	}
	if code := FTPStatus(err); code == 421 || code == 426 {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "connection lost") || strings.Contains(message, "use of closed network connection")
}

// Session is one worker's connection to an endpoint. After a lost connection
// the next call dials again (no new password prompt: credentials stay in Config).
type Session struct {
	endpoint Endpoint
	ctx      context.Context
	o        *fsops.Operation
	fs       FS
	lost     bool
	closed   bool
}

// NewSession wraps an already dialed remote FS. Local endpoints need no session.
func NewSession(endpoint Endpoint, ctx context.Context, o *fsops.Operation, f FS) FS {
	if endpoint.Remote == nil {
		return f
	}
	return &Session{endpoint: endpoint, ctx: ctx, o: o, fs: f}
}

// Observe marks the session lost after an error from a stream it returned.
func Observe(f FS, err error) {
	if s, ok := f.(*Session); ok && ConnectionLost(err) && s.ctx.Err() == nil {
		s.lost = true
	}
}

func (s *Session) current() (FS, error) {
	if s.closed {
		return nil, net.ErrClosed
	}
	if !s.lost && s.fs != nil {
		return s.fs, nil
	}
	if s.fs != nil {
		_ = s.fs.Close()
		s.fs = nil
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	s.o.Begin("Reconnecting to "+s.endpoint.Remote.Label(), 0)
	f, err := s.endpoint.Open(s.ctx)
	if err != nil {
		if s.ctx.Err() != nil {
			return nil, s.ctx.Err()
		}
		return nil, err
	}
	s.fs, s.lost = f, false
	return f, nil
}

func use[T any](s *Session, fn func(FS) (T, error)) (T, error) {
	f, err := s.current()
	if err != nil {
		var zero T
		return zero, err
	}
	v, err := fn(f)
	Observe(s, err)
	return v, err
}
func do(s *Session, fn func(FS) error) error {
	_, err := use(s, func(f FS) (struct{}, error) { return struct{}{}, fn(f) })
	return err
}

func (s *Session) ReadDir(p string) ([]os.FileInfo, error) {
	return use(s, func(f FS) ([]os.FileInfo, error) { return f.ReadDir(p) })
}
func (s *Session) Lstat(p string) (os.FileInfo, error) {
	return use(s, func(f FS) (os.FileInfo, error) { return f.Lstat(p) })
}
func (s *Session) Stat(p string) (os.FileInfo, error) {
	return use(s, func(f FS) (os.FileInfo, error) { return f.Stat(p) })
}
func (s *Session) Open(p string) (io.ReadCloser, error) {
	return use(s, func(f FS) (io.ReadCloser, error) { return f.Open(p) })
}
func (s *Session) Create(p string) (io.WriteCloser, error) {
	return use(s, func(f FS) (io.WriteCloser, error) { return f.Create(p) })
}
func (s *Session) Mkdir(p string) error { return do(s, func(f FS) error { return f.Mkdir(p) }) }
func (s *Session) Remove(p string, dir bool) error {
	return do(s, func(f FS) error { return f.Remove(p, dir) })
}
func (s *Session) Rename(a, b string) error { return do(s, func(f FS) error { return f.Rename(a, b) }) }
func (s *Session) Chmod(p string, m os.FileMode) error {
	return do(s, func(f FS) error { return f.Chmod(p, m) })
}
func (s *Session) Chtimes(p string, t time.Time) error {
	return do(s, func(f FS) error { return f.Chtimes(p, t) })
}
func (s *Session) Readlink(p string) (string, error) {
	return use(s, func(f FS) (string, error) { return f.Readlink(p) })
}
func (s *Session) Symlink(target, p string) error {
	return do(s, func(f FS) error { return f.Symlink(target, p) })
}
func (s *Session) Close() error {
	s.closed = true
	if s.fs == nil {
		return nil
	}
	err := s.fs.Close()
	s.fs = nil
	return err
}

// Identity and Canonical keep same-server recursion checks working.
func (s *Session) Identity() string {
	f, err := s.current()
	if c, ok := f.(canonicalFS); ok && err == nil {
		return c.Identity()
	}
	return ""
}
func (s *Session) Canonical(p string) (string, error) {
	return use(s, func(f FS) (string, error) {
		if c, ok := f.(canonicalFS); ok {
			return c.Canonical(p)
		}
		return p, nil
	})
}
