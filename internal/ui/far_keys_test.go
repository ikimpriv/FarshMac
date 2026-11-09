package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFarQuickViewToggleAndPanelKeys(t *testing.T) {
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Panels[1].View = "Wide"
	a.Panels[1].Hidden = true
	path := a.Panels[1].Path
	a.key(Message{Key: "q", Ctrl: true})
	if a.Panels[1].View != "Quick view" || a.Panels[1].Hidden || a.Active != 0 || a.Quit {
		t.Fatal("Ctrl Q did not open inactive preview")
	}
	a.key(Message{Key: "q", Ctrl: true})
	if a.Panels[1].View != "Wide" || !a.Panels[1].Hidden || a.Panels[1].Path != path {
		t.Fatal("original panel not restored")
	}
	a.key(Message{Key: "F9", Alt: true})
	if !a.Zoom {
		t.Fatal("Alt F9")
	}
	a.Zoom = false
	a.key(Message{Key: "p", Ctrl: true})
	if a.Panels[1].Hidden {
		t.Fatal("Ctrl P")
	}
	a.key(Message{Key: "b", Ctrl: true})
	if a.Settings.ShowKeys {
		t.Fatal("Ctrl B")
	}
	a.key(Message{Key: "o", Ctrl: true})
	if !a.TermVisible {
		t.Fatal("Ctrl O")
	}
	a.key(Message{Key: "o", Ctrl: true})
	if a.TermVisible {
		t.Fatal("Ctrl O return")
	}
	a.key(Message{Key: "ArrowLeft", Ctrl: true})
	_, w := a.panelRect(0)
	if w >= a.Cols/2 {
		t.Fatal("resize width")
	}
	a.key(Message{Code: "Numpad5", Key: "5", Ctrl: true})
	_, w = a.panelRect(0)
	if w != a.Cols/2 {
		t.Fatal("reset width")
	}
}
func TestFarSortRangeAndCurrentCopy(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.go", "c.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := New(root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	p := &a.Panels[0]
	a.key(Message{Key: "F4", Ctrl: true})
	if p.Sort != "Extension" || a.Doc != nil {
		t.Fatal("Ctrl F4 opened editor")
	}
	a.key(Message{Key: "F4", Ctrl: true})
	if !p.Reverse {
		t.Fatal("repeat sort direction")
	}
	a.key(Message{Key: "F3", Ctrl: true})
	selectEntry(t, p, "a.txt")
	a.key(Message{Key: "ArrowDown", Shift: true})
	a.key(Message{Key: "ArrowDown", Shift: true})
	if !p.Marked["a.txt"] || !p.Marked["b.go"] || p.Marked[".."] {
		t.Fatal(p.Marked)
	}
	a.key(Message{Key: "F5", Shift: true})
	if a.Form == nil || !strings.Contains(a.Form.Fields[0].Label, "1 item") {
		t.Fatal("Shift F5 ignored marks incorrectly")
	}
	a.key(Message{Key: "Escape"})
	if !p.Marked["a.txt"] {
		t.Fatal("Shift F5 erased selection")
	}
	a.key(Message{Key: "Enter", Ctrl: true})
	if !strings.Contains(a.Command, "c.txt") {
		t.Fatal("Ctrl Enter", a.Command)
	}
	a.Command = ""
	a.key(Message{Key: "Insert", Ctrl: true})
	if a.Clipboard == nil || !strings.Contains(*a.Clipboard, "a.txt") {
		t.Fatal("copy names")
	}
}

func TestFarQuickViewRestoresNestedViewsAndDetailedLayout(t *testing.T) {
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Panels[0].View = "Detailed"
	a.Panels[1].View = "Brief"
	a.key(Message{Key: "l", Ctrl: true})
	a.key(Message{Key: "q", Ctrl: true})
	_, w := a.panelRect(1)
	if w == 0 {
		t.Fatal("inactive preview hidden by detailed panel")
	}
	a.key(Message{Key: "q", Ctrl: true})
	if a.Panels[1].View != "Info panel" {
		t.Fatal("lost prior Info view")
	}
	a.key(Message{Key: "l", Ctrl: true})
	if a.Panels[1].View != "Brief" {
		t.Fatal("lost original file view")
	}
}
