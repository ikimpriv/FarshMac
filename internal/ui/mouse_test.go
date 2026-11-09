package ui

import (
	"encoding/base64"
	"encoding/json"
	"farshmac/internal/terminal"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mouseApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	for i := 0; i < 60; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("file%02d", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		a.Close()
		if a.historyCleanup != nil {
			<-a.historyCleanup
		}
	})
	return a
}
func TestMouseMarkRangeDragAndHeaderHitTesting(t *testing.T) {
	a := mouseApp(t)
	p := &a.Panels[0]
	a.click(Message{X: 4, Y: 4, Button: 2})
	if !p.Marked["file00"] {
		t.Fatal("right click did not mark")
	}
	a.dragMouse(Message{X: 4, Y: 7, Button: 2, Buttons: 2})
	for _, n := range []string{"file00", "file01", "file02", "file03"} {
		if !p.Marked[n] {
			t.Fatal("drag missed", n)
		}
	}
	a.Handle(`{"Type":"mouseUp"}`)
	a.click(Message{X: 4, Y: 4, Button: 2})
	a.dragMouse(Message{X: 4, Y: 7, Button: 2, Buttons: 2})
	if len(p.Marked) != 0 {
		t.Fatal("right drag did not unmark", p.Marked)
	}
	a.click(Message{X: 4, Y: 4})
	a.click(Message{X: 4, Y: 7, Shift: true})
	if len(p.Marked) != 4 {
		t.Fatal("shift range", p.Marked)
	}
	a.click(Message{X: 4, Y: 5, Meta: true})
	if p.Marked["file01"] {
		t.Fatal("Cmd click did not toggle")
	}
	a.setView(0, "Brief")
	before := p.Cursor
	for _, y := range []int{1, 2, a.Rows - 6, a.Rows - 5} {
		a.click(Message{X: 25, Y: y})
		if p.Cursor != before {
			t.Fatal("header/status selected a file", y)
		}
	}
	a.click(Message{X: 0, Y: 6})
	if p.Cursor != before {
		t.Fatal("border selected a file")
	}
}
func TestMouseWheelAndDocumentSelection(t *testing.T) {
	a := mouseApp(t)
	before := a.Panels[0].Cursor
	a.scrollMouse(Message{X: a.Cols/2 + 5, Y: 5, Delta: 5})
	if a.Panels[1].Offset != 5 || a.Panels[0].Cursor != before {
		t.Fatal("wheel scrolled the wrong panel")
	}
	a.Panels[1].Offset = 0
	a.Panels[1].Cursor = 0
	a.copyDocument(false)
	if a.Clipboard == nil {
		t.Fatal("panel copy ignored")
	}
	d := newDocument("doc", strings.Repeat("hello world\n", 80))
	d.Edit = true
	a.Doc = d
	a.documentClick(Message{X: 7, Y: 1, Clicks: 2}, false)
	if d.Selected() != "world" {
		t.Fatal("word selection", d.Selected())
	}
	a.documentClick(Message{X: 7, Y: 1, Clicks: 3}, false)
	if d.Selected() != "hello world\n" {
		t.Fatal("line selection", d.Selected())
	}
	cursor := d.Cursor
	a.scrollMouse(Message{Delta: 10})
	a.Draw()
	if d.Cursor != cursor || d.Offset != 10 {
		t.Fatal("wheel moved caret or snapped viewport", d.Cursor, d.Offset)
	}
	a.documentClick(Message{X: a.Cols - 1, Y: 20}, false)
	if !a.documentScrollbarDrag || d.Offset < 10 {
		t.Fatal("scrollbar click ignored")
	}
	a.Handle(`{"Type":"mouseUp"}`)
	if a.documentScrollbarDrag {
		t.Fatal("scrollbar drag stuck")
	}
}
func TestTerminalOutputBackpressureAndChunkBound(t *testing.T) {
	a := mouseApp(t)
	original := strings.Repeat("output", 12000)
	a.pending = []byte(original)
	a.historyWrite = make(chan terminal.HistoryStatus, 1)
	a.historyQueuedBytes = terminal.MaxHistoryBatch / 2
	if a.Poll().Terminal != "" {
		t.Fatal("renderer consumed output while history queue was full")
	}
	a.historyQueuedBytes = 0
	a.historyWrite <- terminal.HistoryStatus{}
	var out strings.Builder
	for len(a.pending) > 0 {
		u := a.Poll()
		b, err := base64.StdEncoding.DecodeString(u.Terminal)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) > 16*1024 || len(b) == 0 {
			t.Fatal("unbounded or stalled update", len(b))
		}
		out.Write(b)
	}
	if out.String() != original {
		t.Fatal("backpressure lost output")
	}
}
func TestWindowHistoryCleanupAndIsolation(t *testing.T) {
	a := mouseApp(t)
	b := mouseApp(t)
	a.appendTerminalHistory([]json.RawMessage{json.RawMessage(`{"c":80,"w":false,"r":[["first",1,0,0]]}`)})
	b.appendTerminalHistory([]json.RawMessage{json.RawMessage(`{"c":80,"w":false,"r":[["second",2,0,0]]}`)})
	<-a.historyWrite
	a.historyWrite = nil
	<-b.historyWrite
	b.historyWrite = nil
	page, err := a.terminalHistory.Page(0, 10)
	if err != nil || !strings.Contains(string(page.Lines[0]), "first") {
		t.Fatal(page, err)
	}
	g := NewWindowGroup(a)
	g.CloseAll()
	if _, err = a.terminalHistory.Page(0, 10); err == nil {
		t.Fatal("history cleanup not awaited at quit")
	}
	page, err = b.terminalHistory.Page(0, 10)
	if err != nil || !strings.Contains(string(page.Lines[0]), "second") {
		t.Fatal("closing another window destroyed history", err)
	}
}
