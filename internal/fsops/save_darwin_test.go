package fsops

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func lockedFolder(t *testing.T) (string, string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "file")
	put(t, path, "original content")
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0755) })
	return dir, path
}

func TestSaveFallsBackToInplace(t *testing.T) {
	_, path := lockedFolder(t)
	info, _ := os.Stat(path)
	if _, err := SaveWith(path, []byte("new"), info, SaveOptions{}, nil); !errors.Is(err, ErrInPlaceRequired) {
		t.Fatal("want in-place requirement", err)
	}
	result, err := SaveWith(path, []byte("new"), info, SaveOptions{InPlace: true}, nil)
	if err != nil || !result.InPlace {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if b, _ := os.ReadFile(path); string(b) != "new" || !os.SameFile(info, after) {
		t.Fatal("in-place content or identity", string(b))
	}
	// In place still refuses to clobber a file changed by someone else.
	if _, err = SaveWith(path, []byte("x"), info, SaveOptions{InPlace: true}, nil); !errors.Is(err, ErrChangedOnDisk) {
		t.Fatal("stale in-place save", err)
	}
}

func TestSaveAccessStrategies(t *testing.T) {
	dir, path := lockedFolder(t)
	if s, _ := SaveAccess(path); s != SaveInPlace {
		t.Fatal("writable file in locked folder", s)
	}
	os.Chmod(path, 0444)
	if s, reason := SaveAccess(path); s != SaveNone || reason != "folder is not writable" {
		t.Fatal("read-only file in locked folder", s, reason)
	}
	if s, _ := SaveAccess(filepath.Join(dir, "new")); s != SaveNone {
		t.Fatal("new file in locked folder", s)
	}
	os.Chmod(dir, 0755)
	if s, _ := SaveAccess(path); s != SaveReplace {
		t.Fatal("read-only file in writable folder is replaced", s)
	}
	if err := syscall.Chflags(path, 0x2); err != nil {
		t.Skip("chflags", err)
	}
	defer syscall.Chflags(path, 0)
	if s, reason := SaveAccess(path); s != SaveNone || !strings.Contains(reason, "uchg") {
		t.Fatal("immutable file", s, reason)
	}
}

func TestSavePreservesXattrsAndFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	put(t, path, "old")
	if out, err := exec.Command("xattr", "-w", "com.example.test", "kept", path).CombinedOutput(); err != nil {
		t.Skip("xattr", string(out))
	}
	if err := syscall.Chflags(path, 0x8000); err != nil { // UF_HIDDEN
		t.Skip("chflags", err)
	}
	info, _ := os.Stat(path)
	result, err := SaveWith(path, []byte("new"), info, SaveOptions{}, nil)
	if err != nil || result.MetadataLost != nil {
		t.Fatal(err, result.MetadataLost)
	}
	after, _ := os.Stat(path)
	if os.SameFile(info, after) {
		t.Fatal("expected an atomic replacement")
	}
	out, err := exec.Command("xattr", "-p", "com.example.test", path).Output()
	if err != nil || strings.TrimSpace(string(out)) != "kept" {
		t.Fatal("xattr lost", string(out), err)
	}
	if after.Sys().(*syscall.Stat_t).Flags&0x8000 == 0 {
		t.Fatal("hidden flag lost")
	}
}
