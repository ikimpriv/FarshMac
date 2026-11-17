package ui

import (
	"context"
	"farshmac/internal/fsops"
	"testing"
	"time"
)

func previewLifecycleApp(root string) *App {
	a := &App{jobs: make(chan func(), 16)}
	a.Panels[0] = Panel{Path: root, Entries: []Entry{{Name: "first", Dir: true}, {Name: "second", Dir: true}}}
	a.Panels[1] = Panel{Path: root}
	a.Panels[1].View = "Quick view"
	a.syncQuickPreviewTarget()
	return a
}

func TestQuickPreviewContextCancellation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*App)
	}{
		{"cursor", func(a *App) { a.Panels[0].Cursor = 1 }},
		{"folder", func(a *App) { a.Panels[0].Path += "/other" }},
		{"hidden", func(a *App) { a.Panels[1].Hidden = true }},
		{"closed", func(a *App) { a.Panels[1].View = "Full" }},
		{"terminal", func(a *App) { a.TermVisible = true }},
		{"empty", func(a *App) { a.Panels[0].Entries = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := previewLifecycleApp(t.TempDir())
			o := fsops.NewOperation("preview")
			a.previewOperation = o
			a.previewPending = true
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a.markdownCancel = cancel
			tc.change(a)
			a.syncQuickPreviewTarget()
			if o.Check() == nil || ctx.Err() == nil || a.previewPending || a.previewOperation != nil {
				t.Fatal("preview was not canceled and detached immediately")
			}
		})
	}
	a := previewLifecycleApp(t.TempDir())
	o := fsops.NewOperation("preview")
	defer o.Cancel()
	a.previewOperation = o
	a.Active = 1
	a.syncQuickPreviewTarget()
	if o.Check() != nil {
		t.Fatal("focusing Quick view canceled its own preview")
	}
}

func TestStaleFolderPreviewCannotFinishNewRequest(t *testing.T) {
	a := previewLifecycleApp(t.TempDir())
	a.updateFolderPreview(&a.Panels[0])
	old := a.previewOperation
	var finish func()
	select {
	case finish = <-a.jobs:
	case <-time.After(3 * time.Second):
		t.Fatal("preview did not finish")
	}
	a.Panels[0].Cursor = 1
	a.syncQuickPreviewTarget()
	current := fsops.NewOperation("new preview")
	defer current.Cancel()
	a.previewOperation = current
	a.previewPending = true
	a.previewText = "new result"
	finish()
	if old.Check() == nil || a.previewOperation != current || !a.previewPending || a.previewText != "new result" {
		t.Fatal("stale completion modified new preview")
	}
}
