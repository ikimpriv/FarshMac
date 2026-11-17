package ui

import (
	"context"
	"encoding/base64"
	"farshmac/internal/shellsession"
	"farshmac/internal/terminal"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"farshmac/internal/remotefs"
	"farshmac/internal/testnet"
)

func TestRemoteShellCommandLifecycle(t *testing.T) { testRemoteShellLifecycle(t, false) }
func TestRemoteShellAlreadyOpen(t *testing.T)      { testRemoteShellLifecycle(t, true) }
func testRemoteShellLifecycle(t *testing.T, openFirst bool) {
	root := t.TempDir()
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAndWait(a)
	c := testnet.SSHShell(t)
	a.Panels[0].Remote = &c
	a.Panels[0].Path = root
	var output strings.Builder
	wait := func(done func() bool) {
		t.Helper()
		until := time.Now().Add(5 * time.Second)
		for time.Now().Before(until) {
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
	if openFirst {
		a.showTerminal()
		wait(func() bool { return a.Shells.Remote() != nil && a.Shells.Remote().Ready() })
		a.key(Message{Key: "o", Meta: true})
	}
	a.Command = "export SAVED_VALUE=remote; printf REMOTE_RESULT"
	a.execute()
	wait(func() bool { return !a.TermVisible })
	first := a.Foreign
	if first == nil {
		t.Fatal("no persistent SSH shell", a.Status)
	}
	a.key(Message{Key: "o", Meta: true})
	if !a.TermVisible || a.Foreign != first {
		t.Fatal("Cmd+O lost shell")
	}
	a.key(Message{Key: "o", Meta: true})
	a.Command = "printf '%s' \"$SAVED_VALUE\" > result; false"
	a.execute()
	wait(func() bool { return !a.TermVisible })
	data, err := os.ReadFile(filepath.Join(root, "result"))
	if err != nil || string(data) != "remote" {
		t.Fatal("wrong cwd/state", string(data), err)
	}
	if a.Foreign != first {
		t.Fatal("shell replaced")
	}
	if !strings.Contains(output.String(), "REMOTE_RESULT") || strings.Contains(output.String(), "any key returns") || strings.Contains(output.String(), "777;farshmac-") {
		t.Fatal(output.String())
	}
	// A different remote panel folder applies to the next command.
	next := filepath.Join(root, "other folder")
	os.Mkdir(next, 0700)
	a.Panels[0].Path = next
	a.Command = "printf NEXT > next"
	a.execute()
	wait(func() bool { return !a.TermVisible })
	if _, err = os.Stat(filepath.Join(next, "next")); err != nil {
		t.Fatal(err)
	}
	// Switching to local context must not retain routing to the remote shell.
	a.Panels[0].Remote = nil
	a.showTerminal()
	if a.Foreign != nil || a.Shells.RemoteKey() != "" {
		t.Fatal("remote routing survived local switch")
	}
}

func TestUnsupportedRemoteShellNeverRunsLocally(t *testing.T) {
	for _, protocol := range []string{"ftp", "ftps", "sftp"} {
		t.Run(protocol, func(t *testing.T) {
			root := t.TempDir()
			a, err := New(root, root)
			if err != nil {
				t.Fatal(err)
			}
			defer closeAndWait(a)
			c := remotefs.Config{Protocol: protocol, Host: "127.0.0.1", Port: 1, User: "test"}
			if protocol == "sftp" {
				c = testnet.SFTP(t)
			} // authenticated SFTP-only server rejects a PTY.
			a.Panels[0].Remote = &c
			a.Panels[0].Path = "/"
			a.pending = []byte("LOCAL_OUTPUT")
			a.showTerminal()
			until := time.Now().Add(5 * time.Second)
			for a.Foreign != nil && time.Now().Before(until) {
				a.pollForeign()
				time.Sleep(time.Millisecond)
			}
			if a.Foreign != nil || !a.TermVisible || a.Shells.RemoteKey() == "" {
				t.Fatal("remote error screen missing")
			}
			output := string(a.pending)
			if strings.Contains(output, "LOCAL_OUTPUT") {
				t.Fatal("local output leaked")
			}
			if protocol == "sftp" && !strings.Contains(output, "interactive SSH terminal") {
				t.Fatal(output)
			}
			if protocol != "sftp" && !strings.Contains(output, "does not support") {
				t.Fatal(output)
			}
			a.TermVisible = false
			a.Command = "touch " + filepath.Join(root, "must-not-exist")
			a.execute()
			until = time.Now().Add(5 * time.Second)
			for a.Foreign != nil && time.Now().Before(until) {
				a.pollForeign()
				time.Sleep(time.Millisecond)
			}
			if a.TermVisible {
				t.Fatal("failed panel command did not return")
			}
			if _, err := os.Stat(filepath.Join(root, "must-not-exist")); !os.IsNotExist(err) {
				t.Fatal("executed locally", err)
			}
			if a.Term != nil {
				t.Fatal("started a local shell")
			}
		})
	}
}

func TestRemoteShellReadinessTimeout(t *testing.T) {
	a := &App{Cols: 80, Rows: 25}
	s := newDeferredTerminal(80, 24, func(ctx context.Context) (foreignStream, error) { <-ctx.Done(); return nil, ctx.Err() })
	a.startForeign(s, "Remote", nil)
	m, _ := shellsession.NewRemote(shellsession.Context{ID: "test-remote"}, time.Now().Add(-31*time.Second))
	a.Shells.SetRemote(m, s)
	a.pollShellContext()
	a.pollForeign()
	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("readiness timeout did not cancel")
	}
	a.pollForeign()
	if !strings.Contains(string(a.pending), "did not become ready") {
		t.Fatal(string(a.pending))
	}
}

func TestRemoteShellContextNotifications(t *testing.T) {
	root := t.TempDir()
	next := filepath.Join(root, "from-shell")
	if err := os.Mkdir(next, 0700); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAndWait(a)
	c := testnet.SSHShell(t)
	// Mirror directories in the fixture's independent in-memory SFTP tree.
	fs, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	current := "/"
	for _, part := range strings.Split(strings.TrimPrefix(next, "/"), "/") {
		current = filepath.Join(current, part)
		if _, err := fs.Stat(current); err != nil {
			if err = fs.Mkdir(current); err != nil {
				t.Fatal(err)
			}
		}
	}
	p := &a.Panels[0]
	p.Remote = &c
	p.Path = root
	a.showTerminal()
	awaitShell(t, a, func() bool { return a.Shells.Remote() != nil && a.Shells.Remote().Ready() })
	sendContainerTestMessage(a, Message{Type: "input", Text: "cd " + terminal.Quote(next) + "\n"})
	awaitShell(t, a, func() bool { return a.Shells.Current().Directory == next && a.Shells.Remote().Ready() })
	if p.Path != root {
		t.Fatal("panel changed before returning")
	}
	a.key(Message{Key: "o", Meta: true})
	waitNetwork(t, a)
	if p.Path != next {
		t.Fatal("shell cwd not applied to remote panel", p.Path, a.Status)
	}
	a.error(p.Load(root))
	waitNetwork(t, a)
	awaitShell(t, a, func() bool { return a.Shells.Remote().Ready() && a.Shells.Remote().Context().Directory == root })
	if a.TermVisible {
		t.Fatal("directory synchronization opened terminal")
	}
	// Stale notifications from another session cannot change current context.
	a.Shells.ShellChanged(shellsession.Context{ID: "unrelated", Directory: "/wrong"})
	if a.Shells.Current().Directory != root {
		t.Fatal("cross-session cwd leak")
	}
}

func TestShellContextRoundTripRetainsProcessStateAndOutput(t *testing.T) {
	root := t.TempDir()
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAndWait(a)
	c := testnet.SSHShell(t)
	a.Panels[0].Remote = &c
	a.Panels[0].Path = root
	a.showTerminal()
	remote := a.Foreign
	key := a.currentTerminalKey()
	wait := func(check func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			a.Poll()
			if check() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("shell not ready")
	}
	wait(func() bool { return a.Shells.Remote().Ready() })
	a.Command = "export FARSHMAC_SESSION_TEST=retained"
	a.execute()
	wait(func() bool { return !a.TermVisible })
	a.Panels[0].Remote = nil
	a.showTerminal()
	if a.Foreign != nil || a.currentTerminalKey() != "local" {
		t.Fatal("local routing not restored")
	}
	if a.parkedTerminals[key] == nil || a.parkedTerminals[key].stream != remote {
		t.Fatal("remote session not retained")
	}
	a.pending = append(a.pending, []byte("LOCAL_RETAINED")...)
	a.Panels[0].Remote = &c
	a.showTerminal()
	if a.Foreign != remote {
		t.Fatal("return started a different shell")
	}
	a.Command = "printf %s \"$FARSHMAC_SESSION_TEST\" > " + terminal.Quote(filepath.Join(root, "state-result"))
	a.execute()
	wait(func() bool { return !a.TermVisible })
	b, err := os.ReadFile(filepath.Join(root, "state-result"))
	if err != nil || string(b) != "retained" {
		t.Fatal("remote environment lost", string(b), err)
	}
	a.Panels[0].Remote = nil
	a.showTerminal()
	// Background output may already have been sent to its own renderer packet.
	if a.currentTerminalKey() != "local" {
		t.Fatal("wrong restored terminal")
	}
}
