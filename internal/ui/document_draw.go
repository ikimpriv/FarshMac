package ui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"farshmac/internal/textfile"
	"github.com/mattn/go-runewidth"
)

type visualRow struct {
	Start, End, Next int
	Text             string
}

func (d *Document) visualRows(width, count int) []visualRow {
	start := max(0, min(d.ViewStart, len(d.Text)))
	var out []visualRow
	for len(out) < count && start <= len(d.Text) {
		end := strings.IndexByte(d.Text[start:], '\n')
		if end < 0 {
			end = len(d.Text)
		} else {
			end += start
		}
		next := end
		if next < len(d.Text) {
			next++
		}
		if d.Wrap {
			col, lastSpace := 0, -1
			cut := end
			for i := start; i < end; {
				r, n := utf8.DecodeRuneInString(d.Text[i:])
				w := runewidth.RuneWidth(r)
				if r == '\t' {
					w = d.tabWidth() - col%d.tabWidth()
				}
				if col+w > width && i > start {
					cut = i
					break
				}
				col += w
				i += n
				if unicode.IsSpace(r) {
					lastSpace = i
				}
			}
			if cut < end {
				if d.WordWrap && lastSpace > start && lastSpace <= cut {
					cut = lastSpace
				}
				end = cut
				next = cut
			}
		}
		out = append(out, visualRow{start, end, next, d.Text[start:end]})
		if next <= start || next >= len(d.Text) && end == len(d.Text) {
			break
		}
		start = next
	}
	return out
}
func (d *Document) previousVisualStart(width int) int {
	if d.ViewStart <= 0 {
		return 0
	}
	row := d.Pos(d.ViewStart).Row
	start := d.At(row, 0)
	if start == d.ViewStart && row > 0 {
		start = d.At(row-1, 0)
	}
	original := d.ViewStart
	d.ViewStart = start
	rows := d.visualRows(width, utf8.RuneCountInString(d.Line(max(0, row-1)))+utf8.RuneCountInString(d.Line(row))+3)
	d.ViewStart = original
	prev := start
	for _, r := range rows {
		if r.Start >= original {
			break
		}
		prev = r.Start
	}
	return prev
}
func (a *App) binaryWidth() int {
	d := a.Doc
	if d.Mode == "dump" {
		// Dump uses one column per byte; the hex ceiling does not apply.
		if d.BytesPerLine > 0 {
			return max(1, min(d.BytesPerLine, a.Cols-14))
		}
		return max(1, a.Cols-14)
	}
	if d.BytesPerLine > 0 {
		return min(d.BytesPerLine, max(1, (a.Cols-14)/4))
	}
	return max(1, min(16, (a.Cols-14)/4))
}
func (a *App) drawDoc(g *Grid) {
	d := a.Doc
	d.TabSize = a.editorTabWidth()
	syntax := a.documentSyntax(d)
	g.Fill(0, 0, g.W, g.H, LightCyan, Blue)
	top, bottom := 1, g.H-1
	if d.HideStatus {
		top = 0
	}
	if d.HideKeys {
		bottom = g.H
	}
	height := max(1, bottom-top)
	mode := strings.ToUpper(d.Mode)
	if d.Edit {
		mode = "EDIT"
		if d.Overwrite {
			mode += " OVR"
		} else {
			mode += " INS"
		}
	}
	if d.ReadOnly {
		mode += " RO"
	}
	if d.Dirty {
		mode += " *"
	}
	if d.readError {
		mode += " !"
	}
	pos := d.Pos(d.Cursor)
	title := fmt.Sprintf(" %s  %s  %s %s  Ln %d Col %d", filepathLabel(d.displayPath()), mode, d.Codepage, d.EOL, pos.Row+1, pos.Col+1)
	if a.Settings.EditorSyntax && a.syntax.doc == d && a.syntax.status != "" {
		title += "  [" + a.syntax.status + "]"
	}
	if !d.Edit && d.Mode != "text" {
		title = fmt.Sprintf(" %s  %s  Offset %d / %d", filepathLabel(d.displayPath()), mode, d.ByteOffset, d.Size)
	}
	if !d.HideStatus {
		g.Fill(0, 0, g.W, 1, Black, Cyan)
		g.Text(0, 0, title, Black, Cyan, g.W)
	}
	if !d.Edit && d.Mode != "text" {
		a.drawBinary(g, top, height)
		if d.readError && d.notice != "" {
			a.pagedReadFailed(d)
		}
	} else {
		if d.Edit && !d.ManualScroll {
			if pos.Row < d.Offset {
				d.Offset = pos.Row
			}
			if pos.Row >= d.Offset+height {
				d.Offset = pos.Row - height + 1
			}
			col := displayWidthTabs(d.Line(pos.Row)[:min(len(d.Line(pos.Row)), d.Cursor-d.At(pos.Row, 0))], d.tabWidth())
			if col < d.Column {
				d.Column = col
			}
			if col >= d.Column+g.W-1 {
				d.Column = max(0, col-g.W+2)
			}
		}
		d.Offset = max(0, min(d.Offset, len(d.Lines())-1))
		if !d.Wrap || d.Edit {
			d.ViewStart = d.At(d.Offset, 0)
		}
		wrap := d.Wrap
		if d.Edit {
			d.Wrap = false
		}
		rows := d.visualRows(g.W-1, height)
		d.Wrap = wrap
		lo, hi := d.Bounds()
		p, q := pos, pos
		if d.Anchor >= 0 {
			q = d.Pos(d.Anchor)
		}
		for i, row := range rows {
			yy := top + i
			col := 0
			rowIndex := d.Pos(row.Start).Row
			runeCol := 0
			for byteIndex, r := range row.Text {
				rc := runeCol
				runeCol++
				if col >= d.Column+g.W {
					break
				}
				at := row.Start + byteIndex
				w := runewidth.RuneWidth(r)
				if r == '\t' {
					w = d.tabWidth() - col%d.tabWidth()
				} else if unicode.IsControl(r) {
					w = 1
				}
				// Do not allocate strings or look up syntax for the invisible prefix of
				// a horizontally scrolled long line. Rectangular selection counts runes
				// incrementally instead of re-scanning that prefix for every character.
				if (!d.Wrap || d.Edit) && col+w <= d.Column && w > 0 {
					col += w
					continue
				}
				text := string(r)
				if r == '\t' {
					text = strings.Repeat(" ", w)
				} else if unicode.IsControl(r) {
					text = "·"
				}

				fg, bg := syntax.Color(at), uint32(Blue)
				selected := d.Anchor >= 0 && at >= lo && at < hi
				if d.Rect && d.Anchor >= 0 {
					selected = rowIndex >= min(p.Row, q.Row) && rowIndex <= max(p.Row, q.Row) && rc >= min(p.Col, q.Col) && rc < max(p.Col, q.Col)
				}
				if selected {
					fg, bg = Black, Cyan
				}
				x := col - d.Column
				if d.Wrap && !d.Edit {
					x = col
				}
				if w == 0 && x > 0 && x < g.W {
					prev := x - 1
					for prev > 0 && g.Cells[yy*g.W+prev].Text == "" {
						prev--
					}
					g.Cells[yy*g.W+prev].Text += string(r)
					continue
				}
				if x >= 0 && x+w <= g.W-1 {
					g.Text(x, yy, text, fg, bg, w)
				}
				col += w
			}
			if d.Edit && pos.Row == rowIndex {
				line := d.Line(pos.Row)
				prefix := line[:min(len(line), d.Cursor-d.At(pos.Row, 0))]
				x := displayWidthTabs(prefix, d.tabWidth()) - d.Column
				if x >= 0 && x < g.W-1 {
					cell := g.Cells[yy*g.W+x]
					g.Set(x, yy, cell.Text, Blue, Gray)
				}
			}
		}
		if d.Scrollbar && len(d.Lines()) > height {
			for y := top; y < bottom; y++ {
				g.Set(g.W-1, y, "░", Cyan, Blue)
			}
			thumb := top + d.Offset*(height-1)/max(1, len(d.Lines())-1)
			g.Set(g.W-1, thumb, "█", LightCyan, Blue)
		}
	}
	if !d.HideKeys {
		labels := []string{"Help", "Wrap", "Close", "Hex", "", "Edit", "Search", "Codepg", "Options", "Close"}
		if d.Edit {
			labels = []string{"Help", "Save", "", "Close", "", "View", "Search", "Codepg", "Options", "Close"}
		}
		for i, label := range labels {
			x := i * g.W / 10
			end := (i + 1) * g.W / 10
			g.Fill(x, g.H-1, end-x, 1, Black, Cyan)
			key := fmt.Sprint(i + 1)
			g.Text(x, g.H-1, key, Gray, Black, len(key))
			g.Text(x+len(key), g.H-1, label, Black, Cyan, end-x-len(key))
		}
	}
	if a.Status != "" && !d.HideStatus {
		status := tail(a.Status, min(32, g.W/3))
		g.Text(g.W-len([]rune(status)), 0, status, Yellow, Blue, len([]rune(status)))
	}
}
func filepathLabel(path string) string { return tail(path, 32) }
func (a *App) editorTabWidth() int {
	if a.Settings.EditorTabSize < 1 || a.Settings.EditorTabSize > 16 {
		return 4
	}
	return a.Settings.EditorTabSize
}
func (d *Document) tabWidth() int {
	if d.TabSize < 1 || d.TabSize > 16 {
		return 4
	}
	return d.TabSize
}
func displayWidth(s string) int { return displayWidthTabs(s, 4) }
func displayWidthTabs(s string, width int) int {
	col := 0
	for _, r := range s {
		if r == '\t' {
			col += width - col%width
		} else {
			col += runewidth.RuneWidth(r)
		}
	}
	return col
}
func (a *App) documentClick(m Message, drag bool) {
	d := a.Doc
	if d == nil || a.Dialog != nil || a.Form != nil {
		return
	}
	if m.Y == a.Rows-1 && !d.HideKeys {
		if !drag {
			n := min(10, m.X*10/a.Cols+1)
			a.documentKey(Message{Key: fmt.Sprintf("F%d", n)})
		}
		return
	}
	if d.Mode != "text" && !d.Edit {
		return
	}
	top := 1
	if d.HideStatus {
		top = 0
	}
	if d.Scrollbar && m.X == a.Cols-1 {
		d.Offset = max(0, min(len(d.Lines())-1, (m.Y-top)*(len(d.Lines())-1)/max(1, a.Rows-top-2)))
		d.ViewStart = d.At(d.Offset, 0)
		d.ManualScroll = true
		if !drag {
			a.documentScrollbarDrag = true
		}
		return
	}
	d.ManualScroll = false
	row := m.Y - top
	if row < 0 {
		return
	}
	wrap := d.Wrap
	if d.Edit {
		d.Wrap = false
	}
	rows := d.visualRows(a.Cols-1, a.Rows)
	d.Wrap = wrap
	if row >= len(rows) {
		return
	}
	line := rows[row]
	target := line.End
	col := 0
	wanted := m.X + d.Column
	if d.Wrap && !d.Edit {
		wanted = m.X
	}
	for i, r := range line.Text {
		w := runewidth.RuneWidth(r)
		if r == '\t' {
			w = d.tabWidth() - col%d.tabWidth()
		}
		if col+w > wanted {
			target = line.Start + i
			break
		}
		col += w
	}
	d.Move(target, drag || m.Shift, m.Alt)
	if !drag && m.Clicks >= 3 {
		r := d.Pos(target).Row
		d.Anchor = d.At(r, 0)
		d.Cursor = min(len(d.Text), d.At(r, 0)+len(d.Line(r))+1)
		d.Rect = false
	} else if !drag && m.Clicks == 2 && target < len(d.Text) {
		kind := func(r rune) int {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
				return 1
			}
			if unicode.IsSpace(r) {
				return 2
			}
			return 3
		}
		r, _ := utf8.DecodeRuneInString(d.Text[target:])
		k := kind(r)
		lo, hi := target, d.Next(target)
		for lo > 0 {
			prev := d.Prev(lo)
			r, _ = utf8.DecodeRuneInString(d.Text[prev:])
			if kind(r) != k || r == '\n' {
				break
			}
			lo = prev
		}
		for hi < len(d.Text) {
			r, _ = utf8.DecodeRuneInString(d.Text[hi:])
			if kind(r) != k || r == '\n' {
				break
			}
			hi = d.Next(hi)
		}
		d.Anchor, d.Cursor, d.Rect = lo, hi, false
	}
}
func (a *App) drawBinary(g *Grid, top, height int) {
	d := a.Doc
	width := a.binaryWidth()
	data := d.dataAt(d.ByteOffset, width*height)
	for row := 0; row < height; row++ {
		start := row * width
		if start >= len(data) {
			break
		}
		line := data[start:min(start+width, len(data))]
		offset := d.ByteOffset + int64(start)
		g.Text(0, top+row, fmt.Sprintf("%010X: ", offset), Gray, Blue, 12)
		if d.Mode == "dump" {
			for i, b := range line {
				r := rune(b)
				if decoded, _, err := textfile.Decode([]byte{b}, d.Codepage); err == nil && decoded != "" {
					r, _ = utf8.DecodeRuneInString(decoded)
				} else if b > 126 {
					r = '·'
				}
				if b < 32 || b == 127 {
					r = '·'
				}
				fg, bg := uint32(LightCyan), uint32(Blue)
				at := offset + int64(i)
				if at >= d.MatchStart && at < d.MatchEnd {
					fg, bg = Black, Cyan
				}
				g.Text(12+i, top+row, string(r), fg, bg, 1)
			}
			continue
		}
		for i, b := range line {
			fg, bg := uint32(LightCyan), uint32(Blue)
			at := offset + int64(i)
			if at >= d.MatchStart && at < d.MatchEnd {
				fg, bg = Black, Cyan
			}
			g.Text(12+i*3, top+row, fmt.Sprintf("%02X ", b), fg, bg, 3)
			r := rune(b)
			if b < 32 || b > 126 {
				r = '·'
			}
			g.Text(13+width*3+i, top+row, string(r), fg, bg, 1)
		}
	}
}
