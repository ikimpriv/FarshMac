package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRequestErrorMessages(t *testing.T) {
	huge := strings.Repeat("x", 1024*1024+1)
	for _, c := range []struct{ cwd, command, want string }{
		{"/tmp", huge, "command exceeds 1 MiB"},
		{"/" + huge, "", "directory path exceeds 1 MiB"},
		{"relative", "ls", "directory must be absolute"},
		{"/tmp", "a\x00b", "command contains NUL"},
		{"/tmp\x00x", "ls", "directory contains NUL"},
	} {
		err := requestError(c.cwd, c.command)
		if err == nil || err.Error() != c.want {
			t.Fatalf("%q: got %v, want %s", c.want, err, c.want)
		}
	}
	if err := requestError("/tmp", "ls"); err != nil {
		t.Fatal(err)
	}
}

func TestStaleRequestFileIsReplaced(t *testing.T) {
	s := &Session{dir: t.TempDir(), input: make(chan []byte, 4), done: make(chan struct{})}
	stale := filepath.Join(s.dir, "panel-command")
	if err := os.WriteFile(stale, []byte("old\x00old\x00run\x00"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.request("/tmp", "ls", "run", "KEY"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(stale)
	if string(b) != "/tmp\x00ls\x00run\x00" || string(<-s.input) != "KEY" {
		t.Fatalf("stale request not replaced: %q", b)
	}
	s.CancelRequest()
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("cancel kept request file", err)
	}
}

func TestLostIntegrationKeyRecovers(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("ENV", "")
	s, err := StartConfigured(root, 80, 24, "bash", false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAndWait(s)
	waitReady(t, s)
	// Simulate a startup file or plugin removing the integration widget.
	if err = s.Write("bind -r '\\C-x\\C-g'\r"); err != nil {
		t.Fatal(err)
	}
	waitReady(t, s)
	if err = s.Run(root, "true"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err = os.Stat(filepath.Join(s.dir, "panel-command")); err != nil {
		t.Fatal("lost key must leave the request file for this test:", err)
	}
	if err = s.Run(root, "true"); err != nil {
		t.Fatal("stale request must be replaced:", err)
	}
}
