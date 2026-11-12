package ui

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

const statusLogLimit = 20

// statusLog keeps recent status-line messages of one window, so a message
// overwritten or truncated on screen can still be read (P5-22).
type statusLog struct {
	messages    []string
	lastStatus  string
	shellStatus string // last "Exit N" text, which the next one may replace
}

// setShellStatus reports a finished shell command without overwriting a
// panel message the user has not seen in the terminal.
func (a *App) setShellStatus(status string) {
	if a.TermVisible || a.Status == "" || a.Status == a.shellStatus {
		a.Status = status
	}
	a.shellStatus = status
}

// recordStatus appends a new status-line message to the window's log.
func (a *App) recordStatus() {
	s := a.Status
	if s == a.lastStatus {
		return
	}
	a.lastStatus = s
	if s == "" || s == a.shellStatus || a.Busy {
		return
	}
	if n := len(a.messages); n > 0 && a.messages[n-1] == s {
		return
	}
	a.messages = append(a.messages, s)
	if len(a.messages) > statusLogLimit {
		a.messages = a.messages[len(a.messages)-statusLogLimit:]
	}
}

// showMessages opens the recent messages, newest first, in full.
func (a *App) showMessages() {
	a.recordStatus()
	if len(a.messages) == 0 {
		a.Status = "No messages"
		return
	}
	lines := make([]string, 0, len(a.messages))
	for i := len(a.messages) - 1; i >= 0; i-- {
		lines = append(lines, a.messages[i])
	}
	a.confirm("Messages", strings.Join(lines, "\n\n"), []string{"OK"}, func(int) {})
}

// fitStatus truncates the status line with an ellipsis when it does not fit.
func fitStatus(s string, width int) string {
	if width <= 0 || runewidth.StringWidth(s) <= width {
		return s
	}
	return runewidth.Truncate(s, width, "…")
}
