package ui

import (
	"errors"
	"fmt"
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
			// "Confirm deletion" off: Trash proceeds at once, permanent deletion still asks.
			if err := os.WriteFile(path, []byte("again"), 0600); err != nil {
				t.Fatal(err)
			}
			_ = os.Remove(filepath.Join(bin, "item"))
			a.Settings.ConfirmDelete = false
			a.refresh()
			selectEntry(t, &a.Panels[0], "item")
			a.key(tc.key)
			if asked := a.Form != nil && a.Form.Title == "Delete permanently"; asked != tc.permanent {
				t.Fatalf("ConfirmDelete=false: permanent=%v asked=%v", tc.permanent, asked)
			}
			if tc.permanent {
				a.key(Message{Key: "Escape"})
			}
			waitWork(t, a)
			_, err = os.Stat(path)
			if tc.permanent != (err == nil) {
				t.Fatal("unexpected deletion state", err)
			}
		})
	}
}

func TestDeleteConfirmationSummarisesManyItems(t *testing.T) {
	root := t.TempDir()
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	marked := map[string]bool{}
	for i := 0; i < 1204; i++ {
		name := fmt.Sprintf("f%04d", i)
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
		marked[name] = true
	}
	if err := os.WriteFile(filepath.Join(root, "odd\rname"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	a.refresh()
	a.Panels[0].Marked = marked
	a.key(Message{Key: "F8", Shift: true})
	if a.Form == nil || a.Form.Message != "Permanently delete 1,204 items?" {
		t.Fatalf("message %+v", a.Form)
	}
	a.key(Message{Key: "Escape"})
	a.Panels[0].Marked = map[string]bool{}
	selectEntry(t, &a.Panels[0], "odd\rname")
	a.key(Message{Key: "F8"})
	if a.Form == nil || a.Form.Message != `Move odd\rname to Trash?` {
		t.Fatalf("message %+v", a.Form)
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
		if !cancel {
			waitOperationQuestion(t, a, "Error")
			if !strings.Contains(a.Form.Message, "Trash unavailable") {
				t.Fatal(a.Form.Message)
			}
			a.key(Message{Key: "Escape"})
		}
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

		}
	}
}
