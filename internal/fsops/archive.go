package fsops

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CreateZIP publishes a new archive only after every entry was written successfully.
func CreateZIP(paths []string, dst string, ops ...*Operation) error {
	o := operation(ops)
	if e := o.Scan(paths); e != nil {
		return e
	}
	for _, src := range paths {
		info, err := os.Lstat(src)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			continue
		}
		realSrc, err := filepath.EvalSymlinks(src)
		if err != nil {
			return err
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(dst))
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(realSrc, parent)
		if err != nil {
			return err
		}
		if rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("archive destination is inside the source directory")
		}
	}
	f, err := os.CreateTemp(filepath.Dir(dst), ".farshmac-zip-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	z := zip.NewWriter(f)
	for _, root := range paths {
		err = WalkDir(root, o, func(path string, d fs.DirEntry, e error) error {
			if errors.Is(e, ErrSkipped) {
				return nil
			}
			if e != nil {
				return e
			}
			if e = o.Check(); e != nil {
				return e
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			rel, e := filepath.Rel(filepath.Dir(root), path)
			if e != nil {
				return e
			}
			var in *os.File
			if info.Mode().IsRegular() {
				if in, e = openArchiveSource(path, o); errors.Is(e, ErrSkipped) {
					return nil
				} else if e != nil {
					return e
				}
				defer in.Close()
			}
			h, e := zip.FileInfoHeader(info)
			if e != nil {
				return e
			}
			h.Name = filepath.ToSlash(rel)
			if d.IsDir() {
				h.Name += "/"
			} else {
				h.Method = zip.Deflate
			}
			if !d.IsDir() && !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("unsupported special file: %s", path)
			}
			w, e := z.CreateHeader(h)
			if e != nil {
				return e
			}
			if d.IsDir() {
				o.Complete(path)
				return nil
			}
			if info.Mode()&os.ModeSymlink != 0 {
				target, e := os.Readlink(path)
				if e != nil {
					return e
				}
				_, e = io.WriteString(w, target)
				if e == nil {
					o.Complete(path)
				}
				return e
			}
			o.Begin(path, info.Size())
			_, e = copyProgress(w, in, o)
			if e == nil {
				o.Complete(path)
			}
			return e
		})
		if err != nil {
			z.Close()
			return err
		}
	}
	if err = z.Close(); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = o.Check(); err != nil {
		return err
	}
	return os.Link(f.Name(), dst)
}
