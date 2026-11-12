package ui

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
	if a.Form.Choice[0] != "✓ Name" || a.Form.Choice[6] != "  Mix directories and files" || a.Form.Choice[4] != "  Reverse order" {
		t.Fatal(a.Form.Choice)
	}
	a.Form.Select(6)
	a.Form = nil
	a.Panels[0].Reverse = true
	a.Panels[0].Sort = "Size"
	a.sortMenu(0)
	c := a.Form.Choice
	if c[0] != "  Name" || c[2] != "✓ Size" || c[4] != "✓ Reverse order" || c[6] != "✓ Mix directories and files" || c[5] != "  Case sensitive" || c[7] != "  Selected first" {
		t.Fatal(c)
	}
	if !a.Panels[0].MixDirectories {
		t.Fatal("Mix directories item must toggle MixDirectories")
	}
}

func panelFooter(a *App, index int) string {
	g := a.Draw()
	x, w := a.panelRect(index)
	line := []rune(gridLine(g, a.Rows-5))
	return strings.TrimSpace(string(line[x+1 : x+w-1]))
}

func TestFolderSizeVisibleInBriefFooter(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dir", "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dir", "sub", "f"), []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Panels[0].View = "Brief"
	selectEntry(t, &a.Panels[0], "dir")
	a.key(Message{Key: "F3"})
	waitWork(t, a)
	if !strings.HasSuffix(a.Status, " · dir: 5 bytes (1 files, 2 dirs)") {
		t.Fatalf("status %q", a.Status)
	}
	if footer := panelFooter(a, 0); footer != "5 bytes in dir" {
		t.Fatalf("footer %q", footer)
	}
	selectEntry(t, &a.Panels[0], "..")
	if footer := panelFooter(a, 0); strings.Contains(footer, " in dir") {
		t.Fatalf("footer for an unmeasured entry %q", footer)
	}
}

func TestTreePanelFolderSizeInFooter(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "b", "f"), []byte("123"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.setView(0, "Tree panel")
	p := &a.Panels[0]
	waitTree(t, p)
	selectEntry(t, p, filepath.Join("a", "b"))
	a.key(Message{Key: "F3"})
	waitWork(t, a)
	if footer := panelFooter(a, 0); footer != "3 bytes in a/b" {
		t.Fatalf("footer %q (status %q)", footer, a.Status)
	}
}

func TestFolderSizeCountsPrivacyDenials(t *testing.T) {
	walk := func(root string, o *fsops.Operation, visit fs.WalkDirFunc) error {
		visit(root, nil, &fs.PathError{Op: "open", Path: root, Err: syscall.EPERM})
		return visit(root+"/x", nil, &fs.PathError{Op: "open", Path: root + "/x", Err: syscall.EACCES})
	}
	r, err := measureFolder("/x", fsops.NewOperation("test"), walk)
	if err != nil || r.Unreadable != 2 || r.privacyDenied != 1 {
		t.Fatal(r, err)
	}
}

func TestInvalidPanelFilterShowsAllAndWarns(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.go", "b.txt"} {
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
	p.Filter = "<src>"
	a.configurePanel(0)
	a.error(p.Load(root))
	if len(p.Entries) != 3 {
		t.Fatal("invalid filter must show all files", p.Entries)
	}
	if a.Status != invalidFilterStatus {
		t.Fatalf("status %q", a.Status)
	}
	if footer := panelFooter(a, 0); footer != "[<src>] invalid filter — showing all" {
		t.Fatalf("footer %q", footer)
	}
	a.Settings.MaskGroups["src"] = "*.go"
	a.configurePanel(0)
	a.error(p.Load(root))
	if len(p.Entries) != 2 || !strings.HasPrefix(panelFooter(a, 0), "[<src>] ") {
		t.Fatal("valid filter not applied", p.Entries, panelFooter(a, 0))
	}
}

func TestDeleteMaskGroupInUseAsksConfirmation(t *testing.T) {
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Settings.MaskGroups = map[string]string{"src": "*.go"}
	a.Panels[1].Filter = "<src>"
	a.configurePanel(1)
	edit := func(name, masks string) {
		a.settingsMenu("Groups of file masks")
		f := a.Form
		a.Form = nil
		f.Select(1)
		f = a.Form
		f.Fields[0].Value, f.Fields[1].Value = name, masks
		a.Form = nil
		f.Submit(f.Fields)
	}
	edit("src", "")
	if a.Form == nil || a.Form.Message != "Group src is used by a panel filter. Delete anyway?" {
		t.Fatal("no confirmation", a.Form)
	}
	a.Form.Select(1) // Cancel
	if a.Settings.MaskGroups["src"] != "*.go" {
		t.Fatal("cancel deleted the group")
	}
	edit("renamed", "*.go")
	if a.Form == nil {
		t.Fatal("rename of a used group must confirm")
	}
	a.Form.Select(0) // Delete
	if _, ok := a.Settings.MaskGroups["src"]; ok || a.Settings.MaskGroups["renamed"] != "*.go" {
		t.Fatal("confirmed rename not applied", a.Settings.MaskGroups)
	}
	a.Panels[1].Filter = ""
	edit("renamed", "")
	if a.Form != nil || len(a.Settings.MaskGroups) != 0 {
		t.Fatal("unused group must be deleted without confirmation", a.Settings.MaskGroups)
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
