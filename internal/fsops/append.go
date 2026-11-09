package fsops

import (
	"fmt"
	"os"
	"path/filepath"
)

// Append stages the complete result so cancellation leaves the old file intact.
func appendFile(src, dst string, si, di os.FileInfo, move bool, o *Operation) error {
	if !si.Mode().IsRegular() || !di.Mode().IsRegular() {
		return fmt.Errorf("append requires two regular files")
	}
	stage, err := os.MkdirTemp(filepath.Dir(dst), ".farshmac-append-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	tmp := filepath.Join(stage, "item")
	// Copy the original destination with cancellation, retaining its metadata.
	prefix := o
	if o != nil {
		prefix = o.Fork(o.Context())
		prefix.CopyMetadata = nil
		defer prefix.Cancel()
	}
	if err = Copy(dst, tmp, prefix); err != nil {
		return err
	}
	// A read-only original may have been cloned read-only. Only our private stage is changed.
	if err = os.Chmod(tmp, 0600); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	n, err := copyProgress(out, in, o)
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if n != si.Size() {
		return fmt.Errorf("source size changed during append")
	}
	if err = os.Chmod(tmp, di.Mode().Perm()); err != nil {
		return err
	}
	current, err := os.Lstat(dst)
	if err != nil {
		return err
	}
	if !os.SameFile(di, current) || di.Size() != current.Size() || !di.ModTime().Equal(current.ModTime()) {
		return fmt.Errorf("destination changed during append")
	}
	if err = o.Check(); err != nil {
		return err
	}
	if err = os.Rename(tmp, dst); err != nil {
		return err
	}
	o.Complete(src)
	if move {
		return o.Attempt("delete source", src, "", func() error { return os.Remove(src) })
	}
	return nil
}
