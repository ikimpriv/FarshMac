package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"farshmac/internal/highlight"
)

func syntaxFinish(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for a.syntax.job != nil && time.Now().Before(deadline) {
		a.pollSyntax()
		time.Sleep(time.Millisecond)
	}
	if a.syntax.job != nil {
		t.Fatal("syntax worker did not finish")
	}
}
func TestSyntaxDebounceStaleResultsAndClose(t *testing.T) {
	a := textApp(t, "package main", true)
	started := make(chan string, 2)
	release := make(chan struct{})
	a.syntaxRun = func(ctx context.Context, path, text string) (highlight.Result, error) {
		started <- text
		<-ctx.Done()
		<-release // Simulate a worker that finishes after the next edit.
		return highlight.Result{Language: "stale", Spans: []highlight.Span{{Start: 0, End: 7, Color: White}}}, nil
	}
	a.pollSyntax()
	if a.syntax.job != nil {
		t.Fatal("debounce ignored")
	}
	a.syntax.due = time.Time{}
	a.pollSyntax()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("not started")
	}
	a.Doc.Replace(0, 7, "changed")
	a.pollSyntax()
	if len(a.documentSyntax(a.Doc).Spans) != 0 {
		t.Fatal("stale color visible")
	}
	close(release)
	syntaxFinish(t, a)
	if a.syntax.result.Language == "stale" {
		t.Fatal("stale result installed")
	}
	exited := make(chan struct{})
	a.syntaxRun = func(ctx context.Context, path, text string) (highlight.Result, error) {
		<-ctx.Done()
		close(exited)
		return highlight.Result{}, ctx.Err()
	}
	a.syntax.due = time.Time{}
	a.pollSyntax()
	a.Close()
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("close did not cancel")
	}
}
func TestSyntaxLargeFileAndSelectionColors(t *testing.T) {
	a := textApp(t, "package main", true)
	a.Doc.Path = "main.go"
	a.syntaxRun = func(ctx context.Context, path, text string) (highlight.Result, error) {
		return highlight.Parse(path, text)
	}
	a.pollSyntax()
	a.syntax.due = time.Time{}
	a.pollSyntax()
	syntaxFinish(t, a)
	g := NewGrid(80, 25)
	a.drawDoc(g)
	// First character is caret; the next keyword character must be highlighted.
	if g.Cells[81].FG != White {
		t.Fatalf("keyword color: %x", g.Cells[81].FG)
	}
	a.Doc.Anchor = 0
	a.Doc.Cursor = 7
	a.drawDoc(g)
	if g.Cells[81].FG != Black || g.Cells[81].BG != Cyan {
		t.Fatal("syntax covered selection")
	}
	a.Doc.Replace(0, len(a.Doc.Text), strings.Repeat("x", highlight.MaxBytes+1))
	a.pollSyntax()
	if a.syntax.job != nil || a.syntax.text != "" || a.syntax.status != "syntax off: >1 MiB" {
		t.Fatal("large file scheduled or retained")
	}
	a.Doc.Replace(0, len(a.Doc.Text), "")
	a.pollSyntax()
	if a.syntax.large || a.syntax.attempted {
		t.Fatal("large file state survived deletion")
	}
}
func TestEditorTabsMouseAndAutoindent(t *testing.T) {
	a := textApp(t, "\t界x", true)
	a.Settings.EditorTabSize = 8
	a.Settings.EditorExpandTabs = true
	a.Doc.Cursor = len("\t界")
	a.editorKey(Message{Key: "Tab"})
	if a.Doc.Text != "\t界      x" {
		t.Fatalf("tab stop: %q", a.Doc.Text)
	}
	a.Doc.Undo()
	a.Doc.Cursor = 0
	a.drawDoc(NewGrid(a.Cols, a.Rows))
	a.documentClick(Message{X: 10, Y: 1}, false)
	if a.Doc.Cursor != len("\t界") {
		t.Fatalf("mouse tab width: %d", a.Doc.Cursor)
	}
	a.Doc.Cursor = len(a.Doc.Text)
	a.editorKey(Message{Key: "Enter"})
	if a.Doc.Text != "\t界x\n\t" {
		t.Fatalf("autoindent: %q", a.Doc.Text)
	}
}
