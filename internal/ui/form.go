package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type Field struct {
	Label, Value    string
	Toggle, Checked bool
	Secret          bool
}
type Form struct {
	Scroll        int
	CheckColumns  int
	Primary       string
	Title         string
	Message       string
	Buttons       bool
	Cancel        func()
	Fields        []Field
	Focus, Cursor int
	Selected      bool
	Submit        func([]Field)
	Choice        []string
	Select        func(int)
}

func (a *App) form(title string, fields []Field, submit func([]Field)) {
	cursor := 0
	if len(fields) > 0 {
		cursor = utf8.RuneCountInString(fields[0].Value)
	}
	a.Form = &Form{Title: title, Fields: fields, Cursor: cursor, Submit: submit}
}
func (a *App) choose(title string, choices []string, selectItem func(int)) {
	if len(choices) == 0 {
		a.Status = title + ": empty"
		return
	}
	a.Form = &Form{Title: title, Choice: choices, Select: selectItem}
}
func (a *App) confirm(title, message string, buttons []string, selectItem func(int)) {
	a.Form = &Form{Title: title, Message: message, Buttons: true, Choice: buttons, Select: selectItem, Cancel: func() { selectItem(len(buttons) - 1) }}
}
func (a *App) formKey(m Message) {
	f := a.Form
	if m.Key == "Escape" {
		a.Form = nil
		if f.Cancel != nil {
			f.Cancel()
		}
		return
	}
	count := len(f.Fields) + 2
	if len(f.Choice) > 0 {
		count = len(f.Choice)
	}
	if m.Alt && utf8.RuneCountInString(m.Key) == 1 {
		pressed := unicode.ToLower([]rune(m.Key)[0])
		for i, key := range f.mnemonics() {
			if key == pressed {
				if len(f.Choice) > 0 {
					f.Focus = i
					if f.Buttons {
						a.formKey(Message{Key: "Enter"})
					}
					return
				}
				f.Focus = i
				f.Selected = false
				if i >= len(f.Fields) {
					a.formKey(Message{Key: "Enter"})
				} else if f.Fields[i].Toggle {
					f.Fields[i].Checked = !f.Fields[i].Checked
				} else {
					f.Cursor = utf8.RuneCountInString(f.Fields[i].Value)
				}
				return
			}
		}
	}
	switch m.Key {
	case "Tab", "ArrowDown", "ArrowUp", "ArrowLeft", "ArrowRight":
		f.Selected = false
		if (m.Key == "ArrowLeft" || m.Key == "ArrowRight") && !f.Buttons && f.Focus < len(f.Fields) {
			break
		}
		delta := 1
		if m.Key == "ArrowUp" || m.Key == "ArrowLeft" || m.Shift {
			delta = -1
		}
		if a.Settings.MenuWrap || len(f.Choice) == 0 {
			f.Focus = (f.Focus + delta + count) % count
		} else {
			f.Focus = max(0, min(count-1, f.Focus+delta))
		}
		if f.Focus < len(f.Fields) {
			f.Cursor = utf8.RuneCountInString(f.Fields[f.Focus].Value)
		}
		return
	case "Enter":
		a.Form = nil
		if len(f.Choice) > 0 {
			f.Select(f.Focus)
		} else if f.Focus == len(f.Fields)+1 {
			if f.Cancel != nil {
				f.Cancel()
			}
		} else {
			f.Submit(f.Fields)
		}
		return
	}
	if len(f.Choice) > 0 {
		if m.Key == "Home" {
			f.Focus = 0
		}
		if m.Key == "End" {
			f.Focus = count - 1
		}
		return
	}
	if f.Focus >= len(f.Fields) {
		if m.Key == " " {
			a.formKey(Message{Key: "Enter"})
		}
		return
	}
	field := &f.Fields[f.Focus]
	if field.Toggle {
		if m.Key == " " || m.Key == "ArrowLeft" || m.Key == "ArrowRight" {
			field.Checked = !field.Checked
		}
		return
	}
	r := []rune(field.Value)
	f.Cursor = max(0, min(f.Cursor, len(r)))
	if m.Meta && strings.EqualFold(m.Key, "a") {
		f.Selected = true
		return
	}
	if f.Selected {
		if m.Key == "Backspace" || m.Key == "Delete" || utf8.RuneCountInString(m.Key) == 1 && !m.Meta && !m.Ctrl {
			r = nil
			f.Cursor = 0
		}
		f.Selected = false
	}
	switch m.Key {
	case "ArrowLeft":
		f.Cursor = max(0, f.Cursor-1)
	case "ArrowRight":
		f.Cursor = min(len(r), f.Cursor+1)
	case "Home":
		f.Cursor = 0
	case "End":
		f.Cursor = len(r)
	case "Backspace":
		if f.Cursor > 0 {
			r = append(r[:f.Cursor-1], r[f.Cursor:]...)
			f.Cursor--
		}
	case "Delete":
		if f.Cursor < len(r) {
			r = append(r[:f.Cursor], r[f.Cursor+1:]...)
		}
	default:
		if utf8.RuneCountInString(m.Key) == 1 && !m.Meta && !m.Ctrl {
			r = append(r[:f.Cursor], append([]rune(m.Key), r[f.Cursor:]...)...)
			f.Cursor++
		}
	}
	field.Value = string(r)
}
func (a *App) formPaste(s string) {
	f := a.Form
	if f.Focus >= len(f.Fields) {
		return
	}
	field := &f.Fields[f.Focus]
	if field.Toggle {
		return
	}
	if f.Selected {
		field.Value = ""
		f.Cursor = 0
		f.Selected = false
	}
	r := []rune(field.Value)
	at := min(f.Cursor, len(r))
	field.Value = string(r[:at]) + s + string(r[at:])
	f.Cursor = at + utf8.RuneCountInString(s)
}

func (f *Form) buttons() []string {
	primary := f.Primary
	if primary == "" {
		primary = "OK"
	}
	return []string{primary, "Cancel"}
}
func (f *Form) mnemonics() []rune {
	if len(f.Choice) > 0 {
		return mnemonicKeys(f.Choice)
	}
	labels := make([]string, 0, len(f.Fields)+2)
	for _, field := range f.Fields {
		labels = append(labels, field.Label)
	}
	keys := mnemonicKeys(append(f.buttons(), labels...))
	return append(keys[2:], keys[:2]...)
}

type formFieldRect struct{ X, Y, W, H int }
type formGeometry struct {
	modalRect
	Fields                []formFieldRect
	Rules                 []int
	Top, Bottom, ButtonsY int
}

func (a *App) formGeometry() formGeometry {
	f := a.Form
	rows := 0
	var fields []formFieldRect
	var rules []int
	for i := 0; i < len(f.Fields); {
		if i > 0 && f.Fields[i-1].Toggle != f.Fields[i].Toggle {
			rules = append(rules, rows)
			rows++
		}
		if f.Fields[i].Toggle && f.CheckColumns == 2 {
			end := i
			for end < len(f.Fields) && f.Fields[end].Toggle {
				end++
			}
			count := (end - i + 1) / 2
			for j := i; j < end; j++ {
				fields = append(fields, formFieldRect{X: (j - i) / count, Y: rows + (j-i)%count, H: 1})
			}
			rows += count
			i = end
		} else {
			h := 2
			if f.Fields[i].Toggle {
				h = 1
			}
			fields = append(fields, formFieldRect{Y: rows, H: h})
			rows += h
			i++
		}
	}
	if len(f.Choice) > 0 {
		rows = len(f.Choice)
	}
	r := modalBounds(a.Cols, a.Rows, 88, rows+8)
	bottom := r.Y + r.H - 5
	available := max(1, bottom-(r.Y+2))
	start, end := 0, 0
	if len(f.Choice) > 0 {
		start = f.Focus
		end = start + 1
	} else if f.Focus < len(fields) {
		start = fields[f.Focus].Y
		end = start + fields[f.Focus].H
	} else {
		start = f.Scroll
		end = start
	}
	if start < f.Scroll {
		f.Scroll = start
	}
	if end > f.Scroll+available {
		f.Scroll = end - available
	}
	f.Scroll = max(0, min(f.Scroll, max(0, rows-available)))
	out := formGeometry{modalRect: r, Top: r.Y + 2, Bottom: bottom, ButtonsY: r.Y + r.H - 4}
	for i, field := range fields {
		column := field.X
		field.X = r.X + 4
		field.W = r.W - 8
		if f.Fields[i].Toggle && f.CheckColumns == 2 {
			field.W = (r.W - 12) / 2
			field.X += column * (field.W + 4)
		}
		field.Y += out.Top - f.Scroll
		out.Fields = append(out.Fields, field)
	}
	for _, row := range rules {
		out.Rules = append(out.Rules, out.Top+row-f.Scroll)
	}
	return out
}
func (a *App) drawForm(g *Grid) {
	f := a.Form
	if f.Buttons {
		a.drawConfirmation(g)
		return
	}
	box := a.formGeometry()
	drawModal(g, box.modalRect, f.Title)
	keys := f.mnemonics()
	if len(f.Choice) > 0 {
		for i := f.Scroll; i < len(f.Choice) && box.Top+i-f.Scroll < box.Bottom; i++ {
			y := box.Top + i - f.Scroll
			bg := uint32(Gray)
			if i == f.Focus {
				bg = Cyan
			}
			g.Fill(box.X+4, y, box.W-8, 1, Black, bg)
			modalLabel(g, box.X+4, y, f.Choice[i], keys[i], bg, box.W-8)
		}
	} else {
		for _, y := range box.Rules {
			if y >= box.Top && y < box.Bottom {
				modalRule(g, box.modalRect, y)
			}
		}
		for i, pos := range box.Fields {
			if pos.Y < box.Top || pos.Y+pos.H > box.Bottom {
				continue
			}
			field := f.Fields[i]
			if field.Toggle {

				mark := "[ ] "
				if field.Checked {
					mark = "[x] "
				}
				bg := uint32(Gray)
				if i == f.Focus {
					bg = Cyan
				}
				modalLabel(g, pos.X, pos.Y, mark+field.Label, keys[i], bg, pos.W)
			} else {
				label := field.Label
				if !strings.HasSuffix(label, ":") {
					label += ":"
				}
				modalLabel(g, pos.X, pos.Y, label, keys[i], Gray, pos.W)
				cursor := utf8.RuneCountInString(field.Value)
				if i == f.Focus {
					cursor = f.Cursor
				}
				value := field.Value
				if field.Secret {
					value = strings.Repeat("•", utf8.RuneCountInString(value))
				}
				modalInput(g, pos.X, pos.Y+1, pos.W, value, cursor, i == f.Focus, i == f.Focus && f.Selected)
			}
		}
	}
	modalRule(g, box.modalRect, box.Bottom)
	if f.CheckColumns == 2 {
		for i := 0; i < len(f.Fields); {
			if !f.Fields[i].Toggle {
				i++
				continue
			}
			end := i
			top, bottom := box.Fields[i].Y, box.Fields[i].Y+1
			for end < len(f.Fields) && f.Fields[end].Toggle {
				top = min(top, box.Fields[end].Y)
				bottom = max(bottom, box.Fields[end].Y+1)
				end++
			}
			limit := box.Bottom
			for _, y := range box.Rules {
				if y >= bottom {
					limit = min(limit, y)
				}
			}
			x := box.X + box.W/2
			for y := max(top, box.Top); y < min(limit, box.Bottom); y++ {
				g.Set(x, y, "│", Black, Gray)
			}
			if top-1 >= box.Top {
				g.Set(x, top-1, "┬", Black, Gray)
			}
			if limit >= box.Top && limit <= box.Bottom {
				g.Set(x, limit, "┴", Black, Gray)
			}
			i = end
		}
	}

	labels := f.buttons()
	focus := f.Focus - len(f.Fields)
	buttonKeys := keys[len(f.Fields):]
	if len(f.Choice) > 0 {
		labels = []string{"Select", "Cancel"}
		focus = -1
		buttonKeys = nil
	}
	drawModalButtons(g, box.modalRect, box.ButtonsY, labels, focus, buttonKeys)
	if f.Scroll > 0 {
		g.Set(box.X+box.W-3, box.Top, "▲", Black, Gray)
	}
}
func (a *App) confirmationBounds() (int, int, int, int) {
	w := min(88, a.Cols-4)
	buttons := buttonLayout(a.Form.Choice, w)
	extra := 0
	if len(buttons) > 0 {
		extra = buttons[len(buttons)-1].Y
	}
	r := modalBounds(a.Cols, a.Rows, w, 10+extra)
	return r.X, r.Y, r.W, r.H
}
func (a *App) drawConfirmation(g *Grid) {
	f := a.Form
	x, y, w, h := a.confirmationBounds()
	r := modalRect{x, y, w, h}
	drawModal(g, r, f.Title)
	message := []rune(f.Message)
	for line := 0; line < 3 && line*(w-8) < len(message); line++ {
		at := line * (w - 8)
		g.Text(x+4, y+2+line, string(message[at:min(len(message), at+w-8)]), Black, Gray, w-8)
	}
	modalRule(g, r, y+5)
	drawModalButtons(g, r, y+6, f.Choice, f.Focus, f.mnemonics())
}
func (a *App) formClick(m Message) {
	f := a.Form
	if f.Buttons {
		x, y, w, _ := a.confirmationBounds()
		for i, pos := range buttonLayout(f.Choice, w) {
			if m.Y == y+6+pos.Y && m.X >= x+pos.X && m.X < x+pos.X+pos.W {
				f.Focus = i
				a.formKey(Message{Key: "Enter"})
				return
			}
		}
		return
	}
	box := a.formGeometry()
	labels := f.buttons()
	if len(f.Choice) > 0 {
		labels = []string{"Select", "Cancel"}
	}
	for i, pos := range buttonLayout(labels, box.W) {
		if m.Y == box.ButtonsY+pos.Y && m.X >= box.X+pos.X && m.X < box.X+pos.X+pos.W {
			if i == 1 {
				a.formKey(Message{Key: "Escape"})
			} else {
				if len(f.Choice) == 0 {
					f.Focus = len(f.Fields)
				}
				a.formKey(Message{Key: "Enter"})
			}
			return
		}
	}
	if m.Y < box.Top || m.Y >= box.Bottom {
		return
	}
	if len(f.Choice) > 0 {
		index := m.Y - box.Top + f.Scroll
		if m.X >= box.X+4 && m.X < box.X+box.W-4 && index >= 0 && index < len(f.Choice) {
			f.Focus = index
			a.formKey(Message{Key: "Enter"})
		}
		return
	}
	for i, pos := range box.Fields {
		if pos.Y < box.Top || pos.Y+pos.H > box.Bottom {
			continue
		}
		if m.X >= pos.X && m.X < pos.X+pos.W && m.Y >= pos.Y && m.Y < pos.Y+pos.H {
			f.Focus = i
			f.Selected = false
			f.Cursor = utf8.RuneCountInString(f.Fields[i].Value)
			if f.Fields[i].Toggle {
				f.Fields[i].Checked = !f.Fields[i].Checked
			}
			return
		}
	}
}
