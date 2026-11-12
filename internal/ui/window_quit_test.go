package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQuitAbortedByBusyWindowRaisesAttention(t *testing.T) {
	root := t.TempDir()
	first, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	g := NewWindowGroup(first)
	g.StartShell = false
	id, err := g.NewWindow(1)
	if err != nil {
		t.Fatal(err)
	}
	busy := g.Apps[id]
	defer busy.Close()
	busy.Busy = true
	g.BeginQuit()
	if g.quitting || g.Terminating {
		t.Fatal("quit must be cancelled by a busy window")
	}
	if u := g.Poll(1); u.AttentionWindow != id {
		t.Fatalf("busy window not raised: %d", u.AttentionWindow)
	}
	if g.Poll(1).AttentionWindow != 0 {
		t.Fatal("attention must be requested once")
	}
	if first.Status != "Quit cancelled: window 2 is running a file operation" || busy.Status != "Wait for the file operation to finish" {
		t.Fatalf("statuses %q / %q", first.Status, busy.Status)
	}
}

func TestQuitWithOpenFormCancelsFormFirst(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("ENV", "")
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Settings.ShellCustomPrompt = false
	if err = a.StartTerminal(); err != nil {
		t.Fatal(err)
	}
	a.ShellReady = false // A command is running.
	for _, c := range []struct {
		close          bool
		title, message string
	}{{false, "Quit", "A command is running. Close the application?"}, {true, "Close window", "A command is running. Close this window?"}} {
		a.settingsMenu("Terminal settings")
		cancelled := false
		a.Form.Cancel = func() { cancelled = true }
		a.Form.Fields[0].Value = "edited"
		if c.close {
			a.RequestClose()
		} else {
			a.RequestQuit()
		}
		if !cancelled || a.Form == nil || a.Form.Title != c.title || a.Form.Message != c.message {
			t.Fatalf("cancelled=%v form=%+v", cancelled, a.Form)
		}
		a.key(Message{Key: "Escape"})
		if a.Quit || a.Settings.Shell == "edited" {
			t.Fatal("cancelled quit changed state")
		}
	}
}

func TestNewWindowInheritsCommandHistory(t *testing.T) {
	root := t.TempDir()
	first, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	first.history = []string{"make", "ls -la"}
	first.folderHistory = []string{root}
	first.viewHistory = []viewRecord{{Path: "/tmp/x"}}
	first.dialogValues = map[string]string{"Make folder": "new"}
	g := NewWindowGroup(first)
	g.StartShell = false
	id, err := g.NewWindow(1)
	if err != nil {
		t.Fatal(err)
	}
	b := g.Apps[id]
	defer b.Close()
	if len(b.history) != 2 || b.historyAt != 2 || len(b.folderHistory) != 1 || len(b.viewHistory) != 1 || b.dialogValues["Make folder"] != "new" {
		t.Fatalf("histories not inherited: %v %d %v %v %v", b.history, b.historyAt, b.folderHistory, b.viewHistory, b.dialogValues)
	}
	b.key(Message{Key: "ArrowUp", Ctrl: true})
	if b.Command != "ls -la" {
		t.Fatal("Ctrl+Up in the new window:", b.Command)
	}
	b.history[0] = "changed"
	if first.history[0] != "make" {
		t.Fatal("windows must not share history storage")
	}
}

func TestSessionSaveFailureReportedOnce(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "file")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	g := NewWindowGroup(a)
	g.StartShell = false
	g.sessionPath = filepath.Join(blocker, "session.json")
	g.Poll(1)
	if !strings.HasPrefix(a.Status, "Cannot save window session: ") {
		t.Fatalf("failure not reported: %q", a.Status)
	}
	a.Status = "Setup saved"
	for i := 0; i < 3; i++ {
		g.lastSessionWrite = time.Time{}
		g.Poll(1)
	}
	if a.Status != "Setup saved" {
		t.Fatal("failure retried within the retry interval:", a.Status)
	}
	// The same failure after the retry interval is not shown again.
	g.sessionErrorAt = time.Now().Add(-sessionRetryInterval)
	g.lastSessionWrite = time.Time{}
	g.Poll(1)
	if a.Status != "Setup saved" || g.sessionError == "" {
		t.Fatal("repeated failure overwrote the status:", a.Status)
	}
	g.sessionPath = filepath.Join(root, "session.json")
	g.sessionErrorAt = time.Now().Add(-sessionRetryInterval)
	g.lastSessionWrite = time.Time{}
	g.Poll(1)
	if g.sessionError != "" {
		t.Fatal("successful save must clear the remembered error")
	}
	if _, err = os.Stat(g.sessionPath); err != nil {
		t.Fatal(err)
	}
}
