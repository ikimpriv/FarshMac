// Package fsops implements local filesystem operations without overwriting targets.
package fsops

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Copy copies a regular file, directory, or symbolic link. Links are not followed.
// The destination must not exist. A failed copy removes only its own new output.
func Copy(src, dst string, ops ...*Operation) error {
	o := operation(ops)
	if o.Ignored(src) {
		return o.Check()
	}
	if e := o.Check(); e != nil {
		return e
	}
	src, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	dst, err = filepath.Abs(dst)
	if err != nil {
		return err
	}
	o.ensureRoots(src)
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		realSrc, err := filepath.EvalSymlinks(src)
		if err != nil {
			return err
		}
		parent, err := filepath.EvalSymlinks(filepath.Dir(dst))
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(realSrc, filepath.Join(parent, filepath.Base(dst)))
		if err != nil {
			return err
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
			return fmt.Errorf("cannot copy a directory into itself")
		}
	}
	return copyEntry(src, dst, o)
}

func copyEntry(src, dst string, o *Operation) (err error) {
	if o.symlinkMode() == SymlinksContents {
		if info, e := os.Lstat(src); e == nil && info.Mode()&os.ModeSymlink != 0 {
			// Retry/Skip apply to this link only; metadata comes from the target.
			return o.Attempt("copy", src, dst, func() error { return o.copyLinkContents(src, dst) })
		}
	}
	var release func(bool)
	options, explicit := o.metadataOptions()
	// All destinations here are newly created. On metadata rejection keep the
	// source intact and discard only this operation's incomplete output.
	defer func() {
		if err == nil {
			if err = o.preserveMetadata(src, dst); err != nil {
				_ = os.RemoveAll(dst)
			}
		}
		if release != nil {
			release(err == nil)
		}
	}()
	if err = o.Check(); err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if o.symlinkMode() == SymlinksContents {
		o.Add(src, regularSize(info)) // Followed folders were not prescanned.
	}
	linked := false
	if linked, release, err = o.copyHardlink(src, dst, info); err != nil || linked {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		err = os.Symlink(o.copiedLinkText(src, dst, target), dst)
		if err == nil {
			o.Complete(src)
		}
		return err
	}
	if info.IsDir() {
		mode := os.FileMode(0700)
		if explicit && !options.Permissions {
			mode = 0777
		} // umask and parent ACL apply
		if err = os.Mkdir(dst, mode); err != nil {
			return err
		}
		defer func() {
			if err != nil {
				_ = os.RemoveAll(dst)
			}
		}()
		if err = ReadDirectory(src, o, func(entry os.DirEntry) error {
			return IgnoreSkipped(copyEntry(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name()), o))
		}); err != nil {
			return err
		}
		if !explicit {
			if err = os.Chmod(dst, info.Mode().Perm()); err != nil {
				return err
			}
			err = os.Chtimes(dst, info.ModTime(), info.ModTime())
		}
		if err == nil {
			o.Complete(src)
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported special file: %s", src)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	o.Begin(src, info.Size())
	if err = o.Check(); err != nil {
		return err
	}
	// clonefile also copies metadata. Use streaming for selective copies so
	// unchecked permissions/attributes cannot leak from an APFS clone.
	cloneErr := error(syscall.ENOTSUP)
	if !explicit || (options.Permissions && options.Attributes) {
		cloneErr = cloneRegular(in, dst)
	}
	if cloneErr == nil {
		if err = o.Check(); err != nil {
			_ = os.Remove(dst)
			return err
		}
		o.Advance(info.Size())
		o.Complete(src)
		return nil
	}
	if !errors.Is(cloneErr, syscall.ENOTSUP) && !errors.Is(cloneErr, syscall.EXDEV) && !errors.Is(cloneErr, syscall.EINVAL) && !errors.Is(cloneErr, syscall.ENOSYS) && !errors.Is(cloneErr, syscall.EPERM) {
		return cloneErr
	}
	return copyRegularStream(in, dst, info, o)
}

func copyRegularStream(in *os.File, dst string, info os.FileInfo, o *Operation) (err error) {
	src := in.Name()
	options, explicit := o.metadataOptions()
	// Publish by exclusive same-volume rename; FAT/exFAT have no hard links.
	mode := os.FileMode(0600)
	if explicit && !options.Permissions {
		mode = 0666
	}
	out, err := createCopyTemp(filepath.Dir(dst), mode)
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	if _, err = copyProgress(out, in, o); err != nil {
		return err
	}
	if !explicit {
		if err = out.Chmod(info.Mode().Perm()); err != nil {
			return err
		}
	}
	// Ordinary copies use the OS writeback cache, like cp. Editor saves below
	// retain fsync; forcing durability for each copied file cripples small trees.
	if err = out.Close(); err != nil {
		return err
	}
	if !explicit {
		if err = os.Chtimes(out.Name(), info.ModTime(), info.ModTime()); err != nil {
			return err
		}
	}
	if err = o.Check(); err != nil {
		return err
	}
	err = Move(out.Name(), dst)
	if err == nil {
		o.Complete(src)
	}
	return err
}

func regularSize(info os.FileInfo) int64 {
	if info.Mode().IsRegular() {
		return info.Size()
	}
	return 0
}

// Create in the destination parent so its ACL and the process umask apply
// exactly as for a normal new file, without changing the process-wide umask.
func createCopyTemp(dir string, mode os.FileMode) (*os.File, error) {
	for attempt := 0; attempt < 10; attempt++ {
		file, err := os.OpenFile(filepath.Join(dir, ".farshmac-copy-"+rand.Text()), os.O_RDWR|os.O_CREATE|os.O_EXCL, mode)
		if !os.IsExist(err) {
			return file, err
		}
	}
	return nil, fmt.Errorf("could not reserve copy temporary file")
}

// Save atomically replaces an existing regular file if its identity and timestamp
// still match the version opened by the editor. Symlinks are resolved by caller.
func Save(path string, content []byte, original os.FileInfo, ops ...*Operation) error {
	_, err := SaveWith(path, content, original, SaveOptions{}, operation(ops))
	return err
}

// SaveWith saves like Save. Without InPlace a folder that refuses the
// temporary file yields ErrInPlaceRequired when the file itself is writable,
// so the caller can ask before rewriting it non-atomically.
func SaveWith(path string, content []byte, original os.FileInfo, options SaveOptions, o *Operation) (SaveResult, error) {
	if err := o.Check(); err != nil {
		return SaveResult{}, err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return SaveResult{}, err
	}
	if !current.Mode().IsRegular() {
		return SaveResult{}, fmt.Errorf("%s is not a regular file", path)
	}
	if original == nil || !os.SameFile(current, original) || current.Size() != original.Size() || !current.ModTime().Equal(original.ModTime()) {
		return SaveResult{}, ErrChangedOnDisk
	}
	if options.InPlace {
		return SaveResult{InPlace: true}, saveInPlace(path, content, o)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".farshmac-save-*")
	if err != nil {
		if denied(err) && syscall.Access(path, accessWrite) == nil && immutableFlags(current) == 0 {
			return SaveResult{}, fmt.Errorf("%w: %w", ErrInPlaceRequired, err)
		}
		return SaveResult{}, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = copyProgress(f, bytes.NewReader(content), o); err != nil {
		return SaveResult{}, err
	}
	if err = f.Chmod(current.Mode().Perm()); err != nil {
		return SaveResult{}, err
	}
	// Tags, comments, ACLs and user flags belong to the document, not to the
	// inode that happens to hold it. Losing them is reported, never fatal.
	metadataErr := copySaveMetadata(path, f)
	if err = f.Sync(); err != nil {
		return SaveResult{}, err
	}
	if err = f.Close(); err != nil {
		return SaveResult{}, err
	}
	if err = o.Check(); err != nil {
		return SaveResult{}, err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return SaveResult{}, err
	}
	return SaveResult{MetadataLost: metadataErr}, nil
}

// saveInPlace rewrites the existing inode. It is not atomic: a failed write
// leaves the file truncated, which the error states explicitly.
func saveInPlace(path string, content []byte, o *Operation) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	_, err = copyProgress(f, bytes.NewReader(content), o)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("%w (file may be truncated; retry or save elsewhere)", err)
	}
	return nil
}

// Create publishes a completed file without replacing an existing path.
func Create(path string, content []byte, mode os.FileMode, ops ...*Operation) error {
	o := operation(ops)
	if err := o.Check(); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".farshmac-new-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = copyProgress(f, bytes.NewReader(content), o); err != nil {
		return err
	}
	if err = f.Chmod(mode); err != nil {
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
	return os.Link(f.Name(), path)
}
