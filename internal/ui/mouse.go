package ui

import "strings"

type mouseSelection struct {
	active                      bool
	panel, button, anchor, last int
	marked                      bool
	base                        map[string]bool
	path                        string
}

func (a *App) panelAt(x, y int) int {
	if y < 1 || y >= a.Rows-4 {
		return -1
	}
	for i := range a.Panels {
		px, w := a.panelRect(i)
		if w > 0 && !a.Panels[i].Hidden && x > px && x < px+w-1 {
			return i
		}
	}
	return -1
}
func (a *App) panelItem(x, y int) (int, int, bool) {
	panel := a.panelAt(x, y)
	if panel < 0 || y < 3 || y >= a.Rows-6 {
		return 0, 0, false
	}
	p := &a.Panels[panel]
	if p.View == "Info panel" || p.View == "Quick view" {
		return 0, 0, false
	}
	px, w := a.panelRect(panel)
	columns := panelColumns(p)
	column := min(columns-1, (x-px-1)/max(1, (w-2)/columns))
	index := p.Offset + column*(a.Rows-9) + y - 3
	return panel, index, index >= 0 && index < len(p.Entries)
}
func markRange(p *Panel, first, last int, value bool) {
	for i := max(0, min(first, last)); i <= min(len(p.Entries)-1, max(first, last)); i++ {
		name := p.Entries[i].Name
		if name == ".." {
			continue
		}
		if value {
			p.Marked[name] = true
		} else {
			delete(p.Marked, name)
		}
	}
}
func (a *App) panelMouse(m Message) {
	panel, index, ok := a.panelItem(m.X, m.Y)
	if !ok {
		return
	}
	a.Active = panel
	p := &a.Panels[panel]
	old := p.Cursor
	if a.mouseAnchorPath[panel] != p.Path {
		a.mouseAnchors[panel] = old
		a.mouseAnchorPath[panel] = p.Path
	}
	anchor := index
	base := cloneMarks(p.Marked)
	if m.Shift {
		anchor = max(0, min(a.mouseAnchors[panel], len(p.Entries)-1))
		markRange(p, anchor, index, true)
	} else {
		a.mouseAnchors[panel] = index
	}
	p.Cursor = index
	mark := true
	if m.Button == 2 || m.Meta {
		mark = !p.Marked[p.Entries[index].Name]
		markRange(p, index, index, mark)
	}
	a.mouse = mouseSelection{active: true, panel: panel, button: m.Button, anchor: anchor, last: index, marked: mark, base: base, path: p.Path}
	if m.Button == 0 && m.Clicks == 2 && !m.Shift && !m.Meta {
		a.mouse.active = false
		a.key(Message{Key: "Enter"})
	}
}
func (a *App) dragMouse(m Message) {
	if a.Busy || a.Form != nil || a.Dialog != nil || a.Menu != nil {
		return
	}
	if a.Doc != nil {
		if a.documentScrollbarDrag {
			m.X = a.Cols - 1
		}
		if m.Y < 1 || m.Y >= a.Rows-1 {
			step := 1
			if m.Y < 1 {
				step = -1
			}
			a.scrollMouse(Message{Delta: step})
			m.Y = max(1, min(a.Rows-2, m.Y))
		}
		a.documentClick(m, true)
		return
	}
	state := &a.mouse
	if !state.active {
		return
	}
	p := &a.Panels[state.panel]
	if p.Path != state.path {
		state.active = false
		return
	}
	x, w := a.panelRect(state.panel)
	m.X = max(x+1, min(x+w-2, m.X))
	if m.Y < 3 {
		p.Offset = max(0, p.Offset-1)
	}
	if m.Y >= a.Rows-6 {
		p.Offset = min(max(0, len(p.Entries)-(a.Rows-9)*panelColumns(p)), p.Offset+1)
	}
	m.Y = max(3, min(a.Rows-7, m.Y))
	_, index, ok := a.panelItem(m.X, m.Y)
	if !ok || index == state.last {
		return
	}
	if state.button == 2 {
		markRange(p, state.last, index, state.marked)
	} else {
		p.Marked = cloneMarks(state.base)
		markRange(p, state.anchor, index, true)
	}
	p.Cursor = index
	state.last = index
}
func (a *App) scrollMouse(m Message) {
	if a.Busy || a.Dialog != nil {
		return
	}
	delta := max(-12, min(12, m.Delta))
	if delta == 0 {
		return
	}
	if a.Form != nil {
		n := len(a.Form.Choice)
		if n == 0 {
			n = len(a.Form.Fields)
		}
		a.Form.Focus = max(0, min(n-1, a.Form.Focus+delta))
		return
	}
	if a.Menu != nil {
		n := len(a.menuSections()[a.Menu.Column].Items)
		a.Menu.Row = max(0, min(n-1, a.Menu.Row+delta))
		return
	}
	if d := a.Doc; d != nil {
		if !d.Edit && d.Mode != "text" {
			d.ByteOffset = max(0, min(d.Size-1, d.ByteOffset+int64(delta*a.binaryWidth())))
			return
		}
		if d.Wrap && !d.Edit {
			for i := 0; i < absInt(delta); i++ {
				if delta < 0 {
					d.ViewStart = d.previousVisualStart(a.Cols - 1)
				} else {
					rows := d.visualRows(a.Cols-1, 2)
					if len(rows) > 1 {
						d.ViewStart = rows[1].Start
					}
				}
			}
			d.Offset = d.Pos(d.ViewStart).Row
		} else {
			d.Offset = max(0, min(len(d.Lines())-1, d.Offset+delta))
			d.ViewStart = d.At(d.Offset, 0)
		}
		d.ManualScroll = true
		return
	}
	index := a.panelAt(m.X, m.Y)
	if index < 0 {
		return
	}
	p := &a.Panels[index]
	if p.View == "Info panel" {
		p.InfoOffset = max(0, p.InfoOffset+delta)
		a.dirty = true
		return
	}
	if p.View == "Quick view" {
		return
	}
	capacity := (a.Rows - 9) * panelColumns(p)
	p.Offset = max(0, min(max(0, len(p.Entries)-capacity), p.Offset+delta))
	p.Cursor = max(p.Offset, min(min(len(p.Entries)-1, p.Offset+capacity-1), p.Cursor))
}
func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func (a *App) copyPanelCommand(cut bool) {
	s := a.Command
	if s == "" && !cut {
		var names []string
		for _, e := range a.Panels[a.Active].Entries {
			if e.Name != ".." && a.Panels[a.Active].Marked[e.Name] {
				names = append(names, e.Name)
			}
		}
		if len(names) == 0 {
			p := &a.Panels[a.Active]
			if len(p.Entries) > 0 {
				names = append(names, p.Entries[p.Cursor].Name)
			}
		}
		s = strings.Join(names, "\n")
	}
	if s != "" {
		a.Clipboard = &s
	}
	if cut {
		a.Command = ""
	}
}
