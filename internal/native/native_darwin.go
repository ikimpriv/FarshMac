// Package native embeds an offline WebKit terminal surface in a Cocoa window.
package native

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -mmacosx-version-min=12.0
#cgo LDFLAGS: -framework Cocoa -framework WebKit -mmacosx-version-min=12.0
#include <stdlib.h>
void farshmac_run(const char *path);
int farshmac_prepare_disk_access(void);
*/
import "C"

import (
	"embed"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"farshmac/internal/ui"
)

//go:embed assets
var assets embed.FS
var windows *ui.WindowGroup

// PrepareDiskAccess offers the system privacy settings once, before any panels
// or restored sessions read protected folders. Call on the main OS thread.
// False means the user opened Settings and will relaunch after granting access.
func PrepareDiskAccess() bool { return C.farshmac_prepare_disk_access() != 0 }

//export goMessage
func goMessage(id C.int, message *C.char) {
	if windows != nil {
		windows.Handle(int(id), C.GoString(message))
	}
}

//export goPoll
func goPoll(id C.int) *C.char {
	if windows == nil {
		return C.CString("{}")
	}
	b, err := json.Marshal(windows.Poll(int(id)))
	if err != nil {
		return C.CString("{}")
	}
	return C.CString(string(b))
}

//export goNewWindow
func goNewWindow(parent C.int) C.int {
	if windows == nil {
		return 0
	}
	id, e := windows.NewWindow(int(parent))
	if e != nil {
		if a := windows.Apps[int(parent)]; a != nil {
			a.Status = e.Error()
		}
		return 0
	}
	return C.int(id)
}

//export goCloseWindow
func goCloseWindow(id C.int) {
	if windows != nil {
		windows.CloseWindow(int(id))
	}
}

//export goQuitAll
func goQuitAll() {
	if windows != nil {
		windows.BeginQuit()
	}
}

//export goStartupWindows
func goStartupWindows() *C.char {
	b, _ := json.Marshal(windows.StartupWindows())
	return C.CString(string(b))
}

//export goWindowFrame
func goWindowFrame(id C.int, x, y, w, h C.double) {
	if windows != nil {
		windows.SetFrame(int(id), ui.WindowFrame{X: float64(x), Y: float64(y), Width: float64(w), Height: float64(h)})
	}
}

//export goWindowFocused
func goWindowFocused(id C.int) {
	if windows != nil {
		windows.Focus(int(id))
	}
}

//export goSaveSession
func goSaveSession() {
	if windows != nil {
		_ = windows.SaveSession(true)
	}
}

func Run(a *ui.App, restoreSession ...bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dir, err := os.MkdirTemp("", "farshmac-ui-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	err = fs.WalkDir(assets, "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		b, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0600)
	})
	if err != nil {
		return err
	}
	windows = ui.NewWindowGroup(a)
	restore := len(restoreSession) == 0 || restoreSession[0]
	if e := windows.EnableSession(ui.SessionPath(), restore); e != nil {
		a.Status = e.Error()
	}
	if a.Term == nil {
		a.StartTerminalOrRetry()
	}
	defer func() { windows.CloseAll(); windows = nil }()
	path := C.CString(filepath.Join(dir, "assets", "index.html"))
	defer C.free(unsafe.Pointer(path))
	C.farshmac_run(path)
	return nil
}
