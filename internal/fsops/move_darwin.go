package fsops

/*
#cgo CFLAGS: -mmacosx-version-min=12.0
#include <stdio.h>
#include <errno.h>
#include <stdlib.h>
static int move_exclusive(const char *src, const char *dst) {
    if (renamex_np(src, dst, RENAME_EXCL) == 0) return 0;
    return errno;
}
*/
import "C"

import (
	"os"
	"syscall"
	"unsafe"
)

// Move uses macOS's atomic no-replace rename. Across volumes the rename fails
// with EXDEV; the transfer engine (moveAcrossVolumes) then copies each file with
// its metadata, asks before any loss, and deletes the source afterwards.
func Move(src, dst string) error {
	s, d := C.CString(src), C.CString(dst)
	defer C.free(unsafe.Pointer(s))
	defer C.free(unsafe.Pointer(d))
	if code := C.move_exclusive(s, d); code != 0 {
		return &os.LinkError{Op: "move", Old: src, New: dst, Err: syscall.Errno(code)}
	}
	return nil
}
