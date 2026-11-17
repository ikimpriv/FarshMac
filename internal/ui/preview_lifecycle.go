package ui

import "fmt"

// quickPreviewTarget identifies the source, not the active panel: focusing the
// Quick view itself must not interrupt the work needed to display it.
func (a *App) quickPreviewTarget() string {
	if a.traffic != nil || a.compactMetrics != nil || a.Doc != nil || a.Image != nil || a.TermVisible || a.Busy {
		return ""
	}
	for i := range a.Panels {
		p := &a.Panels[i]
		if p.View != "Quick view" || p.Hidden {
			continue
		}
		source := &a.Panels[1-i]
		if source.load != nil || len(source.Entries) == 0 || source.Cursor < 0 || source.Cursor >= len(source.Entries) {
			return ""
		}
		e := source.Entries[source.Cursor]
		key := fmt.Sprintf("%d:%s:%s:%s:%p:%p:%p", i, source.label(), source.Path, source.Current(), source.Kube, source.Docker, source.Archive)
		if e.Info != nil {
			key += fmt.Sprint(e.Info.Size(), e.Info.ModTime())
		}
		return key
	}
	return ""
}

// Called immediately after input, and before processing background results.
// Detach canceled jobs so a slow backend cannot block the next selection.
func (a *App) syncQuickPreviewTarget() {
	target := a.quickPreviewTarget()
	if target == a.previewTarget {
		return
	}
	a.previewTarget = target
	if a.previewOperation != nil {
		a.previewOperation.Cancel()
	}
	a.previewOperation = nil
	a.previewPending = false
	a.previewKey, a.previewPath, a.previewImage, a.previewText, a.previewSent = "", "", "", "", ""
	a.previewOffset = 0
	if a.Doc == nil && a.markdownCancel != nil {
		a.markdownCancel()
	}
	a.dirty = true
}
