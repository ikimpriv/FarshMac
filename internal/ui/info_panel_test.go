package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInfoPanelFollowsOppositeFolder(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(left, "README.md"), []byte("OPPOSITE_DESCRIPTION"), 0600)
	a, _ := New(left, right)
	defer a.Close()
	a.toggleOtherView("Info panel")
	deadline := time.Now().Add(5 * time.Second)
	for len(a.Panels[1].infoRows) == 0 && time.Now().Before(deadline) {
		a.Poll()
		time.Sleep(time.Millisecond)
	}
	var text strings.Builder
	for _, r := range a.Panels[1].infoRows {
		text.WriteString(r.Label + " " + r.Value + "\n")
	}
	for _, want := range []string{"Computer name", "User name", "Total bytes", "Free bytes", "Volume UUID", "Memory", "Installed memory", "Swap total", "Power status", "OPPOSITE_DESCRIPTION"} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("missing %q: %s", want, text.String())
		}
	}
	if a.infoDescriptionPaths[1] != filepath.Join(left, "README.md") {
		t.Fatal("wrong description path")
	}
	a.Active = 1
	a.key(Message{Key: "F12", Ctrl: true})
	if a.Form == nil || a.Form.Title != "Info panel settings" {
		t.Fatal("missing info section settings")
	}
	a.Form = nil
	a.scrollMouse(Message{X: a.Cols - 5, Y: 4, Delta: 4})
	if a.Panels[1].InfoOffset == 0 {
		t.Fatal("info does not scroll")
	}
}

func TestInfoPanelVisualFixture(t *testing.T) {
	out := os.Getenv("FARSHMAC_INFO_FIXTURE")
	if out == "" {
		t.Skip("visual export not requested")
	}
	a, _ := New(t.TempDir(), t.TempDir())
	defer a.Close()
	a.Cols = 150
	a.Rows = 43
	a.Panels[0].Path = "/Users/example/Projects"
	a.Panels[0].Entries = []Entry{{Name: "..", Dir: true}, {Name: "large", Dir: true}, {Name: "small", Dir: true}}
	a.Panels[0].folderSizes = map[string]folderSizeResult{"large": {Bytes: 1582034500}, "small": {Bytes: 23522}}
	a.Panels[1].View = "Info panel"
	a.Panels[1].infoRows = readInfoPanel(os.TempDir(), "", a.Settings).rows
	b, err := json.Marshal(a.Draw())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(out, b, 0600); err != nil {
		t.Fatal(err)
	}
}
