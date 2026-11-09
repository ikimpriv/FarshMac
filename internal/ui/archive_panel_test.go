package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"farshmac/internal/fsops"
)

func settlePanels(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.Poll()
		if !a.Busy && a.Panels[0].load == nil && a.Panels[1].load == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("UI did not settle", a.Status)
}
func TestArchivePanelAsyncNavigationCopyMarksAndReadOnly(t *testing.T) {
	root := t.TempDir()
	right := filepath.Join(root, "right")
	src := filepath.Join(root, "source")
	os.Mkdir(right, 0700)
	os.MkdirAll(filepath.Join(src, "folder"), 0700)
	os.WriteFile(filepath.Join(src, "folder", "first.txt"), []byte("one"), 0600)
	os.WriteFile(filepath.Join(src, "folder", "second.txt"), []byte("two"), 0600)
	os.WriteFile(filepath.Join(src, "outside.txt"), []byte("skip"), 0600)
	arc := filepath.Join(root, "test.zip")
	if err := fsops.CreateArchive([]string{src}, arc); err != nil {
		t.Fatal(err)
	}
	a, err := NewAsync(root, right)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	settlePanels(t, a)
	p := &a.Panels[0]
	selectEntry(t, p, "test.zip")
	original := p.snapshot()
	a.key(Message{Key: "Enter"})
	settlePanels(t, a)
	a.calculateFolderSizes(true)
	settlePanels(t, a)
	if r, ok := p.folderSizes["source"]; !ok || r.Bytes != 10 {
		t.Fatal("archive folder size", p.folderSizes, a.Status)
	}
	selectEntry(t, p, "source")
	a.key(Message{Key: "Enter"})
	settlePanels(t, a)
	selectEntry(t, p, "folder")
	a.key(Message{Key: "Enter"})
	settlePanels(t, a)
	p.Marked["first.txt"] = true
	p.Marked["second.txt"] = true
	a.key(Message{Key: "F5"})
	if a.Form == nil || a.Form.Fields[0].Value != right {
		t.Fatal("marked-copy dialog", a.Status)
	}
	a.key(Message{Key: "Enter"})
	settlePanels(t, a)
	for name, want := range map[string]string{"first.txt": "one", "second.txt": "two"} {
		got, e := os.ReadFile(filepath.Join(right, name))
		if e != nil || string(got) != want {
			t.Fatal(name, string(got), e, a.Status)
		}
	}
	if _, err = os.Stat(filepath.Join(right, "outside.txt")); !os.IsNotExist(err) {
		t.Fatal("copied unselected file")
	}
	for _, key := range []string{"F4", "F6", "F7", "F8"} {
		a.key(Message{Key: key})
		if a.Dialog != nil || a.Form != nil || a.Busy {
			t.Fatal("archive modification allowed", key)
		}
	}
	selectEntry(t, p, "..")
	a.key(Message{Key: "Enter"})
	settlePanels(t, a)
	if filepath.Base(p.Current()) != "folder" {
		t.Fatal("inner cursor not restored", p.Current())
	}
	selectEntry(t, p, "..")
	a.key(Message{Key: "Enter"})
	settlePanels(t, a)
	if filepath.Base(p.Current()) != "source" {
		t.Fatal("archive cursor not restored")
	}
	selectEntry(t, p, "..")
	a.key(Message{Key: "Enter"})
	settlePanels(t, a)
	if p.Archive != nil || p.Path != root || p.Entries[p.Cursor].Name != original.CursorName {
		t.Fatal("physical parent state lost")
	}
}
func TestPanelHotkeysAndArchiveSync(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	a, _ := New(root, other)
	defer a.Close()
	a.key(Message{Key: "F1", Meta: true})
	if !a.Panels[0].Hidden || a.Active != 1 {
		t.Fatal("CmdF1")
	}
	a.key(Message{Key: "F1", Ctrl: true})
	if a.Panels[0].Hidden {
		t.Fatal("CtrlF1 alias")
	}
	a.key(Message{Key: "F2", Meta: true})
	if !a.Panels[1].Hidden {
		t.Fatal("CmdF2")
	}
	a.key(Message{Key: "F2", Alt: true})
	if a.Form == nil || a.Form.Title != "Volumes and home" {
		t.Fatal("AltF2")
	}
	a.Form = nil
	a.Active = 0
	a.key(Message{Key: "=", Meta: true})
	if a.Panels[1].Path != root || a.Panels[1].Hidden {
		t.Fatal("same folder")
	}
	src := filepath.Join(root, "test.txt")
	os.WriteFile(src, []byte("data"), 0600)
	arc := filepath.Join(root, "test.zip")
	fsops.CreateArchive([]string{src}, arc)
	a.refresh()
	selectEntry(t, &a.Panels[0], "test.zip")
	a.key(Message{Key: "Enter"})
	settlePanels(t, a)
	selectEntry(t, &a.Panels[0], "test.txt")
	a.key(Message{Key: "|", Meta: true, Shift: true})
	settlePanels(t, a)
	if a.Panels[1].Archive != a.Panels[0].Archive || a.Panels[1].Current() != a.Panels[0].Current() {
		t.Fatal("archive sync failed")
	}
	selectEntry(t, &a.Panels[0], "..")
	a.key(Message{Key: "Enter"})
	if a.Panels[0].Archive != nil || a.Panels[1].Archive == nil {
		t.Fatal("shared archive navigation changed other panel")
	}
}
func TestCancelOpeningArchiveLeavesPanel(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a")
	os.WriteFile(src, []byte("data"), 0600)
	arc := filepath.Join(root, "test.zip")
	fsops.CreateArchive([]string{src}, arc)
	a, _ := New(root, root)
	defer a.Close()
	selectEntry(t, &a.Panels[0], "test.zip")
	a.key(Message{Key: "Enter"})
	a.key(Message{Key: "Escape"})
	settlePanels(t, a)
	if a.Panels[0].Archive != nil || a.Panels[0].Current() != arc {
		t.Fatal("cancel changed panel")
	}
}
