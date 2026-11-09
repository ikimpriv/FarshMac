package remotefs

import (
	"io"
	"os"
	"time"
)

type Local struct{}

func (Local) ReadDir(p string) ([]os.FileInfo, error) {
	entries, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	out := make([]os.FileInfo, 0, len(entries))
	for _, e := range entries {
		i, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, nil
}
func (Local) Lstat(p string) (os.FileInfo, error)  { return os.Lstat(p) }
func (Local) Open(p string) (io.ReadCloser, error) { return os.Open(p) }
func (Local) Create(p string) (io.WriteCloser, error) {
	return os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
}
func (Local) Mkdir(p string) error                { return os.Mkdir(p, 0777) }
func (Local) Remove(p string, _ bool) error       { return os.Remove(p) }
func (Local) Rename(a, b string) error            { return os.Rename(a, b) }
func (Local) Chmod(p string, m os.FileMode) error { return os.Chmod(p, m) }
func (Local) Chtimes(p string, t time.Time) error { return os.Chtimes(p, t, t) }
func (Local) Readlink(p string) (string, error)   { return os.Readlink(p) }
func (Local) Symlink(target, p string) error      { return os.Symlink(target, p) }
func (Local) Close() error                        { return nil }
