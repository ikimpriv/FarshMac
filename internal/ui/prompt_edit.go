package ui

import "unicode/utf8"

type promptSpec struct {
	title, message string
	fn             func(string)
}

// rejectPrompt reopens the prompt whose Confirm is running with the value the
// user entered, instead of losing it to a status-line error (P5-15).
func (a *App) rejectPrompt(value, status string) {
	p := a.activePrompt
	if p == nil {
		a.Status = status
		return
	}
	a.promptRejected = true
	a.prompt(p.title, p.message, value, p.fn)
	a.Dialog.Input = value
	a.Dialog.Selected = true
	a.Status = status
}

// The caret is stored as a distance from the end, so code that replaces
// Input directly keeps the caret at the end, as before.
func (d *Dialog) caret() int {
	n := utf8.RuneCountInString(d.Input)
	return n - max(0, min(n, d.Back))
}
func (d *Dialog) insert(s string) {
	r := []rune(d.Input)
	at := d.caret()
	d.Input = string(r[:at]) + s + string(r[at:])
}
func (d *Dialog) moveCaret(key string) {
	n := utf8.RuneCountInString(d.Input)
	back := max(0, min(n, d.Back))
	switch key {
	case "ArrowLeft":
		back = min(n, back+1)
	case "ArrowRight":
		back = max(0, back-1)
	case "Home":
		back = n
	case "End":
		back = 0
	}
	d.Back = back
}
func (d *Dialog) deleteAtCaret(before bool) {
	r := []rune(d.Input)
	at := d.caret()
	if before && at > 0 {
		d.Input = string(append(r[:at-1:at-1], r[at:]...))
	} else if !before && at < len(r) {
		d.Input = string(append(r[:at:at], r[at+1:]...))
		d.Back = max(0, d.Back-1)
	}
}
