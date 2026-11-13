package ui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"farshmac/internal/textfile"
)

func textApp(t *testing.T, text string, edit bool) *App {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	a.openDocument(path, edit)
	if a.Doc == nil {
		t.Fatal(a.Status)
	}
	t.Cleanup(a.Close)
	return a
}
func TestRequestedExitAndBackspaceBehavior(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	os.Mkdir(child, 0755)
	a, _ := New(child, root)
	a.key(Message{Key: "Backspace"})
	if a.Panels[0].Path != child {
		t.Fatal("Backspace left folder")
	}
	a.Command = "aб"
	a.key(Message{Key: "Backspace"})
	if a.Command != "a" {
		t.Fatal("Backspace did not edit command")
	}
	a.key(Message{Key: "F10"})
	if a.Quit {
		t.Fatal("F10 quit app")
	}
	a.function(10)
	if a.Quit {
		t.Fatal("function bar quit app")
	}
	b := textApp(t, "hello", false)
	b.key(Message{Key: "F10"})
	if b.Doc != nil || b.Quit {
		t.Fatal("F10 must close document only")
	}
	a.key(Message{Key: "q", Meta: true})
	if !a.Quit {
		t.Fatal("Cmd+Q did not quit")
	}
}
func TestEditorKeyboardAndSaveRoundTrip(t *testing.T) {
	a := textApp(t, "one\r\ntwo\r\n", true)
	d := a.Doc
	a.key(Message{Key: "End"})
	a.key(Message{Key: "!"})
	a.key(Message{Key: "ArrowDown"})
	a.key(Message{Key: "Home"})
	a.key(Message{Key: "T"})
	if d.Text != "one!\nTtwo\n" || !d.Dirty {
		t.Fatal(d.Text, d.Dirty)
	}
	a.key(Message{Key: "z", Meta: true})
	a.key(Message{Key: "z", Meta: true, Shift: true})
	a.key(Message{Key: "F2"})
	raw, err := os.ReadFile(d.Path)
	if err != nil || string(raw) != "one!\r\nTtwo\r\n" || d.Dirty {
		t.Fatal(string(raw), err, d.Dirty)
	}
	a.key(Message{Key: "F6"})
	if d.Edit {
		t.Fatal("F6 did not switch to viewer")
	}
	a.key(Message{Key: "F6"})
	if !d.Edit {
		t.Fatal("F6 did not switch to editor")
	}
	a.key(Message{Key: "l", Meta: true})
	before := d.Text
	a.key(Message{Key: "x"})
	if d.Text != before {
		t.Fatal("read-only modified")
	}
}
func TestCloseAndQuitProtectAllDocuments(t *testing.T) {
	a := textApp(t, "first", true)
	first := a.Doc
	a.key(Message{Key: "!"})
	second := newDocument(filepath.Join(t.TempDir(), "new.txt"), "second")
	second.Edit = true
	second.markSaved()
	a.showDocument(second)
	a.RequestQuit()
	if a.Quit || a.Form == nil {
		t.Fatal("hidden unsaved document not protected")
	}
	a.key(Message{Key: "Escape"})
	if !first.Dirty {
		t.Fatal("cancel cleared dirty state")
	}
	a.key(Message{Key: "F10"})
	if a.Doc != first || a.Quit {
		t.Fatal("screen stack lost")
	}
	a.key(Message{Key: "F10"})
	if a.Form == nil || a.Doc != first {
		t.Fatal("unsaved close unguarded")
	}
	a.key(Message{Key: "ArrowDown"})
	a.key(Message{Key: "Enter"})
	if a.Doc != nil || a.Quit {
		t.Fatal("discard close quit app")
	}
}
func TestSearchFormsAndReplaceAll(t *testing.T) {
	a := textApp(t, "red red RED", true)
	a.key(Message{Key: "F7", Meta: true})
	if a.Form == nil || len(a.Form.Fields) != 8 {
		t.Fatal("replace form missing")
	}
	f := a.Form
	f.Fields[0].Value = "red"
	f.Fields[6].Value = "blue"
	f.Fields[7].Checked = true
	a.key(Message{Key: "Enter"})
	if a.Doc.Text != "blue blue blue" {
		t.Fatal(a.Doc.Text)
	}
	a.key(Message{Key: "z", Meta: true})
	if a.Doc.Text != "red red RED" {
		t.Fatal("undo replace-all")
	}
	a.key(Message{Key: "F7"})
	a.Form.Fields[0].Value = "red"
	a.key(Message{Key: "Enter"})
	if a.Doc.Selected() != "red" {
		t.Fatal("search selection", a.Doc.Selected())
	}
	a.key(Message{Key: "F7", Shift: true})
	if a.Doc.Anchor != 4 {
		t.Fatal("next search", a.Doc.Anchor)
	}
}
func TestBinaryViewHexSearchAndLargePaging(t *testing.T) {
	a := textApp(t, string([]byte{0, 1, 2, 0x48, 0x69, 0xff}), false)
	if a.Doc.Mode != "hex" {
		t.Fatal("binary not hex")
	}
	a.Doc.HexQuery = []byte{0x48, 0x69}
	if !a.findDocument(false) || a.Doc.ByteOffset != 3 {
		t.Fatal("hex search failed")
	}
	a.Draw()
	root := t.TempDir()
	path := filepath.Join(root, "large.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	size := int64(maxTextBytes + 4096)
	f.Truncate(size)
	f.WriteAt([]byte("END"), size-3)
	f.Close()
	a.openDocument(path, false)
	if a.Doc.File == nil || len(a.Doc.Raw) != 0 {
		t.Fatal("large file loaded into RAM")
	}
	a.Doc.ByteOffset = size - 3
	if string(a.Doc.dataAt(size-3, 20)) != "END" {
		t.Fatal("paged read failed")
	}
	a.Draw()
	a.closeDocument()
}
func TestCreateSaveAsAndEncodingSafety(t *testing.T) {
	a := textApp(t, "Привет", true)
	d := a.Doc
	originalPath := d.Path
	d.Codepage = "Windows-1251"
	d.EOL = "CRLF"
	d.modified()
	a.save()
	raw, _ := os.ReadFile(d.Path)
	want, _ := textfile.Encode("Привет", "Windows-1251", false)
	if !bytes.Equal(raw, want) {
		t.Fatal("legacy save failed")
	}
	d.Insert("😀")
	before := append([]byte{}, raw...)
	if err := a.saveDocument(d, d.Path, false); err == nil {
		t.Fatal("lossy save accepted")
	}
	raw, _ = os.ReadFile(d.Path)
	if !bytes.Equal(raw, before) {
		t.Fatal("failed save changed disk")
	}
	d.Codepage = "UTF-8"
	target := filepath.Join(t.TempDir(), "new.txt")
	if err := a.saveDocument(d, target, false); err != nil {
		t.Fatal(err)
	}
	if err := a.saveDocument(d, originalPath, false); err == nil {
		t.Fatal("existing target overwritten")
	}
	untouched, _ := os.ReadFile(originalPath)
	if !bytes.Equal(untouched, before) {
		t.Fatal("Save As changed existing target without approval")
	}
}
func TestWrapGotoBookmarksAndRendering(t *testing.T) {
	a := textApp(t, strings.Repeat("word ", 100)+"\nsecond\nthird", false)
	d := a.Doc
	a.key(Message{Key: "F2"})
	rows := d.visualRows(20, 10)
	if len(rows) != 10 || rows[1].Start <= rows[0].Start {
		t.Fatal("wrap failed")
	}
	a.key(Message{Key: "ArrowDown"})
	if d.ViewStart == 0 {
		t.Fatal("wrapped navigation failed")
	}
	a.key(Message{Key: "£", Code: "Digit3", Meta: true, Alt: true})
	saved := d.ViewStart
	a.key(Message{Key: "Home"})
	a.key(Message{Key: "3", Meta: true})
	if d.ViewStart != saved {
		t.Fatal("bookmark wrap position lost")
	}
	a.key(Message{Key: "g", Meta: true})
	a.Dialog.Input = "3:2"
	a.key(Message{Key: "Enter"})
	if d.Pos(d.Cursor).Row != 2 {
		t.Fatal("goto failed")
	}
	for _, size := range [][2]int{{60, 15}, {120, 36}, {200, 60}} {
		a.Cols, a.Rows = size[0], size[1]
		for _, mode := range []string{"text", "hex", "dump"} {
			d.Mode = mode
			a.Draw()
		}
		d.Edit = true
		a.Draw()
		d.Edit = false
	}
}
func TestClipboardAndMouseSelection(t *testing.T) {
	a := textApp(t, "alpha beta\nline two", true)
	a.Draw()
	a.documentClick(Message{X: 0, Y: 1}, false)
	a.documentClick(Message{X: 5, Y: 1}, true)
	if a.Doc.Selected() != "alpha" {
		t.Fatal(a.Doc.Selected())
	}
	a.Handle(`{"Type":"cut"}`)
	u := a.Poll()
	if u.Clipboard == nil || *u.Clipboard != "alpha" || a.Doc.Text != " beta\nline two" {
		t.Fatal("cut bridge")
	}
	a.Handle(`{"Type":"paste","Text":"привет"}`)
	if !strings.HasPrefix(a.Doc.Text, "привет") {
		t.Fatal("paste bridge")
	}
}

func TestModeSwitchAndNextFileProtectUnsavedText(t *testing.T) {
	a := textApp(t, "first", true)
	d := a.Doc
	a.key(Message{Key: "😀"})
	d.Codepage = "CP866"
	a.key(Message{Key: "F6"})
	if !d.Edit {
		t.Fatal("lossy conversion switched to stale byte view")
	}
	d.Codepage = "UTF-8"
	a.key(Message{Key: "F6"})
	if d.Edit || !d.Dirty {
		t.Fatal("F6 lost edit state")
	}
	path := filepath.Join(a.Panels[0].Path, "next.txt")
	if err := os.WriteFile(path, []byte("next"), 0644); err != nil {
		t.Fatal(err)
	}
	a.refresh()
	selectEntry(t, &a.Panels[0], "file.txt")
	a.key(Message{Key: "+"})
	if a.Form == nil || a.Doc != d {
		t.Fatal("next file silently discarded edits")
	}
	a.key(Message{Key: "Escape"})
	if a.Doc != d || !d.Dirty {
		t.Fatal("cancel lost edited viewer")
	}
}
func TestCutInModalDoesNotCutDocument(t *testing.T) {
	a := textApp(t, "leave document alone", true)
	a.Doc.Anchor = 0
	a.Doc.Cursor = len(a.Doc.Text)
	a.key(Message{Key: "F7"})
	a.Form.Fields[0].Value = "search phrase"
	a.Handle(`{"Type":"cut"}`)
	if a.Doc.Text != "leave document alone" || a.Form.Fields[0].Value != "" || a.Clipboard == nil || *a.Clipboard != "search phrase" {
		t.Fatal("modal cut affected wrong target")
	}
}
func TestUnchangedMixedEndingsSaveIsByteExact(t *testing.T) {
	a := textApp(t, "a\r\nb\nc\r", true)
	before, _ := os.ReadFile(a.Doc.Path)
	a.save()
	after, _ := os.ReadFile(a.Doc.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("unchanged save normalized mixed endings")
	}
}

func TestViewerCannotSwitchToLossyEditor(t *testing.T) {
	// UTF-16LE BOM followed by an unpaired high surrogate.
	a := textApp(t, string([]byte{0xff, 0xfe, 0x00, 0xd8}), false)
	if a.Doc == nil {
		t.Fatal("viewer should open malformed UTF-16 for inspection")
	}
	a.key(Message{Key: "F6"})
	if a.Doc.Edit || !strings.Contains(a.Status, "not lossless") {
		t.Fatalf("lossy viewer became editable: %q", a.Status)
	}
}

func TestModeCycleAndUndoPreserveSavedBytes(t *testing.T) {
	a := textApp(t, "a\r\nb\nc\r", true)
	original, _ := os.ReadFile(a.Doc.Path)
	// Unchanged transitions must retain mixed endings.
	a.key(Message{Key: "F6"})
	if !bytes.Equal(a.Doc.Raw, original) {
		t.Fatal("mode switch changed original bytes")
	}
	a.key(Message{Key: "F6"})
	a.key(Message{Key: "X"})
	a.key(Message{Key: "F6"})
	a.key(Message{Key: "F6"})
	a.key(Message{Key: "z", Meta: true})
	a.save()
	after, err := os.ReadFile(a.Doc.Path)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatalf("undo saved stale preview bytes: %q, %v", after, err)
	}
}

func TestEditorMacNavigationKeys(t *testing.T) {
	a := textApp(t, "alpha beta gamma\nsecond line\n", true)
	d := a.Doc
	a.key(Message{Key: "ArrowRight", Alt: true})
	if d.Cursor != 6 {
		t.Fatal("Option+Right by word", d.Cursor)
	}
	a.key(Message{Key: "ArrowRight", Meta: true})
	if d.Cursor != len("alpha beta gamma") {
		t.Fatal("Cmd+Right line end", d.Cursor)
	}
	a.key(Message{Key: "ArrowLeft", Meta: true})
	if d.Cursor != 0 {
		t.Fatal("Cmd+Left line start", d.Cursor)
	}
	a.key(Message{Key: "ArrowDown", Meta: true})
	if d.Cursor != len(d.Text) {
		t.Fatal("Cmd+Down document end", d.Cursor)
	}
	a.key(Message{Key: "ArrowUp", Meta: true})
	if d.Cursor != 0 {
		t.Fatal("Cmd+Up document start", d.Cursor)
	}
	a.key(Message{Key: "ArrowRight", Ctrl: true})
	if d.Cursor != 6 {
		t.Fatal("Ctrl+Right by word", d.Cursor)
	}
	a.key(Message{Key: "ArrowRight", Alt: true, Shift: true})
	if d.Anchor != 6 || d.Cursor != 11 || d.Rect {
		t.Fatal("Option+Shift+Right selects by word", d.Anchor, d.Cursor, d.Rect)
	}
}

func TestYAMLEditorNeverInsertsTabs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	os.WriteFile(path, []byte("a:\n"), 0644)
	a, _ := New(root, root)
	defer a.Close()
	a.Settings.EditorExpandTabs = false
	a.Settings.EditorTabSize = 8
	a.openDocument(path, true)
	d := a.Doc
	d.Cursor = len(d.Text)
	a.key(Message{Key: "Tab"})
	if !strings.HasSuffix(d.Text, "\n  ") {
		t.Fatalf("Tab in YAML must insert two spaces: %q", d.Text)
	}
	a.Handle(`{"Type":"paste","Text":"\tb: 1\n"}`)
	if strings.Contains(d.Text, "\t") {
		t.Fatalf("pasted tabs must become spaces: %q", d.Text)
	}
	// Indenting a selection with Tab (and Alt+I) adds spaces as well.
	d.Anchor, d.Cursor = 0, len(d.Text)
	a.key(Message{Key: "Tab"})
	if strings.Contains(d.Text, "\t") || !strings.HasPrefix(d.Text, "  a:") {
		t.Fatalf("selection indent in YAML: %q", d.Text)
	}
	a.key(Message{Key: "i", Alt: true})
	if strings.Contains(d.Text, "\t") || !strings.HasPrefix(d.Text, "    a:") {
		t.Fatalf("Alt+I indent in YAML: %q", d.Text)
	}
	// Other documents follow the settings.
	txt := filepath.Join(root, "notes.txt")
	os.WriteFile(txt, []byte("x\n"), 0644)
	a.openDocument(txt, true)
	a.Doc.Cursor = len(a.Doc.Text)
	a.key(Message{Key: "Tab"})
	if !strings.HasSuffix(a.Doc.Text, "\t") {
		t.Fatalf("text files keep real tabs when configured: %q", a.Doc.Text)
	}
}
