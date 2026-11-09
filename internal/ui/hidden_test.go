package ui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"farshmac/internal/fsops"
)

func panelContains(p *Panel, name string) bool {
	for _, entry := range p.Entries {
		if entry.Name == name {
			return true
		}
	}
	return false
}

func TestHiddenSettingsAndHotkeyIndependent(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"file", ".hidden", ".DS_Store", "macHidden"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := syscall.Chflags(filepath.Join(root, "macHidden"), 0x8000); err != nil {
		t.Fatal(err)
	}
	a, err := NewAsync(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	settlePanels(t, a)
	a.settingsMenu("Panel settings")
	a.Form.Fields[0].Checked = true
	a.Form.Fields[3].Checked = true
	a.formKey(Message{Key: "Enter"})
	settlePanels(t, a)
	for i := range a.Panels {
		p := &a.Panels[i]
		if !panelContains(p, ".hidden") || panelContains(p, ".DS_Store") {
			t.Fatal("independent filter", i)
		}
		selectEntry(t, p, "file")
	}
	for _, key := range []Message{{Key: ">", Code: "Period", Meta: true, Shift: true}, {Key: ".", Meta: true, Shift: true}} {
		a.key(key)
		settlePanels(t, a)
		for i := range a.Panels {
			p := &a.Panels[i]
			if panelContains(p, ".hidden") != a.Settings.ShowHidden || panelContains(p, "macHidden") != a.Settings.ShowHidden || panelContains(p, ".DS_Store") || filepath.Base(p.Current()) != "file" {
				t.Fatal("toggle/cursor", i)
			}
		}
	}
	data, err := json.Marshal(savedSetup{Settings: a.Settings})
	if err != nil {
		t.Fatal(err)
	}
	var saved savedSetup
	if err := json.Unmarshal(data, &saved); err != nil || !saved.Settings.IgnoreDSStore || !saved.Settings.ShowHidden {
		t.Fatal("settings lost", err)
	}
	// Turning off just the DS_Store option makes it available again.
	a.Settings.IgnoreDSStore = false
	a.refresh()
	settlePanels(t, a)
	if !panelContains(&a.Panels[0], ".DS_Store") {
		t.Fatal("filter cannot be disabled")
	}
}

func TestHiddenFolderCopyAndWholeFolderMove(t *testing.T) {
	for _, move := range []bool{false, true} {
		root, dest := t.TempDir(), t.TempDir()
		src := filepath.Join(root, "tree")
		if err := os.Mkdir(src, 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{".hidden", ".DS_Store"} {
			if err := os.WriteFile(filepath.Join(src, name), []byte(name), 0600); err != nil {
				t.Fatal(err)
			}
		}
		original, _ := os.Stat(src)
		a, err := New(root, dest)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		a.Settings.ShowHidden = false
		a.Settings.IgnoreDSStore = true
		a.refresh()
		selectEntry(t, &a.Panels[0], "tree")
		a.transferDialog(move)
		a.key(Message{Key: "Enter"})
		waitWork(t, a)
		out := filepath.Join(dest, "tree")
		if _, err := os.Stat(filepath.Join(out, ".hidden")); err != nil {
			t.Fatal("hidden files excluded", a.Status, err)
		}
		_, err = os.Stat(filepath.Join(out, ".DS_Store"))
		if move {
			actual, _ := os.Stat(out)
			if err != nil || !os.SameFile(original, actual) {
				t.Fatal("move filtered or copied tree", err)
			}
		} else if !os.IsNotExist(err) {
			t.Fatal("copy did not ignore DS_Store", err)
		}
	}
}

func TestMoveMetadataQuestionAllAndEsc(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		a, err := New(t.TempDir(), t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		completed := false
		a.backgroundTask("Move", func(o *fsops.Operation) (func(), func(), error) {
			a.moveMetadataQuestions(o)
			for _, file := range []string{"one", "two"} {
				if err := o.MetadataFailure(file, "destination", errors.New("ACL unsupported")); err != nil {
					return nil, nil, err
				}
			}
			return func() { completed = true }, nil, nil
		})
		deadline := time.Now().Add(3 * time.Second)
		for a.Form == nil && time.Now().Before(deadline) {
			a.Poll()
			time.Sleep(time.Millisecond)
		}
		if a.Form == nil || a.Form.Title != "Cannot preserve metadata" {
			t.Fatal("missing metadata question")
		}
		if cancel {
			a.key(Message{Key: "Escape"})
		} else {
			a.Form.Focus = 1
			a.key(Message{Key: "Enter"})
		}
		waitWork(t, a)
		if completed == cancel || a.Form != nil {
			t.Fatal("all/cancel handling", completed, a.Status)
		}
	}
}

func TestArchiveHiddenTogglesKeepCatalog(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".DS_Store", ".hidden", "file"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(t.TempDir(), "test.zip")
	if err := fsops.CreateArchive([]string{root}, archive); err != nil {
		t.Fatal(err)
	}
	index, err := fsops.ReadArchiveIndex(archive, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := Panel{Archive: index}
	path := filepath.Join(archive, filepath.Base(root))
	for _, show := range []bool{false, true} {
		for _, ignore := range []bool{false, true} {
			p.HideHidden = !show
			p.IgnoreDSStore = ignore
			if err := p.Load(path); err != nil {
				t.Fatal(err)
			}
			if panelContains(&p, ".hidden") != show || panelContains(&p, ".DS_Store") != (show && !ignore) {
				t.Fatal("archive display filter")
			}
		}
	}
}
