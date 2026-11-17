package shellsession

import (
	"bytes"
	"context"
	"farshmac/internal/terminal"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeStream struct {
	writes [][]byte
	closed int
}

func (f *fakeStream) Write(b []byte) error {
	f.writes = append(f.writes, append([]byte(nil), b...))
	return nil
}
func (f *fakeStream) Resize(int, int) error { return nil }
func (f *fakeStream) Output() <-chan []byte { return nil }
func (f *fakeStream) Done() <-chan struct{} { return nil }
func (f *fakeStream) Err() error            { return nil }
func (f *fakeStream) Close()                { f.closed++ }
func remote(t *testing.T) (*Session, *fakeStream) {
	t.Helper()
	s, err := NewRemote(Context{Kind: "ssh", ID: "server-a", Directory: "/start"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeStream{}
	s.Bind(f)
	return s, f
}
func prompt(s *Session, logical, execution string) []byte {
	return []byte(s.prefix + logical + "\x00" + execution + "\a")
}
func TestPromptFragmentsAndBoundedParser(t *testing.T) {
	for split := 0; split < 100; split++ {
		s, _ := remote(t)
		s.Sent()
		data := prompt(s, "/folder with spaces", "/folder with spaces")
		at := min(split, len(data))
		first := s.Feed(append([]byte("before"), data[:at]...))
		second := s.Feed(append(data[at:], []byte("after")...))
		if !(first.Completed || second.Completed) || !s.Ready() || s.Context().Directory != "/folder with spaces" {
			t.Fatal(split, s.Context())
		}
		if string(append(first.Output, second.Output...)) != "beforeafter" {
			t.Fatal("protocol leaked")
		}
		s.Feed([]byte(s.prefix + strings.Repeat("x", 1<<20)))
		if len(s.tail) > 32<<10 {
			t.Fatal("unbounded incomplete frame")
		}
	}
}
func TestQueueBusyCwdSyncAndCompletion(t *testing.T) {
	s, f := remote(t)
	if err := s.Queue(Command{Text: "printf done", Path: "/next"}); err != nil {
		t.Fatal(err)
	}
	if s.Dispatch().Started || len(f.writes) != 0 {
		t.Fatal("sent while connecting")
	}
	r := s.Feed(prompt(s, "/start", "/start"))
	if !r.Started || r.Completed || !s.Running() {
		t.Fatal(r)
	}
	if !bytes.Contains(f.writes[0], []byte("/next")) {
		t.Fatal("cwd missing")
	}
	if s.Queue(Command{Text: "second"}) == nil {
		t.Fatal("overwrote running command")
	}
	s.RequestDirectory("/from-panel")
	r = s.Feed(prompt(s, "/from-command", "/from-command"))
	if !r.Completed || s.Ready() {
		t.Fatal("directory request should wait for its own prompt", r)
	}
	if len(f.writes) != 2 {
		t.Fatal("directory not sent")
	}
	r = s.Feed(prompt(s, "/from-panel", "/from-panel"))
	if r.Completed || !s.Ready() {
		t.Fatal("directory sync counted as panel command")
	}
	s.Input([]byte("read value\n"))
	s.Queue(Command{Text: "later", Path: "/from-panel"})
	if s.Dispatch().Started {
		t.Fatal("injected into foreground stdin")
	}
}
func TestContextOriginAndSessionIsolation(t *testing.T) {
	var m Manager
	s, f := remote(t)
	m.SetRemote(s, f)
	r := s.Feed(prompt(s, "/shell-cd", "/shell-cd"))
	m.ShellChanged(r.Context)
	m.PanelChanged(Context{Kind: "ssh", ID: "server-a", Directory: "/start"})
	if m.Current().Directory != "/shell-cd" {
		t.Fatal("unchanged panel overwrote shell cwd")
	}
	m.PanelChanged(Context{Kind: "ssh", ID: "server-a", Directory: "/panel-cd"})
	if !strings.Contains(directoryLine(s.desiredDirectory), "/panel-cd") {
		t.Fatal("panel notification lost")
	}
	m.PanelChanged(Context{Kind: "ssh", ID: "server-b", Directory: "/different"})
	m.ShellChanged(r.Context)
	if m.Current().ID != "server-b" {
		t.Fatal("background session changed active context")
	}
	replacement, _ := NewRemote(Context{ID: "server-b"}, time.Now())
	other := &fakeStream{}
	m.SetRemote(replacement, other)
	if f.closed != 0 {
		t.Fatal("context switch closed the previous shell")
	}
	local := &fakeStream{}
	m.AttachLocal(local)
	m.Close()
	if f.closed != 1 || other.closed != 1 || local.closed != 1 {
		t.Fatal("manager did not close all owned sessions")
	}
}
func TestUnmappedEphemeralCwdAndInvalidProtocol(t *testing.T) {
	s, _ := remote(t)
	r := s.Feed(prompt(s, "", "/debugger/tools"))
	if !r.Changed || s.Context().Directory != "" || s.Context().ExecutionDirectory != "/debugger/tools" {
		t.Fatal(r)
	}
	s.Busy()
	s.Feed(prompt(s, "/invalid\npath", "/normal"))
	if s.Ready() {
		t.Fatal("accepted invalid directory")
	}
}
func TestTimeoutCancellation(t *testing.T) {
	s, f := remote(t)
	s.connectingSince = time.Now().Add(-31 * time.Second)
	if !s.Expire(time.Now()) || f.closed != 1 || s.Expire(time.Now()) {
		t.Fatal("timeout must close once")
	}
	d := NewDeferred(80, 24, func(ctx context.Context) (Stream, error) { <-ctx.Done(); return nil, ctx.Err() })
	d.Close()
	select {
	case <-d.Done():
	case <-time.After(time.Second):
		t.Fatal("connecting cancellation blocked")
	}
}
func TestLocalLifecycleUsesSameCommandState(t *testing.T) {
	var m Manager
	m.PanelChanged(Context{Kind: "local", ID: "local", Directory: "/a"})
	s := m.Local()
	s.Queue(Command{Text: "pwd", Path: "/a"})
	if c := s.TakeQueued(); c == nil || c.Text != "pwd" {
		t.Fatal("queue lost")
	}
	s.Sent()
	s.Busy()
	if !s.Prompt() {
		t.Fatal("no completion")
	}
	m.LocalDirectory("/b")
	if m.Current().Directory != "/b" {
		t.Fatal("local cwd notification missing")
	}
}

func TestToolboxLogicalAndExecutionDirectories(t *testing.T) {
	for _, shell := range []string{"/bin/bash", "/bin/sh"} {
		for _, outside := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/outside=%t", shell, outside), func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "target root")
				dir := filepath.Join(root, "folder")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				s, _ := remote(t)
				script := "export TARGET_ROOT=" + terminal.Quote(root) + "; cd " + terminal.Quote(dir) + "; " + s.Launch(shell, "target | PodToast")
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, shell, "-c", script)
				input := "printf hello\nexit\n"
				if outside {
					input = "cd /\nexit\n"
				}
				cmd.Stdin = strings.NewReader(input)
				output, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatal(err, string(output))
				}
				result := s.Feed(output)
				if outside {
					if result.Context.Directory != "" || result.Context.ExecutionDirectory != "/" {
						t.Fatal(result.Context, string(output))
					}
				} else if result.Context.Directory != "/folder" || result.Context.ExecutionDirectory != dir {
					t.Fatal(result.Context, string(output))
				}
			})
		}
	}
}
func TestQueuedLocalCommandSurvivesRestart(t *testing.T) {
	var m Manager
	m.Local().Queue(Command{Text: "first", Path: "/work"})
	if m.LocalExited() {
		t.Fatal("queued command was counted as running")
	}
	c := m.Local().TakeQueued()
	if c == nil || c.Text != "first" {
		t.Fatal("lost startup command")
	}
}

func TestFailedDirectoryChangeReportsActualContext(t *testing.T) {
	var m Manager
	s, f := remote(t)
	m.SetRemote(s, f)
	s.Feed(prompt(s, "/start", "/start"))
	m.PanelChanged(Context{Kind: "ssh", ID: "server-a", Directory: "/unavailable"})
	s.Dispatch()
	r := s.Feed(prompt(s, "/start", "/start")) // cd failed; cwd did not change.
	if !r.ContextReported {
		t.Fatal("unchanged actual cwd was not reported")
	}
	m.ShellChanged(r.Context)
	if m.Current().Directory != "/start" {
		t.Fatal("manager retained unconfirmed requested cwd")
	}
}
