package fsops

/*
#cgo CFLAGS: -mmacosx-version-min=12.0
#include <copyfile.h>
#include <errno.h>
#include <stdint.h>
#include <stdlib.h>
#include <sys/attr.h>
#include <sys/stat.h>
#include <unistd.h>

extern int farshmacMetadataCancelled(uintptr_t);
static int metadata_callback(int what, int stage, copyfile_state_t state,
    const char *src, const char *dst, void *ctx) {
    if (farshmacMetadataCancelled((uintptr_t)ctx)) return COPYFILE_QUIT;
    if (stage == COPYFILE_ERR) return COPYFILE_QUIT;
    return COPYFILE_CONTINUE;
}
static int preserve_metadata(const char *src, const char *dst, uintptr_t ctx, int permissions, int attributes) {
    struct stat s, d;
    if (lstat(src, &s) || lstat(dst, &d)) return errno;
    // copyfile can ignore ownership failures. Make them explicit before copying
    // mode/ACL, since chown may clear setuid and setgid bits.
    if (permissions && (s.st_uid != d.st_uid || s.st_gid != d.st_gid) &&
        lchown(dst, s.st_uid, s.st_gid)) return errno;
    copyfile_state_t state = copyfile_state_alloc();
    if (!state) return ENOMEM;
    copyfile_state_set(state, COPYFILE_STATE_STATUS_CB, metadata_callback);
    copyfile_state_set(state, COPYFILE_STATE_STATUS_CTX, (void *)ctx);
    // COPYFILE_STAT combines permissions and attributes; copy these separately
    // so disabling one group really leaves it at the destination's defaults.
    copyfile_flags_t flags = COPYFILE_NOFOLLOW;
    if (permissions) flags |= COPYFILE_ACL;
    if (attributes) flags |= COPYFILE_XATTR;
    int result = copyfile(src, dst, state, flags);
    int code = result == 0 ? 0 : (errno ? errno : EIO);
    copyfile_state_free(state);
    // copyfile may round directory timestamps. setattrlist preserves the full
    // nanosecond precision and creation date, including for symbolic links.
    if (!code && permissions && lchmod(dst, s.st_mode & ALLPERMS)) code = errno;
    if (!code && attributes) {
        struct attrlist attrs = {0};
        attrs.bitmapcount = ATTR_BIT_MAP_COUNT;
        attrs.commonattr = ATTR_CMN_CRTIME | ATTR_CMN_MODTIME | ATTR_CMN_ACCTIME;
        struct { struct timespec birth, modified, accessed; } times = {
            s.st_birthtimespec, s.st_mtimespec, s.st_atimespec
        };
        if (setattrlist(dst, &attrs, &times, sizeof(times), FSOPT_NOFOLLOW)) code = errno;
        if (!code && lchflags(dst, s.st_flags)) code = errno;
    }
    return code;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"runtime/cgo"
	"strings"
	"syscall"
	"unsafe"
)

//export farshmacMetadataCancelled
func farshmacMetadataCancelled(handle C.uintptr_t) C.int {
	if cgo.Handle(handle).Value().(*Operation).Check() != nil {
		return 1
	}
	return 0
}

func preserveMetadata(src, dst string, o *Operation) error {
	options, _ := o.metadataOptions()
	if err := o.Check(); err != nil {
		return err
	}
	s, d := C.CString(src), C.CString(dst)
	defer C.free(unsafe.Pointer(s))
	defer C.free(unsafe.Pointer(d))
	handle := cgo.NewHandle(o)
	defer handle.Delete()
	permissions, attributes := C.int(0), C.int(0)
	if options.Permissions {
		permissions = 1
	}
	if options.Attributes {
		attributes = 1
	}
	code := C.preserve_metadata(s, d, C.uintptr_t(handle), permissions, attributes)
	if err := o.Check(); err != nil {
		return err
	}
	if code != 0 {
		return &os.LinkError{Op: "preserve metadata", Old: src, New: dst, Err: syscall.Errno(code)}
	}
	// Some filesystems silently approximate mode/ownership/timestamps. Do not
	// delete the source without asking when the result differs.
	var before, after syscall.Stat_t
	if err := syscall.Lstat(src, &before); err != nil {
		return err
	}
	if err := syscall.Lstat(dst, &after); err != nil {
		return err
	}
	var different []string
	if options.Permissions && before.Mode != after.Mode {
		different = append(different, "permissions")
	}
	if options.Permissions && (before.Uid != after.Uid || before.Gid != after.Gid) {
		different = append(different, "owner/group")
	}
	if options.Attributes && before.Flags != after.Flags {
		different = append(different, "file flags")
	}
	if options.Attributes && before.Mtimespec != after.Mtimespec {
		different = append(different, "modification time")
	}
	if options.Attributes && before.Birthtimespec != after.Birthtimespec {
		different = append(different, "creation time")
	}
	if len(different) > 0 {
		return fmt.Errorf("%s: destination did not preserve %s", src, strings.Join(different, ", "))
	}
	return nil
}

func (o *Operation) preserveMetadata(src, dst string) error {
	options, explicit := o.metadataOptions()
	if !explicit || (!options.Permissions && !options.Attributes) {
		return nil
	}
	copyMetadata := o.metadataCopier
	if copyMetadata == nil {
		copyMetadata = preserveMetadata
	}
	for {
		err := copyMetadata(src, dst, o)
		if err == nil {
			return nil
		}
		if cancelled := o.Check(); cancelled != nil {
			return cancelled
		}
		if o.MetadataFailure == nil {
			return err
		}
		result := o.MetadataFailure(src, dst, err)
		if errors.Is(result, ErrRetryMetadata) {
			continue
		}
		return result
	}
}
