package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

type modalRect struct{ X, Y, W, H int }

func modalBounds(cols, rows, width, height int) modalRect {
	w, h := min(width, max(12, cols-4)), min(height, max(8, rows-2))
	return modalRect{(cols - w) / 2, (rows - h) / 2, w, h}
}
func drawModal(g *Grid, r modalRect, title string) {
	g.Fill(r.X+2, r.Y+1, r.W, r.H, Black, Black)
	g.Fill(r.X, r.Y, r.W, r.H, Black, Gray)
	g.Box(r.X+2, r.Y+1, r.W-4, r.H-2, Black, Gray)
	title = " " + tail(title, r.W-10) + " "
	g.Text(r.X+(r.W-displayWidth(title))/2, r.Y+1, title, Black, Gray, r.W-8)
}
func modalRule(g *Grid, r modalRect, y int) {
	g.Text(r.X+3, y, strings.Repeat("─", max(0, r.W-6)), Black, Gray, r.W-6)
	g.Set(r.X+2, y, "╟", Black, Gray)
	g.Set(r.X+r.W-3, y, "╢", Black, Gray)
}
func mnemonicKeys(labels []string) []rune {
	keys := make([]rune, len(labels))
	used := map[rune]bool{}
	for i, label := range labels {
		for _, r := range label {
			r = unicode.ToLower(r)
			if unicode.IsLetter(r) && !used[r] {
				keys[i] = r
				used[r] = true
				break
			}
		}
	}
	return keys
}
func modalLabel(g *Grid, x, y int, text string, key rune, bg uint32, width int) {
	g.Text(x, y, text, Black, bg, width)
	col := 0
	for _, r := range text {
		w := runewidth.RuneWidth(r)
		if unicode.ToLower(r) == key && key != 0 {
			if col+w <= width {
				g.Text(x+col, y, string(r), Yellow, bg, w)
			}
			return
		}
		col += w
	}
}

// Show the part of an edit line containing the caret, measured in screen cells.
func editWindow(value string, cursor, width int) (string, int) {
	cursor = max(0, min(cursor, utf8.RuneCountInString(value)))
	runes := []rune(value)
	prefix := string(runes[:cursor])
	caret := displayWidth(prefix)
	start := 0
	for caret >= width && start < cursor {
		r := runes[start]
		w := runewidth.RuneWidth(r)
		if r == '\t' {
			w = 1
		}
		caret -= w
		start++
	}
	return string(runes[start:]), max(0, min(caret, width-1))
}
func modalInput(g *Grid, x, y, width int, value string, cursor int, focused, selected bool) {
	if width < 1 {
		return
	}
	g.Fill(x, y, width, 1, LightCyan, Cyan)
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	shown, caret := editWindow(value, cursor, width)
	fg, bg := uint32(LightCyan), uint32(Cyan)
	if selected {
		fg, bg = White, Blue
	}
	g.Text(x, y, shown, fg, bg, width)
	if focused {
		at := y*g.W + x + caret
		if at >= 0 && at < len(g.Cells) {
			g.Set(x+caret, y, g.Cells[at].Text, White, Black)
		}
	}
}

type buttonRect struct{ X, Y, W int }

func buttonLayout(labels []string, width int) []buttonRect {
	var out []buttonRect
	x, y, start := 4, 0, 0
	center := func(end int) {
		if end <= start {
			return
		}
		used := out[end-1].X + out[end-1].W - 4
		offset := max(0, (width-8-used)/2)
		for i := start; i < end; i++ {
			out[i].X += offset
		}
		start = end
	}
	for _, label := range labels {
		w := min(displayWidth(label)+4, max(1, width-8))
		if x+w > width-4 && x > 4 {
			center(len(out))
			x = 4
			y++
		}
		out = append(out, buttonRect{x, y, w})
		x += w + 1
	}
	center(len(out))
	return out
}
func drawModalButtons(g *Grid, r modalRect, y int, labels []string, focus int, keys []rune) {
	for i, pos := range buttonLayout(labels, r.W) {
		bg := uint32(Gray)
		if i == focus {
			bg = Cyan
		}
		left, right := "[ ", " ]"
		if i == 0 {
			left, right = "{ ", " }"
		}
		key := rune(0)
		if i < len(keys) {
			key = keys[i]
		}
		modalLabel(g, r.X+pos.X, y+pos.Y, left+labels[i]+right, key, bg, pos.W)
	}
}
