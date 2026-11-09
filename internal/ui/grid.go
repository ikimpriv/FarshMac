package ui

import (
	"fmt"
	"github.com/mattn/go-runewidth"
	"strings"
	"unicode"
)

const (
	Black     = 0x000000
	Blue      = 0x000080
	Cyan      = 0x008080
	LightCyan = 0x55ffff
	White     = 0xffffff
	Gray      = 0xc0c0c0
	Yellow    = 0xffff55
)

type Cell struct {
	Text   string
	FG, BG uint32
}
type Grid struct {
	W, H  int
	Cells []Cell
}

func NewGrid(w, h int) *Grid {
	g := &Grid{W: w, H: h, Cells: make([]Cell, w*h)}
	g.Fill(0, 0, w, h, LightCyan, Blue)
	return g
}
func (g *Grid) Set(x, y int, text string, fg, bg uint32) {
	if x >= 0 && y >= 0 && x < g.W && y < g.H {
		g.Cells[y*g.W+x] = Cell{text, fg, bg}
	}
}
func (g *Grid) Text(x, y int, s string, fg, bg uint32, limit int) {
	i := 0
	last := -1
	for _, r := range s {
		if i >= limit {
			break
		}
		if unicode.IsControl(r) {
			r = ' '
		}
		w := runewidth.RuneWidth(r)
		if w == 0 {
			if last >= 0 && x+last >= 0 && x+last < g.W && y >= 0 && y < g.H {
				g.Cells[y*g.W+x+last].Text += string(r)
			}
			continue
		}
		if i+w > limit {
			break
		}
		g.Set(x+i, y, string(r), fg, bg)
		last = i
		if w == 2 {
			g.Set(x+i+1, y, "", fg, bg)
		}
		i += w
	}
}
func (g *Grid) Fill(x, y, w, h int, fg, bg uint32) {
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			g.Set(col, row, " ", fg, bg)
		}
	}
}
func (g *Grid) Box(x, y, w, h int, fg, bg uint32) {
	g.Fill(x, y, w, h, fg, bg)
	for i := 1; i < w-1; i++ {
		g.Set(x+i, y, "═", fg, bg)
		g.Set(x+i, y+h-1, "═", fg, bg)
	}
	for i := 1; i < h-1; i++ {
		g.Set(x, y+i, "║", fg, bg)
		g.Set(x+w-1, y+i, "║", fg, bg)
	}
	g.Set(x, y, "╔", fg, bg)
	g.Set(x+w-1, y, "╗", fg, bg)
	g.Set(x, y+h-1, "╚", fg, bg)
	g.Set(x+w-1, y+h-1, "╝", fg, bg)
}
func tail(s string, n int) string {
	r := []rune(s)
	if n <= 0 {
		return ""
	}
	if len(r) > n {
		return "…" + string(r[len(r)-n+1:])
	}
	return s
}
func (g *Grid) ANSI() string {
	var b strings.Builder
	b.WriteString("\x1b[?25l\x1b[?7l\x1b[H")
	var fg, bg uint32 = 0xffffffff, 0xffffffff
	for y := 0; y < g.H; y++ {
		if y > 0 {
			fmt.Fprintf(&b, "\x1b[%d;1H", y+1)
		}
		for x := 0; x < g.W; x++ {
			c := g.Cells[y*g.W+x]
			if c.Text == "" {
				continue // continuation of a wide Go grid cell
			}
			if c.FG != fg || c.BG != bg {
				fg, bg = c.FG, c.BG
				fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm", fg>>16, (fg>>8)&255, fg&255, bg>>16, (bg>>8)&255, bg&255)
			}
			// xterm's Unicode table and Go's table can disagree (notably on
			// emoji). Anchor the next cell after non-ASCII text, and paint the
			// continuation explicitly so an old glyph cannot survive there.
			unicodeCell := len(c.Text) != 1 || c.Text[0] >= 0x80
			width := 1
			if x+1 < g.W && g.Cells[y*g.W+x+1].Text == "" {
				width = 2
				b.WriteString("  ")
				fmt.Fprintf(&b, "\x1b[%d;%dH", y+1, x+1)
			}
			b.WriteString(c.Text)
			if unicodeCell && x+width < g.W {
				fmt.Fprintf(&b, "\x1b[%d;%dH", y+1, x+width+1)
			}
		}
	}
	b.WriteString("\x1b[?7h")
	return b.String()
}
