package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"farshmac/internal/fsops"
)

func TestShiftEnterUsesSystemOpen(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"folder", "Example.app"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"file ' ; $(echo test).txt", "run.command", "archive.zip"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("test"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("folder", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"folder", "Example.app", "file ' ; $(echo test).txt", "run.command", "archive.zip", "link", ".."} {
		t.Run(name, func(t *testing.T) {
			a, err := New(root, root)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			a.Active = 1
			selectEntry(t, &a.Panels[1], name)
			a.Command = "echo keep this draft"
			a.Settings.Associations = []Association{{Mask: "*", Command: "wrong-command {}", Viewer: "wrong-viewer {}"}}
			a.Settings.ExternalViewerDefault = true
			a.FastFind = &FastFind{Query: name}
			var got []string
			a.runExternal = func(_ context.Context, args []string) ([]byte, error) {
				got = args
				return nil, nil
			}
			a.key(Message{Key: "Enter", Shift: true})
			waitWork(t, a)
			if !reflect.DeepEqual(got, []string{"/usr/bin/open", filepath.Join(root, name)}) {
				t.Fatalf("unexpected external invocation: %q", got)
			}
			if a.Command != "echo keep this draft" || a.TermVisible || a.Doc != nil || a.Panels[1].Path != root || a.Panels[1].Archive != nil || a.FastFind != nil {
				t.Fatal("system open changed panel, document, terminal or command draft")
			}
		})
	}
}

func TestSystemOpenReportsFailureAndRejectsVirtualPaths(t *testing.T) {
	root := t.TempDir()
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.runExternal = func(_ context.Context, _ []string) ([]byte, error) {
		return []byte("No application is registered"), errors.New("exit status 1")
	}
	a.key(Message{Key: "Enter", Shift: true})
	waitWork(t, a)
	if !strings.Contains(a.Status, "No application is registered") {
		t.Fatal("open failure not shown", a.Status)
	}
	a.Panels[0] = Panel{Path: filepath.Join(root, "archive.zip"), Archive: &fsops.ArchiveIndex{Path: filepath.Join(root, "archive.zip")}, Entries: []Entry{{Name: "inside.txt"}}}
	a.key(Message{Key: "Enter", Shift: true})
	if a.Busy || !strings.Contains(a.Status, "F5") {
		t.Fatal("attempted to open a nonexistent virtual archive path")
	}
	a.Panels[0].Entries = nil
	a.Status = "unchanged"
	a.key(Message{Key: "Enter", Shift: true})
	if a.Busy || a.Status != "unchanged" {
		t.Fatal("empty panel should do nothing")
	}
}
