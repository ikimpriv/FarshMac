package ui

import (
	"strings"
	"testing"
)

func TestShellReadyDoesNotOverwritePanelStatus(t *testing.T) {
	a := formTestApp(t)
	a.Status = "Setup saved"
	a.setShellStatus("Exit 0 · ⌘O panels / terminal")
	if a.Status != "Setup saved" {
		t.Fatal("shell overwrote panel message")
	}
	a.Status = ""
	a.setShellStatus("Exit 1 · ⌘O panels / terminal")
	a.setShellStatus("Exit 2 · ⌘O panels / terminal")
	if a.Status != "Exit 2 · ⌘O panels / terminal" {
		t.Fatal("shell status not refreshed", a.Status)
	}
	a.Status = "Connected"
	a.TermVisible = true
	a.setShellStatus("Exit 3 · ⌘O panels / terminal")
	if a.Status != "Exit 3 · ⌘O panels / terminal" {
		t.Fatal("terminal mode status not updated")
	}
}
func TestLongStatusIsTruncatedWithEllipsis(t *testing.T) {
	a := formTestApp(t)
	a.Cols, a.Rows = 40, 20
	a.Status = strings.Repeat("x", 60)
	g := a.Draw()
	if got := g.Cells[(g.H-3)*g.W+g.W-1].Text; got != "…" {
		t.Fatalf("last column %q", got)
	}
}
func TestStatusClickShowsFullMessage(t *testing.T) {
	a := formTestApp(t)
	a.Cols, a.Rows = 40, 20
	long := "no usable SSH identity: " + strings.Repeat("detail ", 10)
	a.Status = "first message"
	a.Draw()
	a.Status = long
	a.Draw()
	a.click(Message{X: 3, Y: a.Rows - 3})
	if a.Form == nil || a.Form.Title != "Messages" || !strings.HasPrefix(a.Form.Message, long) || !strings.Contains(a.Form.Message, "first message") {
		t.Fatal("messages not shown", a.Form)
	}
}
