package remotefs

import (
	"fmt"
	"os"
	"path"
	"strings"

	"farshmac/internal/fsops"
)

// Move stays on one server. A server that cannot rename across volumes reports
// an error instead of silently degrading to a lossy copy/delete operation.
func Move(f FS, endpoint Endpoint, src, dst string, o *fsops.Operation) error {
	return fsops.IgnoreSkipped(o.Attempt("move", src, dst, func() error { return moveEntry(f, endpoint, src, dst, o, 0) }))
}
func moveEntry(f FS, endpoint Endpoint, src, dst string, o *fsops.Operation, depth int) error {
	if depth > 256 {
		return fmt.Errorf("transfer tree limit exceeded")
	}
	if err := o.Check(); err != nil {
		return err
	}
	src, dst = path.Clean(src), path.Clean(dst)
	if src == dst || strings.HasPrefix(dst, src+"/") {
		return fmt.Errorf("destination is inside source")
	}
	if err := validateRemotePair(f, f, Pair{Source: src, Destination: dst}); err != nil {
		return err
	}
	si, err := f.Lstat(src)
	if err != nil {
		return err
	}
	di, err := f.Lstat(dst)
	if os.IsNotExist(err) {
		if err = f.Rename(src, dst); err == nil {
			o.Complete(src)
		}
		return err
	}
	if err != nil {
		return err
	}
	if si.IsDir() && di.IsDir() {
		entries, err := f.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Name() == "." || entry.Name() == ".." {
				continue
			}
			if !ValidName(entry.Name()) {
				return fmt.Errorf("unsafe server filename")
			}
			child := path.Join(src, entry.Name())
			to := path.Join(dst, entry.Name())
			if err = fsops.IgnoreSkipped(o.Attempt("move", child, to, func() error { return moveEntry(f, endpoint, child, to, o, depth+1) })); err != nil {
				return err
			}
		}
		remaining, err := f.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range remaining {
			if entry.Name() != "." && entry.Name() != ".." {
				return nil
			}
		}
		return o.Attempt("delete source folder", src, "", func() error { return f.Remove(src, true) })
	}
	resolution := o.Resolve(fsops.Collision{Source: src, Destination: dst, SourceInfo: si, DestinationInfo: di}, nil)
	switch resolution.Action {
	case fsops.Skip:
		o.SkipItem(src)
		return nil
	case fsops.Rename:
		if resolution.Destination == "" || resolution.Destination == dst {
			return fmt.Errorf("choose a different destination name")
		}
		return moveEntry(f, endpoint, src, resolution.Destination, o, depth+1)
	case fsops.Append:
		// The collision was already answered; don't ask a second time for this file.
		child := o.Fork(o.Context())
		defer child.Cancel()
		child.Collision = func(fsops.Collision) fsops.Resolution { return fsops.Resolution{Action: fsops.Append} }
		source, e := endpoint.Open(o.Context())
		if e != nil {
			return e
		}
		defer source.Close()
		target, e := endpoint.Open(o.Context())
		if e != nil {
			return e
		}
		defer target.Close()
		if err = copyFile(source, target, endpoint, endpoint, job{Pair{src, dst}, si}, CopyOptions{}, child); err != nil {
			return err
		}
		return o.Attempt("delete source", src, "", func() error { return f.Remove(src, false) })
	case fsops.Replace:
		if si.IsDir() || di.IsDir() {
			return fmt.Errorf("cannot replace a file with a directory or a directory with a file")
		}
		current, e := f.Lstat(dst)
		if e != nil {
			return e
		}
		if current.Size() != di.Size() || !current.ModTime().Equal(di.ModTime()) || current.Mode() != di.Mode() {
			return fmt.Errorf("destination changed during move")
		}
		if err = f.Rename(src, dst); err == nil {
			o.Complete(src)
		}
		return err
	default:
		return fsops.ErrCancelled
	}
}
