package ui

import (
	"farshmac/internal/remotefs"
	"os"
	"strings"
	"testing"
)

func TestControlFolderShortcuts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	left, right := t.TempDir(), t.TempDir()
	a, err := New(left, right)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for n := 1; n <= 9; n++ {
		key := string(rune('0' + n))
		a.key(Message{Key: "!", Code: "Digit" + key, Ctrl: true, Shift: true})
		if a.Settings.Shortcuts[n] != left {
			t.Fatal("shifted digit not saved", n)
		}
	}
	b, err := New(right, right)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.LoadSetup()
	b.key(Message{Key: "3", Code: "Digit3", Ctrl: true})
	if b.Panels[0].Path != left {
		t.Fatal("shortcut not restored", b.Panels[0].Path)
	}
	b.Active = 1
	b.key(Message{Key: "9", Ctrl: true})
	if b.Panels[1].Path != left {
		t.Fatal("wrong active panel")
	}
	b.Settings.Shortcuts[4] = ""
	b.key(Message{Key: "4", Ctrl: true})
	if !strings.Contains(b.Status, "empty") {
		t.Fatal("empty slot")
	}
	a.Panels[0].Remote = &remotefs.Config{Protocol: "sftp", Host: "test", Port: 22, User: "demo", Password: "TOP_SECRET"}
	a.Panels[0].Path = "/remote/folder"
	a.key(Message{Key: "@", Code: "Digit2", Ctrl: true, Shift: true})
	raw, _ := os.ReadFile(setupPath())
	if strings.Contains(string(raw), "TOP_SECRET") {
		t.Fatal("password persisted")
	}
	b.LoadSetup()
	b.key(Message{Key: "2", Ctrl: true})
	if b.Form == nil || b.Form.Fields[0].Value != "sftp://test:22/remote/folder" {
		t.Fatal("remote shortcut lost")
	}
}
