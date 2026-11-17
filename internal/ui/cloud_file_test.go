package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCloudPlaceholderOpensContentNotStub(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dir, "note.txt")
	stub := filepath.Join(dir, ".note.txt.icloud")
	if err := os.WriteFile(real, []byte("actual document"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stub, []byte("placeholder metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	p := &a.Panels[0]
	p.Entries = []Entry{{Name: filepath.Base(stub)}}
	p.Cursor = 0
	for _, edit := range []bool{false, true} {
		a.openConfigured(edit, false, true)
		waitNetwork(t, a)
		if a.Doc == nil || a.Doc.Path != canonical || a.Doc.Edit != edit || string(a.Doc.Raw) != "actual document" {
			t.Fatalf("edit=%v: opened stub or wrong mode: %+v, status %s", edit, a.Doc, a.Status)
		}
		a.Doc = nil
	}
}
