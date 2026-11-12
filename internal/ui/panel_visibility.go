package ui

import (
	"path/filepath"
	"strconv"
	"strings"
)

// copyPanelNames is shared by Ctrl+Insert and ⌘C with an empty command line.
// Marked entries (or the entry under the cursor, never "..") are copied one
// per line; full paths when full is set, resolved through symlinks (or with
// the remote label) when resolve is set. With nothing to copy, the panel path
// is copied instead.
func (a *App) copyPanelNames(full, resolve bool) {
	if !a.requireFilePanel() {
		return
	}
	p := &a.Panels[a.Active]
	var names []string
	for _, path := range p.Selected() {
		switch {
		case full && resolve && p.Remote != nil:
			path = p.Remote.Label() + path
		case full && resolve:
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				a.error(err)
				return
			}
			path = resolved
		case !full && p.View == "Tree panel":
			if rel, err := filepath.Rel(p.Path, path); err == nil {
				path = rel
			}
		case !full:
			path = filepath.Base(path)
		}
		names = append(names, path)
	}
	value := strings.Join(names, "\n")
	switch len(names) {
	case 0:
		value = p.Path
		a.Status = "Copied panel path"
	case 1:
		a.Status = "Copied 1 name"
	default:
		a.Status = "Copied " + strconv.Itoa(len(names)) + " names"
	}
	a.Clipboard = &value
}

// filePanelHidden reports whether the panel's file entries are invisible:
// Quick view and Info panel keep the previous listing and cursor in memory,
// and a hidden panel shows nothing at all. Keys that act on entries must not
// reach such a panel, otherwise they would launch, copy or delete files the
// user cannot see.
func (p *Panel) filePanelHidden() bool {
	return p.Hidden || p.View == "Quick view" || p.View == "Info panel"
}

// requireFilePanel returns true when the active panel shows its entries. It
// otherwise leaves the panel untouched and explains how to reach a file panel.
func (a *App) requireFilePanel() bool {
	if !a.Panels[a.Active].filePanelHidden() {
		return true
	}
	a.FastFind = nil
	a.Status = "Switch to a file panel first (Tab)"
	return false
}

// switchPanels implements Tab: focus never moves to a hidden panel. When both
// panels are hidden, Tab reveals the active one instead.
func (a *App) switchPanels() {
	if !a.Panels[1-a.Active].Hidden {
		a.Active = 1 - a.Active
		return
	}
	if a.Panels[a.Active].Hidden {
		a.Panels[a.Active].Hidden = false
		return
	}
	a.Status = "Other panel is hidden (⌘F1/⌘F2 shows it)"
}

// quickViewClick activates a Quick view panel on a single click, matching the
// Info panel. It never moves the hidden cursor or opens anything.
func (a *App) quickViewClick(m Message) bool {
	i := a.panelAt(m.X, m.Y)
	if i < 0 || a.Panels[i].View != "Quick view" {
		return false
	}
	if m.Button == 0 {
		a.Active = i
	}
	return true
}
