package ui

import (
	"context"
	"farshmac/internal/kube"
	"farshmac/internal/kube/kubetest"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMatchKubePanelAndWatchDeleteLastObject(t *testing.T) {
	a, f := kubeApp(t)
	enterKube(t, a, "shop")
	enterKube(t, a, "widgets")
	selectEntry(t, &a.Panels[0], "w1")
	a.key(Message{Key: "=", Meta: true})
	waitNetwork(t, a)
	k := a.Panels[0].Kube
	if a.Panels[1].Kube != k || a.Panels[1].Path != a.Panels[0].Path {
		t.Fatal("cluster location not matched")
	}
	for i := range a.Panels {
		a.Panels[i].kubeWatchRevision = k.changed.Load()
	}
	before := k.deleted.Load()
	waitKubeWatch(t, f, "/apis/example.com/v1/namespaces/shop/widgets")
	f.Emit("/apis/example.com/v1/namespaces/shop/widgets", "DELETED", kubetest.Obj("Widget", "shop", "w1", "widget-1", nil, nil, nil))
	deadline := time.Now().Add(3 * time.Second)
	for k.deleted.Load() == before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if k.deleted.Load() == before {
		t.Fatal("watch did not confirm deletion")
	}
	for i := range a.Panels {
		a.Panels[i].kubeWatchUpdated = time.Now()
	}
	a.pollKubeWatches()
	for i := range a.Panels {
		p := &a.Panels[i]
		if strings.Contains(strings.Join(entryNames(p), ","), "w1") || p.Err != "" || p.load != nil {
			t.Fatal("deleted object still visible", i, entryNames(p), p.Err)
		}
	}
	a.disconnectRemote(0)
	waitNetwork(t, a)
	if a.Panels[1].Kube != k || !k.watching("shop") {
		t.Fatal("disconnect stopped the other panel's watcher")
	}
	a.Active = 0
	a.key(Message{Key: "=", Meta: true})
	waitNetwork(t, a)
	if a.Panels[1].Kube != nil || a.Panels[1].Path != a.Panels[0].Path {
		t.Fatal("local location retained a Kubernetes backend")
	}
}

type countingNoExec struct{ calls atomic.Int32 }

func (x *countingNoExec) Stream(context.Context, kube.ContainerRef, kube.ExecOptions) error {
	x.calls.Add(1)
	return fmt.Errorf("unexpected synchronous exec")
}

func TestSameSingleContainerPodNeverExecutesFromWatchPoll(t *testing.T) {
	a, f := kubeApp(t)
	podWithContainers(f, "solo", "app")
	k := a.Panels[0].Kube
	k.invalidate("")
	a.error(a.Panels[0].Load("/shop/pods"))
	waitNetwork(t, a)
	if _, err := k.podSummary(context.Background(), "shop", "solo"); err != nil {
		t.Fatal(err)
	}
	x := &countingNoExec{}
	k.executor = x
	if k.cachedFor("/shop/pods/solo") {
		t.Fatal("single-container filesystem root must require an async read")
	}
	for i := range a.Panels {
		a.Panels[i].Kube = k
		a.Panels[i].Path = "/shop/pods/solo"
		a.Panels[i].Async = true
	}
	for i := 0; i < 30; i++ {
		k.changed.Add(1)
		a.pollKubeWatches()
	}
	if err := a.Panels[0].loadKubeMode("/shop/pods/solo", true); err == nil {
		t.Fatal("cached-only read accepted a container filesystem")
	}
	if x.calls.Load() != 0 {
		t.Fatal("watch refresh ran container exec on UI thread", x.calls.Load())
	}
	// Explicit navigation is a cancellable worker, even with the pod cached.
	a.error(a.Panels[0].Load("/shop/pods/solo"))
	if a.Panels[0].load == nil {
		t.Fatal("container read was synchronous")
	}
	waitNetwork(t, a)
}

func TestMatchOtherPanelPreservesQuickView(t *testing.T) {
	a, _ := visibilityFixture(t)
	before := a.Panels[1].Path
	a.Panels[1].View = "Quick view"
	a.key(Message{Key: "=", Meta: true})
	if a.Panels[1].View != "Quick view" || a.Panels[1].Path != before {
		t.Fatal("quick view replaced")
	}
	a.Panels[1].View = "Full"
	a.Panels[0].View = "Quick view"
	a.key(Message{Key: "=", Meta: true})
	if a.Panels[1].Path != before {
		t.Fatal("quick view used as a filesystem location")
	}
}

// FakeAPI delivers events only to established streams; unlike a real API
// server it does not replay events from an earlier resourceVersion.
func waitKubeWatch(t *testing.T, f *kubetest.FakeAPI, collection string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for f.Watchers(collection) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if f.Watchers(collection) == 0 {
		t.Fatalf("watch did not subscribe: %s", collection)
	}
}
