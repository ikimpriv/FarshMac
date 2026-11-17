package ui

import (
	"strings"
	"testing"
	"time"
)

func TestMarkdownViewerAndQuickView(t *testing.T) {
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Doc = newDocument("readme.md", "# Title\n\n| A | B |\n| --- | --- |\n| one | two |")
	p := a.markdownPreview()
	if p == nil || !p.Full {
		t.Fatal(p)
	}
	deadline := time.Now().Add(3 * time.Second)
	for a.markdownPending && time.Now().Before(deadline) {
		select {
		case fn := <-a.jobs:
			fn()
		default:
			time.Sleep(time.Millisecond)
		}
	}
	p = a.markdownPreview()
	if p == nil || !strings.Contains(p.HTML, "<table>") {
		t.Fatal(p)
	}
	a.Form = &Form{Title: "dialog", Choice: []string{"OK"}}
	p = a.markdownPreview()
	if p == nil || !p.Blocked {
		t.Fatal("dialog hid Markdown")
	}
	a.Form = nil
	a.key(Message{Key: "M", Meta: true, Shift: true})
	if a.markdownPreview() != nil {
		t.Fatal("source toggle failed")
	}
	a.key(Message{Key: "M", Meta: true, Shift: true})
	if a.markdownPreview() == nil {
		t.Fatal("formatted toggle failed")
	}
	a.Doc.Edit = true
	if a.markdownPreview() != nil {
		t.Fatal("editor overlaid")
	}
	a.Doc = nil
	a.Panels[1].View = "Quick view"
	a.previewPath = a.Panels[0].Current()
	if a.previewPath == "" {
		a.previewPath = "missing.md"
	}
	// Quick view must never reuse a rendered document for a different selection.
	a.previewPath = "/different.md"
	a.previewText = "# stale"
	if a.markdownPreview() != nil {
		t.Fatal("stale selection")
	}
}
