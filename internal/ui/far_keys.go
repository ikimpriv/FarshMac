package ui

import (
	"farshmac/internal/fsops"
	"farshmac/internal/terminal"
	"path/filepath"
	"strings"
	"unicode"
)

type panelViewReturn struct {
	View   string
	Hidden bool
}

func (a *App) toggleOtherView(view string) {
	p := &a.Panels[1-a.Active]
	if p.Remote != nil && view != "Quick view" {
		a.Status = "This panel view requires a local folder"
		return
	}
	if p.Archive != nil && view == "Tree panel" {
		a.Status = "Tree view is unavailable inside archives"
		return
	}
	p.cancelTree()
	p.cancelPanelLoad()
	if p.View == view {
		old := p.viewReturns[view]
		p.View = old.View
		p.Hidden = old.Hidden
		delete(p.viewReturns, view)
	} else {
		if p.viewReturns == nil {
			p.viewReturns = map[string]panelViewReturn{}
		}
		p.viewReturns[view] = panelViewReturn{p.View, p.Hidden}
		p.View = view
		p.Hidden = false
	}

	if p.View == "Tree panel" {
		a.error(p.Load(p.Path))
	}
	a.previewKey = ""
	a.previewImage = ""
	if a.previewOperation != nil {
		a.previewOperation.Cancel()
	}
}
func (a *App) appendPanelValue(value string) {
	if value == "" {
		return
	}
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		a.Status = "Cannot insert a path containing control characters"
		return
	}
	if a.Command != "" && !strings.HasSuffix(a.Command, " ") {
		a.Command += " "
	}
	a.Command += terminal.Quote(value) + " "
}
func (a *App) currentTransfer(move bool) {
	p := &a.Panels[a.Active]
	marked, restore := p.Marked, p.RestoreMarked
	p.Marked = map[string]bool{}
	a.transferDialog(move)
	p.Marked, p.RestoreMarked = marked, restore
}
func (a *App) farPanelKey(m Message) bool {
	p := &a.Panels[a.Active]
	cmd := m.Ctrl || m.Meta
	k := strings.ToLower(m.Key)
	if cmd && m.Alt && !m.Shift && m.Key == "F3" {
		a.calculateFolderSizes(true)
		return true
	}
	if m.Shift && !cmd && !m.Alt {
		switch m.Key {
		case "F1":
			a.archiveCreate()
			return true
		case "F2":
			a.archiveExtract()
			return true
		case "F3":
			a.archiveCommands()
			return true
		case "F5":
			a.currentTransfer(false)
			return true
		case "F6":
			a.currentTransfer(true)
			return true
		case "F9":
			a.saveSetup()
			return true
		case "F11":
			p.DisableSortGroups = !p.DisableSortGroups
			a.error(p.Load(p.Path))
			return true
		case "F12":
			p.SelectedFirst = !p.SelectedFirst
			a.error(p.Load(p.Path))
			return true
		case "ArrowUp", "ArrowDown", "PageUp", "PageDown", "Home", "End":
			if len(p.Entries) == 0 {
				return true
			}
			old := p.Cursor
			to := old
			step := max(1, (a.Rows-9)*panelColumns(p))
			switch m.Key {
			case "ArrowUp":
				to--
			case "ArrowDown":
				to++
			case "PageUp":
				to -= step
			case "PageDown":
				to += step
			case "Home":
				to = 0
			case "End":
				to = len(p.Entries) - 1
			}
			to = max(0, min(len(p.Entries)-1, to))
			on := !p.Marked[p.Entries[old].Name]
			p.RestoreMarked = cloneMarks(p.Marked)
			if p.Marked == nil {
				p.Marked = map[string]bool{}
			}
			direction := 1
			if to < old {
				direction = -1
			}
			for i := old; i != to; i += direction {
				if p.Entries[i].Name != ".." {
					p.Marked[p.Entries[i].Name] = on
				}
			}
			p.Cursor = to
			return true
		}
	}
	if m.Alt && !cmd && !m.Shift {
		switch m.Key {
		case "F9":
			a.Zoom = true
			return true
		case "F6":
			a.createLink()
			return true
		case "F11":
			a.historyMenu("Files")
			return true
		case "F12":
			a.historyMenu("Folders")
			return true
		}
	}
	if m.Key == "Insert" && (cmd || m.Alt && m.Shift) {
		value := a.Command
		if value == "" || m.Shift || m.Alt {
			var names []string
			for _, path := range p.Selected() {
				if m.Alt {
					if m.Ctrl {
						if p.Remote != nil {
							path = p.Remote.Label() + path
						} else {
							resolved, err := filepath.EvalSymlinks(path)
							if err != nil {
								a.error(err)
								return true
							}
							path = resolved
						}
					}
					names = append(names, path)
				} else {
					names = append(names, filepath.Base(path))
				}
			}
			if len(names) == 0 {
				names = []string{p.Path}
			}
			value = strings.Join(names, "\n")
		}
		a.Clipboard = &value
		return true
	}
	if !cmd && !m.Alt && !m.Shift && m.Code == "Numpad5" {
		a.function(3)
		return true
	}
	if !cmd || m.Alt {
		return false
	}
	if !m.Shift {
		sorts := map[string]string{"F3": "Name", "F4": "Extension", "F5": "Modified", "F6": "Size", "F7": "Unsorted", "F8": "Created", "F9": "Accessed", "F10": "Description", "F11": "Owner"}
		if mode, ok := sorts[m.Key]; ok {
			if p.Remote != nil && (mode == "Created" || mode == "Accessed" || mode == "Owner" || mode == "Description") {
				a.Status = "This sort mode requires local file metadata"
				return true
			}
			if p.Sort == mode {
				p.Reverse = !p.Reverse
			} else {
				p.Sort = mode
				p.Reverse = false
			}
			a.error(p.Load(p.Path))
			return true
		}
		if m.Key == "F12" {
			a.sortMenu(a.Active)
			return true
		}
		switch k {
		case "q":
			if m.Ctrl && !m.Meta {
				a.toggleOtherView("Quick view")
				return true
			}
		case "l":
			if m.Ctrl && !m.Meta {
				a.toggleOtherView("Info panel")
				return true
			}
		case "t":
			a.toggleOtherView("Tree panel")
			return true
		case "p":
			a.togglePanel(1 - a.Active)
			return true
		case "b":
			a.Settings.ShowKeys = !a.Settings.ShowKeys
			return true
		case "r":
			a.rereadPanel(a.Active)
			return true
		case "h":
			if m.Ctrl && !m.Meta {
				a.Settings.ShowHidden = !a.Settings.ShowHidden
				for i := range a.Panels {
					a.Panels[i].ForcedVisiblePath = ""
					a.configurePanel(i)
				}
				a.refresh()
				return true
			}
		case "y":
			a.Command = ""
			return true
		case "e", "x":
			if m.Ctrl && !m.Meta {
				if k == "e" {
					a.historyAt = max(0, a.historyAt-1)
				} else {
					a.historyAt = min(len(a.history), a.historyAt+1)
				}
				a.Command = ""
				if a.historyAt < len(a.history) {
					a.Command = a.history[a.historyAt]
				}
				return true
			}
		case "j":
			a.insertFilename()
			return true
		case "f":
			a.appendPanelValue(p.Current())
			return true
		case ";":
			a.appendPanelValue(a.Panels[1-a.Active].Current())
			return true
		}
	}
	if m.Key == "Enter" {
		if m.Shift {
			other := &a.Panels[1-a.Active]
			if len(other.Entries) > 0 {
				a.appendPanelValue(other.Entries[other.Cursor].Name)
			}
		} else {
			a.insertFilename()
		}
		return true
	}
	if m.Shift && k == "s" {
		a.Settings.HumanReadableSizes = !a.Settings.HumanReadableSizes
		for i := range a.Panels {
			a.configurePanel(i)
		}
		return true
	}
	if m.Code == "BracketLeft" || m.Key == "[" || m.Key == "{" {
		index := 0
		if m.Shift {
			index = a.Active
		}
		a.appendPanelValue(a.Panels[index].Path)
		return true
	}
	if m.Code == "BracketRight" || m.Key == "]" || m.Key == "}" {
		index := 1
		if m.Shift {
			index = 1 - a.Active
		}
		a.appendPanelValue(a.Panels[index].Path)
		return true
	}
	if !m.Shift && m.Key == "PageUp" {
		a.error(p.Load(filepath.Dir(p.Path)))
		return true
	}
	if !m.Shift && m.Key == "PageDown" {
		if len(p.Entries) > 0 && (p.Entries[p.Cursor].Dir || fsops.ArchiveFormat(p.Entries[p.Cursor].Name) != "") {
			a.enterFile()
		}
		return true
	}
	if !m.Shift && a.Command == "" && (m.Key == "ArrowLeft" || m.Key == "ArrowRight") {
		if a.panelSplit == 0 {
			a.panelSplit = 50
		}
		delta := 2
		if m.Key == "ArrowLeft" {
			delta = -2
		}
		a.panelSplit = max(20, min(80, a.panelSplit+delta))
		return true
	}
	if m.Code == "Numpad5" {
		a.panelSplit = 0
		return true
	}
	return false
}
