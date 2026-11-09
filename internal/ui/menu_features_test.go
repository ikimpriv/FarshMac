package ui

import (
	"encoding/json"
	"farshmac/internal/fsops"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func waitWork(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for a.Busy && time.Now().Before(deadline) {
		a.Poll()
		time.Sleep(time.Millisecond)
	}
	if a.Busy {
		t.Fatal("operation did not finish")
	}
}

func TestDiskAccessMenuEmitsOneNativeRequest(t *testing.T) {
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	found := false
	for _, section := range a.menuSections() {
		if section.Name != "Options" {
			continue
		}
		for _, item := range section.Items {
			if item.Label == "Full Disk Access…" {
				item.Run()
				found = true
			}
		}
	}
	if !found {
		t.Fatal("Full Disk Access menu item missing")
	}
	update := a.Poll()
	if !update.DiskAccessSettings || update.Quit || a.Form != nil || a.Dialog != nil {
		t.Fatal("menu must open system settings without quitting or another confirmation")
	}
	encoded, err := json.Marshal(update)
	if err != nil || !strings.Contains(string(encoded), `"diskAccessSettings":true`) {
		t.Fatal("native request missing from update")
	}
	if a.Poll().DiskAccessSettings {
		t.Fatal("request repeats on polling")
	}
}
func TestSimpleDeleteConfirmation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "item")
	os.WriteFile(path, []byte("data"), 0600)
	a, _ := New(root, root)
	defer a.Close()
	trashed := filepath.Join(t.TempDir(), "item")
	a.trashItem = func(path string, _ *fsops.Operation) error { return os.Rename(path, trashed) }
	selectEntry(t, &a.Panels[0], "item")
	a.function(8)
	if a.Dialog != nil || a.Form == nil || !a.Form.Buttons {
		t.Fatal("delete must use buttons")
	}
	a.key(Message{Key: "Escape"})
	if _, e := os.Stat(path); e != nil {
		t.Fatal("cancel deleted file")
	}
	a.function(8)
	a.key(Message{Key: "Enter"})
	waitWork(t, a)
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("Enter did not delete")
	}
	if b, err := os.ReadFile(trashed); err != nil || string(b) != "data" {
		t.Fatal("default deletion did not preserve file in Trash")
	}
}
func TestMenuScrollClickAndViews(t *testing.T) {
	a, _ := New(t.TempDir(), t.TempDir())
	a.Rows = 15
	a.Cols = 60
	a.key(Message{Key: "F9"})
	a.key(Message{Key: "End"})
	g := a.Draw()
	if g.H != 15 || a.Menu.Top == 0 {
		t.Fatal("long menu not scrolled")
	}
	x, y, _, h := a.menuBounds()
	if y+h > a.Rows {
		t.Fatal("menu exceeds window")
	}
	a.click(Message{X: x + 3, Y: y + h - 2})
	if a.Form == nil || !strings.Contains(a.Form.Title, "volume") && !strings.Contains(a.Form.Title, "Volume") {
		t.Fatal("scrolled click dispatched wrong command")
	}
	a.Form = nil
	// Drawing all modes at minimum and maximum dimensions must stay in bounds.
	for _, view := range panelViews {
		a.setView(0, view)
		for _, size := range [][2]int{{60, 15}, {300, 120}} {
			a.Cols, a.Rows = size[0], size[1]
			g = a.Draw()
			if len(g.Cells) != size[0]*size[1] {
				t.Fatal(view)
			}
		}
	}
}
func TestMaskSortSelectionAndDescriptions(t *testing.T) {
	root := t.TempDir()
	for name, value := range map[string]string{"b.go": "bbbb", "a.txt": "a", "c.go": "cc", ".hidden": "h"} {
		os.WriteFile(filepath.Join(root, name), []byte(value), 0600)
	}
	os.WriteFile(filepath.Join(root, "descript.ion"), []byte("b.go Compiler\n\"a.txt\" Notes\n"), 0600)
	a, _ := New(root, root)
	p := &a.Panels[0]
	p.Filter = "<source>|b*"
	a.refresh()
	if len(p.Entries) != 2 || p.Entries[1].Name != "c.go" {
		t.Fatal(p.Entries)
	}
	p.Filter = "*.go"
	p.Sort = "Size"
	p.Reverse = true
	a.refresh()
	if p.Entries[1].Name != "b.go" || p.Descriptions["b.go"] != "Compiler" {
		t.Fatal("sort or descriptions")
	}
	a.selectGroup("Select")
	a.Dialog.Confirm("*.go")
	a.Dialog = nil
	if len(p.Selected()) != 2 {
		t.Fatal("group select")
	}
	a.selectGroup("Invert")
	if p.Marked["b.go"] || p.Marked["c.go"] {
		t.Fatal("invert")
	}
	a.selectGroup("Restore")
	if !p.Marked["b.go"] || !p.Marked["c.go"] {
		t.Fatal("restore")
	}
	if validateMasks("[", nil) == nil || matchesMask("b.txt", "*.go", nil) {
		t.Fatal("mask validation")
	}
}
func TestCopyOverwriteSkipAndCancel(t *testing.T) {
	for _, choice := range []string{"Overwrite", "Skip", "Cancel"} {
		t.Run(choice, func(t *testing.T) {
			left, right := t.TempDir(), t.TempDir()
			os.WriteFile(filepath.Join(left, "f"), []byte("new"), 0600)
			os.WriteFile(filepath.Join(right, "f"), []byte("old"), 0600)
			a, _ := New(left, right)
			selectEntry(t, &a.Panels[0], "f")
			a.function(5)
			a.key(Message{Key: "Enter"})
			deadline := time.Now().Add(5 * time.Second)
			for a.Form == nil && a.Busy && time.Now().Before(deadline) {
				a.Poll()
				time.Sleep(time.Millisecond)
			}
			if a.Form == nil || !a.Form.Buttons {
				t.Fatal("overwrite confirmation absent")
			}
			if choice == "Cancel" {
				a.key(Message{Key: "Escape"})
			} else {
				for i, v := range a.Form.Choice {
					if v == choice {
						a.Form.Focus = i
						break
					}
				}
				a.key(Message{Key: "Enter"})
			}
			waitWork(t, a)
			b, _ := os.ReadFile(filepath.Join(right, "f"))
			want := "old"
			if choice == "Overwrite" {
				want = "new"
			}
			if string(b) != want {
				t.Fatal(string(b))
			}
		})
	}
}
func TestFindFilesAndCompareFolders(t *testing.T) {
	root, right := t.TempDir(), t.TempDir()
	os.Mkdir(filepath.Join(root, "child"), 0700)
	os.WriteFile(filepath.Join(root, "child", "needle.go"), []byte("find THIS text"), 0600)
	os.WriteFile(filepath.Join(root, "only-left"), nil, 0600)
	a, _ := New(root, right)
	a.findFiles(false)
	a.Form.Fields[0].Value = "*.go"
	a.Form.Fields[1].Value = "this"
	a.key(Message{Key: "Enter"})
	waitWork(t, a)
	if a.Form == nil || len(a.Form.Choice) != 1 {
		t.Fatal("recursive search failed", a.Status)
	}
	a.key(Message{Key: "Enter"})
	if filepath.Base(a.Panels[0].Current()) != "needle.go" {
		t.Fatal("find result navigation")
	}
	a.Panels[0].Load(root)
	a.compareFolders()
	if !a.Panels[0].Marked["only-left"] {
		t.Fatal("comparison")
	}
}
func TestTreeSymlinksAndBriefMouse(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "a", "b"), 0700)
	os.Symlink(root, filepath.Join(root, "loop"))
	for _, name := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"} {
		os.WriteFile(filepath.Join(root, name), nil, 0600)
	}
	a, _ := New(root, root)
	a.setView(0, "Tree panel")
	waitTree(t, &a.Panels[0])
	if len(a.Panels[0].Entries) > 10 {
		t.Fatal("tree followed a loop")
	}
	found := false
	for _, e := range a.Panels[0].Entries {
		if e.Name == "a/b" {
			found = true
		}
	}
	if !found {
		t.Fatal("tree not expanded")
	}
	a.setView(0, "Brief")
	a.Rows = 15
	a.Draw()
	x, w := a.panelRect(0)
	a.click(Message{X: x + 1 + (w-2)/3, Y: 3})
	if a.Panels[0].Cursor != 6 {
		t.Fatal("brief column hit test", a.Panels[0].Cursor)
	}
}
func TestResizeBurstAndLongLineRender(t *testing.T) {
	a := textApp(t, strings.Repeat("x", 4*1024*1024), false)
	a.Poll()
	a.Handle(`{"Type":"resize","Cols":120,"Rows":36}`)
	if a.Poll().Grid != "" {
		t.Fatal("duplicate resize redraws")
	}
	for i := 0; i < 200; i++ {
		b, _ := json.Marshal(Message{Type: "resize", Cols: 60 + i%100, Rows: 15 + i%50})
		a.Handle(string(b))
	}
	u := a.Poll()
	if u.Cols != 159 || u.Rows != 64 || u.Grid == "" {
		t.Fatal("last resize not rendered")
	}
}
func TestSettingsRoundtrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, _ := New(t.TempDir(), t.TempDir())
	a.Settings.FontSize = 18
	a.Settings.MaskGroups["docs"] = "*.md"
	a.Panels[0].View = "Medium"
	a.Settings.Shortcuts[1] = a.Panels[0].Path
	a.saveSetup()
	b, _ := New(a.Panels[0].Path, a.Panels[1].Path)
	b.LoadSetup()
	if b.Settings.FontSize != 18 || b.Panels[0].View != "Medium" || b.Settings.MaskGroups["docs"] != "*.md" {
		t.Fatal("settings did not roundtrip")
	}
}

func TestCompletionQuotesAndDialogSelection(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "two words.txt"), nil, 0600)
	a, _ := New(root, root)
	a.Command = "cat tw"
	a.completeCommand()
	if a.Command != "cat 'two words.txt'" {
		t.Fatal(a.Command)
	}
	a.gotoPrompt()
	a.key(Message{Key: "x"})
	if a.Dialog.Input != "x" {
		t.Fatal("initial field was not selected")
	}
	a.key(Message{Key: "Escape"})
	a.Cols = 60
	a.Rows = 15
	a.confirm("Overwrite", "File exists", []string{"Overwrite", "All", "Skip", "Skip all", "Cancel"}, func(i int) { a.Status = string(rune('0' + i)) })
	x, y, w, h := a.confirmationBounds()
	if h > 13 {
		t.Fatal("confirmation too tall")
	}
	layout := buttonLayout(a.Form.Choice, w)
	for _, b := range layout {
		if b.X+b.W > w-2 {
			t.Fatal("button clipped")
		}
	}
	last := layout[len(layout)-1]
	a.formClick(Message{X: x + last.X, Y: y + 6 + last.Y})
	if a.Status != "4" || a.Form != nil {
		t.Fatal("wrapped Cancel button")
	}
}
