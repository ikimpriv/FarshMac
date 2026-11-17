package ui

import "testing"

func TestTrafficFormIsExplicitAndScoped(t *testing.T) {
	a, api := kubeApp(t)
	podWithContainers(api, "traffic", "app")
	p := &a.Panels[0]
	p.Kube.invalidate("")
	a.error(p.Load("/shop/pods"))
	waitNetwork(t, a)
	selectEntry(t, p, "traffic")
	a.trafficForm(p)
	if a.Form == nil || a.Busy || a.traffic != nil || len(a.Form.Fields) != 2 {
		t.Fatal("opening options started traffic", a.Form)
	}

	a.key(Message{Key: "Escape"})
	if a.Form != nil {
		t.Fatal("Esc did not dismiss options")
	}
}

func TestTrafficWindowOwnsSession(t *testing.T) {
	a, _ := kubeApp(t)
	m := &trafficMonitor{title: "ns/pod", updates: make(chan trafficUpdate, 1), changed: true}
	a.traffic = m
	a.detachTraffic = true
	g := NewWindowGroup(a)
	id, err := g.NewWindow(1)
	if err != nil {
		t.Fatal(err)
	}
	detached := g.Apps[id]
	if !detached.TrafficOnly || detached.traffic != m || a.traffic != nil || a.detachTraffic {
		t.Fatal("monitor not transferred")
	}
	u := detached.Poll()
	if u.Mode != "traffic" || u.Traffic == nil || u.Traffic.Title != "ns/pod" {
		t.Fatalf("wrong surface: %+v", u)
	}
	if detached.Poll().Traffic != nil {
		t.Fatal("unchanged packet sent on every frame")
	}
	detached.Handle(`{"Type":"key","Key":"Escape"}`)
	if !detached.Quit {
		t.Fatal("inactive traffic window did not close")
	}
	// This synthetic monitor has no transport; avoid App.Close dereferencing it.
	detached.traffic = nil
}
