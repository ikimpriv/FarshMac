package fsops

/*
#cgo CFLAGS: -mmacosx-version-min=12.0
#include <sys/clonefile.h>
#include <fcntl.h>
#include <errno.h>
#include <stdlib.h>
static int clone_regular(int fd, const char *dst) {
    if (fclonefileat(fd, AT_FDCWD, dst, CLONE_NOOWNERCOPY) == 0) return 0;
    return errno;
}
*/
import "C"

import (
	"os"
	"syscall"
	"unsafe"
)

// cloneRegular publishes atomically without replacing an existing destination.
// Unsupported volumes and cross-volume copies fall back to streaming.
func cloneRegular(src *os.File, dst string) error {
	d := C.CString(dst)
	defer C.free(unsafe.Pointer(d))
	if code := C.clone_regular(C.int(src.Fd()), d); code != 0 {
		return &os.LinkError{Op: "clone", Old: src.Name(), New: dst, Err: syscall.Errno(code)}
	}
	return nil
}
