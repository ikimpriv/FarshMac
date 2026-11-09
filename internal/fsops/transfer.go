package fsops

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var ErrCancelled = errors.New("operation cancelled")

// Conflict returns 0 to replace, 1 to skip, or 2 to stop.
type Conflict func(src, dst string) int

func Transfer(src, dst string, move bool, conflict Conflict, ops ...*Operation) error {
	o := operation(ops)
	label := "copy"
	if move {
		label = "move"
	}
	return IgnoreSkipped(o.Attempt(label, src, dst, func() error { return transfer(src, dst, move, conflict, o) }))
}
func transfer(src, dst string, move bool, conflict Conflict, o *Operation) error {
	if move {
		if o == nil {
			o = NewOperation("Move")
			defer o.Cancel()
		}
		previous := o.moving
		o.moving = true // Moving always preserves the whole selected tree.
		defer func() { o.moving = previous }()
	}
	if o.Ignored(src) {
		return o.Check()
	}
	if e := o.Check(); e != nil {
		return e
	}
	src, _ = filepath.Abs(src)
	dst, _ = filepath.Abs(dst)
	si, e := os.Lstat(src)
	if e != nil {
		return e
	}
	if move {
		size := int64(0)
		if si.Mode().IsRegular() {
			size = si.Size()
		}
		o.Add(src, size)
	}
	parent, e := filepath.EvalSymlinks(filepath.Dir(dst))
	if e != nil {
		return e
	}
	realDst := filepath.Join(parent, filepath.Base(dst))
	realSrc, e := filepath.EvalSymlinks(src)
	if e != nil && si.Mode()&os.ModeSymlink == 0 {
		return e
	}
	if si.IsDir() {
		rel, _ := filepath.Rel(realSrc, realDst)
		if rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("destination is inside source")
		}
	}
	di, e := os.Lstat(dst)
	if os.IsNotExist(e) {
		if move {
			e = Move(src, dst)
			if !errors.Is(e, syscall.EXDEV) {
				if e == nil {
					o.CompleteTree(src)
				}
				return e
			}
			return moveAcrossVolumes(src, dst, o)
		}
		if e = Copy(src, dst, o); e != nil {
			return e
		}
		return nil
	}
	if e != nil {
		return e
	}
	if src == dst || os.SameFile(si, di) {
		return fmt.Errorf("source and destination are the same file")
	}
	if si.IsDir() && di.IsDir() {
		if e = ReadDirectory(src, o, func(entry os.DirEntry) error {
			return Transfer(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name()), move, conflict, o)
		}); e != nil {
			return e
		}
		o.Complete(src)
		if move {
			if e = o.Check(); e != nil {
				return e
			}
			remaining, e := readOne(src)
			if e != nil {
				return e
			}
			if len(remaining) == 0 {
				return o.Attempt("delete source", src, "", func() error { return os.Remove(src) })
			}
		}
		return nil
	}
	resolution := o.Resolve(Collision{src, dst, si, di}, conflict)
	switch resolution.Action {
	case Skip:
		o.SkipItem(src)
		return nil
	case Rename:
		if resolution.Destination == "" || resolution.Destination == dst {
			return fmt.Errorf("choose a different destination name")
		}
		return transfer(src, resolution.Destination, move, conflict, o)
	case Append:
		return appendFile(src, dst, si, di, move, o)
	case Replace:
	default:
		return ErrCancelled
	}
	if si.IsDir() || di.IsDir() {
		return fmt.Errorf("cannot replace a file with a directory or a directory with a file")
	}
	if move {
		if e = o.Check(); e != nil {
			return e
		}
		current, err := os.Lstat(dst)
		if err != nil || !os.SameFile(di, current) || di.Size() != current.Size() || !di.ModTime().Equal(current.ModTime()) {
			return fmt.Errorf("destination changed during move")
		}
		if e = os.Rename(src, dst); !errors.Is(e, syscall.EXDEV) {
			if e == nil {
				o.CompleteTree(src)
			}
			return e
		}
	}
	// Stage first; a failed copy leaves the destination intact. Symlink targets are never followed.
	stage, e := os.MkdirTemp(filepath.Dir(dst), ".farshmac-transfer-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	tmp := filepath.Join(stage, "item")
	if e = Copy(src, tmp, o); e != nil {
		return e
	}
	current, e := os.Lstat(dst)
	if e != nil {
		return e
	}
	if !os.SameFile(di, current) || di.Size() != current.Size() || !di.ModTime().Equal(current.ModTime()) {
		return fmt.Errorf("destination changed during copy")
	}
	if e = o.Check(); e != nil {
		return e
	}
	if e = os.Rename(tmp, dst); e != nil {
		return e
	}
	o.relocateHardlinks(tmp, dst)
	if move {
		return o.Attempt("delete source", src, "", func() error { return os.Remove(src) })
	}
	return nil
}

// Only called after rename reports EXDEV. Copy every entry and its metadata
// successfully (or obtain explicit consent for metadata loss) before removal.
func moveAcrossVolumes(src, dst string, o *Operation) error {
	previous := o.moving
	o.moving = true
	defer func() { o.moving = previous }()
	if err := o.Scan([]string{src}); err != nil {
		return err
	}
	if err := Copy(src, dst, o); err != nil {
		return err
	}
	return o.Attempt("delete source", src, "", func() error { return Remove(src, o) })
}

func readOne(path string) ([]os.DirEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	d, err := f.ReadDir(1)
	if err == io.EOF {
		err = nil
	}
	return d, err
}
