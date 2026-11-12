package fsops

/*
#cgo CFLAGS: -mmacosx-version-min=12.0
#include <copyfile.h>
#include <errno.h>
#include <sys/stat.h>

// Copy xattrs (Finder tags and comments, quarantine) and the ACL onto the
// replacement, then the user flags that describe the document. Immutable and
// append-only flags are left out: they would block the publishing rename.
static int copy_save_metadata(int src, int dst) {
    struct stat s;
    if (fstat(src, &s)) return errno;
    int code = 0;
    if (fcopyfile(src, dst, NULL, COPYFILE_XATTR | COPYFILE_ACL)) code = errno ? errno : EIO;
    unsigned int flags = s.st_flags & (UF_NODUMP | UF_OPAQUE | UF_HIDDEN);
    if (flags && fchflags(dst, flags) && !code) code = errno;
    return code;
}
*/
import "C"

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// ErrChangedOnDisk means the file is no longer the version the editor opened.
var ErrChangedOnDisk = errors.New("file changed on disk")

// ErrInPlaceRequired means the folder refuses a temporary file but the file
// itself is writable, so it can only be rewritten in place.
var ErrInPlaceRequired = errors.New("folder is not writable; the file can only be rewritten in place")

type SaveOptions struct {
	InPlace bool // Rewrite the existing inode instead of replacing it.
}

type SaveResult struct {
	InPlace      bool
	MetadataLost error // Saved, but some attributes were not carried over.
}

// SaveStrategy is how an editor save can publish its content.
type SaveStrategy int

const (
	SaveReplace SaveStrategy = iota // Temporary file in the folder + rename.
	SaveInPlace                     // Rewrite the existing file; folder is not writable.
	SaveNone                        // Neither is expected to work.
)

const accessWrite = 2 // W_OK

const (
	appendFlags   = 0x4 | 0x40000 // UF_APPEND, SF_APPEND
	immutableMask = 0x2 | 0x20000 | appendFlags
)

func immutableFlags(info os.FileInfo) uint32 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Flags & immutableMask
	}
	return 0
}

func denied(err error) bool {
	return errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EROFS)
}

// SaveAccess predicts which save strategy will work for path. It is only a
// heuristic: permissions change, and network volumes may answer incorrectly,
// so real save errors must still be handled. A missing path is checked as a
// new file in its folder. The reason explains SaveNone in a few words.
func SaveAccess(path string) (SaveStrategy, string) {
	dir := filepath.Dir(path)
	info, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return SaveNone, err.Error()
		}
		if err := syscall.Access(dir, accessWrite); err != nil {
			return SaveNone, accessReason(err)
		}
		return SaveReplace, ""
	}
	if flags := immutableFlags(info); flags != 0 {
		if flags&appendFlags != 0 {
			return SaveNone, "file is append-only (uappnd)"
		}
		return SaveNone, "file is locked (uchg)"
	}
	dirErr := syscall.Access(dir, accessWrite)
	if dirErr == nil {
		return SaveReplace, ""
	}
	if errors.Is(dirErr, syscall.EROFS) {
		return SaveNone, "read-only volume"
	}
	if syscall.Access(path, accessWrite) == nil {
		return SaveInPlace, ""
	}
	return SaveNone, accessReason(dirErr)
}

func accessReason(err error) string {
	switch {
	case errors.Is(err, syscall.EROFS):
		return "read-only volume"
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return "folder is not writable"
	}
	return err.Error()
}

func copySaveMetadata(original string, replacement *os.File) error {
	src, err := os.Open(original)
	if err != nil {
		return err
	}
	defer src.Close()
	if code := C.copy_save_metadata(C.int(src.Fd()), C.int(replacement.Fd())); code != 0 {
		if errors.Is(syscall.Errno(code), syscall.ENOTSUP) {
			return nil
		}
		return &os.PathError{Op: "preserve attributes", Path: original, Err: syscall.Errno(code)}
	}
	return nil
}
