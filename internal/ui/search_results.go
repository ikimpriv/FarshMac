package ui

import (
	"farshmac/internal/fsops"
	"farshmac/internal/preview"
	"fmt"
	"github.com/mattn/go-runewidth"
	"strings"
)

type searchHit struct {
	path  string
	entry Entry
}

// The original list object retains its filter, scroll and selected result.
func (a *App) restoreSearchResults() {
	if a.searchResults != nil && a.Doc == nil && a.Image == nil && !a.Busy && a.Form == nil && a.Dialog == nil {
		a.Form = a.searchResults
		a.searchResults = nil
		a.dirty = true
	}
}
func (a *App) viewSearchHit(source Panel, hit searchHit) {
	if obj, ok := hit.entry.Info.(*kubeEntry); ok && obj.object != nil && source.Kube != nil {
		a.openSourceDocument(&manifestSource{k: source.Kube, ref: obj.object.Ref()}, false, a.showDocument, nil)
		return
	}
	endpoint := source.endpoint()
	if source.Docker != nil && endpoint.Opener == nil {
		a.backgroundProgress("Reading object", func(o *fsops.Operation) (func(), error) {
			text, err := source.Docker.describe(o.Context(), hit.path)
			return func() { a.report(hit.path, text) }, err
		})
		return
	}
	if hit.entry.Dir {
		a.backgroundProgress("Reading directory", func(o *fsops.Operation) (func(), error) {
			b, err := source.browse(o)
			if err != nil {
				return nil, err
			}
			defer b.Close()
			files, dirs := 0, 0
			stats, err := walkBrowse(hit.path, b, o, browseOptions{Recursive: true}, func(_ string, e Entry) error {
				if e.Dir {
					dirs++
				} else {
					files++
				}
				return nil
			})
			text := fmt.Sprintf("%s\n\nFolders: %d\nFiles: %d\nUnreadable: %d\nBoundaries skipped: %d\nLimited: %t", hit.path, dirs, files, stats.Skipped, stats.Boundaries, stats.Limited)
			return func() { a.report(hit.path, text) }, err
		})
		return
	}
	if source.Archive != nil {
		a.viewArchivePath(source.Archive, hit.path)
		return
	}
	if endpoint.Opener != nil {
		a.openSourceDocument(&remoteSource{Opener: endpoint.Opener, Label: source.label(), Path: hit.path}, false, a.showDocument, nil)
		return
	}
	if preview.Supported(hit.path) {
		a.openImage(hit.path)
		return
	}
	a.openDocument(hit.path, false)
}

// searchResultLabel compresses only tree indentation, never the backing result.
// +N reports hidden ancestor levels; filtering and F3 retain the original row.
func searchResultLabel(label string, width int, tree bool) string {
	if tree {
		prefix := 0
		for strings.HasPrefix(label[prefix:], "   ") || strings.HasPrefix(label[prefix:], "│  ") {
			if strings.HasPrefix(label[prefix:], "│  ") {
				prefix += len("│  ")
			} else {
				prefix += 3
			}
		}
		if prefix > 0 && (strings.HasPrefix(label[prefix:], "├─ ") || strings.HasPrefix(label[prefix:], "└─ ")) {
			indent := label[:prefix]
			levels := runewidth.StringWidth(indent) / 3
			keep := max(0, min(4, width/12))
			if levels > keep {
				label = fmt.Sprintf("…(+%d) ", levels-keep) + strings.Repeat("   ", keep) + label[prefix:]
			}
		}
	}
	return runewidth.Truncate(label, max(1, width), "…")
}
