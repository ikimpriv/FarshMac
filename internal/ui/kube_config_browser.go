package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"farshmac/internal/kube"
)

type kubeConfigBrowser struct {
	sources                []kube.ConfigSource
	names                  []string
	master, detail, column int
	scroll                 [2]int
	selectContext          func(int, int)
	other                  func()
}

func (b *kubeConfigBrowser) contexts() []string {
	if len(b.sources) == 0 {
		return nil
	}
	return b.sources[b.master].Contexts
}
func (b *kubeConfigBrowser) selectMaster(i int) {
	b.master = max(0, min(len(b.sources)-1, i))
	b.detail = 0
	b.scroll[1] = 0
	for j, name := range b.contexts() {
		if name == b.sources[b.master].Current {
			b.detail = j
		}
	}
}
func (a *App) kubeBrowserRect() modalRect {
	return modalBounds(a.Cols, a.Rows, 112, min(30, max(14, a.Rows-4)))
}
func (a *App) drawKubeConfigBrowser(g *Grid) {
	b := a.Form.ConfigBrowser
	r := a.kubeBrowserRect()
	drawModal(g, r, a.Form.Title)
	mid := r.X + r.W/2
	top := r.Y + 4
	count := max(1, r.H-10)
	g.Text(r.X+4, r.Y+2, "Configuration files", Black, Gray, r.W/2-5)
	g.Text(mid+2, r.Y+2, "Contexts", Black, Gray, r.W/2-6)
	for y := r.Y + 2; y < r.Y+r.H-5; y++ {
		g.Set(mid, y, "│", Black, Gray)
	}
	lists := [][]string{b.names, b.contexts()}
	selected := []int{b.master, b.detail}
	for col, list := range lists {
		b.scroll[col] = max(0, min(b.scroll[col], max(0, len(list)-count)))
		if selected[col] < b.scroll[col] {
			b.scroll[col] = selected[col]
		}
		if selected[col] >= b.scroll[col]+count {
			b.scroll[col] = selected[col] - count + 1
		}
		x, w := r.X+4, r.W/2-5
		if col == 1 {
			x, w = mid+2, r.X+r.W-4-(mid+2)
		}
		for row := 0; row < count && b.scroll[col]+row < len(list); row++ {
			i := b.scroll[col] + row
			bg := uint32(Gray)
			if i == selected[col] {
				bg = Cyan
			}
			fg := uint32(Black)
			prefix := "  "
			if col == b.column && i == selected[col] {
				prefix = "› "
			}
			text := list[i]
			if col == 1 && text == b.sources[b.master].Current {
				text = "✓ " + text
			}
			g.Fill(x, top+row, w, 1, fg, bg)
			g.Text(x, top+row, prefix+text, fg, bg, w)
		}
	}
	if len(b.sources) > 0 {
		g.Text(r.X+4, r.Y+r.H-5, b.sources[b.master].Path, Black, Gray, r.W-8)
	}
	modalRule(g, r, r.Y+r.H-4)
	drawModalButtons(g, r, r.Y+r.H-3, []string{"Connect", "Other file…", "Cancel"}, -1, nil)
	g.Text(r.X+4, r.Y+r.H-2, "Tab: switch list · Enter: select · F4: other file · Esc: cancel", Black, Gray, r.W-8)
}
func (a *App) kubeBrowserConnect() {
	b := a.Form.ConfigBrowser
	if len(b.contexts()) == 0 {
		return
	}
	a.Form = nil
	b.selectContext(b.master, b.detail)
}
func (a *App) kubeBrowserKey(m Message) {
	b := a.Form.ConfigBrowser
	switch m.Key {
	case "Escape":
		a.Form = nil
		return
	case "F4":
		a.Form = nil
		b.other()
		return
	case "Tab":
		b.column = 1 - b.column
		return
	case "ArrowLeft":
		b.column = 0
		return
	case "ArrowRight":
		b.column = 1
		return
	case "Enter":
		if b.column == 0 {
			b.column = 1
		} else {
			a.kubeBrowserConnect()
		}
		return
	}
	at, n := b.master, len(b.sources)
	if b.column == 1 {
		at, n = b.detail, len(b.contexts())
	}
	switch m.Key {
	case "ArrowDown":
		at++
	case "ArrowUp":
		at--
	case "PageDown":
		at += max(1, a.kubeBrowserRect().H-10)
	case "PageUp":
		at -= max(1, a.kubeBrowserRect().H-10)
	case "Home":
		at = 0
	case "End":
		at = n - 1
	default:
		if utf8.RuneCountInString(m.Key) != 1 || m.Meta || m.Ctrl || m.Alt {
			return
		}
		list := b.names
		if b.column == 1 {
			list = b.contexts()
		}
		for step := 1; step <= n; step++ {
			i := (at + step) % n
			if strings.HasPrefix(strings.ToLower(list[i]), strings.ToLower(m.Key)) {
				at = i
				break
			}
		}
	}
	at = max(0, min(n-1, at))
	if b.column == 0 {
		if at != b.master {
			b.selectMaster(at)
		}
	} else {
		b.detail = at
	}
}
func (a *App) kubeBrowserClick(m Message) {
	b := a.Form.ConfigBrowser
	r := a.kubeBrowserRect()
	for i, pos := range buttonLayout([]string{"Connect", "Other file…", "Cancel"}, r.W) {
		if m.Y == r.Y+r.H-3+pos.Y && m.X >= r.X+pos.X && m.X < r.X+pos.X+pos.W {
			switch i {
			case 0:
				a.kubeBrowserConnect()
			case 1:
				a.Form = nil
				b.other()
			case 2:
				a.Form = nil
			}
			return
		}
	}
	row := m.Y - (r.Y + 4)
	if row < 0 || row >= max(1, r.H-10) || m.X < r.X+4 || m.X >= r.X+r.W-4 {
		return
	}
	col := 0
	if m.X >= r.X+r.W/2 {
		col = 1
	}
	b.column = col
	i := row + b.scroll[col]
	if col == 0 && i < len(b.sources) {
		b.selectMaster(i)
	} else if col == 1 && i < len(b.contexts()) {
		b.detail = i
		if m.Clicks >= 2 {
			a.kubeBrowserConnect()
		}
	}
}
func (a *App) chooseKubeContext(index int, p kube.Profile, sources []kube.ConfigSource, warnings []string) {
	if len(sources) == 0 {
		return
	}
	b := &kubeConfigBrowser{sources: sources, other: func() { a.kubeOtherFile(index, p) }}
	for _, source := range sources {
		name := filepath.Base(source.Path)
		if p.StoredConfig {
			name = kubeConfigDisplayName(p)
		}
		b.names = append(b.names, fmt.Sprintf("%s (%d)", name, len(source.Contexts)))
	}
	b.selectMaster(0)
	for i, s := range sources {
		if s.Path == p.Kubeconfig {
			b.selectMaster(i)
			for j, c := range s.Contexts {
				if c == p.Context {
					b.detail = j
				}
			}
		}
	}
	b.selectContext = func(master, detail int) {
		p.Kubeconfig = sources[master].Path
		p.Context = sources[master].Contexts[detail]
		a.restoreNextConnection = true
		a.connectKube(index, p)
	}
	a.Form = &Form{Title: "Kubernetes · configurations and contexts", ConfigBrowser: b}
	if len(warnings) > 0 {
		a.Status = strings.Join(warnings, "; ")
	}
}
