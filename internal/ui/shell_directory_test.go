package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPanelDirectorySyncWaitsForShell(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "next folder")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("ZDOTDIR", root)
	// Startup may still be loading configuration when Cmd+O is pressed.
	if err := os.WriteFile(filepath.Join(root, ".bashrc"), []byte("sleep 0.1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, target)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err = a.StartTerminal(); err != nil {
		t.Fatal(err)
	}
	a.Active = 1
	a.key(Message{Key: "o", Meta: true})
	if !a.TermVisible || a.ShellReady || a.pendingShellCwd != target {
		t.Fatal("startup request not queued")
	}
	wait := func() {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			a.Poll()
			if a.ShellReady && a.pendingShellCwd == "" {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("shell never ready", a.Status)
	}
	wait()
	equal := func(want string) {
		t.Helper()
		got, _ := filepath.EvalSymlinks(a.ShellCwd)
		want, _ = filepath.EvalSymlinks(want)
		if got != want {
			t.Fatalf("cwd %q want %q", got, want)
		}
	}
	equal(target)
	// A command consumes stdin: queued directory sync must not inject keys into it.
	a.Command = ": > waiting; read -r line; printf '%s' \"$line\" > received"
	a.execute()
	a.key(Message{Key: "o", Meta: true})
	if err = a.Panels[a.Active].Load(root); err != nil {
		t.Fatal(err)
	}
	a.key(Message{Key: "o", Meta: true})
	if a.pendingShellCwd != root {
		t.Fatal("busy shell lost queued directory")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(target, "waiting")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("command never started")
		}
		a.Poll()
		time.Sleep(5 * time.Millisecond)
	}
	a.Handle(`{"Type":"input","Text":"ONLY_USER_INPUT\n"}`)
	wait()
	equal(root)
	data, err := os.ReadFile(filepath.Join(target, "received"))
	if err != nil || string(data) != "ONLY_USER_INPUT" {
		t.Fatalf("injected into running command: %q %v", data, err)
	}
	// Ordinary folder change while the panels are visible.
	a.key(Message{Key: "o", Meta: true})
	if err = a.Panels[a.Active].Load(target); err != nil {
		t.Fatal(err)
	}
	a.key(Message{Key: "o", Meta: true})
	wait()
	equal(target)
}

func TestShellCDReturnsToActivePanelBeforePromptEvent(t *testing.T) {
	root, target := t.TempDir(), t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("ZDOTDIR", root)
	a, _ := New(root, root)
	defer a.Close()
	if err := a.StartTerminal(); err != nil {
		t.Fatal(err)
	}
	wait := func() {
		t.Helper()
		end := time.Now().Add(5 * time.Second)
		for time.Now().Before(end) {
			a.Poll()
			if a.ShellReady {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("shell not ready")
	}
	wait()
	a.showTerminal()
	wait()
	a.ShellReady = false
	if err := a.Term.Write("cd '" + target + "'\r"); err != nil {
		t.Fatal(err)
	}
	a.key(Message{Key: "o", Meta: true}) // no Poll: readiness still in flight
	wait()
	got, _ := filepath.EvalSymlinks(a.Panels[a.Active].Path)
	want, _ := filepath.EvalSymlinks(target)
	if got != want {
		t.Fatalf("panel %s expected %s", got, want)
	}
	if a.Panels[1].Path != root {
		t.Fatal("passive panel changed")
	}
	// If user navigates before the command finishes, do not overwrite their choice.
	a.TermVisible = true
	a.ShellReady = false
	a.Term.Write("sleep 0.1; cd '" + root + "'\r")
	a.key(Message{Key: "o", Meta: true})
	chosen := t.TempDir()
	a.Panels[0].Load(chosen)
	wait()
	if a.Panels[0].Path != chosen {
		t.Fatal("late shell event overwrote panel navigation")
	}
}
