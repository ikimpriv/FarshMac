package ui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
)

func archivePanel(root string) Panel {
	archive := filepath.Join(root, "archive.zip")
	return Panel{Path: archive, Archive: &fsops.ArchiveIndex{Path: archive}, Entries: []Entry{{Name: "inside.txt"}}}
}

func TestUserMenuRefusesArchiveMember(t *testing.T) {
	root := t.TempDir()
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Panels[0] = archivePanel(root)
	a.runUserCommand("cat {}")
	if a.Command != "" || !strings.Contains(a.Status, "Use F5 to copy this archive item out before running commands on it") {
		t.Fatalf("command %q status %q", a.Command, a.Status)
	}
}

func TestAssociationsMenuOpenUsesSystemOpenChecks(t *testing.T) {
	root := t.TempDir()
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	opened := false
	a.runExternal = func(context.Context, []string) ([]byte, error) { opened = true; return nil, nil }
	for name, panel := range map[string]Panel{
		"remote":  {Path: "/etc", Remote: &remotefs.Config{Protocol: "sftp", Host: "example.invalid"}, Entries: []Entry{{Name: "hosts"}}},
		"archive": archivePanel(root),
	} {
		a.Panels[0] = panel
		a.Status = ""
		a.fileAssociations()
		a.Form.Focus = 1 // Open current file with macOS default app
		a.formKey(Message{Key: "Enter"})
		waitWork(t, a)
		if opened || a.Status == "" {
			t.Fatalf("%s: opened=%v status=%q", name, opened, a.Status)
		}
	}
}
