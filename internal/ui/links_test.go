package ui

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
)

func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func symlinkField(t *testing.T, a *App) *Field {
	t.Helper()
	for i := range a.Form.Fields {
		if a.Form.Fields[i].Label == "Symbolic links" {
			return &a.Form.Fields[i]
		}
	}
	return nil
}

func TestTransferDialogSymlinkModes(t *testing.T) {
	for _, key := range []string{"F5", "F6"} {
		t.Run(key, func(t *testing.T) {
			root := realTempDir(t)
			left, right := filepath.Join(root, "left"), filepath.Join(root, "right", "deeper")
			for _, dir := range []string{left, right} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(left, "f.txt"), []byte("file"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("f.txt", filepath.Join(left, "lf")); err != nil {
				t.Fatal(err)
			}
			a, err := New(left, right)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			selectEntry(t, &a.Panels[0], "lf")
			a.key(Message{Key: key})
			field := symlinkField(t, a)
			if field == nil || field.Value != "Smart" || len(field.Options) != 3 {
				t.Fatalf("symbolic links choice %+v", a.Form)
			}
			a.key(Message{Key: "Enter"})
			waitWork(t, a)
			if b, err := os.ReadFile(filepath.Join(right, "lf")); err != nil || string(b) != "file" {
				t.Fatal("smart link dangling", a.Status, err)
			}
			if err := os.Remove(filepath.Join(right, "lf")); err != nil {
				t.Fatal(err)
			}
			if key == "F6" {
				if err := os.Symlink("f.txt", filepath.Join(left, "lf")); err != nil {
					t.Fatal(err)
				}
				a.refresh()
				settlePanels(t, a)
			}
			// As is: Space cycles the choice; it is remembered for the next dialog.
			selectEntry(t, &a.Panels[0], "lf")
			a.key(Message{Key: key})
			a.Form.Focus = len(a.Form.Fields) - 1
			a.key(Message{Key: " "})
			if a.Form.Fields[a.Form.Focus].Value != "As is" {
				t.Fatal("cycle", a.Form.Fields[a.Form.Focus].Value)
			}
			a.key(Message{Key: "Enter"})
			waitWork(t, a)
			if text, _ := os.Readlink(filepath.Join(right, "lf")); text != "f.txt" {
				t.Fatal("as is", text, a.Status)
			}
			if a.Settings.CopySymlinks != "As is" {
				t.Fatal("choice not remembered", a.Settings.CopySymlinks)
			}
			a.key(Message{Key: key})
			if symlinkField(t, a).Value != "As is" {
				t.Fatal("remembered choice not offered")
			}
			a.key(Message{Key: "Escape"})
		})
	}
}

func TestSymlinkChoiceClickAndArrows(t *testing.T) {
	a := &App{Cols: 120, Rows: 40}
	a.form("Copy", []Field{{Label: "To", Value: "x"}, {Label: "Symbolic links", Toggle: true, Value: "Smart", Options: []string{"Smart", "As is", "Copy contents"}}}, func([]Field) {})
	a.Form.Focus = 1
	a.formKey(Message{Key: "ArrowLeft"})
	if a.Form.Fields[1].Value != "Copy contents" {
		t.Fatal("ArrowLeft", a.Form.Fields[1].Value)
	}
	pos := a.formGeometry().Fields[1]
	text, starts := a.Form.Fields[1].choiceText()
	if !strings.Contains(text, "(•) Copy contents") {
		t.Fatal(text)
	}
	a.formClick(Message{X: pos.X + starts[1] + 1, Y: pos.Y})
	if a.Form.Fields[1].Value != "As is" {
		t.Fatal("click", a.Form.Fields[1].Value)
	}
	g := NewGrid(a.Cols, a.Rows)
	a.drawForm(g)
	var b strings.Builder
	for _, c := range g.Cells {
		b.WriteString(c.Text)
	}
	if !strings.Contains(b.String(), "(•) As is") {
		t.Fatal("choice not drawn")
	}
}

func TestSymlinkChoiceHiddenForArchiveAndRemote(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(left, "file"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(left, right)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	selectEntry(t, &a.Panels[0], "file")
	a.Panels[1].Remote = &remotefs.Config{Protocol: "sftp", Host: "example.invalid", Port: 22, Directory: "/"}
	a.key(Message{Key: "F5"})
	if a.Form == nil || symlinkField(t, a) != nil {
		t.Fatalf("symbolic links offered for a remote destination: %+v", a.Form)
	}
}

func TestCollisionDialogShowsSourceLink(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	if err := os.Symlink("target.txt", filepath.Join(left, "name")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(right, "name"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(left, right)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	selectEntry(t, &a.Panels[0], "name")
	a.key(Message{Key: "F5"})
	a.key(Message{Key: "Enter"})
	waitOperationQuestion(t, a, "Warning")
	if !strings.Contains(a.Form.Message, "New: broken link → target.txt") || strings.Contains(a.Form.Message, "New: 10 bytes") {
		t.Fatal(a.Form.Message)
	}
	a.key(Message{Key: "Escape"})
	waitWork(t, a)
	if err := os.WriteFile(filepath.Join(left, "target.txt"), []byte("t"), 0600); err != nil {
		t.Fatal(err)
	}
	a.key(Message{Key: "F5"})
	a.key(Message{Key: "Enter"})
	waitOperationQuestion(t, a, "Warning")
	if !strings.Contains(a.Form.Message, "New: symbolic link → target.txt") {
		t.Fatal(a.Form.Message)
	}
	a.key(Message{Key: "Escape"})
	waitWork(t, a)
}

func TestCreateLinkRelativeIntoFolderAndStatus(t *testing.T) {
	root := realTempDir(t)
	left, right := filepath.Join(root, "left"), filepath.Join(root, "right")
	for _, dir := range []string{left, filepath.Join(right, "existing")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(left, "file"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(left, right)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	selectEntry(t, &a.Panels[0], "file")
	a.key(Message{Key: "F6", Alt: true})
	if a.Form == nil || len(a.Form.Fields) != 3 || !a.Form.Fields[2].Checked {
		t.Fatalf("create link form %+v", a.Form)
	}
	a.key(Message{Key: "Enter"})
	link := filepath.Join(right, "file")
	if text, err := os.Readlink(link); err != nil || text != "../left/file" {
		t.Fatal("relative link", text, err, a.Status)
	}
	if a.Status != "Link created: file → ../left/file" {
		t.Fatal(a.Status)
	}
	// An existing folder as the name: the link goes inside it (Far).
	a.key(Message{Key: "F6", Alt: true})
	a.Form.Fields[0].Value = filepath.Join(right, "existing")
	a.Form.Fields[2].Checked = false
	a.key(Message{Key: "Enter"})
	if text, err := os.Readlink(filepath.Join(right, "existing", "file")); err != nil || text != filepath.Join(left, "file") {
		t.Fatal("absolute link inside folder", text, err, a.Status)
	}
	// The same name again is a readable error, not a raw syscall message.
	a.key(Message{Key: "F6", Alt: true})
	a.key(Message{Key: "Enter"})
	if !strings.Contains(a.Status, "already exists") || strings.HasPrefix(a.Status, "symlink") {
		t.Fatal(a.Status)
	}
	// Hard link.
	a.key(Message{Key: "F6", Alt: true})
	a.Form.Fields[0].Value = filepath.Join(right, "hard")
	a.Form.Fields[1].Checked = false
	a.key(Message{Key: "Enter"})
	if info, err := os.Lstat(filepath.Join(right, "hard")); err != nil || info.Sys().(*syscall.Stat_t).Nlink != 2 {
		t.Fatal("hard link", err, a.Status)
	}
	if a.Status != "Hard link created: hard → "+filepath.Join(left, "file") {
		t.Fatal(a.Status)
	}
}

func TestCreateLinkRefusals(t *testing.T) {
	root := realTempDir(t)
	if err := os.Mkdir(filepath.Join(root, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := makeLink(filepath.Join(root, "folder"), filepath.Join(root, "hard"), false, false); err == nil || !strings.Contains(err.Error(), "hard links to folders are not supported") {
		t.Fatal(err)
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if fsops.ParseSymlinkMode("copy CONTENTS") != fsops.SymlinksContents {
		t.Fatal("parse")
	}
	a.Panels[0].Cursor = 0
	if a.Panels[0].Entries[0].Name != ".." {
		t.Skip("no .. entry")
	}
	a.key(Message{Key: "F6", Alt: true})
	if a.Form != nil || a.Status != "Choose a file or folder to link to" {
		t.Fatal("link to ..", a.Status)
	}
	selectEntry(t, &a.Panels[0], "folder")
	a.key(Message{Key: "F6", Alt: true})
	a.Form.Fields[0].Value = ""
	a.key(Message{Key: "Enter"})
	if a.Form == nil || a.Status != "Enter a link name" {
		t.Fatal("empty name", a.Status)
	}
}
