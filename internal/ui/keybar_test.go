package ui

import (
	"strings"
	"testing"
	"time"

	"farshmac/internal/remotefs"
)

func TestKeyBarFollowsHeldModifiers(t *testing.T) {
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Settings.ShowKeys = true
	bar := gridLine(a.Draw(), a.Rows-1)
	if !strings.Contains(bar, "Trash") || !strings.Contains(bar, "MkFold") {
		t.Fatal("plain row", bar)
	}
	a.Handle(`{"Type":"modifiers","Shift":true}`)
	bar = gridLine(a.Draw(), a.Rows-1)
	if !strings.Contains(bar, "AddArc") || !strings.Contains(bar, "Rename") || strings.Contains(bar, "Trash") {
		t.Fatal("shift row", bar)
	}
	a.Handle(`{"Type":"modifiers","Ctrl":true}`)
	if bar = gridLine(a.Draw(), a.Rows-1); !strings.Contains(bar, "Unsort") || !strings.Contains(bar, "Descr") {
		t.Fatal("ctrl row", bar)
	}
	a.Handle(`{"Type":"modifiers","Alt":true}`)
	if bar = gridLine(a.Draw(), a.Rows-1); !strings.Contains(bar, "Find") || !strings.Contains(bar, "Histry") {
		t.Fatal("alt row", bar)
	}
	a.Handle(`{"Type":"modifiers","Alt":true,"Shift":true}`)
	if bar = gridLine(a.Draw(), a.Rows-1); strings.Contains(bar, "Find") || strings.Contains(bar, "AddArc") {
		t.Fatal("alt+shift row must be empty", bar)
	}
	a.Handle(`{"Type":"modifiers"}`)
	if bar = gridLine(a.Draw(), a.Rows-1); !strings.Contains(bar, "Trash") {
		t.Fatal("released", bar)
	}
	// A lost release: a key without the modifier after a quiet spell resets the row.
	a.Handle(`{"Type":"modifiers","Shift":true}`)
	a.modsAt = time.Now().Add(-3 * time.Second)
	a.Handle(`{"Type":"key","Key":"ArrowDown"}`)
	if a.mods != 0 {
		t.Fatal("stuck modifier not reset")
	}
	// Remote panels: F8 deletes, no archive commands on the Shift row.
	a.Panels[0].Remote = &remotefs.Config{Protocol: "sftp", Host: "h", Port: 22, User: "u"}
	if bar = gridLine(a.Draw(), a.Rows-1); !strings.Contains(bar, "Delete") || strings.Contains(bar, "Trash") {
		t.Fatal("remote plain row", bar)
	}
	a.Handle(`{"Type":"modifiers","Shift":true}`)
	if bar = gridLine(a.Draw(), a.Rows-1); strings.Contains(bar, "AddArc") || !strings.Contains(bar, "Rename") {
		t.Fatal("remote shift row", bar)
	}
}
