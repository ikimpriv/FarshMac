package fsops

import (
	"archive/zip"
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
			in, e := os.Open(path)
			if e != nil {
				return e
			}
			defer in.Close()
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

// ExtractZIP uses a fresh destination directory. Existing paths are never merged.
// Symlink entries are rejected so subsequent entries cannot escape the destination.
func ExtractZIP(src, dst string, ops ...*Operation) (err error) {
	o := operation(ops)
	z, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, entry := range z.File {
		if err = o.Check(); err != nil {
			return err
		}
		if !o.ignoredArchivePath(entry.Name) {
			o.Add(entry.Name, int64(entry.UncompressedSize64))
		}
		name := entry.Name
		clean := filepath.Clean(filepath.FromSlash(name))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.Contains(name, "\\") || entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe or symlink ZIP entry: %s", name)
		}
		if !entry.Mode().IsRegular() && !entry.FileInfo().IsDir() {
			return fmt.Errorf("unsupported ZIP entry: %s", name)
		}
	}
	o.Ready()
	if err = os.Mkdir(dst, 0755); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dst)
		}
	}()
	for _, entry := range z.File {
		if err = o.Check(); err != nil {
			return err
		}
		if o.ignoredArchivePath(entry.Name) {
			continue
		}
		o.Begin(entry.Name, int64(entry.UncompressedSize64))
		path := filepath.Join(dst, filepath.FromSlash(entry.Name))
		if entry.FileInfo().IsDir() {
			if err = os.MkdirAll(path, 0755); err != nil {
				return err
			}
			o.Complete(entry.Name)
			continue
		}
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		in, e := entry.Open()
		if e != nil {
			return e
		}
		out, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, entry.Mode().Perm()|0600)
		if e != nil {
			in.Close()
			return e
		}
		_, e = copyProgress(out, in, o)
		in.Close()
		ce := out.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		o.Complete(entry.Name)
		if !entry.Modified.IsZero() {
			_ = os.Chtimes(path, entry.Modified, entry.Modified)
		}
	}
	return nil
}
func ZIPContents(src string, test bool, ops ...*Operation) (string, error) {
	o := operation(ops)
	z, e := zip.OpenReader(src)
	if e != nil {
		return "", e
	}
	defer z.Close()
	var b strings.Builder
	for _, f := range z.File {
		o.Add(f.Name, int64(f.UncompressedSize64))
	}
	o.Ready()
	for _, f := range z.File {
		if e := o.Check(); e != nil {
			return "", e
		}
		o.Begin(f.Name, int64(f.UncompressedSize64))
		fmt.Fprintf(&b, "%12d  %s\n", f.UncompressedSize64, f.Name)
		if test && !f.FileInfo().IsDir() {
			r, e := f.Open()
			if e != nil {
				return "", e
			}
			_, e = copyProgress(io.Discard, r, o)
			r.Close()
			if e != nil {
				return "", e
			}
		}
		o.Complete(f.Name)
	}
	if test {
		b.WriteString("\nArchive integrity: OK\n")
	}
	return b.String(), nil
}
