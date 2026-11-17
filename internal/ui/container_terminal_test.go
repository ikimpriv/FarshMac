package ui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"farshmac/internal/kube/localexec"
	"farshmac/internal/shellsession"
	"farshmac/internal/terminal"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCmdOUsesContainerShellAndKeepsSession(t *testing.T) {
	a, f := kubeApp(t)
	p := &a.Panels[0]
	podWithContainers(f, "solo", "app")
	p.Kube.invalidate("")
	root := localexec.Fixture(t)
	p.Kube.executor = localexec.New(t, "", strings.Fields(localexec.BaseTools)...)
	a.error(p.Load("/shop/pods/solo" + root))
	waitNetwork(t, a)
	a.pending = []byte("LOCAL SHELL OUTPUT")
	a.key(Message{Key: "o", Meta: true})
	if !a.TermVisible || a.Foreign == nil || a.Shells.RemoteKey() == "" {
		t.Fatal("no container terminal")
	}
	first := a.Foreign
	if strings.Contains(string(a.pending), "LOCAL SHELL OUTPUT") {
		t.Fatal("local screen leaked")
	}
	// Repeated Cmd+O returns to the same live shell.
	a.key(Message{Key: "o", Meta: true})
	a.key(Message{Key: "o", Meta: true})
	if a.Foreign != first {
		t.Fatal("shell was restarted")
	}
	deadline := time.Now().Add(5 * time.Second)
	var output strings.Builder
	sent := false
	for time.Now().Before(deadline) {
		if d, ok := a.Foreign.(*deferredTerminal); ok && !sent {
			ready := d.Connected()
			if ready {
				sendContainerTestMessage(a, Message{Type: "input", Text: "printf 'REMOTE_MARKER'; pwd\n"})
				sent = true
			}
		}
		u := a.Poll()
		data, _ := base64.StdEncoding.DecodeString(u.Terminal)
		output.Write(data)
		if strings.Contains(output.String(), "REMOTE_MARKER") && strings.Contains(output.String(), root) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(output.String(), root) {
		t.Fatal("container cwd missing", output.String())
	}
	a.key(Message{Key: ".", Meta: true})
	deadline = time.Now().Add(2 * time.Second)
	for a.Foreign != nil && time.Now().Before(deadline) {
		a.Poll()
		time.Sleep(time.Millisecond)
	}
	if a.Foreign != nil {
		t.Fatal("Cmd+. did not stop shell")
	}
}
func TestContainerShellErrorDoesNotFallBackToLocal(t *testing.T) {
	a := &App{Cols: 80, Rows: 25}
	s := newDeferredTerminal(80, 24, func(context.Context) (foreignStream, error) { return nil, errors.New("no /bin/sh in target") })
	a.startForeign(s, "Container app", nil)
	m, _ := shellsession.NewRemote(shellsession.Context{ID: "test"}, time.Now())
	a.Shells.SetRemote(m, s)
	a.pending = []byte("\x1bc")
	<-s.Done()
	a.pollForeign()
	if !a.TermVisible || !strings.Contains(string(a.pending), "no /bin/sh") {
		t.Fatal("missing failure screen", string(a.pending))
	}
	sendContainerTestMessage(a, Message{Type: "commandHistoryRun", Text: "touch forbidden-local-file"})
	a.appendLocalTerminal([]byte("LOCAL"))
	if strings.Contains(string(a.pending), "LOCAL") {
		t.Fatal("local output leaked")
	}
	sendContainerTestMessage(a, Message{Type: "commandHistory"})
	if a.shellCommands == nil || !strings.Contains(a.commandsError, "local history is unavailable") {
		t.Fatal("local history exposed")
	}
}
func TestConnectingShellCanBeCancelled(t *testing.T) {
	s := newDeferredTerminal(80, 24, func(ctx context.Context) (foreignStream, error) { <-ctx.Done(); return nil, ctx.Err() })
	_ = s.Write([]byte{27})
	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("resolution did not cancel")
	}
}

func sendContainerTestMessage(a *App, m Message) { data, _ := json.Marshal(m); a.Handle(string(data)) }

func TestContainerShellContextNotifications(t *testing.T) {
	a, f := kubeApp(t)
	p := &a.Panels[0]
	podWithContainers(f, "solo", "app")
	p.Kube.invalidate("")
	root := localexec.Fixture(t)
	next := filepath.Join(root, "from-shell")
	if err := os.Mkdir(next, 0700); err != nil {
		t.Fatal(err)
	}
	p.Kube.executor = localexec.New(t, "", strings.Fields(localexec.BaseTools)...)
	a.error(p.Load("/shop/pods/solo" + root))
	waitNetwork(t, a)
	a.showTerminal()
	awaitShell(t, a, func() bool { return a.Shells.Remote() != nil && a.Shells.Remote().Ready() })
	sendContainerTestMessage(a, Message{Type: "input", Text: "cd " + terminal.Quote(next) + "\n"})
	awaitShell(t, a, func() bool { return a.Shells.Current().Directory == next && a.Shells.Remote().Ready() })
	a.key(Message{Key: "o", Meta: true})
	waitNetwork(t, a)
	if p.kubeLevel().FSPath != next {
		t.Fatal("container cwd not applied", p.Path)
	}
	a.error(p.Load("/shop/pods/solo" + root))
	waitNetwork(t, a)
	awaitShell(t, a, func() bool { return a.Shells.Remote().Ready() && a.Shells.Remote().Context().Directory == root })
	if a.TermVisible {
		t.Fatal("sync opened terminal")
	}
}
