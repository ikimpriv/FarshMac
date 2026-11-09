package ui

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"farshmac/internal/fsops"
)

func TestCleanMenuOrderAndSeparators(t *testing.T) {
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	wantPanel := []string{"Brief", "Medium", "Full", "Wide", "Detailed", "", "Info panel", "Tree panel", "Quick view", "", "Sort modes…", "Panel on/off", "Re-read", "Change volume…"}
	for _, index := range []int{0, 4} {
		var got []string
		for _, item := range a.menuSections()[index].Items {
			got = append(got, strings.TrimPrefix(item.Label, "✓ "))
		}
		if !reflect.DeepEqual(got, wantPanel) {
			t.Fatal(got)
		}
		a.Menu = &MenuState{Column: index, Row: 4}
		a.menuKey(Message{Key: "ArrowDown"})
		if a.Menu.Row != 6 {
			t.Fatal("separator received focus")
		}
		a.menuKey(Message{Key: "ArrowUp"})
		if a.Menu.Row != 4 {
			t.Fatal("reverse separator navigation")
		}
		x, y, _, _ := a.menuBounds()
		a.menuClick(Message{X: x + 2, Y: y + 1 + 5})
		if a.Menu == nil {
			t.Fatal("separator closed menu")
		}
	}
	var got []string
	for _, item := range a.menuSections()[3].Items {
		if item.Run != nil {
			got = append(got, item.Label)
		}
	}
	want := []string{"General settings…", "Panel settings…", "Terminal settings…", "Viewer settings…", "Editor settings…", "Advanced…", "Full Disk Access…", "Save setup", "Keyboard reference"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

func TestPanelSizeFormatAndSettingsRoundTrip(t *testing.T) {
	a := &App{Cols: 160, Rows: 25, Settings: defaultSettings()}
	size := int64(123456789012)
	a.Panels[0] = Panel{Path: "/test", TotalBytes: size, ItemCount: 1, Entries: []Entry{{Name: "large", Info: fsops.ArchiveMember{Path: "large", Bytes: size}}}}
	for _, human := range []bool{false, true} {
		a.Settings.HumanReadableSizes = human
		for _, view := range []string{"Full", "Wide", "Detailed"} {
			a.Panels[0].View = view
			g := NewGrid(160, 25)
			a.drawPanel(g, 0, 0, 0, 160, 25)
			var line strings.Builder
			for _, cell := range g.Cells[2*g.W : 3*g.W] {
				line.WriteString(cell.Text)
			}
			if !strings.Contains(line.String(), panelSize(size, human)) {
				t.Fatalf("size missing in %s: %q", view, line.String())
			}
		}
	}
	a.settingsMenu("Panel settings")
	a.Form.Fields[2].Checked = false
	a.formKey(Message{Key: "Enter"})
	if a.Settings.HumanReadableSizes {
		t.Fatal("bytes setting not applied")
	}
	a.settingsMenu("Terminal settings")
	a.Form.Fields[0].Value = "sh"
	a.formKey(Message{Key: "Enter"})
	if a.Settings.Shell != "sh" {
		t.Fatal("shell not applied")
	}
	b, err := json.Marshal(savedSetup{Settings: a.Settings})
	if err != nil {
		t.Fatal(err)
	}
	var restored savedSetup
	if err = json.Unmarshal(b, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Settings.Shell != "sh" || restored.Settings.HumanReadableSizes {
		t.Fatal("settings not persisted")
	}
	a.settingsMenu("Terminal settings")
	a.Form.Fields[0].Value = "zsh; echo bad"
	a.formKey(Message{Key: "Enter"})
	if a.Settings.Shell != "sh" {
		t.Fatal("invalid shell accepted")
	}
	a.Settings.Shell = ""
	a.normalizeSettings()
	if a.Settings.Shell != "bash" {
		t.Fatal("default is not bash")
	}
}

func TestNewWindowStartsSelectedShellAfterCopyingSettings(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("ENV", "")
	t.Setenv("ZDOTDIR", root)
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	a.Settings.Shell = "sh"
	g := NewWindowGroup(a)
	defer g.CloseAll()
	id, err := g.NewWindow(1)
	if err != nil {
		t.Fatal(err)
	}
	if g.Apps[id].Term == nil || g.Apps[id].Term.Shell != "sh" {
		t.Fatal("new window started default shell before copying preferences")
	}
}
