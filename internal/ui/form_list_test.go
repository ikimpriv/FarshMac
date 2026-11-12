package ui

import (
	"fmt"
	"strings"
	"testing"
)

func TestChoosePageKeysAndTypeAhead(t *testing.T) {
	a := formTestApp(t)
	a.Cols, a.Rows = 100, 30
	var items []string
	for i := 0; i < 100; i++ {
		items = append(items, fmt.Sprintf("item %03d", i))
	}
	items[57] = "special abc"
	items[80] = "another abd"
	chosen := -1
	a.choose("List", items, func(i int) { chosen = i })
	rows := a.listRows()
	a.key(Message{Key: "PageDown"})
	if a.Form.Focus != rows {
		t.Fatal("PageDown", a.Form.Focus, rows)
	}
	a.key(Message{Key: "End"})
	a.key(Message{Key: "PageDown"})
	if a.Form.Focus != 99 {
		t.Fatal("PageDown wrapped", a.Form.Focus)
	}
	a.key(Message{Key: "Home"})
	a.key(Message{Key: "a"})
	a.key(Message{Key: "b"})
	if a.Form.Query != "ab" || len(a.Form.Choice) != 2 || !strings.Contains(gridText(a), "List [ab]") {
		t.Fatal("filter", a.Form.Choice)
	}
	a.key(Message{Key: "z"})
	if a.Form.Query != "ab" || len(a.Form.Choice) != 2 {
		t.Fatal("non-matching key changed filter")
	}
	a.key(Message{Key: "ArrowDown"})
	a.key(Message{Key: "Escape"})
	if a.Form == nil || a.Form.Query != "" || len(a.Form.Choice) != 100 || a.Form.Focus != 80 {
		t.Fatal("Esc did not clear filter and keep focus", a.Form)
	}
	a.key(Message{Key: "a"})
	a.key(Message{Key: "Backspace"})
	if len(a.Form.Choice) != 100 {
		t.Fatal("Backspace did not widen filter")
	}
	a.key(Message{Key: "s"})
	a.key(Message{Key: "p"})
	a.key(Message{Key: "Enter"})
	if chosen != 57 {
		t.Fatal("filtered selection mapped wrong", chosen)
	}
}
func TestChooseRepeatedMnemonicCycles(t *testing.T) {
	a := formTestApp(t)
	a.choose("List", []string{"apple", "banana", "avocado"}, func(int) {})
	a.key(Message{Key: "a", Alt: true})
	first := a.Form.Focus
	a.key(Message{Key: "a", Alt: true})
	if a.Form.Focus == first {
		t.Fatal("repeated mnemonic did not move")
	}
}
func TestChooseWheelScrolls(t *testing.T) {
	a := formTestApp(t)
	a.Cols, a.Rows = 100, 30
	var items []string
	for i := 0; i < 100; i++ {
		items = append(items, fmt.Sprint(i))
	}
	a.choose("List", items, func(int) {})
	a.Handle(`{"Type":"wheel","Delta":5}`)
	a.Draw()
	if a.Form.Focus != 0 || a.Form.Scroll != 5 {
		t.Fatal("wheel moved focus or did not scroll", a.Form.Focus, a.Form.Scroll)
	}
}
func gridText(a *App) string {
	var b strings.Builder
	for _, c := range a.Draw().Cells {
		b.WriteString(c.Text)
	}
	return b.String()
}
