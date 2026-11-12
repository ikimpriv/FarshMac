package ui

import (
	"strings"
	"testing"

	"farshmac/internal/remotefs"
)

// menuLabelColor returns the foreground color of the first cell of label in
// the drawn menu.
func menuLabelColor(t *testing.T, a *App, label string) uint32 {
	t.Helper()
	g := a.Draw()
	for y := 0; y < g.H; y++ {
		var row strings.Builder
		for x := 0; x < g.W; x++ {
			row.WriteString(g.Cells[y*g.W+x].Text)
		}
		if i := strings.Index(row.String(), label); i >= 0 {
			col := len([]rune(row.String()[:i]))
			return g.Cells[y*g.W+col].FG
		}
	}
	t.Fatal("label not drawn", label)
	return 0
}
func TestMenuMarksUnavailableItemsForRemoteAndArchive(t *testing.T) {
	a := formTestApp(t)
	a.Cols, a.Rows = 120, 40
	a.Menu = &MenuState{Column: 1}
	if menuLabelColor(t, a, "Link…") != White {
		t.Fatal("local Link… disabled")
	}
	a.Panels[0].Remote = &remotefs.Config{Protocol: "sftp", Host: "example.org", Port: 22}
	a.Active = 0
	if menuLabelColor(t, a, "Link…") != Gray || menuLabelColor(t, a, "Copy…") != White {
		t.Fatal("remote Link… not marked")
	}
	a.Menu = &MenuState{Column: 0}
	if menuLabelColor(t, a, "Quick view") != Gray {
		t.Fatal("remote quick view not marked")
	}
	a.Menu = &MenuState{Column: 4}
	if menuLabelColor(t, a, "Quick view") != White {
		t.Fatal("local right panel quick view marked")
	}
}
func TestMenuShowsFarShortcuts(t *testing.T) {
	a := formTestApp(t)
	want := map[string]string{"Save setup": "⇧F9", "Link…": "⌥F6", "Create archive…": "⇧F1", "Folder history": "⌥F12", "Folder shortcuts…": "⌃1…9", "Quick view on/off": "⌃Q", "Sort modes…": "⌃F12"}
	for _, section := range a.menuSections() {
		for _, item := range section.Items {
			if w, ok := want[item.Label]; ok && item.Shortcut != w {
				t.Fatalf("%s: %q", item.Label, item.Shortcut)
			}
			if strings.HasPrefix(item.Shortcut, "Alt") || strings.HasPrefix(item.Shortcut, "Shift") || strings.HasPrefix(item.Shortcut, "Ctrl") {
				t.Fatalf("mixed notation %q", item.Shortcut)
			}
		}
	}
}
