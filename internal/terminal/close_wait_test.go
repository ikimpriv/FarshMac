package terminal

import (
	"testing"
	"time"
)

// closeAndWait closes the session and waits until the shell has exited, so a
// history file written on SIGHUP cannot race t.TempDir cleanup.
func closeAndWait(s *Session) {
	s.Close()
	select {
	case <-s.exited:
	case <-time.After(shellKillGrace + 3*time.Second):
	}
}

func TestCloseKillsShellThatSurvivesHangup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	s, err := StartConfigured(root, 80, 24, "bash", false)
	if err != nil {
		t.Fatal(err)
	}
	waitReady(t, s)
	if err = s.Write("trap '' HUP\r"); err != nil {
		t.Fatal(err)
	}
	waitReady(t, s)
	s.Close()
	select {
	case <-s.exited:
	case <-time.After(shellKillGrace + 3*time.Second):
		t.Fatal("shell left running after Close")
	}
}
