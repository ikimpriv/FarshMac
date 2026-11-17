package ui

import (
	"farshmac/internal/fsops"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type gatedSearch struct {
	op           *fsops.Operation
	root         string
	gate, closed chan struct{}
}

func (b *gatedSearch) ReadDir(_ string, visit func(Entry) error) error {
	for i, name := range []string{"first.txt", "second.txt"} {
		if i == 1 {
			select {
			case <-b.gate:
			case <-b.op.Context().Done():
				return b.op.Check()
			}
		}
		info, err := os.Stat(filepath.Join(b.root, name))
		if err != nil {
			return err
		}
		if err = visit(Entry{Name: name, Info: info}); err != nil {
			return err
		}
	}
	return nil
}
func (b *gatedSearch) Open(p string) (io.ReadCloser, error) { return os.Open(p) }
func (b *gatedSearch) Descend(string, Entry) bool           { return false }
func (b *gatedSearch) Close() error                         { close(b.closed); return nil }
func streamingSearchApp(t *testing.T) (*App, *gatedSearch) {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"first.txt", "second.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	b := &gatedSearch{root: root, gate: make(chan struct{}), closed: make(chan struct{})}
	a.startSearch(a.Panels[0], 0, "Find file", searchOptions{mask: "*"}, func(o *fsops.Operation) (browseStrategy, error) { b.op = o; return b, nil })
	pollSearchUntil(t, a, func() bool { return len(a.searchSession.hits) == 1 })
	return a, b
}
func pollSearchUntil(t *testing.T, a *App, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !done() && time.Now().Before(deadline) {
		a.Poll()
		time.Sleep(time.Millisecond)
	}
	if !done() {
		t.Fatal("search did not reach expected state", a.Status)
	}
}
func TestStreamingSearchViewWhileRunning(t *testing.T) {
	a, b := streamingSearchApp(t)
	if a.Busy || a.searchSession.done {
		t.Fatal("results must be usable while search is running")
	}
	f := a.Form
	a.setListFilter("first")
	a.key(Message{Key: "F3"})
	waitWorkViewer := func() bool { return !a.Busy && a.Doc != nil }
	pollSearchUntil(t, a, waitWorkViewer)
	if !strings.Contains(a.Doc.Text, "first.txt") {
		t.Fatal("wrong viewer")
	}
	close(b.gate)
	pollSearchUntil(t, a, func() bool { return a.searchSession.done })
	if a.Form != nil || a.Doc == nil {
		t.Fatal("arrival interrupted viewer")
	}
	a.key(Message{Key: "Escape"})
	a.Poll()
	if a.Form != f || f.Query != "first" || len(f.Choice) != 1 {
		t.Fatal("lost filtered list")
	}
	a.setListFilter("")
	if len(f.Choice) != 2 {
		t.Fatal("new result lost while viewing")
	}
}
func TestStreamingSearchStopKeepsResults(t *testing.T) {
	a, b := streamingSearchApp(t)
	a.key(Message{Key: "Escape"})
	if a.Form == nil {
		t.Fatal("stop discarded results")
	}
	pollSearchUntil(t, a, func() bool { return a.searchSession.done })
	if len(a.searchSession.hits) != 1 || !strings.Contains(a.Form.Title, "Stopped") {
		t.Fatal("stop state")
	}
	select {
	case <-b.closed:
	default:
		t.Fatal("strategy not closed")
	}
	a.key(Message{Key: "Escape"})
	a.Poll()
	if a.Form != nil || a.searchSession != nil {
		t.Fatal("close did not dispose search")
	}
}
func TestStreamingSearchNavigateCancelsWorker(t *testing.T) {
	a, b := streamingSearchApp(t)
	a.key(Message{Key: "Enter"})
	if a.searchSession != nil || a.Form != nil || filepath.Base(a.Panels[0].Current()) != "first.txt" {
		t.Fatal("navigation failed")
	}
	select {
	case <-b.closed:
	case <-time.After(time.Second):
		t.Fatal("search not canceled")
	}
	a.Poll()
	if a.Form != nil {
		t.Fatal("late result reopened list")
	}
}
func TestStreamingSearchCloseCancelsWorker(t *testing.T) {
	a, b := streamingSearchApp(t)
	a.Close()
	select {
	case <-b.closed:
	case <-time.After(time.Second):
		t.Fatal("window close leaked search")
	}
}
