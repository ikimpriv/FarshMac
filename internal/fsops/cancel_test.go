package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadDirectoryCancelAtEntry(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		os.WriteFile(filepath.Join(root, name), nil, 0600)
	}
	o := NewOperation("read")
	count := 0
	err := ReadDirectory(root, o, func(os.DirEntry) error { count++; o.Cancel(); return nil })
	if !errors.Is(err, ErrCancelled) || count != 1 {
		t.Fatal(count, err)
	}
}

func TestCancelledSaveAndCreateDoNotPublish(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "original")
	os.WriteFile(path, []byte("original"), 0600)
	info, _ := os.Stat(path)
	o := NewOperation("save")
	o.Cancel()
	if err := Save(path, []byte("changed"), info, o); !errors.Is(err, ErrCancelled) {
		t.Fatal(err)
	}
	if err := Create(filepath.Join(root, "new"), []byte("new"), 0600, o); !errors.Is(err, ErrCancelled) {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	entries, _ := os.ReadDir(root)
	if string(data) != "original" || len(entries) != 1 {
		t.Fatal("cancel published data or left temp files")
	}
}
