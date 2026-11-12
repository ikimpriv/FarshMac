package ui

import "testing"

func TestMetaShortcutsMapCodeForNonLatinKey(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	a, err := New(left, right)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Handle(`{"Type":"key","Key":"г","Code":"KeyU","Meta":true}`)
	if a.Panels[0].Path != right || a.Panels[1].Path != left {
		t.Fatal("Cmd+U in a Cyrillic layout did not swap panels")
	}
	a.Handle(`{"Type":"key","Key":"щ","Code":"KeyO","Ctrl":true}`)
	if !a.TermVisible {
		t.Fatal("Ctrl+O in a Cyrillic layout did not show the terminal")
	}
	if m := latinShortcut(Message{Key: "r", Code: "KeyO", Meta: true}); m.Key != "r" {
		t.Fatal("Latin layouts (Dvorak) must keep e.key", m.Key)
	}
	if m := latinShortcut(Message{Key: "щ", Code: "KeyO"}); m.Key != "щ" {
		t.Fatal("plain typing must keep the layout character")
	}
	if m := latinShortcut(Message{Key: "Щ", Code: "KeyO", Meta: true, Shift: true}); m.Key != "O" {
		t.Fatal("shifted shortcut", m.Key)
	}
}
