package ui

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
	"farshmac/internal/testnet"
)

func skipAsRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
}

// lockFolder makes dir unwritable for the rest of the test.
func lockFolder(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0755) })
}

// press chooses a button of the open confirmation by its label.
func press(t *testing.T, a *App, title, button string) {
	t.Helper()
	if a.Form == nil || a.Form.Title != title {
		got := "<none>"
		if a.Form != nil {
			got = a.Form.Title + ": " + a.Form.Message
		}
		t.Fatalf("want dialog %q, got %s (status %q)", title, got, a.Status)
	}
	for i, label := range a.Form.Choice {
		if label == button {
			a.Form.Focus = i
			a.key(Message{Key: "Enter"})
			return
		}
	}
	t.Fatalf("dialog %q has no %q button: %v", title, button, a.Form.Choice)
}

func editorFixture(t *testing.T, async bool, text string) (*App, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	newApp := New
	if async {
		newApp = NewAsync
	}
	a, err := newApp(root, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	waitWork(t, a)
	a.openDocument(path, true)
	waitWork(t, a)
	if a.Doc == nil || !a.Doc.Edit {
		t.Fatal("editor did not open", a.Status)
	}
	return a, path
}

func TestEditOpenRefusesUnreadableFileWithDialog(t *testing.T) {
	skipAsRoot(t)
	root := t.TempDir()
	path := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(path, []byte("x"), 0000); err != nil {
		t.Fatal(err)
	}
	a, err := NewAsync(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	waitWork(t, a)
	a.openDocument(path, true)
	waitWork(t, a)
	if a.Doc != nil || a.Form == nil || a.Form.Title != "Cannot open file" {
		t.Fatal("unreadable file", a.Form, a.Status)
	}
	if !strings.Contains(a.Form.Message, path) || !strings.Contains(a.Form.Message, "permission denied") || !strings.Contains(a.Form.Message, "Full Disk Access") {
		t.Fatal("open error message", a.Form.Message)
	}
}

func TestEditOpenWarnsWhenFileCannotBeSaved(t *testing.T) {
	skipAsRoot(t)
	root := t.TempDir()
	dir := filepath.Join(root, "locked")
	os.Mkdir(dir, 0755)
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("text"), 0444); err != nil {
		t.Fatal(err)
	}
	lockFolder(t, dir)
	for _, async := range []bool{false, true} {
		newApp := New
		if async {
			newApp = NewAsync
		}
		a, err := newApp(root, root)
		if err != nil {
			t.Fatal(err)
		}
		waitWork(t, a)
		a.openDocument(path, true)
		waitWork(t, a)
		if a.Doc != nil || a.Form == nil || !strings.Contains(a.Form.Message, "folder is not writable") {
			t.Fatal("no read-only warning", async, a.Form, a.Status)
		}
		press(t, a, "Read-only file", "View")
		if a.Doc == nil || a.Doc.Edit {
			t.Fatal("View did not open the viewer")
		}
		a.finishCloseDocument()
		a.openDocument(path, true)
		waitWork(t, a)
		press(t, a, "Read-only file", "Edit anyway")
		if a.Doc == nil || !a.Doc.Edit || a.Doc.ReadOnly || !strings.HasPrefix(a.Status, "Saving may fail") {
			t.Fatal("Edit anyway", a.Status)
		}
		a.finishCloseDocument()
		a.openDocument(path, true)
		waitWork(t, a)
		press(t, a, "Read-only file", "Cancel")
		if a.Doc != nil {
			t.Fatal("Cancel opened the file")
		}
		a.Close()
	}
}

func TestEditOpenDetectsInplaceStrategy(t *testing.T) {
	skipAsRoot(t)
	root := t.TempDir()
	dir := filepath.Join(root, "locked")
	os.Mkdir(dir, 0755)
	path := filepath.Join(dir, "file.txt")
	os.WriteFile(path, []byte("text"), 0644)
	lockFolder(t, dir)
	a, _ := New(root, root)
	defer a.Close()
	a.openDocument(path, true)
	if a.Form != nil || a.Doc == nil || a.Doc.SaveStrategy != fsops.SaveInPlace {
		t.Fatal("in-place strategy", a.Form, a.Doc)
	}
}

func TestImmutableFileOpensReadOnly(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	os.WriteFile(path, []byte("text"), 0644)
	if err := syscall.Chflags(path, 0x2); err != nil { // UF_IMMUTABLE
		t.Skip("chflags uchg", err)
	}
	t.Cleanup(func() { syscall.Chflags(path, 0) })
	a, _ := New(root, root)
	defer a.Close()
	a.openDocument(path, true)
	if a.Form == nil || !strings.Contains(a.Form.Message, "locked (uchg)") {
		t.Fatal("immutable file", a.Form, a.Status)
	}
	press(t, a, "Read-only file", "View")
	a.key(Message{Key: "F6"})
	if a.Form == nil || a.Form.Choice[0] != "Read-only" {
		t.Fatal("F6 did not warn", a.Form)
	}
	press(t, a, "Read-only file", "Read-only")
	if !a.Doc.Edit || !a.Doc.ReadOnly {
		t.Fatal("Read-only editor", a.Doc.Edit, a.Doc.ReadOnly)
	}
}

func TestSaveFailureKeepsBufferAndOffersRetry(t *testing.T) {
	skipAsRoot(t)
	for _, async := range []bool{false, true} {
		a, path := editorFixture(t, async, "one")
		d := a.Doc
		d.Insert("edited ")
		d.modified()
		os.Chmod(path, 0444)
		lockFolder(t, filepath.Dir(path))
		a.save()
		waitWork(t, a)
		if a.Form == nil || a.Form.Title != "Save failed" || !strings.Contains(a.Form.Message, path) || !strings.Contains(a.Form.Message, "permission denied") {
			t.Fatal("save failure dialog", async, a.Form, a.Status)
		}
		if strings.Contains(a.Form.Message, ".farshmac-") {
			t.Fatal("temporary name leaked", a.Form.Message)
		}
		if !d.Dirty || d.Text != "edited one" || a.Status == "Saved" {
			t.Fatal("failed save lost edits or claimed success", d.Dirty, a.Status)
		}
		press(t, a, "Save failed", "Cancel")
		if a.Status != "Not saved" || !d.Dirty {
			t.Fatal("cancel", a.Status)
		}
		a.save()
		waitWork(t, a)
		os.Chmod(filepath.Dir(path), 0755)
		press(t, a, "Save failed", "Retry")
		waitWork(t, a)
		if data, _ := os.ReadFile(path); string(data) != "edited one" || d.Dirty || a.Status != "Saved" {
			t.Fatal("retry", string(data), d.Dirty, a.Status)
		}
	}
}

func TestSaveFailureOffersSaveAs(t *testing.T) {
	skipAsRoot(t)
	a, path := editorFixture(t, true, "one")
	d := a.Doc
	d.Insert("x")
	d.modified()
	os.Chmod(path, 0444)
	lockFolder(t, filepath.Dir(path))
	closed := false
	a.saveDocumentThen(d, d.Path, false, func() { closed = true })
	waitWork(t, a)
	press(t, a, "Save failed", "Save as…")
	if a.Dialog == nil || a.Dialog.Title != "Save as" {
		t.Fatal("Save as prompt")
	}
	other := filepath.Join(t.TempDir(), "copy.txt")
	a.Dialog.Input = other
	a.key(Message{Key: "Enter"})
	waitWork(t, a)
	if data, _ := os.ReadFile(other); string(data) != "xone" || !closed || d.Dirty || d.Path != other {
		t.Fatal("save as after failure", string(data), closed, a.Status)
	}
}

func TestSaveAllStopsOnFailureAndFocusesDocument(t *testing.T) {
	skipAsRoot(t)
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	os.Mkdir(locked, 0755)
	first, second := filepath.Join(root, "a.txt"), filepath.Join(locked, "b.txt")
	os.WriteFile(first, []byte("a"), 0644)
	os.WriteFile(second, []byte("b"), 0444)
	a, _ := New(root, root)
	defer a.Close()
	lockFolder(t, locked)
	a.openDocument(second, true)
	press(t, a, "Read-only file", "Edit anyway")
	b := a.Doc
	a.openDocument(first, true)
	for _, d := range []*Document{a.Doc, b} {
		d.Insert("!")
		d.modified()
	}
	a.RequestQuit()
	press(t, a, "Save changes", "Save all")
	if a.Quit || a.Doc != b || a.Form == nil || a.Form.Title != "Save failed" {
		t.Fatal("save all failure", a.Quit, a.Doc == b, a.Form)
	}
	press(t, a, "Save failed", "Cancel")
	if a.Quit || !b.Dirty {
		t.Fatal("cancel must abort quit")
	}
}

func TestSaveStatFailureDoesNotBreakNextSave(t *testing.T) {
	a, _ := editorFixture(t, false, "one")
	d := a.Doc
	path := d.Path
	statSavedFile = func(string) (os.FileInfo, error) { return nil, errors.New("stat failed") }
	d.Insert("1")
	d.modified()
	a.save()
	statSavedFile = os.Stat
	if d.Dirty || a.Form != nil || d.Path != path {
		t.Fatal("save with failed stat", a.Status)
	}
	d.Insert("2")
	d.modified()
	a.save()
	if data, _ := os.ReadFile(path); string(data) != "12one" || d.Dirty || d.Info == nil {
		t.Fatal("next save", string(data), a.Status)
	}
}

func TestSaveInPlaceWhenFolderNotWritable(t *testing.T) {
	skipAsRoot(t)
	a, path := editorFixture(t, false, "long original text")
	d := a.Doc
	before, _ := os.Stat(path)
	lockFolder(t, filepath.Dir(path))
	d.Replace(0, len(d.Text), "short")
	d.modified()
	a.save()
	press(t, a, "Save in place", "Cancel")
	if !d.Dirty || a.Status != "Not saved" {
		t.Fatal("cancel in place", a.Status)
	}
	if data, _ := os.ReadFile(path); string(data) != "long original text" {
		t.Fatal("cancel changed the file")
	}
	a.save()
	press(t, a, "Save in place", "Save")
	after, _ := os.Stat(path)
	if data, _ := os.ReadFile(path); string(data) != "short" || d.Dirty || a.Status != "Saved (in place)" || !os.SameFile(before, after) {
		t.Fatal("in-place save", string(data), a.Status)
	}
	d.Insert("!")
	d.modified()
	a.save()
	if a.Form != nil || d.Dirty {
		t.Fatal("in-place confirmation must be remembered", a.Form)
	}
}

func TestSaveInPlaceDetectsExternalChange(t *testing.T) {
	skipAsRoot(t)
	a, path := editorFixture(t, false, "text")
	d := a.Doc
	lockFolder(t, filepath.Dir(path))
	os.WriteFile(path, []byte("other program"), 0644)
	d.Insert("x")
	d.modified()
	a.save()
	press(t, a, "File changed on disk", "Cancel")
	if data, _ := os.ReadFile(path); string(data) != "other program" || !d.Dirty {
		t.Fatal("external change overwritten", string(data))
	}
}

func TestSaveOverChangedFileOffersOverwrite(t *testing.T) {
	for _, async := range []bool{false, true} {
		a, path := editorFixture(t, async, "text")
		d := a.Doc
		os.WriteFile(path, []byte("changed elsewhere"), 0644)
		d.Insert("mine ")
		d.modified()
		a.save()
		waitWork(t, a)
		press(t, a, "File changed on disk", "Overwrite")
		waitWork(t, a)
		if data, _ := os.ReadFile(path); string(data) != "mine text" || d.Dirty {
			t.Fatal("overwrite", async, string(data), a.Status)
		}
	}
}

func TestSaveDeletedFileOffersCreate(t *testing.T) {
	a, path := editorFixture(t, false, "text")
	d := a.Doc
	os.Remove(path)
	d.Insert("x")
	d.modified()
	a.save()
	press(t, a, "File was deleted", "Create")
	if data, _ := os.ReadFile(path); string(data) != "xtext" || d.Dirty {
		t.Fatal("recreate", string(data), a.Status)
	}
}

func TestReloadDiscardsChangesAfterConfirm(t *testing.T) {
	a, path := editorFixture(t, false, "first\nsecond\n")
	d := a.Doc
	screen := d.ScreenID
	d.Insert("unsaved ")
	d.modified()
	os.WriteFile(path, []byte("first\nreplaced\n"), 0644)
	a.key(Message{Key: "r", Meta: true})
	press(t, a, "Reload", "Cancel")
	if !d.Dirty {
		t.Fatal("cancel reloaded")
	}
	a.key(Message{Key: "r", Meta: true})
	press(t, a, "Reload", "Reload")
	if a.Doc != d || d.Dirty || d.Text != "first\nreplaced\n" || d.ScreenID != screen || !d.Edit {
		t.Fatal("reload", d.Text, d.Dirty)
	}
	a.save()
	if a.Form != nil {
		t.Fatal("save after reload", a.Form.Title)
	}
}

func TestSaveAsOverDirectoryReportsDirectory(t *testing.T) {
	a, path := editorFixture(t, false, "text")
	dir := filepath.Join(filepath.Dir(path), "folder")
	os.Mkdir(dir, 0755)
	a.saveAs()
	a.Dialog.Input = dir
	a.key(Message{Key: "Enter"})
	if a.Form != nil || a.Status != dir+" is a directory" {
		t.Fatal("directory target", a.Form, a.Status)
	}
}

func TestSaveAsOverSymlinkWritesTarget(t *testing.T) {
	a, path := editorFixture(t, false, "text")
	root := filepath.Dir(path)
	target := filepath.Join(root, "target.txt")
	link := filepath.Join(root, "link.txt")
	os.WriteFile(target, []byte("old"), 0644)
	os.Symlink(target, link)
	a.saveAs()
	a.Dialog.Input = link
	a.key(Message{Key: "Enter"})
	if a.Form == nil || !strings.Contains(a.Form.Message, "is a link to") {
		t.Fatal("link question", a.Form, a.Status)
	}
	press(t, a, "Overwrite", "Overwrite")
	resolved, _ := filepath.EvalSymlinks(target)
	if data, _ := os.ReadFile(target); string(data) != "text" || a.Doc.Path != resolved {
		t.Fatal("link target", string(data), a.Doc.Path, a.Status)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("link replaced")
	}
}

func TestSaveAsOverBrokenSymlinkRefuses(t *testing.T) {
	a, path := editorFixture(t, false, "text")
	link := filepath.Join(filepath.Dir(path), "broken")
	os.Symlink(filepath.Join(filepath.Dir(path), "missing"), link)
	a.saveAs()
	a.Dialog.Input = link
	a.key(Message{Key: "Enter"})
	if a.Status != link+" is a broken symbolic link" {
		t.Fatal(a.Status)
	}
}

func TestViewerOpensTruncatedUTF16AsHex(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "utf16.txt")
	os.WriteFile(path, []byte{0xff, 0xfe, 0x41}, 0644)
	a, _ := New(root, root)
	defer a.Close()
	a.openDocument(path, false)
	if a.Doc == nil || a.Doc.Mode != "hex" || !strings.HasSuffix(a.Status, "shown as hex") {
		t.Fatal("truncated UTF-16 viewer", a.Doc, a.Status)
	}
	a.finishCloseDocument()
	a.openDocument(path, true)
	if a.Doc != nil || a.Form != nil || !strings.HasSuffix(a.Status, "open with F3") {
		t.Fatal("truncated UTF-16 editor", a.Status)
	}
}

func TestHexSearchRefusedInEditorKeepsTextMode(t *testing.T) {
	a := textApp(t, "hello", true)
	a.searchDocument(false)
	form := a.Form
	a.Form = nil
	form.Fields[0].Value = "68 65"
	form.Fields[5].Checked = true
	form.Submit(form.Fields)
	if a.Doc.Mode != "text" || a.Status != "Hex search is available in the viewer (F6)" {
		t.Fatal(a.Doc.Mode, a.Status)
	}
	a.Doc.HexQuery = []byte("he")
	a.key(Message{Key: "F7", Shift: true})
	if a.Doc.Mode != "text" || a.Doc.HexQuery != nil {
		t.Fatal("next hex match in editor", a.Doc.Mode)
	}
}

func TestReadOnlyEditsReportStatus(t *testing.T) {
	a := textApp(t, "hello", true)
	a.Doc.ReadOnly = true
	a.searchDocument(true)
	if a.Form != nil || a.Status != "Read-only mode (⌘L to unlock)" {
		t.Fatal("replace in read-only", a.Status)
	}
	a.Status = ""
	a.Handle(`{"Type":"paste","Text":"x"}`)
	if a.Doc.Text != "hello" || a.Status != "Read-only mode (⌘L to unlock)" {
		t.Fatal("paste in read-only", a.Doc.Text, a.Status)
	}
}

func TestArchiveDocumentSaveRefused(t *testing.T) {
	a := textApp(t, "hello", true)
	a.Doc.ArchiveSource = true
	a.Doc.Insert("x")
	a.Doc.modified()
	a.save()
	if a.Form != nil || a.Status != "Copy the file out with F5 before editing" || !a.Doc.Dirty {
		t.Fatal(a.Status)
	}
}

func TestPagedViewerReportsReadError(t *testing.T) {
	a := textApp(t, "hello", false)
	f, err := os.Open(a.Doc.Path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close() // Reads now fail like on an unmounted volume.
	d := a.Doc
	d.File, d.Mode, d.Size = f, "hex", 1<<20
	a.Draw()
	if !d.readError || !strings.HasPrefix(a.Status, "read error:") {
		t.Fatal("read error", a.Status)
	}
	if a.Form == nil || a.Form.Title != "Cannot read file" {
		t.Fatal("close offer", a.Form)
	}
	d.File = nil
	press(t, a, "Cannot read file", "OK")
	if a.Doc != nil {
		t.Fatal("document kept")
	}
}

func TestBookmarkSavedWithShiftedKeySymbol(t *testing.T) {
	a := textApp(t, "one\ntwo\nthree\n", false)
	a.Doc.Offset = 2
	a.key(Message{Key: "#", Code: "Digit3", Meta: true, Shift: true})
	if _, ok := a.Doc.Bookmarks[3]; !ok || a.Status != "Bookmark 3 saved" {
		t.Fatal("shifted bookmark", a.Status)
	}
	a.Doc.Offset = 0
	a.key(Message{Key: "3", Code: "Digit3", Meta: true})
	if a.Doc.Offset != 2 {
		t.Fatal("bookmark restore", a.Doc.Offset)
	}
	a.key(Message{Key: "№", Code: "Digit5", Ctrl: true, Shift: true})
	if _, ok := a.Doc.Bookmarks[5]; !ok {
		t.Fatal("non-Latin shifted bookmark")
	}
}

func TestViewerF8ReportsDirtyAndPaged(t *testing.T) {
	a := textApp(t, "hello", false)
	a.Doc.Dirty = true
	a.key(Message{Key: "F8"})
	if a.Status != "Save edited text before reinterpreting bytes" {
		t.Fatal("dirty F8", a.Status)
	}
	a.Doc.Dirty = false
	f, _ := os.Open(a.Doc.Path)
	defer f.Close()
	a.Doc.File, a.Doc.Mode = f, "hex"
	a.key(Message{Key: "F8"})
	if a.Doc.Codepage != "CP866" || a.Status != "Code page: CP866" {
		t.Fatal("paged F8", a.Doc.Codepage, a.Status)
	}
	a.Doc.File = nil
}

func TestEditorCtrlInsertCopiesShiftDeleteCuts(t *testing.T) {
	a := textApp(t, "one\ntwo\n", true)
	a.key(Message{Key: "Insert", Ctrl: true})
	if a.Clipboard == nil || *a.Clipboard != "one\n" || a.Doc.Overwrite {
		t.Fatal("Ctrl+Insert", a.Clipboard, a.Doc.Overwrite)
	}
	a.key(Message{Key: "Delete", Shift: true})
	if a.Doc.Text != "two\n" || *a.Clipboard != "one\n" {
		t.Fatal("Shift+Delete", a.Doc.Text)
	}
	a.key(Message{Key: "Insert"})
	if !a.Doc.Overwrite {
		t.Fatal("Insert toggles overwrite")
	}
}

func TestDumpWidthIgnoresHexCeiling(t *testing.T) {
	a := textApp(t, "hello", false)
	a.Cols = 120
	a.Doc.Mode = "dump"
	a.Doc.BytesPerLine = 64
	if w := a.binaryWidth(); w != 64 {
		t.Fatal("dump width", w)
	}
	a.Doc.Mode = "hex"
	if w := a.binaryWidth(); w != (120-14)/4 {
		t.Fatal("hex width", w)
	}
}

func TestLightThemeDarkensBrightColors(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\n"), 0755)
	a, _ := New(root, root)
	defer a.Close()
	a.Settings.Highlight = true
	a.refresh()
	a.Settings.ColorTheme = "Light"
	found := false
	for _, c := range a.Draw().Cells {
		if c.FG == 0x55ff55 || c.FG == 0xaaaaaa || c.FG == 0xff88ff || c.FG == 0xff8888 {
			t.Fatal("bright color left in light theme", c.FG)
		}
		if c.FG == 0x1d6f1d {
			found = true
		}
	}
	if !found {
		t.Fatal("executable color not darkened")
	}
}

func TestFilesMenuListsAlternateAndHistoryKeys(t *testing.T) {
	a, _ := New(t.TempDir(), t.TempDir())
	defer a.Close()
	want := map[string]string{"Alternate viewer": "AltF3", "Alternate editor": "AltF4", "Internal viewer": "⌘⇧F3", "Internal editor": "⌘⇧F4", "File view/edit history": "AltF11"}
	for _, section := range a.menuSections() {
		for _, item := range section.Items {
			if key, ok := want[item.Label]; ok {
				if item.Shortcut != key {
					t.Fatal(item.Label, item.Shortcut)
				}
				delete(want, item.Label)
			}
		}
	}
	if len(want) != 0 {
		t.Fatal("missing menu items", want)
	}
}

func remoteEditorFixture(t *testing.T, edit bool) (*App, remotefs.Config) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	c := testnet.SFTP(t)
	seedRemote(t, c)
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	a.Async = true
	a.connectRemote(0, c)
	waitNetwork(t, a)
	p := &a.Panels[0]
	selectEntry(t, p, "folder")
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	return a, c
}

func writeRemote(t *testing.T, c remotefs.Config, name, data string) {
	t.Helper()
	f, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Remove(name, false)
	w, err := f.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(w, data)
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
}

func readRemote(t *testing.T, c remotefs.Config, name string) string {
	t.Helper()
	f, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := f.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	data, _ := io.ReadAll(r)
	return string(data)
}

func TestRemoteSaveChangedOffersOverwrite(t *testing.T) {
	a, c := remoteEditorFixture(t, true)
	selectEntry(t, &a.Panels[0], "file.txt")
	a.key(Message{Key: "F4"})
	waitNetwork(t, a)
	d := a.Doc
	if d == nil || d.Remote == nil {
		t.Fatal("remote editor", a.Status)
	}
	writeRemote(t, c, "/folder/file.txt", "server side change")
	d.Insert("mine ")
	d.modified()
	a.save()
	waitNetwork(t, a)
	press(t, a, "File changed on disk", "Overwrite")
	waitNetwork(t, a)
	if got := readRemote(t, c, "/folder/file.txt"); got != "mine remote data" || d.Dirty {
		t.Fatal("remote overwrite", got, a.Status)
	}
	// Unknown attributes after an interrupted upload must not block saving.
	d.RemoteInfo = nil
	d.Insert("again ")
	d.modified()
	a.save()
	waitNetwork(t, a)
	if a.Form != nil || d.Dirty || d.RemoteInfo == nil {
		t.Fatal("save with unknown remote attributes", a.Status)
	}
}

func TestRemoteSaveFailureDialog(t *testing.T) {
	a, c := remoteEditorFixture(t, true)
	selectEntry(t, &a.Panels[0], "file.txt")
	a.key(Message{Key: "F4"})
	waitNetwork(t, a)
	d := a.Doc
	d.Insert("x")
	d.modified()
	f, _ := c.Dial(context.Background())
	f.Remove("/folder/file.txt", false)
	f.Close()
	a.save()
	waitNetwork(t, a)
	if a.Form == nil || a.Form.Title != "Save failed" || !strings.Contains(a.Form.Message, "/folder/file.txt") || !d.Dirty {
		t.Fatal("remote save failure", a.Form, a.Status)
	}
	press(t, a, "Save failed", "Cancel")
	if a.Status != "Not saved" || !d.Dirty {
		t.Fatal(a.Status)
	}
}

func TestRemoteSymlinkToFileOpensInViewer(t *testing.T) {
	a, c := remoteEditorFixture(t, false)
	f, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Symlink("/folder/file.txt", "/folder/current"); err != nil {
		f.Close()
		t.Skip("server has no symbolic links", err)
	}
	if err = f.Mkdir("/folder/sub"); err != nil {
		t.Fatal(err)
	}
	if err = f.Symlink("/folder/sub", "/folder/sublink"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	p := &a.Panels[0]
	a.error(p.Load(p.Path))
	waitNetwork(t, a)
	selectEntry(t, p, "current")
	a.key(Message{Key: "F3"})
	waitNetwork(t, a)
	if a.Doc == nil || a.Doc.Text != "remote data" || a.Doc.RemotePath != "/folder/file.txt" {
		t.Fatal("remote link to file", a.Doc, a.Status)
	}
	a.finishCloseDocument()
	waitNetwork(t, a)
	// Listings resolve folder links (P1-05), so the entry is a folder and
	// the document path is never reached; the resolver still refuses it.
	selectEntry(t, p, "sublink")
	if !p.Entries[p.Cursor].Dir {
		t.Fatal("folder link not listed as folder")
	}
	a.openRemoteDocument(false)
	waitNetwork(t, a)
	if a.Doc != nil {
		t.Fatal("remote link to folder opened", a.Status)
	}
	f, err = c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Lstat("/folder/sublink")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = resolveRemoteLink(f, "/folder/sublink", info); err == nil || err.Error() != "sublink is a link to a folder" {
		t.Fatal("resolver", err)
	}
}

func TestAdjacentFileOnRemotePanelDownloads(t *testing.T) {
	a, _ := remoteEditorFixture(t, false)
	p := &a.Panels[0]
	selectEntry(t, p, ".hidden")
	a.key(Message{Key: "F3"})
	waitNetwork(t, a)
	first := a.Doc
	if first == nil || first.TempDir == "" {
		t.Fatal("remote viewer", a.Status)
	}
	temp := first.TempDir
	for i := p.Cursor + 1; i < len(p.Entries); i++ {
		if !p.Entries[i].Dir {
			a.key(Message{Key: "+"})
			waitNetwork(t, a)
			break
		}
	}
	if a.Doc == first || a.Doc == nil || a.Doc.Remote == nil || len(a.Documents) != 0 {
		t.Fatal("adjacent remote file", a.Doc, a.Status)
	}
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatal("replaced document cache kept", err)
	}
}

func TestSaveFallbackTextNamesFolder(t *testing.T) {
	err := &os.PathError{Op: "open", Path: "/x/dir/.farshmac-save-123", Err: syscall.EACCES}
	if got := saveReason(err); got != "cannot write in folder /x/dir: permission denied" {
		t.Fatal(got)
	}
}

func TestNewFileInLockedFolderWarns(t *testing.T) {
	skipAsRoot(t)
	root := t.TempDir()
	a, _ := New(root, root)
	defer a.Close()
	lockFolder(t, root)
	a.newFile()
	a.Dialog.Input = "new.txt"
	a.key(Message{Key: "Enter"})
	if a.Form == nil || len(a.Form.Choice) != 2 || a.Form.Choice[0] != "Edit anyway" {
		t.Fatal("new file warning", a.Form, a.Status)
	}
	press(t, a, "Read-only file", "Edit anyway")
	if a.Doc == nil || !a.Doc.Edit {
		t.Fatal("Edit anyway did not open the editor")
	}
}
