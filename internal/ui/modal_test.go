package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
)

func TestFormFieldsButtonsAndMnemonics(t *testing.T) {
	a := &App{Cols: 100, Rows: 32, Settings: defaultSettings()}
	submitted := false
	fields := []Field{{Label: "Name", Value: "hello"}, {Label: "Case sensitive", Toggle: true}, {Label: "Search subfolders", Toggle: true, Checked: true}}
	a.form("Find file", fields, func([]Field) { submitted = true })
	a.Form.Primary = "Find"
	a.Form.CheckColumns = 2
	box := a.formGeometry()
	g := NewGrid(a.Cols, a.Rows)
	a.drawForm(g)
	if g.Cells[box.Y*g.W+box.X].BG != Gray || g.Cells[(box.Y+1)*g.W+box.X+2].Text != "╔" {
		t.Fatal("missing padded frame")
	}
	pos := box.Fields[0]
	if g.Cells[(pos.Y+1)*g.W+pos.X+pos.W-1].BG != Cyan {
		t.Fatal("input background missing")
	}
	if box.Fields[1].Y != box.Fields[2].Y || box.Fields[1].X >= box.Fields[2].X {
		t.Fatal("checkbox columns")
	}
	a.formKey(Message{Key: "a", Alt: true})
	if !a.Form.Fields[1].Checked || a.Form.Focus != 1 {
		t.Fatal("checkbox mnemonic")
	}
	a.formKey(Message{Key: "Tab"})
	a.formKey(Message{Key: "Tab"})
	if a.Form.Focus != len(fields) {
		t.Fatal("Tab did not focus default button")
	}
	a.formKey(Message{Key: "Tab"})
	a.formKey(Message{Key: "Enter"})
	if submitted || a.Form != nil {
		t.Fatal("Cancel submitted")
	}
	a.form("Find file", fields, func([]Field) { submitted = true })
	a.Form.Primary = "Find"
	box = a.formGeometry()
	button := buttonLayout(a.Form.buttons(), box.W)[0]
	a.formClick(Message{X: box.X + button.X + 2, Y: box.ButtonsY + button.Y})
	if !submitted || a.Form != nil {
		t.Fatal("default button click missed")
	}
}
func TestFormScrollKeepsFocusedFieldVisible(t *testing.T) {
	a := &App{Cols: 80, Rows: 18, Settings: defaultSettings()}
	fields := make([]Field, 20)
	for i := range fields {
		fields[i] = Field{Label: "Field", Value: "long value"}
	}
	a.form("Many fields", fields, func([]Field) {})
	for focus := range fields {
		a.Form.Focus = focus
		box := a.formGeometry()
		pos := box.Fields[focus]
		if pos.Y < box.Top || pos.Y+pos.H > box.Bottom {
			t.Fatalf("clipped focus %d", focus)
		}
		g := NewGrid(a.Cols, a.Rows)
		a.drawForm(g)
		a.formClick(Message{X: pos.X, Y: pos.Y + 1})
		if a.Form.Focus != focus {
			t.Fatal("scrolled hit testing")
		}
	}
}
func TestPromptAndProgressButtonHitTesting(t *testing.T) {
	a := &App{Cols: 100, Rows: 30, Settings: defaultSettings()}
	submitted := false
	a.prompt("Copy", "Destination", "/tmp/out", func(string) { submitted = true })
	r := a.promptBounds()
	button := buttonLayout([]string{"OK", "Cancel"}, r.W)[1]
	a.click(Message{X: r.X + button.X, Y: r.Y + r.H - 3 + button.Y})
	if submitted || a.Dialog != nil {
		t.Fatal("prompt cancel hit box")
	}
	a.prompt("Copy", "Destination", "/tmp/out", func(string) { submitted = true })
	a.key(Message{Key: "Tab"})
	a.key(Message{Key: "Enter"})
	if !submitted {
		t.Fatal("prompt keyboard button")
	}
	a.Operation = fsops.NewOperation("Copy")
	a.Busy = true
	r = a.progressBounds()
	button = buttonLayout([]string{"Cancel"}, r.W)[0]
	a.click(Message{X: r.X + button.X, Y: r.Y + r.H - 4 + button.Y})
	if a.Operation.Check() == nil {
		t.Fatal("progress cancel hit box")
	}
}

// Optional actual Grid fixtures for visual comparison; no mock HTML renderer.
func TestDialogVisualFixtures(t *testing.T) {
	dir := os.Getenv("FARSHMAC_DIALOG_FIXTURES")
	if dir == "" {
		t.Skip("visual export not requested")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a := &App{Cols: 100, Rows: 32, Settings: defaultSettings()}
	for i := range a.Panels {
		a.Panels[i] = Panel{Path: "/Users/example/Projects", Entries: []Entry{{Name: "..", Dir: true}, {Name: "Documents", Dir: true}, {Name: "src", Dir: true}, {Name: "README.md"}}, Marked: map[string]bool{}}
	}
	save := func(name string) {
		t.Helper()
		g := a.Draw()
		b, err := json.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, name+".json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	a.findFiles(false)
	save("find")
	a.Form = nil
	a.settingsMenu("Editor settings")
	save("editor")
	a.Form = nil
	a.Panels[0].Cursor = 1
	a.transferDialog(false)
	save("copy")
	a.Form = nil
	a.confirm("Overwrite", "README.md already exists. Replace it?", []string{"Overwrite", "All", "Skip", "Skip all", "Cancel"}, func(int) {})
	save("overwrite")
	a.Form = nil
	a.Operation = fsops.NewOperation("Copy")
	a.Operation.Add("README.md", 1000)
	a.Operation.Ready()
	a.Operation.Begin("README.md", 1000)
	a.Operation.Advance(420)
	save("progress")
	a.Operation = nil
	a.Menu = &MenuState{Column: 2}
	save("commands-menu")
	a.Menu = &MenuState{Column: 3}
	save("options-menu")
	a.Menu = &MenuState{Column: 4}
	save("panel-menu")
	a.Menu = nil
	a.settingsMenu("Panel settings")
	save("panel-settings")
	a.settingsMenu("Terminal settings")
	save("terminal-settings")
	a.confirm("Cannot preserve metadata", "Move file.txt with changed metadata and remove the source? Destination cannot preserve original permissions.", []string{"Move anyway", "For all", "Cancel"}, func(int) {})
	save("move-metadata")
	a.Form = nil
	a.networkMenu()
	save("network-menu")
	a.Form = nil
	a.connectionDialog(remotefs.Config{Protocol: "sftp", Host: "example.org", Port: 22, User: "demo", Directory: "/home/demo"})
	save("network-connection")
}

func TestMacOptionDialogMnemonic(t *testing.T) {
	a := &App{Cols: 80, Rows: 25, Settings: defaultSettings()}
	a.form("Options", []Field{{Label: "Mode", Value: "test"}}, func([]Field) { t.Fatal("Cancel submitted") })
	a.key(Message{Key: "ç", Code: "KeyC", Alt: true})
	if a.Form != nil {
		t.Fatal("macOS Option+C did not activate Cancel")
	}
}
func TestCompactProgressKeepsFrameIntact(t *testing.T) {
	a := &App{Cols: 60, Rows: 15, Operation: fsops.NewOperation("Copy")}
	r := a.progressBounds()
	g := NewGrid(a.Cols, a.Rows)
	a.drawProgress(g)
	for x := r.X + 3; x < r.X+r.W-3; x++ {
		if g.Cells[(r.Y+r.H-2)*g.W+x].Text != "═" {
			t.Fatal("progress controls overlap lower frame")
		}
	}
}
