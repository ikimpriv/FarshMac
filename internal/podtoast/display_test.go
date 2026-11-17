package podtoast

import (
	"strings"
	"testing"
	"time"
)

func TestProcessCPUIsRateNotLifetime(t *testing.T) {
	at := time.Unix(100, 0)
	f := MonitorFormatter{}
	snapshot := func(seconds, start float64, at time.Time) Snapshot {
		return Snapshot{At: at, Processes: Section{Value: map[string]any{"items": []any{map[string]any{"pid": float64(42), "parent": float64(1), "name": "mediamtx", "cpuSeconds": seconds, "startTicks": start}}}}}
	}
	first := f.Format(snapshot(10000, 1, at))
	if !strings.Contains(first, "TIME(s)") || !strings.Contains(first, "10000.00") {
		t.Fatal(first)
	}
	second := f.Format(snapshot(10003, 1, at.Add(2*time.Second)))
	if !strings.Contains(second, "150.0") || !strings.Contains(second, "10003.00") {
		t.Fatal(second)
	}
	reused := f.Format(snapshot(1, 2, at.Add(4*time.Second)))
	if strings.Contains(reused, "150.0") {
		t.Fatal("reused PID inherited rate", reused)
	}
	if len(f.processes) != 1 {
		t.Fatal("unbounded process history")
	}
}
