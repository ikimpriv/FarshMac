package ui

import (
	"strings"
	"testing"
)

func TestVolumeConnectionSeparators(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.volumes(0)
	f := a.Form
	separator := -1
	for i, label := range f.Choice {
		if strings.Contains(label, "Recent connections") || strings.Contains(label, "─ Connections") {
			t.Fatal("redundant heading", label)
		}
		if f.DisabledChoices[i] && separator < 0 {
			separator = i
		}
	}
	if separator < 1 {
		t.Fatal("no separator")
	}
	f.Focus = separator - 1
	a.formKey(Message{Key: "ArrowDown"})
	if f.Focus != separator+1 {
		t.Fatal("separator received keyboard focus", f.Focus)
	}
	a.formKey(Message{Key: "ArrowUp"})
	if f.Focus != separator-1 {
		t.Fatal("reverse navigation did not skip separator")
	}
	f.Focus = separator
	a.formKey(Message{Key: "Enter"})
	if a.Form != f {
		t.Fatal("separator submitted form")
	}
	if !a.setListFilter("Docker") {
		t.Fatal("filter failed")
	}
	if len(f.Choice) != 1 || !f.choiceSelectable(0) {
		t.Fatal("filtered index mapping incorrect")
	}
}
