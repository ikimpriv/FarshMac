package fsops

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ReadDirectory streams filesystem order with cancellation between batches and
// entries; unlike os.ReadDir it never materializes an entire directory first.
func ReadDirectory(path string, o *Operation, visit func(fs.DirEntry) error) error {
	return readDirectory(path, o, func(entry fs.DirEntry) error {
		if o.Ignored(entry.Name()) {
			return nil
		}
		return visit(entry)
	})
}

func readDirectory(path string, o *Operation, visit func(fs.DirEntry) error) error {
	if err := o.Check(); err != nil {
		return err
	}
	var f *os.File
	err := o.Attempt("read folder", path, "", func() error { var e error; f, e = os.Open(path); return e })
	if err != nil {
		return err
	}
	defer f.Close()
	for {
		if err := o.Check(); err != nil {
			return err
		}
		batch, err := f.ReadDir(256)
		for _, entry := range batch {
			if err := o.Check(); err != nil {
				return err
			}
			if err := visit(entry); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// WalkDir has WalkDirFunc's skip/error semantics without the uninterruptible
// directory-wide read/sort done by filepath.WalkDir. It does not follow links.
func WalkDir(root string, o *Operation, visit fs.WalkDirFunc) error {
	if o.Ignored(root) {
		return o.Check()
	}
	var walk func(string, fs.DirEntry) error
	walk = func(path string, d fs.DirEntry) error {
		if err := o.Check(); err != nil {
			return err
		}
		if err := visit(path, d, nil); err != nil {
			if err == fs.SkipDir && d.IsDir() {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			return nil
		}
		var callbackErr error
		err := ReadDirectory(path, o, func(child fs.DirEntry) error {
			callbackErr = walk(filepath.Join(path, child.Name()), child)
			return callbackErr
		})
		if err == fs.SkipDir {
			return nil
		}
		if err != nil && callbackErr == nil && !errors.Is(err, ErrCancelled) {
			err = visit(path, d, err)
			if err == fs.SkipDir {
				return nil
			}
		}
		return err
	}
	if err := o.Check(); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		err = visit(root, nil, err)
	} else {
		err = walk(root, fs.FileInfoToDirEntry(info))
	}
	if err == fs.SkipAll || err == fs.SkipDir {
		return nil
	}
	return err
}
