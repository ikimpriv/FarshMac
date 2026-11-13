package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func (a *App) editorKey(m Message) {
	d := a.Doc
	cmd := m.Meta || m.Ctrl
	if m.Key == " " && m.Ctrl && !m.Meta && !m.Alt {
		a.manifestComplete()
		return
	}
	if m.Key == "F2" && m.Alt && !cmd && !m.Shift && d.manifestSchema() != nil {
		a.nextProblem()
		return
	}
	pos := d.Pos(d.Cursor)
	target := d.Cursor
	move := true
	// macOS conventions: Cmd+Left/Right line start/end, Cmd+Up/Down document
	// start/end, Option+Left/Right by word (Ctrl+Left/Right too, as in Far).
	// Option with Up/Down/Home/End keeps the rectangular selection.
	rect := m.Alt
	switch m.Key {
	case "ArrowLeft":
		rect = false
		switch {
		case m.Meta && !m.Ctrl && !m.Alt:
			target = d.At(pos.Row, 0)
		case m.Alt || cmd:
			target = d.Word(d.Cursor, -1)
		default:
			target = d.Prev(d.Cursor)
		}
	case "ArrowRight":
		rect = false
		switch {
		case m.Meta && !m.Ctrl && !m.Alt:
			target = d.At(pos.Row, utf8.RuneCountInString(d.Line(pos.Row)))
		case m.Alt || cmd:
			target = d.Word(d.Cursor, 1)
		default:
			target = d.Next(d.Cursor)
		}
	case "ArrowUp":
		if m.Meta && !m.Ctrl && !m.Alt {
			target = 0
		} else {
			target = d.At(pos.Row-1, pos.Col)
		}
	case "ArrowDown":
		if m.Meta && !m.Ctrl && !m.Alt {
			target = len(d.Text)
		} else {
			target = d.At(pos.Row+1, pos.Col)
		}
	case "PageUp":
		if cmd {
			target = 0
		} else {
			target = d.At(pos.Row-a.Rows+2, pos.Col)
		}
	case "PageDown":
		if cmd {
			target = len(d.Text)
		} else {
			target = d.At(pos.Row+a.Rows-2, pos.Col)
		}
	case "Home":
		target = d.At(pos.Row, 0)
		if cmd {
			target = 0
		}
	case "End":
		target = d.At(pos.Row, utf8.RuneCountInString(d.Line(pos.Row)))
		if cmd {
			target = len(d.Text)
		}
	default:
		move = false
	}
	if move {
		d.Move(target, m.Shift, rect)
		return
	}
	switch m.Key {
	case "F2":
		if m.Shift {
			a.saveAs()
		} else {
			a.save()
		}
		return
	case "F4":
		if m.Shift {
			a.newFile()
		}
		return
	case "Insert":
		if !m.Shift && !m.Alt {
			d.Overwrite = !d.Overwrite
		}
		return
	case "Delete":
		if m.Shift && !cmd && !m.Alt {
			a.copyDocument(true) // Far: Shift+Del cuts, Ctrl+Ins copies.
			return
		}
	}
	if d.ReadOnly {
		a.Status = "Read-only mode (⌘L to unlock)"
		return
	}
	if cmd {
		switch strings.ToLower(m.Key) {
		case "y":
			start := d.At(pos.Row, 0)
			end := start + len(d.Line(pos.Row))
			if end < len(d.Text) {
				end++
			}
			d.Replace(start, end, "")
		case "k":
			d.Replace(d.Cursor, d.At(pos.Row, utf8.RuneCountInString(d.Line(pos.Row))), "")
		case "d":
			d.Insert("")
		case "backspace":
			d.Replace(d.Word(d.Cursor, -1), d.Cursor, "")
		case "delete":
			d.Replace(d.Cursor, d.Word(d.Cursor, 1), "")
		}
		d.modified()
		return
	}
	if m.Alt {
		switch strings.ToLower(m.Key) {
		case "i":
			d.IndentWithOptions(false, a.editorTabWidth(), a.Settings.EditorExpandTabs)
		case "u":
			d.IndentWithOptions(true, a.editorTabWidth(), a.Settings.EditorExpandTabs)
		}
		d.modified()
		return
	}
	switch m.Key {
	case "Backspace":
		lo, hi := d.Bounds()
		if lo != hi {
			d.Insert("")
		} else {
			d.Replace(d.Prev(d.Cursor), d.Cursor, "")
		}
	case "Delete":
		lo, hi := d.Bounds()
		if lo != hi {
			d.Insert("")
		} else {
			d.Replace(d.Cursor, d.Next(d.Cursor), "")
		}
	case "Enter":
		if m.Shift {
			d.Insert(filepath.Base(a.Panels[a.Active].Current()))
		} else {
			d.Newline(a.Settings.EditorAutoIndent)
		}
	case "Tab":
		if m.Shift {
			d.IndentWithOptions(true, a.editorTabWidth(), a.Settings.EditorExpandTabs)
		} else if d.Anchor >= 0 && d.Cursor != d.Anchor {
			d.IndentWithOptions(false, a.editorTabWidth(), a.Settings.EditorExpandTabs)
		} else if a.Settings.EditorExpandTabs {
			start := d.Lines()[pos.Row]
			col := displayWidthTabs(d.Text[start:d.Cursor], a.editorTabWidth())
			d.Insert(strings.Repeat(" ", a.editorTabWidth()-col%a.editorTabWidth()))
		} else {
			d.Insert("\t")
		}
	default:
		if utf8.RuneCountInString(m.Key) == 1 {
			d.Insert(m.Key)
		}
	}
	d.modified()
}

func (a *App) viewerKey(m Message) {
	d := a.Doc
	cmd := m.Meta || m.Ctrl
	if m.Key == "F2" {
		if d.Mode != "text" {
			d.Mode = "text"
			if d.File != nil {
				d.Mode = "dump"
				a.Status = "Large files currently support paged hex/dump only"
			}
			return
		}
		if m.Shift {
			d.WordWrap = !d.WordWrap
			d.Wrap = true
		} else {
			d.Wrap = !d.Wrap
		}
		d.ViewStart = d.At(d.Offset, 0)
		return
	}
	if m.Key == "F4" {
		if m.Shift {
			a.choose("View mode", []string{"Text", "Hex", "Dump"}, func(i int) {
				if i == 0 && d.File != nil {
					a.Status = "Large files currently support paged hex/dump only"
					return
				}
				d.Mode = []string{"text", "hex", "dump"}[i]
			})
		} else {
			if d.Mode == "text" {
				d.Mode = "hex"
			} else if d.File == nil {
				d.Mode = "text"
			} else {
				d.Mode = "dump"
			}
		}
		return
	}
	if m.Key == "+" || m.Key == "-" {
		a.adjacentFile(m.Key == "+")
		return
	}
	if m.Key == " " {
		a.findDocument(false)
		return
	}
	if d.Mode != "text" {
		width := a.binaryWidth()
		step := int64(width)
		page := step * int64(max(1, a.Rows-2))
		switch m.Key {
		case "ArrowUp":
			d.ByteOffset -= step
		case "ArrowDown":
			d.ByteOffset += step
		case "PageUp":
			d.ByteOffset -= page
		case "PageDown":
			d.ByteOffset += page
		case "Home":
			d.ByteOffset = 0
		case "End":
			d.ByteOffset = max(int64(0), d.Size-page)
		case "ArrowLeft":
			if m.Alt {
				d.BytesPerLine = max(1, width-1)
			} else {
				d.ByteOffset--
			}
		case "ArrowRight":
			if m.Alt {
				d.BytesPerLine = min(64, width+1)
			} else {
				d.ByteOffset++
			}
		}
		d.ByteOffset = max(0, min(d.ByteOffset, max(int64(0), d.Size-1)))
		return
	}
	step := 1
	if cmd {
		step = 20
	}
	if d.Wrap {
		count := 1
		switch m.Key {
		case "PageDown", "PageUp":
			count = max(1, a.Rows-2)
		}
		switch m.Key {
		case "ArrowDown", "PageDown":
			for i := 0; i < count; i++ {
				rows := d.visualRows(a.Cols-1, 2)
				if len(rows) > 1 {
					d.ViewStart = rows[1].Start
				}
			}
		case "ArrowUp", "PageUp":
			for i := 0; i < count; i++ {
				d.ViewStart = d.previousVisualStart(a.Cols - 1)
			}
		case "Home":
			d.ViewStart = 0
		case "End":
			d.ViewStart = d.At(len(d.Lines())-1, 0)
		}
		d.Offset = d.Pos(d.ViewStart).Row
		return
	}
	switch m.Key {
	case "ArrowDown":
		d.Offset++
	case "ArrowUp":
		d.Offset--
	case "PageDown":
		d.Offset += a.Rows - 2
	case "PageUp":
		d.Offset -= a.Rows - 2
	case "Home":
		d.Offset = 0
		d.Column = 0
	case "End":
		d.Offset = max(0, len(d.Lines())-a.Rows+2)
	case "ArrowLeft":
		d.Column = max(0, d.Column-step)
	case "ArrowRight":
		d.Column += step
	}
	d.Offset = max(0, min(d.Offset, len(d.Lines())-1))
	d.ViewStart = d.At(d.Offset, 0)
}
func (a *App) adjacentFile(next bool) {
	p := &a.Panels[a.Active]
	if a.Doc.ArchiveSource || p.kind() == backendArchive {
		a.Status = "Return to the archive panel to choose another file"
		return
	}
	index := p.Cursor
	step := -1
	if next {
		step = 1
	}
	for i := index + step; i >= 0 && i < len(p.Entries); i += step {
		if !p.Entries[i].Dir {
			old := a.Doc
			target := i
			name := p.Entries[target].Name
			replace := func(d *Document) {
				releaseDocument(old)
				if a.Doc == old {
					a.Doc = nil
				}
				p.Cursor = target
				a.showDocument(d)
			}
			proceed := func() {
				if src := p.backend().DocumentSource(p, filepath.Join(p.Path, name)); src != nil {
					a.openSourceDocument(src, false, replace, nil)
					return
				}
				path := filepath.Join(p.Path, name)
				if a.Async {
					a.openDocumentAsync(path, false, replace)
					return
				}
				d, err := a.loadDocument(path, false)
				if err != nil {
					a.openFailed(path, false, err)
					return
				}
				a.recordView(d.Path, false)
				a.presentDocument(d, replace)
			}
			if old.Dirty {
				a.confirm("Save changes", "Save changes before viewing the next file?", []string{"Save", "Discard", "Cancel"}, func(choice int) {
					if choice == 2 {
						return
					}
					if choice == 0 {
						a.saveDocumentThen(old, old.Path, false, proceed)
						return
					}
					proceed()
				})
			} else {
				proceed()
			}
			return
		}
	}
	a.Status = fmt.Sprintf("No %s file", map[bool]string{true: "next", false: "previous"}[next])
}

// releaseDocument frees what a replaced document holds outside memory.
func releaseDocument(d *Document) {
	if d == nil {
		return
	}
	if d.File != nil {
		d.File.Close()
		d.File = nil
	}
	if d.TempDir != "" {
		os.RemoveAll(d.TempDir)
		d.TempDir = ""
	}
}
