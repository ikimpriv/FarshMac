package ui

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandTerminalToggleAndShellExit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ZDOTDIR", root)
	sub := filepath.Join(root, "child ' quoted")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, sub)
	if err != nil {
		t.Fatal(err)
	}
	a.Settings.Shell = "zsh"
	if err = a.StartTerminal(); err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var output strings.Builder
	wait := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			u := a.Poll()
			b, _ := base64.StdEncoding.DecodeString(u.Terminal)
			output.Write(b)
			if check() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timeout: %s / %s", a.Status, output.String())
	}
	wait(func() bool { return a.ShellReady })
	a.Active = 1
	a.Command = "printf 'UI_COMMAND_OK\\n'; pwd"
	a.key(Message{Key: "Enter"})
	if !a.TermVisible {
		t.Fatal("command did not hide panels")
	}
	a.key(Message{Key: "o", Meta: true})
	if a.TermVisible {
		t.Fatal("cannot restore panels while busy")
	}
	a.key(Message{Key: "o", Meta: true})
	if !a.TermVisible {
		t.Fatal("cannot restore terminal")
	}
	wait(func() bool { return a.ShellReady })
	if !strings.Contains(output.String(), "\r\nUI_COMMAND_OK\r\n") {
		t.Fatalf("command output missing: %q", output.String())
	}
	if strings.Contains(output.String(), "builtin cd") || strings.Contains(output.String(), "cmdand cursh>") {
		t.Fatalf("internal cwd wrapper leaked into terminal: %q", output.String())
	}
	actual, _ := filepath.EvalSymlinks(a.ShellCwd)
	want, _ := filepath.EvalSymlinks(sub)
	if actual != want {
		t.Fatalf("shell cwd %s, want %s", actual, want)
	}
	old := a.Term
	a.Handle(`{"Type":"input","Text":"exit\r"}`)
	wait(func() bool { return a.Term != nil && a.Term != old && a.ShellReady })
	if !a.TermVisible {
		t.Fatal("restart hid terminal")
	}
	actual, _ = filepath.EvalSymlinks(a.ShellCwd)
	if actual != want {
		t.Fatalf("restart lost directory: %s", actual)
	}
}

func TestMacShortcutsAndDirectoryNavigation(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "каталог")
	os.Mkdir(child, 0755)
	a, err := New(root, child)
	if err != nil {
		t.Fatal(err)
	}
	a.key(Message{Key: "o", Meta: true})
	if !a.TermVisible {
		t.Fatal("Cmd+O did not show terminal")
	}
	a.key(Message{Key: "o", Meta: true})
	if a.TermVisible {
		t.Fatal("Cmd+O did not restore panels")
	}
	a.key(Message{Key: "u", Meta: true})
	if a.Panels[0].Path != child {
		t.Fatal("Cmd+U did not swap")
	}
	selectEntry(t, &a.Panels[0], "..")
	a.key(Message{Key: "Enter"})
	if a.Panels[0].Path != root {
		t.Fatal("parent navigation failed")
	}
	a.key(Message{Key: "F7"})
	if a.Dialog == nil {
		t.Fatal("F7 did not open mkdir")
	}
	a.Dialog.Input = "new folder"
	a.key(Message{Key: "Enter"})
	if _, err := os.Stat(filepath.Join(root, "new folder")); err != nil {
		t.Fatal(err)
	}
	a.key(Message{Key: "Tab"})
	if a.Active != 1 {
		t.Fatal("Tab failed")
	}
}
func TestDrawSmallAndLarge(t *testing.T) {
	a, _ := New(t.TempDir(), t.TempDir())
	for _, size := range [][2]int{{60, 15}, {120, 36}, {200, 60}} {
		a.Cols, a.Rows = size[0], size[1]
		a.prompt("Copy", "Long filename: "+strings.Repeat("a", 500), "тест", func(string) {})
		grid := a.Draw()
		if len(grid.Cells) != size[0]*size[1] {
			t.Fatal("bad grid dimensions")
		}
		if !strings.Contains(grid.ANSI(), "\x1b[") {
			t.Fatal("missing ANSI output")
		}
	}
}
func TestEditorRefusesBinaryAndExternalEdits(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	os.WriteFile(path, []byte("hello"), 0644)
	a, _ := New(root, root)
	a.Panels[0].Cursor = 1
	a.open(true)
	if a.Doc == nil || !a.Doc.Edit {
		t.Fatal("editor did not open")
	}
	a.Doc.Text = "edited"
	a.Doc.Dirty = true
	a.key(Message{Key: "Escape"})
	if a.Form == nil || a.Doc == nil {
		t.Fatal("unsaved edit was discarded")
	}
	a.Form = nil
	a.save()
	b, _ := os.ReadFile(path)
	if string(b) != "edited" {
		t.Fatal("save failed")
	}
	os.WriteFile(path, []byte{0, 1, 2}, 0644)
	a.Doc = nil
	a.open(true)
	if a.Doc != nil {
		t.Fatal("binary editor opened")
	}
}
