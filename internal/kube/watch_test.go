package kube

import (
	"context"
	"errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"strings"
	"testing"
	"time"

	"farshmac/internal/kube/kubetest"
)

func podsPath() string { return "/api/v1/namespaces/" + ns + "/pods" }

func nextEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("watch closed")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
	}
	return Event{}
}

func TestSnapshotResourceVersions(t *testing.T) {
	f := kubetest.New(t)
	kubetest.SeedShop(f)
	c := testClient(t, f)
	s, err := c.LoadSnapshot(context.Background(), ns, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.ResourceVersions["pods"] == "" || s.Resources["pods"].GVR != gvrPods || s.Resources["deployments.apps"].Kind != "Deployment" {
		t.Fatalf("resource versions %v resources %v", s.ResourceVersions, s.Resources)
	}
	if _, rv, err := c.NamespacesRV(context.Background()); err != nil || rv == "" {
		t.Fatal("namespaces rv", rv, err)
	}
}

func TestWatchFolderEvents(t *testing.T) {
	f := kubetest.New(t)
	kubetest.SeedShop(f)
	c := testClient(t, f)
	s, err := c.LoadSnapshot(context.Background(), ns, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, wait := c.WatchFolder(ctx, s.Resources["pods"], ns, s.ResourceVersions["pods"])
	deadline := time.Now().Add(5 * time.Second)
	for f.Watchers(podsPath()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	f.Emit(podsPath(), "ADDED", kubetest.Obj("Pod", ns, "fresh", "pod-fresh", []string{"rs-new"}, map[string]string{"app": "shop"}, map[string]string{"STATUS": "Pending", "READY": "0/1"}))
	ev := nextEvent(t, events)
	if ev.Type != Added || ev.Folder != "pods" || ev.Object.Name != "fresh" || ev.Object.UID != "pod-fresh" || ev.Object.ShortStatus() != "Pending" || ev.Object.Owners[0] != "rs-new" || ev.Object.Labels["app"] != "shop" || ev.Object.ResourceVersion == "" {
		t.Fatalf("added %+v", ev.Object)
	}
	// The second event has no columnDefinitions; the remembered ones apply.
	f.Emit(podsPath(), "MODIFIED", kubetest.Obj("Pod", ns, "fresh", "pod-fresh", []string{"rs-new"}, map[string]string{"app": "shop"}, map[string]string{"STATUS": "Running", "READY": "1/1"}))
	ev = nextEvent(t, events)
	if ev.Type != Modified || ev.Object.ShortStatus() != "Running" {
		t.Fatalf("modified %+v", ev.Object)
	}
	// Apply is copy-on-write.
	before := s
	after := s.Apply(ev)
	if _, ok := before.Lookup("pods", "fresh"); ok {
		t.Fatal("previous snapshot changed")
	}
	if o, ok := after.Lookup("pods", "fresh"); !ok || o.ShortStatus() != "Running" || after.Objects["pod-fresh"] != o || after.ResourceVersions["pods"] != o.ResourceVersion {
		t.Fatal("apply upsert", ok)
	}
	app, _ := after.Lookup("deployments.apps", "app")
	if got := strings.Join(names(after.Children(app)["pods"]), ","); got != "app-new-x,app-old-y,fresh,shared" {
		t.Fatal("tree sees the new pod", got)
	}
	f.Emit(podsPath(), "DELETED", kubetest.Obj("Pod", ns, "fresh", "pod-fresh", nil, nil, nil))
	ev = nextEvent(t, events)
	if ev.Type != Deleted {
		t.Fatal(ev.Type)
	}
	gone := after.Apply(ev)
	if _, ok := gone.Lookup("pods", "fresh"); ok || gone.Objects["pod-fresh"] != nil || len(gone.ByFolder["pods"]) != len(s.ByFolder["pods"]) {
		t.Fatal("apply delete")
	}
	if _, ok := after.Lookup("pods", "fresh"); !ok {
		t.Fatal("snapshot before the delete changed")
	}
	// A modified Service row without spec keeps the selector the list knew.
	svcPath := "/api/v1/namespaces/" + ns + "/services"
	svcEvents, _ := c.WatchFolder(ctx, s.Resources["services"], ns, s.ResourceVersions["services"])
	for f.Watchers(svcPath) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	f.Emit(svcPath, "MODIFIED", kubetest.Obj("Service", ns, "web", "svc-web", nil, nil, map[string]string{"TYPE": "NodePort"}))
	sev := nextEvent(t, svcEvents)
	kept := gone.Apply(sev)
	web, _ := kept.Lookup("services", "web")
	if web.ShortStatus() != "NodePort" || web.Selector["app"] != "shop" {
		t.Fatalf("selector kept %+v", web)
	}
	// Cancellation closes the channel without an error.
	cancel()
	if err := wait(); err != nil {
		t.Fatal("cancelled watch", err)
	}
	if _, ok := <-events; ok {
		t.Fatal("channel must be closed")
	}
}

func TestWatchPlainObjectsAndFolderRemoval(t *testing.T) {
	f := kubetest.New(t)
	kubetest.SeedShop(f)
	c := testClient(t, f)
	r := Resource{GVR: gvrPods, Kind: "Pod", Folder: "pods"}
	// A plain object event (server without Table support) decodes too.
	objects, rv, err := decodeWatchObject([]byte(`{"kind":"Pod","metadata":{"name":"p","uid":"u","resourceVersion":"9"},"status":{"phase":"Failed"}}`), r, ns, new([]metav1.TableColumnDefinition))
	if err != nil || len(objects) != 1 || objects[0].ShortStatus() != "Failed" || rv != "9" {
		t.Fatal(objects, rv, err)
	}
	s := &Snapshot{Namespace: ns, Objects: map[types.UID]*Object{}, ByFolder: map[string][]*Object{"pods": {objects[0]}}}
	empty := s.Apply(Event{Type: Deleted, Folder: "pods", Object: objects[0]})
	if _, ok := empty.ByFolder["pods"]; ok {
		t.Fatal("an emptied folder disappears")
	}
	_ = f
	_ = c
}

func TestWatchExpiredAndReconnect(t *testing.T) {
	f := kubetest.New(t)
	kubetest.SeedShop(f)
	c := testClient(t, f)
	r := Resource{GVR: gvrPods, Kind: "Pod", Folder: "pods"}
	f.ExpireWatch(podsPath())
	events, wait := c.WatchFolder(context.Background(), r, ns, "1")
	if _, ok := <-events; ok {
		t.Fatal("expired watch must not deliver events")
	}
	if err := wait(); !errors.Is(err, ErrWatchExpired) {
		t.Fatal("expected ErrWatchExpired, got", err)
	}
	// A dropped stream reconnects and keeps delivering.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, _ = c.WatchFolder(ctx, r, ns, "1")
	deadline := time.Now().Add(5 * time.Second)
	for f.Watchers(podsPath()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	f.DropWatches(podsPath())
	deadline = time.Now().Add(10 * time.Second)
	for f.Watchers(podsPath()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if f.Watchers(podsPath()) == 0 {
		t.Fatal("watch did not reconnect")
	}
	f.Emit(podsPath(), "ADDED", kubetest.Obj("Pod", ns, "after-drop", "pod-after", nil, nil, map[string]string{"STATUS": "Running"}))
	if ev := nextEvent(t, events); ev.Object.Name != "after-drop" || ev.Object.ShortStatus() != "Running" {
		t.Fatalf("event after reconnect %+v", ev.Object)
	}
}
