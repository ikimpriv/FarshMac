package ui

import (
	"encoding/base64"
	"farshmac/internal/fsops"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnterExecutableUsesPTY(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ZDOTDIR", root)
	name := "run ' quoted ;.sh"
	path := filepath.Join(root, name)
	if e := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'FARSHMAC_EXEC_OK\\n'\npwd\n"), 0700); e != nil {
		t.Fatal(e)
	}
	a, e := New(root, root)
	if e != nil {
		t.Fatal(e)
	}
	a.Settings.Shell = "zsh"
	if e = a.StartTerminal(); e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	output := ""
	wait := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			u := a.Poll()
			b, _ := base64.StdEncoding.DecodeString(u.Terminal)
			output += string(b)
			if check() {
				return
			}
			time.Sleep(time.Millisecond * 10)
		}
		t.Fatal("PTY timeout", output)
	}
	wait(func() bool { return a.ShellReady })
	selectEntry(t, &a.Panels[0], name)
	a.key(Message{Key: "Enter"})
	if !a.TermVisible || a.Doc != nil {
		t.Fatal("Enter did not dispatch executable to shell")
	}
	wait(func() bool { return a.ShellReady && strings.Contains(output, "\r\nFARSHMAC_EXEC_OK\r\n") })
	if !strings.Contains(output, root) {
		t.Fatal("wrong working directory", output)
	}
}
func TestViewerEditorDispatchAndAssociations(t *testing.T) {
	a, _ := New(t.TempDir(), t.TempDir())
	path := "/tmp/file ' ; name.txt"
	for _, edit := range []bool{false, true} {
		p := a.viewPlan(path, edit, false, false)
		if !p.Internal {
			t.Fatal("default should be internal")
		}
		p = a.viewPlan(path, edit, true, false)
		if len(p.Args) == 0 || p.Args[0] != "/usr/bin/open" || p.Args[len(p.Args)-1] != path {
			t.Fatal(p)
		}
		if edit && p.Args[1] != "-t" {
			t.Fatal(p)
		}
	}
	a.Settings.ExternalViewerDefault = true
	a.Settings.ExternalViewer = "less -- {}"
	p := a.viewPlan(path, false, false, false)
	if !strings.HasPrefix(p.Command, "less -- '") || p.Internal {
		t.Fatal(p)
	}
	if !a.viewPlan(path, false, true, false).Internal {
		t.Fatal("Alt must reverse external default")
	}
	a.Settings.Associations = []Association{{Mask: "*.txt", Editor: "vim -- {}", Viewer: "cat -- {}"}}
	if !strings.HasPrefix(a.viewPlan(path, true, false, false).Command, "vim") {
		t.Fatal("associated editor ignored")
	}
	if !a.viewPlan(path, true, false, true).Internal {
		t.Fatal("forced internal ignored")
	}
	if !a.viewPlan(path, true, true, false).Internal {
		t.Fatal("Alt association toggle")
	}
}
func TestAltFastFindCycleAndPaste(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"alpha", "alpine", "beta", "ёжик"} {
		os.WriteFile(filepath.Join(root, name), nil, 0600)
	}
	a, _ := New(root, root)
	a.Command = "untouched"
	a.key(Message{Key: "a", Alt: true})
	a.key(Message{Key: "l"})
	if a.FastFind == nil || a.FastFind.Query != "al" || filepath.Base(a.Panels[0].Current()) != "alpha" {
		t.Fatal("fast find")
	}
	a.key(Message{Key: "Enter", Meta: true})
	if filepath.Base(a.Panels[0].Current()) != "alpine" {
		t.Fatal("next match")
	}
	a.key(Message{Key: "Enter", Meta: true, Shift: true})
	if filepath.Base(a.Panels[0].Current()) != "alpha" {
		t.Fatal("previous match")
	}
	a.Handle(`{"Type":"paste","Text":"phaNO_MATCH"}`)
	if a.FastFind.Query != "alpha" {
		t.Fatal(a.FastFind.Query)
	}
	a.key(Message{Key: "Escape"})
	if a.FastFind != nil || a.Command != "untouched" {
		t.Fatal("search touched command line")
	}
	a.key(Message{Key: "ё", Alt: true})
	if filepath.Base(a.Panels[0].Current()) != "ёжик" {
		t.Fatal("unicode fast find")
	}
	a.key(Message{Key: "Escape"})
	a.key(Message{Key: "a", Alt: true})
	a.key(Message{Key: "*"})
	a.key(Message{Key: "e"})
	if filepath.Base(a.Panels[0].Current()) != "alpine" {
		t.Fatal("wildcard fast find")
	}
}
func TestF7FocusesNewDirectory(t *testing.T) {
	root := t.TempDir()
	a, _ := New(root, root)
	a.Settings.ShowHidden = false
	a.refresh()
	for _, name := range []string{"new", "nested/child", ".hidden"} {
		a.Panels[0].Load(root)
		a.function(7)
		a.Dialog.Input = name
		a.key(Message{Key: "Enter"})
		want := filepath.Join(root, name)
		if a.Panels[0].Current() != want || a.Panels[0].Path != filepath.Dir(want) {
			t.Fatal(a.Panels[0].Path, a.Panels[0].Current(), want)
		}
	}
	// Failure must preserve the selected item.
	before := a.Panels[0].Current()
	file := filepath.Join(root, "plain")
	os.WriteFile(file, nil, 0600)
	a.function(7)
	a.Dialog.Input = file
	a.key(Message{Key: "Enter"})
	if a.Panels[0].Current() != before {
		t.Fatal("failed mkdir moved cursor")
	}
}
func TestScreensIncludePanelsAndCycle(t *testing.T) {
	a, _ := New(t.TempDir(), t.TempDir())
	one, two := newDocument("one", "one"), newDocument("two", "two")
	a.showDocument(one)
	a.showDocument(two)
	a.screenMenu()
	if len(a.Form.Choice) != 3 || a.Form.Choice[0] != "Panels" {
		t.Fatal("missing panel screen")
	}
	a.Form.Focus = 0
	a.key(Message{Key: "Enter"})
	if a.Doc != nil || len(a.Documents) != 2 {
		t.Fatal("panels discarded documents")
	}
	a.cycleScreen(false)
	if a.Doc != one {
		t.Fatal("first screen")
	}
	a.cycleScreen(false)
	if a.Doc != two {
		t.Fatal("second screen")
	}
	a.cycleScreen(false)
	if a.Doc != nil {
		t.Fatal("panel cycle")
	}
}
func TestIndependentWindowsAndQuitTransaction(t *testing.T) {
	root := t.TempDir()
	a, _ := New(root, root)
	g := NewWindowGroup(a)
	g.Factory = New
	g.StartShell = false
	defer g.CloseAll()
	id, e := g.NewWindow(1)
	if e != nil {
		t.Fatal(e)
	}
	b := g.Apps[id]
	b.Settings.MaskGroups["new"] = "*.x"
	if a.Settings.MaskGroups["new"] != "" {
		t.Fatal("shared settings map")
	}
	b.Command = "second"
	if a.Command != "" {
		t.Fatal("shared command")
	}
	d1, d2 := newDocument(filepath.Join(root, "one"), "one"), newDocument(filepath.Join(root, "two"), "two")
	d1.Dirty = true
	d2.Dirty = true
	a.showDocument(d1)
	b.showDocument(d2)
	g.BeginQuit()
	if g.Poll(1).AttentionWindow != 1 || a.Form == nil {
		t.Fatal("quit not checking first window")
	}
	a.Form.Focus = 1
	g.Handle(1, `{"Type":"key","Key":"Enter"}`)
	if g.Terminating || b.Form == nil {
		t.Fatal("quit skipped second window")
	}
	g.Handle(id, `{"Type":"key","Key":"Escape"}`)
	if g.Terminating || a.Quit || b.Quit || !d1.Dirty {
		t.Fatal("cancel closed/discarded earlier window")
	}
	g.BeginQuit()
	a.Form.Focus = 1
	g.Handle(1, `{"Type":"key","Key":"Enter"}`)
	b.Form.Focus = 1
	g.Handle(id, `{"Type":"key","Key":"Enter"}`)
	if !g.Terminating {
		t.Fatal("all approvals did not quit")
	}
}
func TestCloseOneWindowKeepsOther(t *testing.T) {
	a, _ := New(t.TempDir(), t.TempDir())
	g := NewWindowGroup(a)
	g.Factory = New
	g.StartShell = false
	defer g.CloseAll()
	id, _ := g.NewWindow(1)
	g.Handle(id, `{"Type":"close"}`)
	if !g.Poll(id).Quit || g.Poll(1).Quit || g.Terminating {
		t.Fatal("window close quit application")
	}
	g.CloseWindow(id)
	if len(g.Apps) != 1 {
		t.Fatal("registry close")
	}
}

func TestProgressUIAndCancel(t *testing.T) {
	a, _ := New(t.TempDir(), t.TempDir())
	entered, release := make(chan struct{}), make(chan struct{})
	a.workProgress("Copying", func(o *fsops.Operation) error {
		o.Add("item", 100)
		o.Ready()
		o.Begin("item", 100)
		o.Advance(25)
		close(entered)
		<-release
		return o.Check()
	})
	<-entered
	for _, size := range [][2]int{{60, 15}, {300, 120}} {
		a.Cols, a.Rows = size[0], size[1]
		var visible strings.Builder
		for _, cell := range a.Draw().Cells {
			visible.WriteString(cell.Text)
		}
		text := visible.String()
		if !strings.Contains(text, "25.0%") || !strings.Contains(text, "Cancel") {
			t.Fatal("progress overlay missing")
		}
	}
	a.key(Message{Key: "Escape"})
	if a.Form != nil || a.Operation.Check() == nil {
		t.Fatal("Esc must cancel directly")
	}
	close(release)
	waitWork(t, a)
	if a.Operation != nil || a.Status != "operation cancelled" {
		t.Fatal(a.Status)
	}
}
func TestIndependentPTYWindows(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ZDOTDIR", root)
	a, _ := New(root, root)
	a.Settings.Shell = "zsh"
	if e := a.StartTerminal(); e != nil {
		t.Fatal(e)
	}
	g := NewWindowGroup(a)
	defer g.CloseAll()
	id, e := g.NewWindow(1)
	if e != nil {
		t.Fatal(e)
	}
	b := g.Apps[id]
	if a.Term == b.Term {
		t.Fatal("shared PTY")
	}
	output := ""
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		a.Poll()
		u := b.Poll()
		v, _ := base64.StdEncoding.DecodeString(u.Terminal)
		output += string(v)
		if a.ShellReady && b.ShellReady {
			break
		}
		time.Sleep(time.Millisecond * 10)
	}
	if !a.ShellReady || !b.ShellReady {
		t.Fatal("shells did not start")
	}
	g.CloseWindow(1)
	b.Command = "printf 'SECOND_WINDOW_ALIVE\\n'"
	b.execute()
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		u := b.Poll()
		v, _ := base64.StdEncoding.DecodeString(u.Terminal)
		output += string(v)
		if b.ShellReady && strings.Contains(output, "\r\nSECOND_WINDOW_ALIVE\r\n") {
			return
		}
		time.Sleep(time.Millisecond * 10)
	}
	t.Fatal("closing first window stopped second shell", output)
}

func TestFastFindIncludesFirstLetterWithoutMatch(t *testing.T) {
	root := t.TempDir()
	a, _ := New(root, root)
	defer a.Close()
	a.key(Message{Key: "я", Alt: true})
	if a.FastFind == nil || a.FastFind.Query != "я" {
		t.Fatal("first letter disappeared", a.FastFind)
	}
	a.key(Message{Key: "Escape"})
	os.WriteFile(filepath.Join(root, "alpha"), nil, 0600)
	a.Panels[0].Load(root)
	a.key(Message{Key: "a", Alt: true})
	if a.FastFind == nil || a.FastFind.Query != "a" || filepath.Base(a.Panels[0].Current()) != "alpha" {
		t.Fatal("first key did not search immediately")
	}
}

func TestOptionFastFindPrintableAndPunctuation(t *testing.T) {
	for _, prefix := range []string{"a", ".", "_", "-", "я"} {
		t.Run(prefix, func(t *testing.T) {
			root := t.TempDir()
			a, err := New(root, root)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			a.Panels[a.Active].Entries = []Entry{{Name: "other"}, {Name: prefix + "name"}}
			a.Handle(`{"Type":"key","Key":"` + prefix + `","Alt":true}`)
			if a.FastFind == nil || a.FastFind.Query != prefix || a.Panels[a.Active].Cursor != 1 {
				t.Fatalf("search not started: %#v cursor %d", a.FastFind, a.Panels[a.Active].Cursor)
			}
			if a.Form != nil || a.Dialog != nil {
				t.Fatal("search opened selection dialog")
			}
			a.Handle(`{"Type":"key","Key":"-"}`)
			if a.FastFind == nil {
				t.Fatal("hyphen during search cancelled it")
			}
		})
	}
}
