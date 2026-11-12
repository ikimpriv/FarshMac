package remotefs

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"os"
	"path"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

type ftpFS struct {
	client  *ftp.ServerConn
	sockets *socketSet
}

func (f *ftpFS) Identity() string { return "ftp:" + f.sockets.identity() }
func (f *ftpFS) Canonical(p string) (string, error) {
	if err := SafePath(p); err != nil {
		return "", err
	}
	if err := f.client.ChangeDir(p); err != nil {
		return "", err
	}
	return f.client.CurrentDir()
}

func (c Config) dialFTP(ctx context.Context) (FS, error) {
	var secure *tls.Config
	if c.Protocol == "ftps" {
		secure = &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12, ClientSessionCache: tls.NewLRUClientSessionCache(8)}
	}
	return c.dialFTPWithTLS(ctx, secure)
}

func (c Config) dialFTPWithTLS(ctx context.Context, secure *tls.Config) (FS, error) {
	if strings.ContainsAny(c.User+c.Password, "\x00\r\n") {
		return nil, fmt.Errorf("FTP credentials contain a protocol control character")
	}
	sockets := newSockets(ctx)
	control := true
	opts := []ftp.DialOption{ftp.DialWithContext(ctx), ftp.DialWithTimeout(15 * time.Second), ftp.DialWithDialFunc(func(network, address string) (net.Conn, error) {
		raw, err := sockets.dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		if control {
			control = false
			return raw, nil
		}
		// The FTP library delegates data TLS to a custom dialer. Control TLS is
		// upgraded by AUTH TLS; every later connection must also be encrypted.
		if secure != nil {
			return tls.Client(raw, secure), nil
		}
		return raw, nil
	})}
	if secure != nil {
		opts = append(opts, ftp.DialWithExplicitTLS(secure))
	}
	client, err := ftp.Dial(c.Address(), opts...)
	if err != nil {
		sockets.finish()
		return nil, err
	}
	user := c.User
	if user == "" {
		user = "anonymous"
	}
	if err = client.Login(user, c.Password); err != nil {
		sockets.finish()
		return nil, err
	}
	return &ftpFS{client, sockets}, nil
}

type ftpInfo struct{ entry *ftp.Entry }

func (i ftpInfo) Name() string { return i.entry.Name }
func (i ftpInfo) Size() int64  { return int64(i.entry.Size) }
func (i ftpInfo) Mode() os.FileMode {
	switch i.entry.Type {
	case ftp.EntryTypeFolder:
		return os.ModeDir | 0755
	case ftp.EntryTypeLink:
		return os.ModeSymlink | 0777
	}
	return 0644
}
func (i ftpInfo) ModTime() time.Time { return i.entry.Time }
func (i ftpInfo) IsDir() bool        { return i.entry.Type == ftp.EntryTypeFolder }
func (i ftpInfo) Sys() any           { return nil }
func (f *ftpFS) ReadDir(p string) ([]os.FileInfo, error) {
	if err := SafePath(p); err != nil {
		return nil, err
	}

	entries, err := f.client.List(p)
	if err != nil {
		return nil, err
	}
	out := make([]os.FileInfo, 0, len(entries))
	for _, e := range entries {
		if e.Size > 1<<63-1 {
			return nil, fmt.Errorf("invalid FTP size")
		}
		out = append(out, ftpInfo{e})
	}
	return out, nil
}
func (f *ftpFS) Lstat(p string) (os.FileInfo, error) {
	if err := SafePath(p); err != nil {
		return nil, err
	}
	entry, statErr := f.client.GetEntry(p)
	if statErr == nil {
		if entry.Size > 1<<63-1 {
			return nil, fmt.Errorf("invalid FTP size")
		}
		entry.Name = path.Base(p)
		return ftpInfo{entry}, nil
	}
	if FTPStatus(statErr) == 550 {
		message := strings.ToLower(statErr.Error())
		if strings.Contains(message, "no such file") || strings.Contains(message, "not found") || strings.Contains(message, "does not exist") {
			return nil, &os.PathError{Op: "stat", Path: p, Err: os.ErrNotExist}
		}
	}
	// Listing the parent also works on servers without MLST. A failed listing is
	// never treated as 'missing' (550 can mean access denied as well).
	entries, err := f.ReadDir(path.Dir(p))
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Name() == path.Base(p) {
			return entry, nil
		}
	}
	if p == "/" {
		return ftpInfo{&ftp.Entry{Name: "/", Type: ftp.EntryTypeFolder}}, nil
	}
	return nil, &os.PathError{Op: "stat", Path: p, Err: os.ErrNotExist}
}

// FTP listings do not reveal link targets: a link is a directory only if it can be listed.
func (f *ftpFS) Stat(p string) (os.FileInfo, error) {
	info, err := f.Lstat(p)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return info, err
	}
	if _, err = f.ReadDir(p); err != nil {
		return info, nil
	}
	return ftpInfo{&ftp.Entry{Name: path.Base(p), Type: ftp.EntryTypeFolder, Time: info.ModTime()}}, nil
}
func (f *ftpFS) Open(p string) (io.ReadCloser, error) {
	if err := SafePath(p); err != nil {
		return nil, err
	}
	return f.client.Retr(p)
}

type ftpWriter struct {
	*io.PipeWriter
	done chan error
}

func (w *ftpWriter) Close() error {
	e := w.PipeWriter.Close()
	remote := <-w.done
	if remote != nil {
		return remote
	}
	return e
}
func (f *ftpFS) Create(p string) (io.WriteCloser, error) {
	if err := SafePath(p); err != nil {
		return nil, err
	}

	r, w := io.Pipe()
	done := make(chan error, 1)
	go func() { err := f.client.Stor(p, r); r.CloseWithError(err); done <- err }()
	return &ftpWriter{w, done}, nil
}
func (f *ftpFS) Mkdir(p string) error {
	if err := SafePath(p); err != nil {
		return err
	}
	return f.client.MakeDir(p)
}
func (f *ftpFS) Remove(p string, dir bool) error {
	if err := SafePath(p); err != nil {
		return err
	}

	if dir {
		return f.client.RemoveDir(p)
	}
	return f.client.Delete(p)
}
func (f *ftpFS) Rename(a, b string) error {
	if err := SafePath(a + b); err != nil {
		return err
	}
	return f.client.Rename(a, b)
}

// ErrNoPermissions is FTP's known inability to set permissions.
var ErrNoPermissions = errors.New("FTP does not provide portable permission preservation")

func (f *ftpFS) Chmod(string, os.FileMode) error {
	return ErrNoPermissions
}
func (f *ftpFS) Chtimes(p string, t time.Time) error {
	if err := SafePath(p); err != nil {
		return err
	}
	return f.client.SetTime(p, t)
}
func (f *ftpFS) Readlink(string) (string, error) {
	return "", errors.New("FTP symbolic links are not supported")
}
func (f *ftpFS) Symlink(string, string) error {
	return errors.New("FTP symbolic links are not supported")
}
func (f *ftpFS) Close() error { f.sockets.finish(); return nil }

// Kept separate from missing-file detection: FTP status 550 is ambiguous.
func FTPStatus(err error) int {
	var e *textproto.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 0
}
