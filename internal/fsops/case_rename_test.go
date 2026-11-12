package fsops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func caseInsensitiveRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put(t, filepath.Join(root, "probe"), "")
	if _, err := os.Lstat(filepath.Join(root, "PROBE")); err != nil {
		t.Skip("volume is case-sensitive")
	}
	if err := os.Remove(filepath.Join(root, "probe")); err != nil {
		t.Fatal(err)
	}
	return root
}

func names(t *testing.T, dir string) string {
	t.Helper()
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	list, err := f.Readdirnames(-1)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(list, ",")
}

func TestMoveCaseOnlyRename(t *testing.T) {
	root := caseInsensitiveRoot(t)
	put(t, filepath.Join(root, "File"), "data")
	if err := Transfer(filepath.Join(root, "File"), filepath.Join(root, "file"), true, nil); err != nil {
		t.Fatal(err)
	}
	if got := names(t, root); got != "file" {
		t.Fatal("entries", got)
	}
	if b, err := os.ReadFile(filepath.Join(root, "file")); err != nil || string(b) != "data" {
		t.Fatal(err)
	}
	if err := Transfer(filepath.Join(root, "file"), filepath.Join(root, "file"), true, nil); err == nil {
		t.Fatal("moving onto the identical name succeeded")
	}
}

func TestMoveCaseOnlyRenameDirectory(t *testing.T) {
	root := caseInsensitiveRoot(t)
	if err := os.Mkdir(filepath.Join(root, "Folder"), 0700); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "Folder", "child"), "child")
	if err := Transfer(filepath.Join(root, "Folder"), filepath.Join(root, "folder"), true, nil); err != nil {
		t.Fatal(err)
	}
	if got := names(t, root); got != "folder" {
		t.Fatal("entries", got)
	}
	if b, err := os.ReadFile(filepath.Join(root, "folder", "child")); err != nil || string(b) != "child" {
		t.Fatal(err)
	}
}

func TestCopyOntoItselfMessage(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	put(t, file, "data")
	err := Transfer(file, file, false, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot copy a file onto itself") {
		t.Fatal(err)
	}
	if b, e := os.ReadFile(file); e != nil || string(b) != "data" {
		t.Fatal("file changed", e)
	}
}
