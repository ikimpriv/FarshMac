package ui

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"farshmac/internal/remotefs"
)

func waitOperationQuestion(t *testing.T, a *App, title string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.Poll()
		if a.Form != nil && a.Form.Title == title {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("missing %s: busy=%v status=%s form=%+v", title, a.Busy, a.Status, a.Form)
}
func chooseOperation(t *testing.T, a *App, label string) {
	t.Helper()
	if a.Form == nil {
		t.Fatal("no question")
	}
	for i, s := range a.Form.Choice {
		if s == label {
			a.Form.Focus = i
			a.key(Message{Key: "Enter"})
			return
		}
	}
	t.Fatalf("missing %s in %+v", label, a.Form.Choice)
}
func TestCopyErrorRetryAndSkipAll(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged user")
	}
	for _, retry := range []bool{true, false} {
		t.Run(map[bool]string{true: "retry", false: "skip-all"}[retry], func(t *testing.T) {
			left, right := t.TempDir(), t.TempDir()
			for _, name := range []string{"one", "two"} {
				if e := os.WriteFile(filepath.Join(left, name), []byte(name), 0600); e != nil {
					t.Fatal(e)
				}
			}
			a, e := New(left, right)
			if e != nil {
				t.Fatal(e)
			}
			defer a.Close()
			a.Settings.CopyWorkers = 2
			a.Panels[0].Marked = map[string]bool{"one": true, "two": true}
			if e = os.Chmod(right, 0500); e != nil {
				t.Fatal(e)
			}
			defer os.Chmod(right, 0700)
			a.key(Message{Key: "F5"})
			a.key(Message{Key: "Enter"})
			waitOperationQuestion(t, a, "Error")
			if !strings.Contains(a.Form.Message, "permission denied") {
				t.Fatal(a.Form.Message)
			}
			if retry {
				if e = os.Chmod(right, 0700); e != nil {
					t.Fatal(e)
				}
				chooseOperation(t, a, "Retry")
			} else {
				chooseOperation(t, a, "Skip all")
			}
			if retry {
				deadline := time.Now().Add(5 * time.Second)
				for a.Busy && time.Now().Before(deadline) {
					a.Poll()
					if a.Form != nil && a.Form.Title == "Error" {
						chooseOperation(t, a, "Retry")
					}
					time.Sleep(time.Millisecond)
				}
			}
			waitWork(t, a)
			for _, name := range []string{"one", "two"} {
				b, e := os.ReadFile(filepath.Join(right, name))
				if retry {
					if e != nil || string(b) != name {
						t.Fatal(string(b), e)
					}
				} else if !os.IsNotExist(e) {
					t.Fatal("skipped file created", e)
				}
			}
			if !retry && !strings.Contains(a.Status, "skipped") {
				t.Fatal(a.Status)
			}
		})
	}
}
func TestCollisionAppendRenameAndRemember(t *testing.T) {
	for _, action := range []string{"Append", "Rename", "Skip"} {
		t.Run(action, func(t *testing.T) {
			left, right := t.TempDir(), t.TempDir()
			for _, root := range []string{left, right} {
				for _, name := range []string{"one", "two"} {
					data := "old"
					if root == left {
						data = "new"
					}
					if e := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); e != nil {
						t.Fatal(e)
					}
				}
			}
			a, e := New(left, right)
			if e != nil {
				t.Fatal(e)
			}
			defer a.Close()
			a.Settings.CopyPermissions = false
			a.Settings.CopyAttributes = false
			a.Settings.ConfirmOverwrite = true
			a.Settings.CopyWorkers = 2
			a.Panels[0].Marked = map[string]bool{"one": true, "two": true}
			a.key(Message{Key: "F5"})
			a.key(Message{Key: "Enter"})
			waitOperationQuestion(t, a, "Warning")
			if !strings.Contains(a.Form.Message, "New:") || !strings.Contains(a.Form.Message, "Existing:") || !a.Form.Remember {
				t.Fatal(a.Form)
			}
			a.Form.Focus = len(a.Form.Choice)
			a.key(Message{Key: " "})
			if !a.Form.RememberChecked {
				t.Fatal("checkbox keyboard broken")
			}
			chooseOperation(t, a, action)
			waitWork(t, a)
			for _, name := range []string{"one", "two"} {
				want := "old"
				if action == "Append" {
					want = "oldnew"
				}
				b, e := os.ReadFile(filepath.Join(right, name))
				if e != nil || string(b) != want {
					t.Fatal(string(b), e)
				}
				if action == "Rename" {
					b, e = os.ReadFile(filepath.Join(right, name+" (copy)"))
					if e != nil || string(b) != "new" {
						t.Fatal(string(b), e)
					}
				}
			}
		})
	}
}
func TestCancelParallelOverwriteQuestion(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	for _, root := range []string{left, right} {
		for _, name := range []string{"one", "two"} {
			if e := os.WriteFile(filepath.Join(root, name), []byte(root), 0600); e != nil {
				t.Fatal(e)
			}
		}
	}
	a, e := New(left, right)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	a.Settings.ConfirmOverwrite = true
	a.Settings.CopyWorkers = 2
	a.Panels[0].Marked = map[string]bool{"one": true, "two": true}
	a.key(Message{Key: "F5"})
	a.key(Message{Key: "Enter"})
	waitOperationQuestion(t, a, "Warning")
	a.key(Message{Key: "Escape"})
	waitWork(t, a)
	if a.Form != nil {
		t.Fatal("cancel left a question")
	}
	for _, name := range []string{"one", "two"} {
		b, e := os.ReadFile(filepath.Join(right, name))
		if e != nil || string(b) != right {
			t.Fatal("cancel changed destination", e)
		}
	}
}

func TestReadOnlyCollisionAndDeleteCanSkip(t *testing.T) {
	left, right := t.TempDir(), t.TempDir()
	src, dst := filepath.Join(left, "file"), filepath.Join(right, "file")
	if e := os.WriteFile(src, []byte("new"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(dst, []byte("old"), 0400); e != nil {
		t.Fatal(e)
	}
	a, e := New(left, right)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	a.Settings.ConfirmOverwrite = false
	selectEntry(t, &a.Panels[0], "file")
	a.key(Message{Key: "F5"})
	a.key(Message{Key: "Enter"})
	waitOperationQuestion(t, a, "Warning")
	if !strings.Contains(a.Form.Message, "read only") {
		t.Fatal(a.Form.Message)
	}
	chooseOperation(t, a, "Skip")
	waitWork(t, a)
	if b, e := os.ReadFile(dst); e != nil || string(b) != "old" {
		t.Fatal(e)
	}
	a.Active = 1
	selectEntry(t, &a.Panels[1], "file")
	a.Settings.ConfirmDelete = false
	a.key(Message{Key: "F8", Shift: true})
	waitOperationQuestion(t, a, "Warning")
	chooseOperation(t, a, "Skip")
	waitWork(t, a)
	if _, e := os.Stat(dst); e != nil {
		t.Fatal("read-only file deleted", e)
	}
}
func TestErrorDialogLongMessageAndMouseCheckbox(t *testing.T) {
	a, e := New(t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	a.Cols = 80
	a.Rows = 25
	a.confirm("Warning", strings.Repeat("long path\n", 60), []string{"Overwrite", "Skip", "Rename", "Append", "Cancel"}, func(int) {})
	a.Form.Remember = true
	r, lines, by, ry := a.confirmationLayout()
	if len(lines) == 0 || by >= r.Y+r.H-1 || ry >= by {
		t.Fatal("invalid dialog layout")
	}
	a.formClick(Message{X: r.X + 6, Y: ry})
	if !a.Form.RememberChecked {
		t.Fatal("mouse checkbox did not toggle")
	}
	a.key(Message{Key: "PageDown"})
	if a.Form.Scroll == 0 {
		t.Fatal("long error not scrollable")
	}
	a.Draw()
}

func TestFatalStepOffersOnlyRetryAndCancel(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged user")
	}
	left, right := t.TempDir(), t.TempDir()
	if e := os.WriteFile(filepath.Join(left, "file"), []byte("data"), 0600); e != nil {
		t.Fatal(e)
	}
	locked := filepath.Join(right, "locked")
	if e := os.Mkdir(locked, 0); e != nil {
		t.Fatal(e)
	}
	defer os.Chmod(locked, 0700)
	a, e := New(left, right)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	selectEntry(t, &a.Panels[0], "file")
	a.key(Message{Key: "F5"})
	a.Form.Fields[0].Value = filepath.Join(locked, "inner", "file")
	a.key(Message{Key: "Enter"})
	waitOperationQuestion(t, a, "Error")
	if strings.Join(a.Form.Choice, ",") != "Retry,Cancel" || !strings.Contains(a.Form.Message, "Cannot read destination") {
		t.Fatalf("fatal step choices %q: %s", a.Form.Choice, a.Form.Message)
	}
	chooseOperation(t, a, "Cancel")
	waitWork(t, a)
	if strings.HasPrefix(a.Status, "Done") {
		t.Fatal("failed step reported as done:", a.Status)
	}
}

func TestConnectSkipDoesNotReportDone(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close() // Nothing listens: every connection attempt is refused.
	left := t.TempDir()
	if e := os.WriteFile(filepath.Join(left, "file"), []byte("data"), 0600); e != nil {
		t.Fatal(e)
	}
	a, e := New(left, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	a.Panels[1].Remote = &remotefs.Config{Protocol: "ftp", Host: "127.0.0.1", Port: port, User: "test", Directory: "/"}
	a.Panels[1].Path = "/"
	selectEntry(t, &a.Panels[0], "file")
	a.key(Message{Key: "F5"})
	a.key(Message{Key: "Enter"})
	waitOperationQuestion(t, a, "Error")
	for _, choice := range a.Form.Choice {
		if strings.HasPrefix(choice, "Skip") {
			t.Fatalf("connect offers %q", a.Form.Choice)
		}
	}
	chooseOperation(t, a, "Cancel")
	waitWork(t, a)
	if strings.HasPrefix(a.Status, "Done") {
		t.Fatal("failed connection reported as done:", a.Status)
	}
}

func TestConfirmationShowsLastErrorAndBothSizes(t *testing.T) {
	a, e := New(t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	a.Cols = 110
	a.Rows = 30
	for _, remember := range []bool{false, true} {
		a.confirm("Warning", "File exists\n/path\n\nNew: 4096 bytes\nExisting: 2048 bytes", []string{"Overwrite", "Skip", "Rename", "Append", "Cancel"}, func(int) {})
		a.Form.Remember = remember
		_, lines, _, _ := a.confirmationLayout()
		if len(lines) != 5 || lines[4] != "Existing: 2048 bytes" {
			t.Fatalf("last message line hidden: %q", lines)
		}
	}
}
