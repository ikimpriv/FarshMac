package ui

import "testing"

func selectionApp() *App {
	a := &App{Settings: defaultSettings()}
	a.Panels[0] = Panel{Entries: []Entry{{Name: "..", Dir: true}, {Name: "main.go"}, {Name: "main.txt"}, {Name: "extra.GO"}, {Name: "README"}, {Name: "folder.go", Dir: true}}, Cursor: 1, Marked: map[string]bool{}}
	return a
}
func TestSelectionMaskKeys(t *testing.T) {
	a := selectionApp()
	p := &a.Panels[0]
	a.key(Message{Key: "+", Code: "NumpadAdd"})
	if a.Dialog == nil {
		t.Fatal("no mask dialog")
	}
	a.Dialog.Confirm("*.go;*.txt|extra.*")
	a.Dialog = nil
	if !p.Marked["main.go"] || !p.Marked["main.txt"] || p.Marked["extra.GO"] || p.Marked["folder.go"] {
		t.Fatal(p.Marked)
	}
	a.key(Message{Key: "m", Meta: true})
	if len(p.Marked) != 0 {
		t.Fatal("restore failed", p.Marked)
	}
	a.key(Message{Key: "+", Code: "NumpadAdd", Ctrl: true})
	if !p.Marked["main.go"] || !p.Marked["extra.GO"] || p.Marked["folder.go"] {
		t.Fatal("extension selection", p.Marked)
	}
	a.key(Message{Key: "-", Code: "NumpadSubtract", Meta: true})
	if len(p.Marked) != 0 {
		t.Fatal("extension deselection")
	}
	a.key(Message{Key: "+", Code: "NumpadAdd", Alt: true})
	if !p.Marked["main.go"] || !p.Marked["main.txt"] || p.Marked["extra.GO"] {
		t.Fatal("base name", p.Marked)
	}
	a.key(Message{Key: "+", Code: "NumpadAdd", Shift: true})
	if len(p.Marked) != 4 {
		t.Fatal("all files", p.Marked)
	}
	a.key(Message{Key: "*", Code: "NumpadMultiply", Ctrl: true})
	if len(p.Marked) != 1 || !p.Marked["folder.go"] {
		t.Fatal("invert including folders", p.Marked)
	}
	a.key(Message{Key: "*", Code: "NumpadMultiply", Alt: true})
	if len(p.Marked) != 4 || p.Marked["folder.go"] {
		t.Fatal("invert files clear folders", p.Marked)
	}
	a.Settings.SelectFolders = true
	a.key(Message{Key: "-", Code: "NumpadSubtract", Shift: true})
	if len(p.Marked) != 0 {
		t.Fatal("clear all")
	}
	a.key(Message{Key: "+", Code: "NumpadAdd", Shift: true})
	if len(p.Marked) != 5 || p.Marked[".."] {
		t.Fatal("include folders", p.Marked)
	}
}
func TestSelectionDoesNotStealCommandOrCancelledMask(t *testing.T) {
	a := selectionApp()
	a.Command = "echo "
	a.key(Message{Key: "*", Code: "Digit8", Shift: true})
	if a.Command != "echo *" {
		t.Fatal(a.Command)
	}
	a.Command = ""
	a.key(Message{Key: "+", Code: "Equal", Shift: true})
	if a.Dialog == nil {
		t.Fatal("laptop mask key")
	}
	a.key(Message{Key: "Escape"})
	if len(a.Panels[0].Marked) != 0 {
		t.Fatal("cancel changed marks")
	}
	a.TermVisible = true
	if a.selectionKey(Message{Key: "x"}) {
		t.Fatal("unexpected key")
	}
}
