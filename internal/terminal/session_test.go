package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParserEveryByteBoundary(t *testing.T) {
	raw := "α\x1b[31mred\x1b[0m\x1b]777;token;busy;/tmp\aoutput\x1b]777;token;7;/tmp/a;b\aprompt"
	for split := 0; split <= len(raw); split++ {
		p := parser{prefix: []byte("\x1b]777;token;")}
		events := append(p.feed([]byte(raw[:split])), p.feed([]byte(raw[split:]))...)
		var out strings.Builder
		ready, running := 0, 0
		for _, e := range events {
			out.Write(e.Data)
			if e.Ready {
				ready++
				if e.Cwd != "/tmp/a;b" || e.Status != 7 {
					t.Fatalf("bad marker: %+v", e)
				}
			}
			if e.Running {
				running++
			}
		}
		if out.String() != "α\x1b[31mred\x1b[0moutputprompt" || ready != 1 || running != 1 {
			t.Fatalf("split %d: %q ready=%d running=%d", split, out.String(), ready, running)
		}
	}
}
func waitReady(t *testing.T, s *Session) (string, Event) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	var out strings.Builder
	for {
		select {
		case e := <-s.Events:
			out.Write(e.Data)
			if e.Exit {
				t.Fatalf("shell exited: %v output=%s", e.Err, out.String())
			}
			if e.Ready {
				return out.String(), e
			}
		case <-timer.C:
			t.Fatalf("shell timed out: %s", out.String())
		}
	}
}
func TestPTYInteractiveLifecycle(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ZDOTDIR", root)
	s, err := Start(root, 90, 24, "zsh")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	waitReady(t, s)
	if err = s.Write("test -t 0 && test -t 1 && export FARSHMAC_TEST_VALUE=alive; printf 'PTY_OK:%s\\n' $FARSHMAC_TEST_VALUE\r"); err != nil {
		t.Fatal(err)
	}
	out, _ := waitReady(t, s)
	if !strings.Contains(out, "PTY_OK:alive") {
		t.Fatalf("not a persistent PTY: %q", out)
	}
	s.Write("printf 'TERMINAL:%s:%s\\n' $TERM $COLORTERM; tput colors\r")
	out, _ = waitReady(t, s)
	if !strings.Contains(out, "TERMINAL:xterm-256color:truecolor") || !strings.Contains(out, "\r\n256\r\n") {
		t.Fatalf("terminal color capabilities: %q", out)
	}
	sub := filepath.Join(root, "folder ' with spaces")
	if err = os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err = s.Resize(101, 33); err != nil {
		t.Fatal(err)
	}
	s.Write("cd -- " + Quote(sub) + "; stty size; printf 'ENV:%s\\n' $FARSHMAC_TEST_VALUE\r")
	out, e := waitReady(t, s)
	resolved, _ := filepath.EvalSymlinks(sub)
	actual, _ := filepath.EvalSymlinks(e.Cwd)
	if actual != resolved || !strings.Contains(out, "33 101") || !strings.Contains(out, "ENV:alive") {
		t.Fatalf("cwd/resize/env: %q %+v", out, e)
	}
	s.Write("sh -c 'printf \"SLEEP_STARTED\\n\"; exec sleep 30'\r")
	deadline := time.After(5 * time.Second)
	running := false
	var sleepOutput strings.Builder
	for !running {
		select {
		case e := <-s.Events:
			sleepOutput.Write(e.Data)
			running = strings.Contains(sleepOutput.String(), "\r\nSLEEP_STARTED\r\n")
		case <-deadline:
			t.Fatal("no preexec marker")
		}
	}
	s.Write("\x03")
	_, e = waitReady(t, s)
	if e.Status == 0 {
		t.Fatal("Ctrl+C did not interrupt job")
	}
	// Real full-screen editor exercises alternate screen, raw mode and job control.
	file := filepath.Join(sub, "edited.txt")
	s.Write("vim -Nu NONE -n " + Quote(file) + "\r")
	time.Sleep(300 * time.Millisecond)
	s.Write("iPTY editor works\x1b:wq\r")
	waitReady(t, s)
	b, err := os.ReadFile(file)
	if err != nil || string(b) != "PTY editor works\n" {
		t.Fatalf("vim interaction: %q %v", b, err)
	}
}
