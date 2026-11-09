package ui

import (
	"farshmac/internal/fsops"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArchiveEnterListExtractAndCreate(t *testing.T) {
	root := t.TempDir()
	right := filepath.Join(root, "right")
	os.Mkdir(right, 0700)
	src := filepath.Join(root, "hello.txt")
	os.WriteFile(src, []byte("hello"), 0600)
	file := filepath.Join(root, "my.backup.tar.bz2")
	if err := fsops.CreateArchive([]string{src}, file); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, right)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	wait := func() {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for a.Busy && time.Now().Before(deadline) {
			a.Poll()
			time.Sleep(time.Millisecond)
		}
		if a.Busy {
			t.Fatal("archive UI stuck")
		}
	}
	selectEntry(t, &a.Panels[0], filepath.Base(file))
	a.key(Message{Key: "Enter"})
	wait()
	if a.Panels[0].Archive == nil || a.Panels[0].Path != file {
		t.Fatal("Enter did not open archive panel", a.Status)
	}
	selectEntry(t, &a.Panels[0], "hello.txt")
	a.key(Message{Key: "F3"})
	wait()
	if a.Doc == nil || a.Doc.Text != "hello" || !a.Doc.ReadOnly {
		t.Fatal("archive viewer", a.Status)
	}
	temp := a.Doc.TempDir
	a.documentKey(Message{Key: "Escape"})
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatal("viewer cache leaked")
	}
	a.key(Message{Key: "F5"})
	if a.Form == nil || a.Form.Title != "Copy" {
		t.Fatal("copy dialog missing")
	}
	a.key(Message{Key: "Enter"})
	wait()
	if data, err := os.ReadFile(filepath.Join(right, "hello.txt")); err != nil || string(data) != "hello" {
		t.Fatal("copy out", err, a.Status)
	}
	selectEntry(t, &a.Panels[0], "..")
	a.key(Message{Key: "Enter"})
	if a.Panels[0].Archive != nil || a.Panels[0].Current() != file {
		t.Fatal("archive parent state not restored")
	}
	a.archiveCommands()
	a.formKey(Message{Key: "Enter"})
	wait()
	if a.Doc == nil || !strings.Contains(a.Doc.Text, "hello.txt") {
		t.Fatal("archive list missing", a.Status)
	}
	a.documentKey(Message{Key: "Escape"})
	a.archiveExtract()
	if a.Dialog == nil || a.Dialog.Input != filepath.Join(right, "my.backup") {
		t.Fatal("compound archive extension in extraction folder")
	}
	input := a.Dialog.Input
	a.key(Message{Key: "Enter"})
	wait()
	if data, err := os.ReadFile(filepath.Join(input, "hello.txt")); err != nil || string(data) != "hello" {
		t.Fatal("UI extraction", string(data), err, a.Status)
	}
	selectEntry(t, &a.Panels[0], "hello.txt")
	a.archiveCreate()
	if a.Dialog == nil {
		t.Fatal("create dialog missing")
	}
	a.Dialog.Input = filepath.Join(right, "created.7z")
	a.key(Message{Key: "Enter"})
	wait()
	if _, err := fsops.ArchiveContents(filepath.Join(right, "created.7z"), true); err != nil {
		t.Fatal("UI creation", err, a.Status)
	}
}
