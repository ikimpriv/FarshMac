package ui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"farshmac/internal/fsops"
	"farshmac/internal/textfile"
)

func waitPanels(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for a.Panels[0].load != nil || a.Panels[1].load != nil {
		a.Poll()
		if time.Now().After(deadline) {
			t.Fatal("panels did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestEscapeCancelsForegroundWithoutClosingEditor(t *testing.T) {
	a, _ := New(t.TempDir(), t.TempDir())
	defer a.Close()
	d := newDocument("unsaved", "text")
	d.Edit, d.Dirty = true, true
	a.Doc = d
	entered := make(chan struct{})
	applied, cleaned := false, false
	a.backgroundTask("Long read", func(o *fsops.Operation) (func(), func(), error) {
		close(entered)
		<-o.Context().Done()
		return func() { applied = true }, func() { cleaned = true }, o.Check()
	})
	<-entered
	a.Handle(`{"Type":"key","Key":"Escape"}`)
	if a.Form != nil || a.Doc != d || a.Operation.Check() == nil {
		t.Fatal("Esc did not cancel directly")
	}
	a.Handle(`{"Type":"paste","Text":"must not edit during cancellation"}`)
	waitWork(t, a)
	if applied || !cleaned || a.Doc != d || d.Text != "text" || a.Status != "operation cancelled" {
		t.Fatal("cancel applied result or closed document", a.Status)
	}
}

func TestEscapeTerminatesExternalHelper(t *testing.T) {
	a, _ := New(t.TempDir(), t.TempDir())
	defer a.Close()
	started := make(chan struct{})
	a.workProgress("Helper", func(o *fsops.Operation) error {
		cmd := exec.CommandContext(o.Context(), "/bin/sleep", "30")
		if err := cmd.Start(); err != nil {
			return err
		}
		close(started)
		return cmd.Wait()
	})
	<-started
	a.key(Message{Key: "Escape"})
	waitWork(t, a)
	if a.Status != "operation cancelled" {
		t.Fatal(a.Status)
	}
}

func TestCancelOverwriteAndCloseReleaseWorker(t *testing.T) {
	for _, closeWindow := range []bool{false, true} {
		t.Run(map[bool]string{true: "close", false: "escape"}[closeWindow], func(t *testing.T) {
			root, dest := t.TempDir(), t.TempDir()
			src, dst := filepath.Join(root, "file"), filepath.Join(dest, "file")
			os.WriteFile(src, []byte("new"), 0600)
			os.WriteFile(dst, []byte("original"), 0600)
			a, _ := New(root, dest)
			defer a.Close()
			selectEntry(t, &a.Panels[0], "file")
			a.transferDialog(false)
			a.key(Message{Key: "Enter"})
			deadline := time.Now().Add(5 * time.Second)
			for a.Form == nil && time.Now().Before(deadline) {
				a.Poll()
				time.Sleep(time.Millisecond)
			}
			if a.Form == nil || a.Form.Title != "Warning" {
				t.Fatal("no conflict", a.Status)
			}
			op := a.Operation
			if closeWindow {
				a.Close()
				if !errors.Is(op.Check(), fsops.ErrCancelled) {
					t.Fatal("close did not wake operation")
				}
			} else {
				a.key(Message{Key: "Escape"})
				waitWork(t, a)
				if a.Form != nil || a.Status != "operation cancelled" {
					t.Fatal(a.Status)
				}
			}
			data, _ := os.ReadFile(dst)
			if string(data) != "original" {
				t.Fatal("cancel overwrote destination")
			}
		})
	}
}

func TestAsyncPanelsCancelRefreshAndRestoreSelection(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "child"), 0700)
	a, err := NewAsync(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.key(Message{Key: "Escape"})
	waitPanels(t, a)
	if a.Panels[0].Path != root || a.Panels[0].Err != "operation cancelled" {
		t.Fatal("cancel lost initial folder", a.Panels[0].Path, a.Panels[0].Err)
	}
	a.refresh()
	waitPanels(t, a)
	selectEntry(t, &a.Panels[0], "child")
	a.Panels[0].Load(filepath.Join(root, "missing"))
	waitPanels(t, a)
	if a.Panels[0].Current() != filepath.Join(root, "child") {
		t.Fatal("failed async navigation lost selection")
	}
	a.function(7)
	a.Dialog.Input = "created"
	a.key(Message{Key: "Enter"})
	waitPanels(t, a)
	if a.Panels[0].Current() != filepath.Join(root, "created") {
		t.Fatal("F7 async cursor")
	}
	a.Command = "cre"
	a.completeCommand()
	waitWork(t, a)
	if !strings.Contains(a.Command, "created") {
		t.Fatal("async completion", a.Command)
	}
}

func TestAsyncDocumentReadReplaceSaveAndCancel(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "document")
	os.WriteFile(path, []byte("old old"), 0600)
	a, _ := NewAsync(root, root)
	defer a.Close()
	waitPanels(t, a)
	a.openDocument(path, true)
	a.key(Message{Key: "Escape"})
	waitWork(t, a)
	if a.Doc != nil || a.Status != "operation cancelled" {
		t.Fatal("cancelled read opened document", a.Status)
	}
	a.openDocument(path, true)
	waitWork(t, a)
	if a.Doc == nil {
		t.Fatal("async open failed", a.Status)
	}
	d := a.Doc
	d.Search = textfile.Search{Query: "old", Case: true}
	a.replaceAllAsync(d, "new")
	a.key(Message{Key: "Escape"})
	waitWork(t, a)
	if d.Text != "old old" || d.Dirty {
		t.Fatal("cancelled replacement committed")
	}
	a.replaceAllAsync(d, "new")
	waitWork(t, a)
	if d.Text != "new new" || !d.Dirty {
		t.Fatal("replace failed", a.Status)
	}
	a.save()
	waitWork(t, a)
	data, _ := os.ReadFile(path)
	if string(data) != "new new" || d.Dirty {
		t.Fatal("async save", a.Status)
	}
	// Once atomic publication has succeeded, a late Esc must still reconcile
	// the editor's inode / saved state, while suppressing a close/quit callback.
	d.Insert("!")
	d.modified()
	closed := false
	a.saveDocumentThen(d, d.Path, false, func() { closed = true })
	result := <-a.task.done
	if result.err != nil {
		t.Fatal(result.err)
	}
	a.task.done <- result
	a.key(Message{Key: "Escape"})
	waitWork(t, a)
	if closed || d.Dirty {
		t.Fatal("late Esc lost saved state or closed document")
	}
	a.openDocument(path, false)
	waitWork(t, a)
	if a.Doc.ScreenID == d.ScreenID || len(a.Documents) != 1 {
		t.Fatal("background open reused another document's screen ID")
	}
}

func TestAsyncSaveAllQuitCanWaitForTask(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	os.WriteFile(path, []byte("old"), 0600)
	a, _ := NewAsync(root, root)
	defer a.Close()
	waitPanels(t, a)
	a.openDocument(path, true)
	waitWork(t, a)
	a.Doc.Insert("new")
	a.Doc.modified()
	g := NewWindowGroup(a)
	g.BeginQuit()
	a.key(Message{Key: "Enter"})
	deadline := time.Now().Add(5 * time.Second)
	for !g.Terminating && time.Now().Before(deadline) {
		g.Poll(1)
		time.Sleep(time.Millisecond)
	}
	if !g.Terminating {
		t.Fatal("async Save all abandoned quit", a.Status)
	}
	g.CloseAll()
}
