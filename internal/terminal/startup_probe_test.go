package terminal

import (
	"os"
	"testing"
	"time"
)

func TestLiveStartupProbe(t *testing.T) {
	if os.Getenv("FARSHMAC_PROBE_STARTUP") == "" {
		t.Skip("opt-in local startup timing")
	}
	home, _ := os.UserHomeDir()
	started := time.Now()
	s, err := StartConfigured(home, 100, 30, "bash", os.Getenv("FARSHMAC_PROBE_CUSTOM_PROMPT") != "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e := <-s.Events:
			if e.Ready {
				t.Logf("bash first ready marker: %s", time.Since(started))
				if err := s.Run(home, ":"); err != nil {
					t.Fatal(err)
				}
				waitReady(t, s)
				t.Logf("bash accepting commands: %s", time.Since(started))
				return
			}
			if e.Exit {
				t.Fatal("shell exited before first prompt")
			}
		case <-timer.C:
			t.Fatal("shell startup exceeds 20 seconds")
		}
	}
}
