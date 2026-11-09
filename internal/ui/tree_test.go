package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func waitTree(t *testing.T, p *Panel) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for p.tree != nil {
		p.pollTree()
		if time.Now().After(deadline) {
			t.Fatal("tree scan did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func treeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"a/b/c", "a/d", "z", ".hidden/child", "line\nbreak", "tab\tname"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func scanOptions() treeScanOptions {
	p := Panel{TreeDepth: 4, HideHidden: true}
	opts := p.treeOptions()
	opts.check = func() error { return nil }
	return opts
}

func TestTreeScanHierarchyDepthAndSafeNames(t *testing.T) {
	root := treeFixture(t)
	opts := scanOptions()
	opts.depth = 2
	var readPaths []string
	opts.progress = func(path string, _ int) { readPaths = append(readPaths, path) }
	r := scanTree(root, opts)
	var names []string
	for _, entry := range r.entries {
		names = append(names, entry.Name)
	}
	want := []string{"a", "a/b", "a/d", "line\nbreak", "tab\tname", "z"}
	if r.err != nil || !reflect.DeepEqual(names, want) {
		t.Fatalf("entries=%q err=%v", names, r.err)
	}
	for _, path := range readPaths {
		if path == filepath.Join(root, "a/b") {
			t.Fatal("read descendants beyond the depth boundary")
		}
	}
	labels := treeLabels(r.entries)
	if labels["a"] != "├─a" || labels["a/b"] != "│ ├─b" || labels["a/d"] != "│ └─d" || labels["z"] != "└─z" {
		t.Fatal("broken branch hierarchy", labels)
	}
	if labels["line\nbreak"] != `├─line\nbreak` || labels["tab\tname"] != `├─tab\tname` {
		t.Fatal("ambiguous control characters", labels)
	}
	if got := treeDisplayName("x\x1b[2J\u202ey"); got != `x\u001b[2J\u202ey` {
		t.Fatal(got)
	}
}

func TestTreeWideFolderLimitsAndCancellation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 1800; i++ {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("dir-%04d", i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	opts := scanOptions()
	opts.nodeLimit = 300
	r := scanTree(root, opts)
	if !r.limited || len(r.entries) != 300 || !strings.Contains(r.status(opts.depth), "Partial") {
		t.Fatalf("silent node truncation: %d %s", len(r.entries), r.status(opts.depth))
	}
	opts = scanOptions()
	opts.entryLimit = 600
	r = scanTree(root, opts)
	if !r.limited || len(r.entries) > 600 {
		t.Fatal("entry budget exceeded", len(r.entries))
	}
	opts = scanOptions()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opts.check = ctx.Err
	visited := 0
	opts.progress = func(_ string, n int) { visited = n; cancel() }
	r = scanTree(root, opts)
	if !errors.Is(r.err, context.Canceled) || visited > 256 || len(r.entries) > 256 {
		t.Fatalf("cancel did not stop between batches: visited=%d err=%v", visited, r.err)
	}
}

func TestTreeFileHeavyFolder(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 6000; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%05d", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	opts := scanOptions()
	opts.depth = 1
	visited := 0
	opts.progress = func(_ string, n int) { visited = n }
	r := scanTree(root, opts)
	if r.err != nil || r.limited || len(r.entries) != 1 || r.entries[0].Name != "folder" || visited != 6001 {
		t.Fatalf("file-heavy tree: entries=%d visited=%d err=%v", len(r.entries), visited, r.err)
	}
}

func TestTreePendingDoesNotBlockUIOrOverwriteNewFolder(t *testing.T) {
	root := treeFixture(t)
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	p := &a.Panels[0]
	p.View = "Tree panel"
	opts := p.treeOptions()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var enteredOnce sync.Once
	opts.check = func() error { enteredOnce.Do(func() { close(entered) }); <-release; return nil }
	p.startTree(root, opts)
	old := p.tree
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	a.Handle(`{"Type":"resize","Cols":100,"Rows":28}`)
	a.Handle(`{"Type":"key","Key":"Tab"}`)
	if a.Poll().Grid == "" || a.Cols != 100 || a.Rows != 28 || a.Active != 1 {
		t.Fatal("pending filesystem scan blocked UI")
	}
	// Swap panel values while their workers run (Cmd+U does this).
	a.Panels[0], a.Panels[1] = a.Panels[1], a.Panels[0]
	p = &a.Panels[1]
	newRoot := filepath.Join(root, "a")
	if err := p.Load(newRoot); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	waitTree(t, p)
	select {
	case <-old.done:
	case <-time.After(time.Second):
		t.Fatal("superseded worker leaked")
	}
	if p.Path != newRoot || p.Current() == filepath.Join(root, "a/b/c") {
		t.Fatal("stale result applied")
	}
	for _, e := range p.Entries {
		if e.Name == "a" || strings.HasPrefix(e.Name, "a/") {
			t.Fatal("old root entries leaked", e.Name)
		}
	}
}

func TestTreeEscapeRefreshAndSessionCursor(t *testing.T) {
	root := treeFixture(t)
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Panels[0].View = "Tree panel"
	opts := a.Panels[0].treeOptions()
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	opts.check = func() error { <-release; return nil }
	a.Panels[0].startTree(root, opts)
	a.key(Message{Key: "Escape"})
	once.Do(func() { close(release) })
	waitTree(t, &a.Panels[0])
	if !strings.Contains(a.Panels[0].Err, "Stopped") {
		t.Fatal(a.Panels[0].Err)
	}
	a.refresh()
	waitTree(t, &a.Panels[0])
	selectEntry(t, &a.Panels[0], "a/b")
	a.Panels[0].Offset = a.Panels[0].Cursor
	g := NewWindowGroup(a)
	path := filepath.Join(t.TempDir(), "session.json")
	g.EnableSession(path, false)
	a.refresh()
	if err := g.SaveSession(true); err != nil {
		t.Fatal(err)
	}
	if got := readSession(t, path).Windows[0].Panels[0].Cursor; got != "a/b" {
		t.Fatal("refresh lost pending cursor", got)
	}
	g.CloseAll()
	g2 := sessionGroup(t, root, root)
	defer g2.CloseAll()
	if err := g2.EnableSession(path, true); err != nil {
		t.Fatal(err)
	}
	p := &g2.Apps[1].Panels[0]
	if p.tree == nil {
		t.Fatal("session restore performed a synchronous tree scan")
	}
	waitTree(t, p)
	if p.Current() != filepath.Join(root, "a/b") || p.Offset != p.Cursor {
		t.Fatal("saved cursor not restored", p.Current(), p.Offset, p.Cursor)
	}
}

func TestTreeMissingDirectory(t *testing.T) {
	p := Panel{TreeDepth: 4}
	p.View = "Tree panel"
	p.Load(filepath.Join(t.TempDir(), "missing"))
	waitTree(t, &p)
	if !strings.Contains(p.Err, "no such file") {
		t.Fatal("missing directory shown as complete", p.Err)
	}
}

func TestTreeF7FocusesCreatedDirectory(t *testing.T) {
	root := t.TempDir()
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Settings.ShowHidden = false
	a.refresh()
	a.setView(0, "Tree panel")
	for _, name := range []string{"new", "nested/child", ".hidden"} {
		a.Panels[0].Load(root)
		a.function(7)
		a.Dialog.Input = name
		a.key(Message{Key: "Enter"})
		waitTree(t, &a.Panels[0])
		want := filepath.Join(root, name)
		if a.Panels[0].Current() != want || a.Panels[0].Path != filepath.Dir(want) {
			t.Fatal("F7 lost pending tree selection", a.Panels[0].Current(), want)
		}
	}
}

func TestTreeMenuMatchesPanelAndDoesNotFollowLinks(t *testing.T) {
	root := treeFixture(t)
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.setView(0, "Tree panel")
	waitTree(t, &a.Panels[0])
	a.treeMenu()
	waitWork(t, a)
	if a.Form == nil || a.Form.Title != "Folder tree" {
		t.Fatal("no tree menu", a.Status)
	}
	if len(a.Form.Choice) != len(a.Panels[0].Entries) {
		t.Fatal("panel/menu differ", a.Form.Choice)
	}
	for i, e := range a.Panels[0].Entries[1:] {
		if a.Form.Choice[i+1] != a.Panels[0].treeLabels[e.Name] {
			t.Fatal("inconsistent branches", e.Name)
		}
	}
	// Rendering uses only visible rows, with branches clipped to their own panel.
	a.Form = nil
	for _, size := range [][2]int{{60, 15}, {300, 120}, {120, 36}} {
		a.Cols, a.Rows = size[0], size[1]
		grid := a.Draw()
		for y := 3; y < a.Rows-6; y++ {
			for _, x := range []int{0, a.Cols/2 - 1, a.Cols / 2, a.Cols - 1} {
				if grid.Cells[y*grid.W+x].Text != "║" {
					t.Fatalf("broken border at %d,%d in %v", x, y, size)
				}
			}
		}
	}
}
