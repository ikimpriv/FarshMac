package ui

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

type FastFind struct{ Query string }

func fastMatch(name, query string) bool {
	ok, _ := filepath.Match(strings.ToLower(query)+"*", strings.ToLower(filepath.Base(name)))
	return ok
}
func (a *App) findPrefix(query string, step int) bool {
	p := &a.Panels[a.Active]
	n := len(p.Entries)
	if n == 0 {
		return false
	}
	start := p.Cursor
	if step != 0 {
		start = (start + step + n) % n
	}
	for j := 0; j < n; j++ {
		i := (start + j + n) % n
		if step < 0 {
			i = (start - j + n) % n
		}
		if p.Entries[i].Name != ".." && fastMatch(p.Entries[i].Name, query) {
			p.Cursor = i
			return true
		}
	}
	return false
}
func (a *App) fastFindText(text string) {
	if a.FastFind == nil {
		a.FastFind = &FastFind{}
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			break
		}
		q := a.FastFind.Query + string(r)
		if !a.findPrefix(q, 0) && a.FastFind.Query != "" {
			break
		}
		a.FastFind.Query = q
	}
}
func (a *App) fastFindKey(m Message) bool {
	if a.FastFind == nil {
		if m.Alt && !m.Ctrl && !m.Meta && utf8.RuneCountInString(m.Key) == 1 {
			if !a.requireFilePanel() {
				return true
			}
			a.fastFindText(m.Key)

			return true
		}
		return false
	}
	f := a.FastFind
	switch m.Key {
	case "Escape":
		a.FastFind = nil
		return true
	case "Backspace":
		r := []rune(f.Query)
		if len(r) > 0 {
			f.Query = string(r[:len(r)-1])
			a.findPrefix(f.Query, 0)
		}
		return true
	case "Enter":
		if m.Meta || m.Ctrl {
			step := 1
			if m.Shift {
				step = -1
			}
			a.findPrefix(f.Query, step)
			return true
		}
		a.FastFind = nil
		a.enterFile()
		return true
	case "ArrowDown":
		a.findPrefix(f.Query, 1)
		return true
	case "ArrowUp":
		a.findPrefix(f.Query, -1)
		return true
	}
	if !m.Ctrl && !m.Meta && utf8.RuneCountInString(m.Key) == 1 {
		a.fastFindText(m.Key)
		return true
	}
	a.FastFind = nil
	return false
}
func (a *App) drawFastFind(g *Grid) {
	if a.FastFind == nil {
		return
	}
	w := min(40, g.W-4)
	x := 2
	if a.Active == 1 {
		x = max(2, g.W-w-2)
	}
	y := g.H - 7
	g.Box(x, y, w, 3, Black, Gray)
	g.Text(x+2, y, " Find ", Black, Gray, w-4)
	g.Fill(x+2, y+1, w-4, 1, Black, Cyan)
	g.Text(x+2, y+1, tail(a.FastFind.Query, w-5)+"▌", Black, Cyan, w-4)
}
