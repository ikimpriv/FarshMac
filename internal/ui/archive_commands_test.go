package ui

import (
	"os"
	"path/filepath"
	"testing"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
)

func TestArchiveCommandsRefuseRemoteOppositePanel(t *testing.T) {
	root, hidden := t.TempDir(), t.TempDir()
	src := filepath.Join(root, "hello.txt")
	if err := os.WriteFile(src, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := fsops.CreateArchive([]string{src}, filepath.Join(root, "pack.zip")); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, hidden)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	// A remote panel keeps its pre-connection local folder out of sight.
	a.Panels[1].Remote = &remotefs.Config{Protocol: "sftp", Host: "example.invalid", Port: 22, Directory: "/"}
	a.Panels[1].RemoteLocalPath = hidden
	a.Panels[1].Path = "/"
	selectEntry(t, &a.Panels[0], "hello.txt")
	a.archiveCreate()
	if a.Dialog != nil || a.Status != archiveRemoteStatus {
		t.Fatalf("Shift+F1: dialog=%v status=%q", a.Dialog, a.Status)
	}
	a.Status = ""
	selectEntry(t, &a.Panels[0], "pack.zip")
	a.archiveExtract()
	if a.Dialog != nil || a.Status != archiveRemoteStatus {
		t.Fatalf("Shift+F2: dialog=%v status=%q", a.Dialog, a.Status)
	}
	a.archiveCommands()
	a.Form.Focus = 2 // Extract to folder…
	a.formKey(Message{Key: "Enter"})
	if a.Dialog == nil || a.Dialog.Input != filepath.Join(root, "pack") {
		t.Fatalf("Shift+F3 extract default: %+v", a.Dialog)
	}
	a.key(Message{Key: "Enter"})
	waitWork(t, a)
	if _, err := os.Stat(filepath.Join(root, "pack", "hello.txt")); err != nil {
		t.Fatal("extracted next to the archive", err, a.Status)
	}
	if entries, _ := os.ReadDir(hidden); len(entries) != 0 {
		t.Fatal("wrote into the remote panel's hidden local folder", entries)
	}
}
