package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pollUntil(t *testing.T, a *App, done func(Update) bool) Update {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if u := a.Poll(); done(u) {
			return u
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout: %s", a.Status)
	return Update{}
}

func TestShellRestartBackoffStopsAfterRepeatedStartupFailures(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("ZDOTDIR", root)
	rc := filepath.Join(root, ".zshrc")
	if err := os.WriteFile(rc, []byte("exit 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Settings.Shell = "zsh"
	a.Settings.ShellCustomPrompt = false
	a.TermVisible = true
	if err = a.StartTerminal(); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= maxShellStartFailures; attempt++ {
		pollUntil(t, a, func(Update) bool { return a.Term == nil })
		if attempt == maxShellStartFailures {
			break
		}
		want := time.Second << (attempt - 1)
		if left := time.Until(a.shellRestartAt); left < want-time.Second/2 || left > want {
			t.Fatalf("attempt %d: restart in %v, want %v", attempt, left, want)
		}
		a.shellRestartAt = time.Now().Add(-time.Millisecond)
		a.restartShellIfDue()
		if a.Term == nil {
			t.Fatal("restart did not start a shell:", a.Status)
		}
	}
	if !a.shellRestartAt.IsZero() || a.shellStopped == "" || !strings.HasPrefix(a.Status, "Shell keeps exiting during startup (5 attempts)") {
		t.Fatalf("restart loop not stopped: %q %v", a.Status, a.shellRestartAt)
	}
	u := a.Poll()
	if !strings.Contains(u.Footer, "Shell stopped: exited 5 times during startup") || !strings.Contains(u.Footer, "Enter: retry") {
		t.Fatalf("footer %q", u.Footer)
	}
	if err = os.WriteFile(rc, nil, 0600); err != nil {
		t.Fatal(err)
	}
	a.Handle(`{"Type":"input","Text":"\r"}`)
	if a.shellRestartAt.IsZero() || a.shellStopped != "" {
		t.Fatal("Enter did not retry")
	}
	pollUntil(t, a, func(Update) bool { return a.ShellReady })
	if a.shellFailures != 0 {
		t.Fatal("prompt must reset failure counter")
	}
}

func TestShellRestartFallsBackToHomeWhenCwdNotExecutable(t *testing.T) {
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("ZDOTDIR", root)
	t.Setenv("ENV", "")
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Settings.ShellCustomPrompt = false
	if err = os.Chmod(locked, 0600); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0700)
	a.ShellCwd = locked
	a.scheduleShellRestart(nil)
	a.shellRestartAt = time.Now().Add(-time.Millisecond)
	a.restartShellIfDue()
	if a.Term == nil || !strings.HasPrefix(a.Status, "Cannot start shell in "+locked) || !strings.HasSuffix(a.Status, "using "+root) {
		t.Fatalf("no home fallback: %q", a.Status)
	}
	pollUntil(t, a, func(Update) bool { return a.ShellReady })
	got, _ := filepath.EvalSymlinks(a.ShellCwd)
	want, _ := filepath.EvalSymlinks(root)
	if got != want {
		t.Fatal("shell not started in home", got)
	}
}

func TestInitialShellStartFailureIsRetried(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("ENV", "")
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Settings.Shell = "fish"
	a.Settings.ShellCustomPrompt = false
	a.StartTerminalOrRetry()
	if a.Term != nil || a.shellRestartAt.IsZero() || a.Status != `Shell: unsupported shell "fish"; retrying…` {
		t.Fatalf("initial failure not scheduled: %q", a.Status)
	}
	a.Handle(`{"Type":"key","Key":"x"}`)
	a.Handle(`{"Type":"key","Key":"Enter"}`)
	if a.Status != "Shell unavailable; retrying…" {
		t.Fatalf("execute status %q", a.Status)
	}
	a.Settings.Shell = "bash"
	a.shellRestartAt = time.Now().Add(-time.Millisecond)
	pollUntil(t, a, func(Update) bool { return a.ShellReady })
}

func TestShellIntegrationWatchdogRestoresReady(t *testing.T) {
	old := shellIntegrationTimeout
	shellIntegrationTimeout = 300 * time.Millisecond
	defer func() { shellIntegrationTimeout = old }()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("ENV", "")
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Settings.Shell = "bash"
	a.Settings.ShellCustomPrompt = false
	if err = a.StartTerminal(); err != nil {
		t.Fatal(err)
	}
	pollUntil(t, a, func(Update) bool { return a.ShellReady })
	// Remove the integration widget, as a keymap-replacing plugin would.
	a.Handle(`{"Type":"input","Text":"bind -r '\\C-x\\C-g'\r"}`)
	pollUntil(t, a, func(Update) bool { return a.ShellReady })
	a.TermVisible = false
	a.Command = "echo watchdog"
	a.Handle(`{"Type":"key","Key":"Enter"}`)
	if a.ShellReady || a.Command != "" {
		t.Fatal("command was not submitted")
	}
	pollUntil(t, a, func(Update) bool { return a.ShellReady })
	if a.Command != "echo watchdog" || !strings.HasPrefix(a.Status, "Shell integration did not respond") {
		t.Fatalf("watchdog: command %q status %q", a.Command, a.Status)
	}
	// The next request must not fail on the abandoned request file.
	a.Handle(`{"Type":"commandHistory"}`)
	if a.ShellReady {
		t.Fatal("history request failed:", a.Status)
	}
	u := pollUntil(t, a, func(u Update) bool { return u.Commands != nil })
	if len(*u.Commands) != 0 || !strings.HasPrefix(u.CommandsError, "Shell integration did not respond") {
		t.Fatalf("overlay not answered: %v %q", *u.Commands, u.CommandsError)
	}
}

func TestCommandHistoryRequestFailureReportsToOverlay(t *testing.T) {
	root := t.TempDir()
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Handle(`{"Type":"commandHistory"}`)
	u := a.Poll()
	if u.Commands == nil || len(*u.Commands) != 0 || u.CommandsError != "Cannot read shell history: shell unavailable" {
		t.Fatalf("no reply without shell: %v %q", u.Commands, u.CommandsError)
	}
	if u = a.Poll(); u.Commands != nil || u.CommandsError != "" {
		t.Fatal("reply must be sent once")
	}
	a.shellCommands = nil
	a.scheduleShellRestart(nil)
	if u = a.Poll(); u.Commands == nil || u.CommandsError != "Shell exited; history unavailable" {
		t.Fatalf("exit not reported: %q", u.CommandsError)
	}
}

func TestWhitespaceCommandIsNotExecuted(t *testing.T) {
	root := t.TempDir()
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Command = " \t "
	a.Handle(`{"Type":"key","Key":"Enter"}`)
	if a.Command != "" || len(a.history) != 0 || strings.HasPrefix(a.Status, "Shell unavailable") {
		t.Fatalf("whitespace command executed: %q %v %q", a.Command, a.history, a.Status)
	}
}
