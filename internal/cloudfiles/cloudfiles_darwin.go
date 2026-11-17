package cloudfiles

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -mmacosx-version-min=12.0
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>
char *farshmac_cloud_materialize(const char *path, char **error);
*/
import "C"

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

func dataless(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Flags&0x40000000 != 0
} // SF_DATALESS
func materialize(path string) (string, error) {
	input := C.CString(path)
	defer C.free(unsafe.Pointer(input))
	var message *C.char
	output := C.farshmac_cloud_materialize(input, &message)
	if message != nil {
		defer C.free(unsafe.Pointer(message))
	}
	if output != nil {
		defer C.free(unsafe.Pointer(output))
		return C.GoString(output), nil
	}
	if message != nil {
		return "", fmt.Errorf("%s", C.GoString(message))
	}
	return "", fmt.Errorf("the cloud provider did not make this file available")
}
