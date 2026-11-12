package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestTreeEnterOnAppBundleReroots(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "Foo.app")
	if err := os.MkdirAll(filepath.Join(bundle, "Contents"), 0700); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	launched := false
	a.runExternal = func(context.Context, []string) ([]byte, error) {
		launched = true
		return nil, nil
	}
	a.setView(0, "Tree panel")
	p := &a.Panels[0]
	waitTree(t, p)
	selectEntry(t, p, "Foo.app")
	a.key(Message{Key: "Enter"})
	waitTree(t, p)
	settleJobs(a)
	if launched || p.Path != bundle {
		t.Fatal("Enter in tree must re-root at the bundle", launched, p.Path)
	}
}

func TestControlPageDownOnlyEntersRealArchives(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.gz"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	launched := false
	a.runExternal = func(context.Context, []string) ([]byte, error) {
		launched = true
		return nil, nil
	}
	selectEntry(t, &a.Panels[0], "file.gz")
	a.key(Message{Key: "PageDown", Ctrl: true})
	settleJobs(a)
	if launched || a.Panels[0].Archive != nil || a.Busy || a.Doc != nil {
		t.Fatal("Ctrl+PgDn on a plain .gz must do nothing", launched)
	}
}

func settleJobs(a *App) {
	for i := 0; i < 20; i++ {
		a.Poll()
		time.Sleep(time.Millisecond)
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
