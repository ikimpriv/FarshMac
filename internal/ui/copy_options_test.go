package ui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"farshmac/internal/fsops"
)

func TestCopyOptionsDialogAndPersistence(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	src := filepath.Join(left, "file")
	if err := os.WriteFile(src, []byte("contents"), 0701); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1500000000, 0)
	if err := os.Chtimes(src, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	a, err := New(left, right)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	selectEntry(t, &a.Panels[0], "file")
	a.key(Message{Key: "F5"})
	if a.Form == nil || len(a.Form.Fields) != 5 || a.Form.Primary != "Copy" || !a.Form.Selected {
		t.Fatal("missing copy form")
	}
	if !a.Form.Fields[1].Checked || !a.Form.Fields[2].Checked {
		t.Fatal("preservation defaults")
	}
	// Tab/Space and mouse both operate independently, without changing the path.
	a.key(Message{Key: "Tab"})
	a.key(Message{Key: " "})
	pos := a.formGeometry().Fields[2]
	a.formClick(Message{X: pos.X + 2, Y: pos.Y})
	if a.Form.Fields[1].Checked || a.Form.Fields[2].Checked {
		t.Fatal("checkbox input")
	}
	a.key(Message{Key: "Enter"})
	waitWork(t, a)
	dst := filepath.Join(right, "file")
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(a.Status, err)
	}
	if info.Mode().Perm() == 0701 || info.ModTime().Equal(stamp) {
		t.Fatal("dialog options not applied")
	}
	if data, _ := os.ReadFile(src); string(data) != "contents" {
		t.Fatal("source changed")
	}
	a.key(Message{Key: "F5"})
	if a.Form.Fields[1].Checked || a.Form.Fields[2].Checked {
		t.Fatal("choices not remembered")
	}
	a.key(Message{Key: "Escape"})
	data, err := json.Marshal(savedSetup{Settings: a.Settings})
	if err != nil {
		t.Fatal(err)
	}
	saved := savedSetup{Settings: defaultSettings()}
	if err := json.Unmarshal(data, &saved); err != nil || saved.Settings.CopyPermissions || saved.Settings.CopyAttributes {
		t.Fatal("choices not persisted", err)
	}
	// Default selection replaces the destination when typing; Cmd+A selects
	// without erasing it until a replacement or deletion is requested.
	a.key(Message{Key: "F5"})
	a.key(Message{Key: "a", Meta: true})
	if a.Form.Fields[0].Value != dst {
		t.Fatal("select all erased destination")
	}
	a.formPaste(filepath.Join(right, "alternate"))
	if a.Form.Fields[0].Value != filepath.Join(right, "alternate") {
		t.Fatal("paste did not replace selection")
	}
	a.key(Message{Key: "Escape"})
}

func TestCopyMetadataQuestionsAllAndCancel(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		a, err := New(t.TempDir(), t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		done := false
		a.backgroundTask("Copy", func(o *fsops.Operation) (func(), func(), error) {
			a.metadataQuestions(o, false)
			for _, name := range []string{"one", "two"} {
				if err := o.MetadataFailure(name, "destination", errors.New("permissions unsupported")); err != nil {
					return nil, nil, err
				}
			}
			return func() { done = true }, nil, nil
		})
		deadline := time.Now().Add(3 * time.Second)
		for a.Form == nil && time.Now().Before(deadline) {
			a.Poll()
			time.Sleep(time.Millisecond)
		}
		if a.Form == nil || a.Form.Choice[0] != "Copy anyway" || strings.Contains(a.Form.Message, "remove") {
			t.Fatal("wrong copy question")
		}
		if cancel {
			a.key(Message{Key: "Escape"})
		} else {
			a.Form.Focus = 1
			a.key(Message{Key: "Enter"})
		}
		waitWork(t, a)
		if done == cancel || a.Form != nil {
			t.Fatal("all/cancel handling", a.Status)
		}
	}
}
