package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"farshmac/internal/fsops"
)

func TestDeleteShortcuts(t *testing.T) {
	for _, tc := range []struct {
		name      string
		key       Message
		permanent bool
	}{
		{"F8", Message{Key: "F8"}, false},
		{"Delete", Message{Key: "Delete"}, false},
		{"Cmd8", Message{Key: "8", Meta: true}, false},
		{"ShiftF8", Message{Key: "F8", Shift: true}, true},
		{"ShiftDelete", Message{Key: "Delete", Shift: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, bin := t.TempDir(), t.TempDir()
			path := filepath.Join(root, "item")
			if err := os.WriteFile(path, []byte("preserve me"), 0600); err != nil {
				t.Fatal(err)
			}
			a, err := New(root, root)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			a.trashItem = func(path string, o *fsops.Operation) error {
				if tc.permanent {
					return errors.New("permanent shortcut called Trash")
				}
				return os.Rename(path, filepath.Join(bin, "item"))
			}
			selectEntry(t, &a.Panels[0], "item")
			a.key(tc.key)
			title := "Move to Trash"
			if tc.permanent {
				title = "Delete permanently"
			}
			if a.Form == nil || a.Form.Title != title {
				t.Fatalf("expected %s confirmation", title)
			}
			a.key(Message{Key: "Escape"})
			if _, err := os.Stat(path); err != nil {
				t.Fatal("cancel removed file", err)
			}
			a.key(tc.key)
			a.key(Message{Key: "Enter"})
			waitWork(t, a)
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("source still exists", a.Status)
			}
			b, err := os.ReadFile(filepath.Join(bin, "item"))
			if tc.permanent {
				if !os.IsNotExist(err) {
					t.Fatal("permanent deletion used Trash")
				}
			} else if err != nil || string(b) != "preserve me" {
				t.Fatal("Trash lost data", err)
			}
		})
	}
}

func TestTrashFailureNeverFallsBackAndCancelStopsBatch(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		root, bin := t.TempDir(), t.TempDir()
		for _, name := range []string{"a", "b"} {
			if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
				t.Fatal(err)
			}
		}
		a, err := New(root, root)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Close()
		a.Settings.ConfirmDelete = false
		a.Panels[0].Marked = map[string]bool{"a": true, "b": true}
		calls := 0
		a.trashItem = func(path string, o *fsops.Operation) error {
			calls++
			if !cancel {
				return errors.New("Trash unavailable")
			}
			if err := os.Rename(path, filepath.Join(bin, filepath.Base(path))); err != nil {
				return err
			}
			o.Cancel()
			return nil
		}
		a.key(Message{Key: "F8"})
		waitWork(t, a)
		if calls != 1 {
			t.Fatal("batch continued after failure/cancellation")
		}
		if _, err := os.Stat(filepath.Join(root, "b")); err != nil {
			t.Fatal("unprocessed file removed")
		}
		if !cancel {
			if _, err := os.Stat(filepath.Join(root, "a")); err != nil {
				t.Fatal("failed trash permanently removed file")
			}
			if !strings.Contains(a.Status, "Trash unavailable") {
				t.Fatal("failure hidden", a.Status)
			}
		}
	}
}
