package ui

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"farshmac/internal/kube/localexec"
)

func TestContainerPanelCommandsReuseShellAndReturn(t *testing.T) {
	a, f := kubeApp(t)
	p := &a.Panels[0]
	podWithContainers(f, "solo", "app")
	p.Kube.invalidate("")
	root := localexec.Fixture(t)
	p.Kube.executor = localexec.New(t, "", strings.Fields(localexec.BaseTools)...)
	a.error(p.Load("/shop/pods/solo" + root))
	waitNetwork(t, a)
	var output strings.Builder
	wait := func(done func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			u := a.Poll()
			b, _ := base64.StdEncoding.DecodeString(u.Terminal)
			output.Write(b)
			if done() {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("timeout", a.Status, output.String())
	}
	run := func(command string) {
		t.Helper()
		a.Command = command
		a.execute()
		wait(func() bool { return !a.TermVisible })
		if strings.Contains(output.String(), "any key returns") || strings.Contains(output.String(), "777;farshmac-") {
			t.Fatal("protocol/pause leaked", output.String())
		}
	}
	// First command starts a shell and waits for its first prompt.
	run("export PANEL_VALUE=kept; printf FIRST_OUTPUT")
	first := a.Foreign
	if first == nil {
		t.Fatal("shell ended with command")
	}
	// Cmd+O opens the same session, then panels can submit another command.
	a.key(Message{Key: "o", Meta: true})
	if !a.TermVisible || a.Foreign != first {
		t.Fatal("output shell not retained")
	}
	a.key(Message{Key: "o", Meta: true})
	run("printf '%s' \"$PANEL_VALUE\" > kept.txt; false")
	if a.Foreign != first {
		t.Fatal("session replaced")
	}
	data, err := os.ReadFile(filepath.Join(root, "kept.txt"))
	if err != nil || string(data) != "kept" {
		t.Fatal("shell state lost", string(data), err)
	}
	if !strings.Contains(output.String(), "FIRST_OUTPUT") {
		t.Fatal("output missing")
	}
	// Do not send a queued panel command to a program reading stdin.
	a.key(Message{Key: "o", Meta: true})
	sendContainerTestMessage(a, Message{Type: "input", Text: "read -r answer; printf '%s' \"$answer\" > answer.txt\n"})
	a.key(Message{Key: "o", Meta: true})
	a.Command = "printf QUEUED > queued.txt"
	a.execute()
	for i := 0; i < 10; i++ {
		a.Poll()
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(root, "queued.txt")); err == nil {
		t.Fatal("ran into busy stdin")
	}
	sendContainerTestMessage(a, Message{Type: "input", Text: "USER_INPUT\n"})
	wait(func() bool { return !a.TermVisible })
	data, err = os.ReadFile(filepath.Join(root, "answer.txt"))
	if err != nil || string(data) != "USER_INPUT" {
		t.Fatal("injected into stdin", string(data), err)
	}
	data, err = os.ReadFile(filepath.Join(root, "queued.txt"))
	if err != nil || string(data) != "QUEUED" {
		t.Fatal("queued command lost", string(data), err)
	}
	// A nested interactive shell is the running command, not its completion.
	a.Command = "/bin/sh"
	a.execute()
	for i := 0; i < 20; i++ {
		a.Poll()
		time.Sleep(5 * time.Millisecond)
	}
	if !a.TermVisible || !a.Shells.Remote().Running() {
		t.Fatal("nested prompt completed parent command")
	}
	sendContainerTestMessage(a, Message{Type: "input", Text: "exit\n"})
	wait(func() bool { return !a.TermVisible })
	run("exit")
	if a.Foreign != nil {
		t.Fatal("exit didn't close shell")
	}
}

func TestLocalPanelCommandReturnsAutomatically(t *testing.T) {
	for _, shell := range []string{"bash", "sh", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			t.Setenv("ZDOTDIR", root)
			a, err := New(root, root)
			if err != nil {
				t.Fatal(err)
			}
			defer closeAndWait(a)
			a.Settings.Shell = shell
			a.Settings.ShellCustomPrompt = false
			if err = a.StartTerminal(); err != nil {
				t.Fatal(err)
			}
			// A panel command may arrive before the startup prompt.
			a.Command = "printf STARTUP > startup.txt"
			a.execute()
			awaitShell(t, a, func() bool { return a.ShellReady && !a.Shells.Local().HasQueued() && !a.Shells.Local().Running() })
			if data, err := os.ReadFile(filepath.Join(root, "startup.txt")); err != nil || string(data) != "STARTUP" {
				t.Fatal("startup command lost", string(data), err)
			}
			first := a.Term
			for _, command := range []string{"printf LOCAL_RESULT", "false", "printf AGAIN"} {
				a.Command = command
				a.execute()
				if !a.TermVisible {
					t.Fatal("command not shown")
				}
				awaitShell(t, a, func() bool { return !a.Shells.Local().Running() })
				if a.TermVisible || a.Term != first {
					t.Fatal("didn't return with live shell")
				}
				a.key(Message{Key: "o", Meta: true})
				if !a.TermVisible || a.Term != first {
					t.Fatal("Cmd+O didn't retain shell")
				}
				a.key(Message{Key: "o", Meta: true})
			}
		})
	}
}
