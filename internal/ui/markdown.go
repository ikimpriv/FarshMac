package ui

import (
	"context"
	"crypto/sha256"
	"farshmac/internal/markdown"
	"farshmac/internal/remotefs"
	"fmt"
	"time"
)

type MarkdownPreview struct {
	HTML                  string `json:"html,omitempty"`
	Key                   string `json:"key"`
	Theme                 string `json:"theme"`
	Full, Active, Blocked bool
	X, Y, W, H            int
	Occlusions            []modalRect `json:"occlusions,omitempty"`
}

func (a *App) markdownPreview() (result *MarkdownPreview) {
	defer func() {
		if result == nil {
			a.markdownSent = ""
			if a.markdownCancel != nil {
				a.markdownCancel()
			}
		}
	}()
	if a.TermVisible || a.Image != nil || a.traffic != nil || a.Metrics != nil {
		return nil
	}
	text, name := "", ""
	cacheScope := a.previewKey
	var opener remotefs.Opener
	imagesAllowed := true
	p := &MarkdownPreview{Theme: a.Settings.ColorTheme, Blocked: a.Form != nil || a.Dialog != nil || a.Menu != nil || a.Busy}
	if d := a.Doc; d != nil {
		if d.Edit || d.markdownRaw || d.Mode != "text" || !markdown.Supported(d.Path) {
			return nil
		}
		text, name = d.Text, d.Path
		cacheScope = fmt.Sprint(d.ScreenID)
		if remote, ok := d.Source.(*remoteSource); ok {
			name = remote.Path
			opener = remote.Opener
		} else if d.Source != nil || d.ArchiveSource {
			imagesAllowed = false
		}
		p.Full = true
		p.Active = true
		p.X = 0
		p.Y = 1
		p.W = a.Cols
		p.H = a.Rows - 2
	} else {
		if a.compactMetrics != nil || a.previewPending || !markdown.Supported(a.previewPath) {
			return nil
		}
		for i := range a.Panels {
			panel := &a.Panels[i]
			source := &a.Panels[1-i]
			if len(source.Entries) > 0 && source.Cursor >= 0 && source.Cursor < len(source.Entries) && source.Entries[source.Cursor].Dir {
				continue
			}
			if panel.View == "Quick view" && !panel.Hidden && a.Panels[1-i].Current() == a.previewPath {
				x, w := a.panelRect(i)
				p.X = x + 1
				p.Y = 2
				p.W = w - 2
				p.H = a.Rows - 7
				p.Active = a.Active == i
				text, name = a.previewText, a.previewPath
				opener = source.endpoint().Opener
				break
			}
		}
		if name == "" {
			return nil
		}
	}
	if p.W < 4 || p.H < 3 {
		return nil
	}
	// Bound hashing and conversion. One worker per window prevents rapid cursor
	// movement from spawning a conversion for every item in a large directory.
	if len(text) > markdown.Limit+1 {
		text = text[:markdown.Limit+1]
	}
	key := fmt.Sprintf("%s:%s:%x", cacheScope, name, sha256.Sum256([]byte(text)))
	if opener != nil {
		key = opener.Identity() + ":" + key
	}
	p.Key = key
	stage := fmt.Sprintf("%s:%d", key, a.markdownVersion)
	if a.markdownKey != key {
		p.HTML = "<p>Rendering Markdown…</p>"
		stage = key + ":loading"
		if a.markdownPending && a.markdownWorkKey != key && a.markdownCancel != nil {
			a.markdownCancel()
		}
		if !a.markdownPending {
			a.markdownPending = true
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			a.markdownCancel = cancel
			a.markdownWorkKey = key
			go func() {
				defer cancel()
				rendered := markdown.Render(text)
				initialHTML := rendered
				a.jobs <- func() {
					if ctx.Err() == nil {
						a.markdownKey = key
						a.markdownHTML = initialHTML
						a.markdownVersion++
						a.dirty = true
					}
				}
				if imagesAllowed && ctx.Err() == nil {
					rendered = markdown.RenderImages(text, markdownImages(ctx, name, opener))
				}
				valid := ctx.Err() != context.Canceled
				finalHTML := rendered
				a.jobs <- func() {
					if !valid && a.markdownKey == key {
						a.markdownKey = ""
					}
					if valid {
						a.markdownKey = key
						a.markdownHTML = finalHTML
						a.markdownVersion++
					}
					a.markdownPending = false
					a.dirty = true
				}
			}()
		}
	} else {
		p.HTML = a.markdownHTML
	}
	if a.markdownSent == stage {
		p.HTML = ""
	} else {
		a.markdownSent = stage
	}
	return p
}

func markdownReadLimit(path string, fallback int64) int64 {
	if markdown.Supported(path) {
		return markdown.Limit + 1
	}
	return fallback
}
