package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
)

func archiveShellApp(t *testing.T) (*App, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("ENV", "")
	src := filepath.Join(root, "dir")
	if err := os.Mkdir(src, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	arc := filepath.Join(root, "test.zip")
	if err := fsops.CreateArchive([]string{src}, arc); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	a.Settings.Shell = "bash"
	a.Settings.ShellCustomPrompt = false
	selectEntry(t, &a.Panels[0], "test.zip")
	a.key(Message{Key: "Enter"})
	settlePanels(t, a)
	selectEntry(t, &a.Panels[0], "dir")
	a.key(Message{Key: "Enter"})
	settlePanels(t, a)
	if a.Panels[0].Archive == nil {
		t.Fatal("archive not entered")
	}
	if err = a.StartTerminal(); err != nil {
		t.Fatal(err)
	}
	pollUntil(t, a, func(Update) bool { return a.ShellReady })
	return a, root
}

func TestTerminalRoundTripKeepsArchivePanel(t *testing.T) {
	a, _ := archiveShellApp(t)
	inside := a.Panels[0].Path
	a.key(Message{Key: "o", Meta: true})
	if !a.TermVisible {
		t.Fatal("terminal not shown")
	}
	pollUntil(t, a, func(Update) bool { return a.ShellReady })
	a.key(Message{Key: "o", Meta: true})
	if a.TermVisible || a.Panels[0].Archive == nil || a.Panels[0].Path != inside {
		t.Fatalf("archive panel lost: %q archive=%v", a.Panels[0].Path, a.Panels[0].Archive != nil)
	}
}

func TestShellCdLeavesArchivePanel(t *testing.T) {
	a, root := archiveShellApp(t)
	a.key(Message{Key: "o", Meta: true})
	pollUntil(t, a, func(Update) bool { return a.ShellReady })
	other := filepath.Join(root, "dir")
	want, _ := filepath.EvalSymlinks(other)
	a.Handle(`{"Type":"input","Text":"cd ` + strings.ReplaceAll(other, `\`, `\\`) + `\r"}`)
	pollUntil(t, a, func(Update) bool {
		cwd, _ := filepath.EvalSymlinks(a.ShellCwd)
		return a.ShellReady && cwd == want
	})
	a.key(Message{Key: "o", Meta: true})
	got, _ := filepath.EvalSymlinks(a.Panels[0].Path)
	if a.Panels[0].Archive != nil || got != want {
		t.Fatalf("shell cd not followed: %q archive=%v", a.Panels[0].Path, a.Panels[0].Archive != nil)
	}
}

func TestCompletionUsesFilesystemPathForRemoteAndArchivePanels(t *testing.T) {
	root, local := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "abc"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Panels[0].Remote = &remotefs.Config{Protocol: "sftp", Host: "test", Port: 22}
	a.Panels[0].Path = "/home/user/remote-only"
	a.Panels[0].RemoteLocalPath = local
	a.Command = "cat a"
	a.completeCommand()
	if a.Command != "cat 'abc'" {
		t.Fatalf("remote panel completion: %q %q", a.Command, a.Status)
	}
	a.Panels[0].RemoteLocalPath = filepath.Join(local, "missing")
	a.Command = "cat a"
	a.completeCommand()
	if !strings.HasPrefix(a.Status, "Completion: cannot read "+filepath.Join(local, "missing")+": ") {
		t.Fatalf("completion error: %q", a.Status)
	}
}
