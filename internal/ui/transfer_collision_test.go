package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultTransferMergesExistingFolder(t *testing.T) {
	for _, key := range []string{"F5", "F6"} {
		t.Run(key, func(t *testing.T) {
			left, right := t.TempDir(), t.TempDir()
			for _, p := range []string{left, right} {
				if e := os.Mkdir(filepath.Join(p, "folder"), 0700); e != nil {
					t.Fatal(e)
				}
			}
			for p, data := range map[string]string{filepath.Join(left, "folder", "file"): "new", filepath.Join(right, "folder", "file"): "old", filepath.Join(right, "folder", "keep"): "keep"} {
				if e := os.WriteFile(p, []byte(data), 0600); e != nil {
					t.Fatal(e)
				}
			}
			a, e := New(left, right)
			if e != nil {
				t.Fatal(e)
			}
			defer a.Close()
			a.Settings.ConfirmOverwrite = false
			selectEntry(t, &a.Panels[0], "folder")
			a.key(Message{Key: key})
			a.key(Message{Key: "Enter"})
			waitWork(t, a)
			for name, want := range map[string]string{"file": "new", "keep": "keep"} {
				b, e := os.ReadFile(filepath.Join(right, "folder", name))
				if e != nil || string(b) != want {
					t.Fatalf("%s: %q %v status %s", name, b, e, a.Status)
				}
			}
			if _, e := os.Stat(filepath.Join(right, "folder", "folder")); !os.IsNotExist(e) {
				t.Fatal("unexpected nested folder")
			}
			_, e = os.Stat(filepath.Join(left, "folder"))
			if key == "F6" && !os.IsNotExist(e) {
				t.Fatal("source not removed", e)
			}
			if key == "F5" && e != nil {
				t.Fatal("source lost", e)
			}
		})
	}
}

func TestLocalTransferPermissionErrorStaysVisible(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged user")
	}
	for _, key := range []string{"F5", "F6"} {
		t.Run(key, func(t *testing.T) {
			left, right := t.TempDir(), t.TempDir()
			src := filepath.Join(left, "file")
			if e := os.WriteFile(src, []byte("source"), 0600); e != nil {
				t.Fatal(e)
			}
			a, e := New(left, right)
			if e != nil {
				t.Fatal(e)
			}
			defer a.Close()
			if e := os.Chmod(right, 0500); e != nil {
				t.Fatal(e)
			}
			defer os.Chmod(right, 0700)
			selectEntry(t, &a.Panels[0], "file")
			a.key(Message{Key: key})
			a.key(Message{Key: "Enter"})
			waitOperationQuestion(t, a, "Error")
			if a.Form == nil || !strings.Contains(strings.ToLower(a.Form.Message), "permission denied") {
				t.Fatalf("permission error not visible: status=%q form=%+v", a.Status, a.Form)
			}
			a.key(Message{Key: "Escape"})
			waitWork(t, a)
			if b, e := os.ReadFile(src); e != nil || string(b) != "source" {
				t.Fatal("source lost", e)
			}
			if _, e := os.Stat(filepath.Join(right, "file")); !os.IsNotExist(e) {
				t.Fatal("unexpected output", e)
			}
		})
	}
}
