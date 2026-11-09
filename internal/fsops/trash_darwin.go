package fsops

/*
#cgo CFLAGS: -mmacosx-version-min=12.0
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>
char *farshmac_trash_item(const char *path, char **result);
*/
import "C"

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
	"unsafe"
)

// Trash uses macOS's per-volume Trash. It never substitutes permanent deletion.
// The native move cannot be interrupted mid-item; cancellation stops the batch
// before the next item. A directory is moved as a whole, without walking it.
func Trash(path string, o *Operation) error {
	return trashWith(path, o, recycleNative)
}

func trashWith(path string, o *Operation, recycle func(string) error) error {
	if o.Ignored(path) {
		return o.Check()
	}
	if err := o.Check(); err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if !utf8.ValidString(abs) || strings.ContainsRune(abs, 0) {
		return &os.PathError{Op: "trash", Path: path, Err: errors.New("invalid filename encoding")}
	}
	if _, err := os.Lstat(abs); err != nil {
		return err
	}
	o.Begin(abs, 0)
	if err := o.Check(); err != nil {
		return err
	}
	if err := recycle(abs); err != nil {
		return &os.PathError{Op: "trash", Path: path, Err: err}
	}
	o.Complete(abs)
	return nil
}

func recycleNative(path string) error {
	_, err := recycleNativeResult(path)
	return err
}

func recycleNativeResult(path string) (string, error) {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	var result *C.char
	message := C.farshmac_trash_item(p, &result)
	if result != nil {
		defer C.free(unsafe.Pointer(result))
	}
	if message == nil {
		return C.GoString(result), nil
	}
	defer C.free(unsafe.Pointer(message))
	return "", errors.New(C.GoString(message))
}
