package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFastFindLiteralBracket(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Makefile", "[Unreleased].md", `\backslash`} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	p := &a.Panels[0]
	a.key(Message{Key: "[", Alt: true})
	a.key(Message{Key: "u"})
	if p.Entries[p.Cursor].Name != "[Unreleased].md" || a.FastFind.Query != "[u" {
		t.Fatal("bracket query", p.Entries[p.Cursor].Name, a.FastFind.Query)
	}
	a.key(Message{Key: "Escape"})
	a.key(Message{Key: `\`, Alt: true})
	if p.Entries[p.Cursor].Name != `\backslash` {
		t.Fatal("backslash query", p.Entries[p.Cursor].Name)
	}
	if !fastMatch("Makefile", "m*f") || !fastMatch("Makefile", "?a") {
		t.Fatal("wildcards must keep working")
	}
}

func TestFastFindMatchesNFDNames(t *testing.T) {
	root := t.TempDir()
	nfd := "café notes.txt"
	if err := os.WriteFile(filepath.Join(root, nfd), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cafeteria"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	p := &a.Panels[0]
	a.fastFindText("café")
	if p.Entries[p.Cursor].Name == "cafeteria" || a.FastFind.Query != "café" {
		t.Fatal("precomposed query must match decomposed name", p.Entries[p.Cursor].Name, a.FastFind.Query)
	}
}
