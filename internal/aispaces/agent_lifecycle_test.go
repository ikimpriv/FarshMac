package aispaces

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestAgentCompletionWaitsForInboxSave(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSpace("Space"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateEnv("Space", "Coder", []Agent{{Name: "claude"}}); err != nil {
		t.Fatal(err)
	}
	// More than a FIFO buffer: saving cannot finish until the test drains it.
	message := strings.Repeat("pending task\n", 64<<10)
	e := s.Spaces[0].Envs[0]
	p := &Process{
		store: s, env: e, name: "claude",
		todo: []delivery{{text: message, fresh: true}}, pending: 1,
		out: make(chan []byte), done: make(chan struct{}),
		closed: make(chan struct{}), ended: make(chan struct{}),
		more: make(chan struct{}, 1),
	}
	s.running[e.ID+":claude"] = p
	fifo := filepath.Join(s.root, ".catalog.tmp")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go p.pump()
	go func() {
		p.finish(nil)
		close(finished)
	}()
	// Opening the reader meets save's writer; the first byte confirms that
	// persistence has started, while the remaining bytes keep it blocked.
	reader, err := os.Open(fifo)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer func() {
		io.Copy(io.Discard, reader)
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("agent finalization did not finish after draining its save")
		}
	}()
	first := make([]byte, 1)
	if _, err := io.ReadFull(reader, first); err != nil {
		t.Fatal(err)
	}
	// Wake an otherwise idle output pump: it must not publish EOF either.
	p.more <- struct{}{}
	select {
	case <-p.ended:
		t.Error("agent ended before its pending inbox was saved")
	case <-p.Done():
		t.Error("console completed before its pending inbox was saved")
	case <-time.After(100 * time.Millisecond):
	}
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	for name, done := range map[string]<-chan struct{}{"agent": p.ended, "console": p.Done()} {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not finish after saving", name)
		}
	}
	var saved []*Space
	if err := json.Unmarshal(append(first, rest...), &saved); err != nil {
		t.Fatal(err)
	}
	a := saved[0].Envs[0].agent("claude")
	if len(a.Inbox) != 1 || a.Inbox[0] != message || !a.Fresh {
		t.Fatal("shutdown did not preserve the pending task and its fresh-context flag")
	}
}
