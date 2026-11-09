package textfile

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Position struct{ Row, Col int }
type change struct {
	At                    int
	Old, New              string
	Before, After, Anchor int
}
type Buffer struct {
	Text                      string
	Cursor, Anchor            int // UTF-8 byte offsets, always at rune boundaries.
	Rect, Overwrite, ReadOnly bool
	UndoStack, RedoStack      []change
	indexed                   string
	lines                     []int
	historyBytes              int
}

// Clone isolates mutable indexes and undo history for background editor work.
func (b *Buffer) Clone() *Buffer {
	c := *b
	c.lines = append([]int(nil), b.lines...)
	c.UndoStack = append([]change(nil), b.UndoStack...)
	c.RedoStack = append([]change(nil), b.RedoStack...)
	return &c
}

func New(text string) *Buffer { return &Buffer{Text: Normalize(text), Anchor: -1} }
func (b *Buffer) Lines() []int {
	if b.lines == nil || b.indexed != b.Text {
		b.lines = make([]int, 1, strings.Count(b.Text, "\n")+1)
		for i, c := range b.Text {
			if c == '\n' {
				b.lines = append(b.lines, i+1)
			}
		}
		b.indexed = b.Text
	}
	return b.lines
}
func (b *Buffer) Pos(at int) Position {
	at = max(0, min(at, len(b.Text)))
	lines := b.Lines()
	row := max(0, sort.SearchInts(lines, at+1)-1)
	return Position{row, utf8.RuneCountInString(b.Text[lines[row]:at])}
}
func (b *Buffer) Line(row int) string {
	starts := b.Lines()
	if row < 0 || row >= len(starts) {
		return ""
	}
	end := len(b.Text)
	if row+1 < len(starts) {
		end = starts[row+1] - 1
	}
	return b.Text[starts[row]:end]
}
func (b *Buffer) At(row, col int) int {
	row = max(0, min(row, len(b.Lines())-1))
	start := b.Lines()[row]
	line := b.Line(row)
	n := 0
	for i := range line {
		if n >= col {
			return start + i
		}
		n++
	}
	return start + len(line)
}
func (b *Buffer) Bounds() (int, int) {
	if b.Anchor < 0 {
		return b.Cursor, b.Cursor
	}
	return min(b.Cursor, b.Anchor), max(b.Cursor, b.Anchor)
}
func (b *Buffer) ClearSelection() { b.Anchor = -1; b.Rect = false }
func (b *Buffer) Move(at int, selecting, rect bool) {
	if selecting {
		if b.Anchor < 0 {
			b.Anchor = b.Cursor
		}
		b.Rect = rect
	} else {
		b.ClearSelection()
	}
	b.Cursor = max(0, min(at, len(b.Text)))
}
func (b *Buffer) Prev(at int) int {
	if at <= 0 {
		return 0
	}
	_, n := utf8.DecodeLastRuneInString(b.Text[:at])
	return at - n
}
func (b *Buffer) Next(at int) int {
	if at >= len(b.Text) {
		return len(b.Text)
	}
	_, n := utf8.DecodeRuneInString(b.Text[at:])
	return at + n
}
func word(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }
func (b *Buffer) Word(at, dir int) int {
	if dir < 0 {
		for at > 0 {
			p := b.Prev(at)
			r, _ := utf8.DecodeRuneInString(b.Text[p:])
			if word(r) {
				break
			}
			at = p
		}
		for at > 0 {
			p := b.Prev(at)
			r, _ := utf8.DecodeRuneInString(b.Text[p:])
			if !word(r) {
				break
			}
			at = p
		}
		return at
	}
	for at < len(b.Text) {
		r, _ := utf8.DecodeRuneInString(b.Text[at:])
		if !word(r) {
			break
		}
		at = b.Next(at)
	}
	for at < len(b.Text) {
		r, _ := utf8.DecodeRuneInString(b.Text[at:])
		if word(r) {
			break
		}
		at = b.Next(at)
	}
	return at
}
func (b *Buffer) Replace(start, end int, s string) {
	if b.ReadOnly {
		return
	}
	s = Normalize(s)
	start = max(0, min(start, len(b.Text)))
	end = max(start, min(end, len(b.Text)))
	if b.Text[start:end] == s {
		return
	}
	// Drop redo records on a branching edit, including their backing storage.
	for _, c := range b.RedoStack {
		b.historyBytes -= changeBytes(c)
	}
	b.RedoStack = nil
	cost := len(b.Text[start:end]) + len(s) + 80
	c := change{At: start, Before: b.Cursor, After: start + len(s), Anchor: b.Anchor}
	if cost <= MaxUndoBytes {
		c.Old = strings.Clone(b.Text[start:end])
		c.New = strings.Clone(s)
	}
	b.replaceText(start, end, s)
	b.Cursor = c.After
	b.ClearSelection()
	if cost > MaxUndoBytes {
		b.UndoStack = nil
		b.historyBytes = 0
		return
	}
	b.UndoStack = append(b.UndoStack, c)
	b.historyBytes += changeBytes(c)
	for b.historyBytes > MaxUndoBytes || len(b.UndoStack) > 10000 {
		b.historyBytes -= changeBytes(b.UndoStack[0])
		b.UndoStack[0] = change{}
		b.UndoStack = b.UndoStack[1:]
	}

}
func (b *Buffer) Selected() string {
	if b.Anchor < 0 {
		return ""
	}
	if !b.Rect {
		s, e := b.Bounds()
		return b.Text[s:e]
	}
	p, q := b.Pos(b.Cursor), b.Pos(b.Anchor)
	var lines []string
	for row := min(p.Row, q.Row); row <= max(p.Row, q.Row); row++ {
		s, e := b.At(row, min(p.Col, q.Col)), b.At(row, max(p.Col, q.Col))
		lines = append(lines, b.Text[s:e])
	}
	return strings.Join(lines, "\n")
}
func (b *Buffer) Insert(s string) {
	if b.ReadOnly {
		return
	}
	if b.Rect && b.Anchor >= 0 {
		p, q := b.Pos(b.Cursor), b.Pos(b.Anchor)
		first, last := min(p.Row, q.Row), max(p.Row, q.Row)
		left, right := min(p.Col, q.Col), max(p.Col, q.Col)
		var lines []string
		insert := strings.Split(Normalize(s), "\n")
		for row := first; row <= last; row++ {
			line := []rune(b.Line(row))
			l, r := min(left, len(line)), min(right, len(line))
			part := ""
			if len(insert) == 1 {
				part = insert[0]
			} else if row-first < len(insert) {
				part = insert[row-first]
			}
			if part != "" && left > len(line) {
				part = strings.Repeat(" ", left-len(line)) + part
			}
			lines = append(lines, string(line[:l])+part+string(line[r:]))
		}
		start, end := b.At(first, 0), b.At(last, utf8.RuneCountInString(b.Line(last)))
		b.Replace(start, end, strings.Join(lines, "\n"))
		b.Cursor = b.At(first, left)
		return
	}
	start, end := b.Bounds()
	if start == end && b.Overwrite && s != "\n" {
		for range s {
			if end < len(b.Text) && b.Text[end] != '\n' {
				end = b.Next(end)
			}
		}
	}
	b.Replace(start, end, s)
}
func (b *Buffer) Undo() {
	if b.ReadOnly || len(b.UndoStack) == 0 {
		return
	}
	c := b.UndoStack[len(b.UndoStack)-1]
	b.UndoStack[len(b.UndoStack)-1] = change{}
	b.UndoStack = b.UndoStack[:len(b.UndoStack)-1]
	b.replaceText(c.At, c.At+len(c.New), c.Old)
	b.Cursor = c.Before
	b.Anchor = c.Anchor
	b.Rect = false
	b.RedoStack = append(b.RedoStack, c)
}
func (b *Buffer) Redo() {
	if b.ReadOnly || len(b.RedoStack) == 0 {
		return
	}
	c := b.RedoStack[len(b.RedoStack)-1]
	b.RedoStack[len(b.RedoStack)-1] = change{}
	b.RedoStack = b.RedoStack[:len(b.RedoStack)-1]
	b.replaceText(c.At, c.At+len(c.Old), c.New)
	b.Cursor = c.After
	b.ClearSelection()
	b.UndoStack = append(b.UndoStack, c)
}

const MaxUndoBytes = 32 << 20

func changeBytes(c change) int { return len(c.Old) + len(c.New) + 80 }

// Keep line offsets in place; ordinary keystrokes no longer re-scan/reallocate
// the whole line index or retain the previous full text through indexed.
func (b *Buffer) replaceText(start, end int, s string) {
	valid := b.lines != nil && b.indexed == b.Text
	if valid {
		first := sort.SearchInts(b.lines, start+1)
		last := sort.SearchInts(b.lines, end+1)
		added := strings.Count(s, "\n")
		oldLen := len(b.lines)
		newLen := oldLen - (last - first) + added
		if newLen > oldLen {
			b.lines = append(b.lines, make([]int, newLen-oldLen)...)
		}
		copy(b.lines[first+added:], b.lines[last:oldLen])
		b.lines = b.lines[:newLen]
		delta := len(s) - (end - start)
		for i := first + added; i < newLen; i++ {
			b.lines[i] += delta
		}
		at := first
		for i := 0; i < len(s); i++ {
			if s[i] == '\n' {
				b.lines[at] = start + i + 1
				at++
			}
		}
	}
	b.Text = b.Text[:start] + s + b.Text[end:]
	if valid {
		b.indexed = b.Text
	} else {
		b.lines = nil
		b.indexed = ""
	}
}

// Newline copies only indentation before the selection start/caret. Replace
// avoids overwrite mode consuming characters on the next line.
func (b *Buffer) Newline(auto bool) {
	start, end := b.Bounds()
	indent := ""
	if auto {
		row := b.Pos(start).Row
		lineStart := b.Lines()[row]
		n := lineStart
		for n < start && (b.Text[n] == ' ' || b.Text[n] == '\t') {
			n++
		}
		indent = b.Text[lineStart:n]
	}
	b.Replace(start, end, "\n"+indent)
}

func (b *Buffer) Indent(reverse bool) { b.IndentWithOptions(reverse, 4, true) }
func (b *Buffer) IndentWithOptions(reverse bool, width int, spaces bool) {
	if b.ReadOnly {
		return
	}
	width = max(1, min(16, width))
	p, q := b.Pos(b.Cursor), b.Pos(b.Cursor)
	selected := b.Anchor >= 0
	rect := b.Rect
	if selected {
		q = b.Pos(b.Anchor)
	}
	first, last := min(p.Row, q.Row), max(p.Row, q.Row)
	if selected && last > first && ((p.Row == last && p.Col == 0) || (q.Row == last && q.Col == 0)) {
		last--
	}
	var out strings.Builder
	deltas := make([]int, last-first+1)
	for row := first; row <= last; row++ {
		line := b.Line(row)
		delta := 0
		if reverse {
			n := 0
			if strings.HasPrefix(line, "\t") {
				n = 1
			} else {
				for n < width && n < len(line) && line[n] == ' ' {
					n++
				}
			}
			line = line[n:]
			delta = -n
		} else {
			prefix := "\t"
			if spaces {
				prefix = strings.Repeat(" ", width)
			}
			line = prefix + line
			delta = len(prefix)
		}
		deltas[row-first] = delta
		if row > first {
			out.WriteByte('\n')
		}
		out.WriteString(line)
	}
	start, end, replacement := b.At(first, 0), b.Lines()[last]+len(b.Line(last)), out.String()
	if b.Text[start:end] == replacement {
		return
	}
	b.Replace(start, end, replacement)
	mapped := func(pos Position) int {
		if pos.Row >= first && pos.Row <= last {
			pos.Col = max(0, pos.Col+deltas[pos.Row-first])
		}
		return b.At(pos.Row, pos.Col)
	}
	b.Cursor = mapped(p)
	if len(b.UndoStack) > 0 {
		b.UndoStack[len(b.UndoStack)-1].After = b.Cursor
	}
	if selected {
		b.Anchor = mapped(q)
		b.Rect = rect
	}
}

type Search struct {
	Query              string
	Case, Whole, Regex bool
}

func (s Search) Compile() (*regexp.Regexp, error) {
	q := s.Query
	if q == "" {
		return nil, fmt.Errorf("enter search text")
	}
	if !s.Regex {
		q = regexp.QuoteMeta(q)
	}
	if !s.Case {
		q = "(?i)" + q
	}
	return regexp.Compile("(?m)" + q)
}
func (s Search) Matches(text string, checks ...func() error) ([][2]int, error) {
	if err := checkSearch(checks); err != nil {
		return nil, err
	}
	r, err := s.Compile()
	if err != nil {
		return nil, err
	}
	var out [][2]int
	for _, m := range r.FindAllStringIndex(text, -1) {
		if err := checkSearch(checks); err != nil {
			return nil, err
		}
		if s.Whole {
			if m[0] > 0 {
				c, _ := utf8.DecodeLastRuneInString(text[:m[0]])
				if word(c) {
					continue
				}
			}
			if m[1] < len(text) {
				c, _ := utf8.DecodeRuneInString(text[m[1]:])
				if word(c) {
					continue
				}
			}
		}
		out = append(out, [2]int{m[0], m[1]})
	}
	return out, checkSearch(checks)
}
func checkSearch(checks []func() error) error {
	if len(checks) > 0 && checks[0] != nil {
		return checks[0]()
	}
	return nil
}
func (b *Buffer) Find(s Search, backward bool, checks ...func() error) (bool, error) {
	matches, err := s.Matches(b.Text, checks...)
	if err != nil {
		return false, err
	}
	if len(matches) == 0 {
		return false, nil
	}
	start := b.Cursor
	if b.Anchor >= 0 {
		lo, hi := b.Bounds()
		if backward {
			start = lo
		} else {
			start = hi
			if hi == lo {
				start = b.Next(start)
			}
		}
	}
	match := matches[0]
	if backward {
		match = matches[len(matches)-1]
		for i := len(matches) - 1; i >= 0; i-- {
			if matches[i][0] < start {
				match = matches[i]
				break
			}
		}
	} else {
		for _, m := range matches {
			if m[0] >= start {
				match = m
				break
			}
		}
	}
	b.Anchor = match[0]
	b.Cursor = match[1]
	b.Rect = false
	return true, nil
}
func (b *Buffer) ReplaceAll(s Search, replacement string, checks ...func() error) (int, error) {
	if b.ReadOnly {
		return 0, fmt.Errorf("editor is read-only")
	}
	matches, err := s.Matches(b.Text, checks...)
	if err != nil || len(matches) == 0 {
		return 0, err
	}
	r, _ := s.Compile()
	submatches := map[int][]int{}
	if s.Regex {
		for _, indices := range r.FindAllStringSubmatchIndex(b.Text, -1) {
			submatches[indices[0]] = indices
		}
	}
	var out strings.Builder
	last := 0
	for _, m := range matches {
		if err := checkSearch(checks); err != nil {
			return 0, err
		}
		out.WriteString(b.Text[last:m[0]])
		if s.Regex {
			out.Write(r.ExpandString(nil, replacement, b.Text, submatches[m[0]]))
		} else {
			out.WriteString(replacement)
		}
		last = m[1]
	}
	out.WriteString(b.Text[last:])
	if err := checkSearch(checks); err != nil {
		return 0, err
	}
	b.Replace(0, len(b.Text), out.String())
	return len(matches), nil
}
