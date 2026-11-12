package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// listFilter is the type-ahead state of a choose() list.
type listFilter struct {
	Query      string
	all        []string
	index      []int // visible row -> original choice index
	freeScroll bool  // wheel scrolled; do not pull the view back to Focus
}

// choiceIndex maps the focused visible row to the index passed to Select.
func (f *Form) choiceIndex(row int) int {
	if f.index != nil && row >= 0 && row < len(f.index) {
		return f.index[row]
	}
	return row
}

// listKey handles keys specific to choose() lists: paging, repeated
// mnemonics and incremental filtering. It reports whether m was consumed.
func (a *App) listKey(m Message) bool {
	f := a.Form
	if len(f.Choice) == 0 || f.Buttons {
		return false
	}
	f.freeScroll = false
	switch {
	case m.Key == "Escape" && f.Query != "":
		a.setListFilter("")
		return true
	case m.Key == "Backspace" && f.Query != "":
		r := []rune(f.Query)
		a.setListFilter(string(r[:len(r)-1]))
		return true
	case m.Key == "PageDown" || m.Key == "PageUp":
		page := max(1, a.listRows())
		if m.Key == "PageUp" {
			page = -page
		}
		f.Focus = max(0, min(len(f.Choice)-1, f.Focus+page))
		return true
	case m.Alt && !m.Meta && !m.Ctrl && utf8.RuneCountInString(m.Key) == 1:
		pressed := unicode.ToLower([]rune(m.Key)[0])
		keys := f.mnemonics()
		for step := 1; step <= len(f.Choice); step++ {
			i := (f.Focus + step) % len(f.Choice)
			first, _ := utf8.DecodeRuneInString(strings.TrimLeft(f.Choice[i], "✓ "))
			if i < len(keys) && keys[i] == pressed || unicode.ToLower(first) == pressed {
				f.Focus = i
				return true
			}
		}
		return false
	case !m.Alt && !m.Meta && !m.Ctrl && utf8.RuneCountInString(m.Key) == 1 && (m.Key != " " || f.Query != ""):
		if !a.setListFilter(f.Query + m.Key) {
			a.Status = "No items match " + f.Query + m.Key
		}
		return true
	}
	return false
}

// setListFilter shows only choices containing query (case-insensitive). An
// extension that matches nothing is refused, like panel fast find.
func (a *App) setListFilter(query string) bool {
	f := a.Form
	if f.all == nil {
		f.all = f.Choice
	}
	var visible []string
	var index []int
	needle := strings.ToLower(query)
	for i, c := range f.all {
		if strings.Contains(strings.ToLower(c), needle) {
			visible = append(visible, c)
			index = append(index, i)
		}
	}
	if len(visible) == 0 {
		return false
	}
	current := f.choiceIndex(f.Focus)
	f.Query, f.Choice, f.index = query, visible, index
	if query == "" {
		f.index = nil
	}
	f.Focus, f.Scroll = 0, 0
	for row, original := range index {
		if original == current {
			f.Focus = row
		}
	}
	return true
}
func (a *App) listRows() int {
	box := a.formGeometry()
	return box.Bottom - box.Top
}

// listWheel scrolls a list without moving the focused item.
func (a *App) listWheel(delta int) {
	f := a.Form
	f.freeScroll = true
	f.Scroll = max(0, min(max(0, len(f.Choice)-a.listRows()), f.Scroll+delta))
}
