package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func awaitShell(t *testing.T, a *App, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		a.Poll()
		if ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("shell timeout: %s", a.Status)
}
func TestParentShellRestartAndNestedExit(t *testing.T) {
	for _, shell := range []string{"bash", "sh", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			t.Setenv("ZDOTDIR", root)
			t.Setenv("ENV", "")
			a, err := New(root, root)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			a.Settings.Shell = shell
			a.Settings.ShellCustomPrompt = false
			a.TermVisible = true
			if err = a.StartTerminal(); err != nil {
				t.Fatal(err)
			}
			awaitShell(t, a, func() bool { return a.ShellReady })
			parent := a.Term
			a.Handle(`{"Type":"input","Text":"/bin/sh\r"}`)
			// Both lines are queued in the same PTY; exit terminates the nested shell.
			a.Handle(`{"Type":"input","Text":"exit\r"}`)
			awaitShell(t, a, func() bool { return a.ShellReady })
			if a.Term != parent {
				t.Fatal("nested exit restarted parent")
			}
			for _, input := range []string{`{"Type":"input","Text":"exit\r"}`, `{"Type":"input","Text":"\u0004"}`} {
				old := a.Term
				a.Handle(input)
				awaitShell(t, a, func() bool { return a.Term != nil && a.Term != old && a.ShellReady })
				if !a.TermVisible {
					t.Fatal("shell restart changed visible panel")
				}
				got, _ := filepath.EvalSymlinks(a.ShellCwd)
				want, _ := filepath.EvalSymlinks(root)
				if got != want {
					t.Fatal("working directory lost", got)
				}
			}
		})
	}
}
func TestShellRestartDeletedDirectoryAndShutdown(t *testing.T) {
	root := t.TempDir()
	removed := filepath.Join(root, "gone")
	if err := os.Mkdir(removed, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("ZDOTDIR", root)
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Settings.ShellCustomPrompt = false
	a.ShellCwd = removed
	if err = os.Remove(removed); err != nil {
		t.Fatal(err)
	}
	a.scheduleShellRestart(nil)
	if a.shellRestartAt.Before(time.Now()) {
		t.Fatal("restart must be rate limited")
	}
	a.shellRestartAt = time.Now().Add(-time.Second)
	a.restartShellIfDue()
	awaitShell(t, a, func() bool { return a.ShellReady })
	got, _ := filepath.EvalSymlinks(a.ShellCwd)
	want, _ := filepath.EvalSymlinks(root)
	if got != want {
		t.Fatal("did not use surviving parent", got)
	}
	a.Quit = true
	a.scheduleShellRestart(nil)
	a.shellRestartAt = time.Now().Add(-time.Second)
	a.restartShellIfDue()
	if a.Term != nil {
		t.Fatal("restarted during shutdown")
	}
}
