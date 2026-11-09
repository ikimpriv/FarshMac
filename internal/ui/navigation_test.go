package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func selectEntry(t *testing.T, p *Panel, name string) {
	t.Helper()
	for i, e := range p.Entries {
		if e.Name == name {
			p.Cursor = i
			return
		}
	}
	t.Fatalf("missing %q", name)
}
func loadPanel(t *testing.T, p *Panel, path string) {
	t.Helper()
	if err := p.Load(path); err != nil {
		t.Fatal(err)
	}
}

func TestReturnRestoresEachPanelAndNestedDirectory(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 60; i++ {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("dir%02d", i)), 0755); err != nil {
			t.Fatal(err)
		}
	}
	child := filepath.Join(root, "dir40")
	if err := os.Mkdir(filepath.Join(child, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	p := &a.Panels[0]
	selectEntry(t, p, "dir40")
	p.Offset = 25
	p.Marked["dir05"] = true
	a.Panels[1].Cursor = 3
	a.Panels[1].Offset = 1
	a.key(Message{Key: "Enter"})
	selectEntry(t, p, "nested")
	p.Marked["nested"] = true
	a.key(Message{Key: "Enter"})
	selectEntry(t, p, "..")
	a.key(Message{Key: "Enter"})
	if filepath.Base(p.Current()) != "nested" || !p.Marked["nested"] {
		t.Fatal("nested return state lost")
	}
	selectEntry(t, p, "..")
	a.key(Message{Key: "Enter"})
	a.Draw()
	if p.Path != root || filepath.Base(p.Current()) != "dir40" || p.Offset != 25 || !p.Marked["dir05"] {
		t.Fatalf("parent state: %+v", p.snapshot())
	}
	if a.Panels[1].Cursor != 3 || a.Panels[1].Offset != 1 {
		t.Fatal("other panel state changed")
	}
	// A second round-trip via the parent entry must retain the same parent viewport.
	a.key(Message{Key: "Enter"})
	selectEntry(t, p, "..")
	a.key(Message{Key: "Enter"})
	a.Draw()
	if filepath.Base(p.Current()) != "dir40" || p.Offset != 25 {
		t.Fatal("parent entry did not restore parent")
	}
}

func TestReturnRefreshesEntriesAndClampsMissingSelection(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "middle")
	for _, name := range []string{"middle", "z-last"} {
		if err := os.Mkdir(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	p := Panel{}
	loadPanel(t, &p, root)
	selectEntry(t, &p, "middle")
	p.Offset = p.Cursor
	p.Marked["z-last"] = true
	loadPanel(t, &p, child)
	if err := os.Mkdir(filepath.Join(root, "a-new"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "z-last")); err != nil {
		t.Fatal(err)
	}
	loadPanel(t, &p, root)
	if filepath.Base(p.Current()) != "middle" || p.Entries[p.Offset].Name != "middle" || p.Marked["z-last"] {
		t.Fatal("state not reconciled with live entries")
	}
	selectEntry(t, &p, "a-new")
	loadPanel(t, &p, child)
	if err := os.Remove(filepath.Join(root, "a-new")); err != nil {
		t.Fatal(err)
	}
	loadPanel(t, &p, root)
	if p.Cursor < 0 || p.Cursor >= len(p.Entries) || p.Offset > p.Cursor {
		t.Fatal("invalid cursor after deletion")
	}
	// Loading a parent with no prior snapshot should highlight the child left.
	q := Panel{}
	loadPanel(t, &q, child)
	loadPanel(t, &q, root)
	if filepath.Base(q.Current()) != "middle" {
		t.Fatal("fresh parent did not focus child")
	}
}

func TestFailedNavigationPreservesState(t *testing.T) {
	root := t.TempDir()
	p := Panel{}
	loadPanel(t, &p, root)
	before := p.snapshot()
	if err := p.Load(filepath.Join(root, "missing")); err == nil {
		t.Fatal("expected failed navigation")
	}
	if p.Path != root || p.Cursor != before.Cursor || p.Offset != before.Offset {
		t.Fatal("failed navigation changed state")
	}
}

func TestCmdEnterInsertsQuotedFilenameWithoutExecuting(t *testing.T) {
	for _, name := range []string{"plain.txt", "with spaces.txt", "apostrophe' $HOME;file.txt", "кириллица.txt"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, name), nil, 0644); err != nil {
				t.Fatal(err)
			}
			a, _ := New(root, root)
			selectEntry(t, &a.Panels[0], name)
			a.Command = "printf '%s' "
			a.key(Message{Key: "Enter", Meta: true})
			if a.TermVisible || a.Doc != nil || a.Command == "" {
				t.Fatal("Cmd+Enter executed/opened instead of inserting")
			}
			out, err := exec.Command("/bin/zsh", "-f", "-c", a.Command).Output()
			if err != nil || string(out) != name {
				t.Fatalf("unsafe argument %q: %q %v", a.Command, out, err)
			}
		})
	}
}

func TestF9MenuKeyboardAndMouse(t *testing.T) {
	a, _ := New(t.TempDir(), t.TempDir())
	a.Command = "existing command"
	a.key(Message{Key: "F9"})
	if a.Menu == nil || a.Dialog != nil {
		t.Fatal("F9 did not open main menu")
	}
	a.key(Message{Key: "ArrowRight"}) // Files
	for _, item := range a.menuSections()[1].Items {
		if item.Label == "Make folder…" {
			break
		}
		a.key(Message{Key: "ArrowDown"})
	}
	a.key(Message{Key: "Enter"})
	if a.Menu != nil || a.Dialog == nil || a.Dialog.Title != "Make folder" {
		t.Fatal("menu did not dispatch mkdir")
	}
	a.key(Message{Key: "Escape"})
	a.key(Message{Key: "9", Meta: true})
	a.key(Message{Key: "Escape"})
	if a.Menu != nil || a.Command != "existing command" {
		t.Fatal("menu cancellation changed command line")
	}
	a.Active = 1
	a.key(Message{Key: "F9"})
	if a.Menu.Column != 4 {
		t.Fatal("F9 did not select active panel menu")
	}
	a.key(Message{Key: "Tab"})
	if a.Menu.Column != 0 {
		t.Fatal("menu Tab did not switch panel menus")
	}
	a.key(Message{Key: "Escape"})
	a.Active = 0
	a.click(Message{X: 2, Y: 0})
	if a.Menu == nil || a.Menu.Column != 0 {
		t.Fatal("top menu click failed")
	}
	x, y, _, _ := a.menuBounds()
	a.click(Message{X: x + 2, Y: y + 1})
	if a.Panels[0].View != "Brief" {
		t.Fatal("menu item click failed")
	}
	a.Dialog = nil
	a.key(Message{Key: "l", Meta: true})
	if a.Dialog == nil {
		t.Fatal("Cmd+L no longer changes directory")
	}
	a.Dialog = nil
	for _, size := range [][2]int{{60, 15}, {120, 36}} {
		a.Cols, a.Rows = size[0], size[1]
		for col := range a.menuSections() {
			a.Menu = &MenuState{Column: col}
			x, y, w, h := a.menuBounds()
			if x < 0 || y < 0 || x+w > a.Cols || y+h > a.Rows {
				t.Fatal("menu outside viewport")
			}
			a.Draw()
		}
	}
}

func TestParentFocusesExitedFolderOverSavedCursor(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(fmt.Sprintf("async=%t", async), func(t *testing.T) {
			root := t.TempDir()
			for _, name := range []string{"a-first", "b-selected", "c-exited", "z-last"} {
				if err := os.Mkdir(filepath.Join(root, name), 0755); err != nil {
					t.Fatal(err)
				}
			}
			p := Panel{Async: async}
			load := func(path string) {
				t.Helper()
				if err := p.Load(path); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(5 * time.Second)
				for p.load != nil && time.Now().Before(deadline) {
					p.pollPanelLoad()
					time.Sleep(time.Millisecond)
				}
				if p.load != nil || p.Err != "" {
					t.Fatalf("load failed: %s", p.Err)
				}
			}
			load(root)
			selectEntry(t, &p, "b-selected")
			p.Offset = 1
			p.Marked["z-last"] = true
			// Enter by path/bookmark, leaving a different saved cursor in the parent.
			load(filepath.Join(root, "c-exited"))
			selectEntry(t, &p, "..")
			load(p.Current())
			if filepath.Base(p.Current()) != "c-exited" {
				t.Fatalf("parent restored stale cursor: %s", p.Current())
			}
			if p.Offset != 1 || !p.Marked["z-last"] {
				t.Fatalf("parent viewport or selection lost: %+v", p.snapshot())
			}
		})
	}
}
