package ui

import (
	"path/filepath"
	"strings"
)

func (a *App) changeSelection(includeFolders bool, action func(Entry, bool) bool) {
	p := &a.Panels[a.Active]
	p.PreviousMarked = cloneMarks(p.Marked)
	p.RestoreMarked = cloneMarks(p.Marked)
	if p.Marked == nil {
		p.Marked = map[string]bool{}
	}
	for _, e := range p.Entries {
		if e.Name == ".." || e.Dir && !includeFolders {
			continue
		}
		if action(e, p.Marked[e.Name]) {
			p.Marked[e.Name] = true
		} else {
			delete(p.Marked, e.Name)
		}
	}
}
func (a *App) selectionKey(m Message) bool {
	p := &a.Panels[a.Active]
	if p.View == "Quick view" || p.View == "Info panel" || p.View == "Tree panel" {
		return false
	}
	if (m.Ctrl || m.Meta) && !m.Alt && !m.Shift && strings.EqualFold(m.Key, "m") {
		a.selectGroup("Restore")
		return true
	}
	key := m.Key
	numpad := true
	switch m.Code {
	case "NumpadAdd":
		key = "+"
	case "NumpadSubtract":
		key = "-"
	case "NumpadMultiply":
		key = "*"
	default:
		numpad = false
	}
	if key != "+" && key != "-" && key != "*" {
		return false
	}
	if !numpad && a.Command != "" {
		return false
	}
	a.FastFind = nil
	cmd := m.Ctrl || m.Meta
	if key == "*" {
		a.changeSelection(a.Settings.SelectFolders || cmd || m.Alt, func(e Entry, on bool) bool {
			if m.Alt && e.Dir {
				return false
			}
			return !on
		})
		return true
	}
	selectIt := key == "+"
	if cmd || m.Alt {
		if len(p.Entries) == 0 || p.Entries[p.Cursor].Name == ".." {
			return true
		}
		current := p.Entries[p.Cursor].Name
		part := func(name string) string {
			ext := filepath.Ext(name)
			if cmd {
				return strings.ToLower(ext)
			}
			return strings.ToLower(strings.TrimSuffix(name, ext))
		}
		wanted := part(current)
		a.changeSelection(a.Settings.SelectFolders, func(e Entry, on bool) bool {
			if part(e.Name) == wanted {
				return selectIt
			}
			return on
		})
		return true
	}
	if numpad && m.Shift {
		a.changeSelection(a.Settings.SelectFolders, func(Entry, bool) bool { return selectIt })
		return true
	}
	if selectIt {
		a.selectGroup("Select")
	} else {
		a.selectGroup("Deselect")
	}
	return true
}
