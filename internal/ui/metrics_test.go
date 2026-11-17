package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"farshmac/internal/metrics"
)

func TestDetachedMetricsSurvivesParentClose(t *testing.T) {
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	s := metrics.NewSession("test pod", []metrics.Target{{ID: "metrics", Name: "metrics", Fetch: func(context.Context) ([]byte, error) { return []byte("# TYPE cpu counter\ncpu 1\n"), nil }}}, nil, func() { close(stopped) })
	a.Metrics = s
	g := NewWindowGroup(a)
	g.StartShell = false
	a.handleMetrics(Message{Type: "metrics", Key: "detach"})
	id, err := g.NewWindow(1)
	if err != nil {
		t.Fatal(err)
	}
	if a.Metrics != nil || g.Apps[id].Metrics != s || !g.Apps[id].MetricsOnly || g.Apps[id].Term != nil {
		t.Fatal("session was not transferred")
	}
	if len(g.sessionSnapshot().Windows) != 1 {
		t.Fatal("metrics window must not become a saved file panel")
	}
	g.CloseWindow(1)
	select {
	case <-stopped:
		t.Fatal("parent closed metrics collector")
	default:
	}
	u := g.Poll(id)
	if u.Mode != "metrics" || u.Metrics == nil || !u.Metrics.Detached {
		t.Fatal(u.Mode, u.Metrics)
	}
	g.CloseWindow(id)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("closing metrics window leaked collector")
	}
}

func TestMetricsSelectionUsesMarkedPods(t *testing.T) {
	a, api := kubeApp(t)
	podWithContainers(api, "first", "app")
	podWithContainers(api, "second", "app")
	p := &a.Panels[0]
	p.Kube.invalidate("")
	a.error(p.Load("/shop/pods"))
	waitNetwork(t, a)
	p.Marked = map[string]bool{"first": true, "second": true}
	selectEntry(t, p, "..")
	refs := metricSelection(p)
	if len(refs) != 2 || refs[0].Container != "" || refs[1].Container != "" {
		t.Fatal(refs)
	}
	a.panelContextMenu()
	if a.Form == nil || a.Form.Choice[0] != "Live metrics…" {
		t.Fatal("marked pods should work even with cursor on parent")
	}
}
func TestCompactMetricsControlsAndCancellation(t *testing.T) {
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	stopped := make(chan struct{})
	session := metrics.NewSession("pod/app", []metrics.Target{{ID: "one", Fetch: func(context.Context) ([]byte, error) {
		return []byte("# TYPE podtoast_memory_usage_bytes gauge\npodtoast_memory_usage_bytes 1048576\n"), nil
	}}}, nil, func() { close(stopped) })
	a.startCompactMetrics(session, "pod/app")
	if a.Foreign != nil || a.TermVisible || a.Panels[1].View != "Quick view" || a.Active != 0 {
		t.Fatal("compact metrics must occupy the neighboring panel")
	}
	deadline := time.Now().Add(time.Second)
	for a.compactMetrics.packet == nil && time.Now().Before(deadline) {
		a.pollCompactMetrics()
		time.Sleep(time.Millisecond)
	}
	text := a.Draw().ANSI()
	if !strings.Contains(text, "Quick Live Metrics") {
		t.Fatal("compact panel not drawn")
	}
	a.Active = 1
	a.compactMetricsKey(Message{Key: "1"})
	if a.compactMetrics.enabled[0] {
		t.Fatal("CPU toggle failed")
	}
	a.compactMetricsKey(Message{Key: "Escape"})
	if a.compactMetrics != nil || a.Panels[1].View == "Quick view" {
		t.Fatal("previous file panel not restored")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("compact collector leaked")
	}
}
