package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"farshmac/internal/fsops"
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

func gridLine(g *Grid, y int) string {
	var b strings.Builder
	for x := 0; x < g.W; x++ {
		b.WriteString(g.Cells[y*g.W+x].Text)
	}
	return b.String()
}

func TestKeyBarLabelsForArchivePanel(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source")
	if err := os.MkdirAll(src, 0700); err != nil {
		t.Fatal(err)
	}
	arc := filepath.Join(root, "test.zip")
	if err := fsops.CreateArchive([]string{src}, arc); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Settings.ShowKeys = true
	bar := gridLine(a.Draw(), a.Rows-1)
	if !strings.Contains(bar, "RenMov") || !strings.Contains(bar, "Trash") {
		t.Fatal("regular key bar", bar)
	}
	selectEntry(t, &a.Panels[0], "test.zip")
	a.key(Message{Key: "Enter"})
	settleJobs(a)
	if a.Panels[0].Archive == nil {
		t.Fatal("archive not opened", a.Status)
	}
	bar = gridLine(a.Draw(), a.Rows-1)
	for _, gone := range []string{"Edit", "RenMov", "MkFold", "Trash", "Copy"} {
		if strings.Contains(bar, gone) {
			t.Fatalf("archive key bar shows %s: %q", gone, bar)
		}
	}
	if !strings.Contains(bar, "View") || !strings.Contains(bar, "Extract") {
		t.Fatalf("archive key bar %q", bar)
	}
	a.function(8)
	if !strings.Contains(a.Status, "read-only") {
		t.Fatal("F8 click must still explain", a.Status)
	}
}

func TestSortMenuShowsState(t *testing.T) {
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.sortMenu(0)
	if a.Form.Choice[0] != "✓ Name" || a.Form.Choice[6] != "Mix directories and files" || a.Form.Choice[4] != "Reverse order" {
		t.Fatal(a.Form.Choice)
	}
	a.Form.Select(6)
	a.Form = nil
	a.Panels[0].Reverse = true
	a.Panels[0].Sort = "Size"
	a.sortMenu(0)
	c := a.Form.Choice
	if c[0] != "Name" || c[2] != "✓ Size" || c[4] != "✓ Reverse order" || c[6] != "✓ Mix directories and files" || c[5] != "Case sensitive" || c[7] != "Selected first" {
		t.Fatal(c)
	}
	if !a.Panels[0].MixDirectories {
		t.Fatal("Mix directories item must toggle MixDirectories")
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
