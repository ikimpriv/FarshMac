package ui

import (
	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"strings"
	"testing"
)

func TestSearchOwnershipMergesMatchesAndAncestors(t *testing.T) {
	a, _ := kubeApp(t)
	source := a.Panels[0]
	source.Path = "/shop"
	o := fsops.NewOperation("search")
	defer o.Cancel()
	strategy, err := source.browse(o)
	if err != nil {
		t.Fatal(err)
	}
	defer strategy.Close()
	b := strategy.(*kubeBrowse)
	var hits []searchHit
	_, err = walkBrowse(source.Path, b, o, browseOptions{Recursive: true}, func(path string, e Entry) error {
		if info, ok := e.Info.(*kubeEntry); ok && info.object != nil {
			obj := info.object
			if obj.Name == "app-new-x" || obj.Name == "app-old-y" || obj.Name == "app" || obj.Name == "web" {
				hits = append(hits, searchHit{path, e})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, labels, limited, err := b.ownershipResults(hits)
	if err != nil || limited {
		t.Fatal(err, limited)
	}
	seen := map[types.UID]bool{}
	for _, row := range rows {
		obj := row.entry.Info.(*kubeEntry).object
		if seen[obj.UID] {
			t.Fatal("duplicate", obj.Name)
		}
		seen[obj.UID] = true
	}
	if len(rows) != 6 {
		t.Fatalf("want Deployment, 2 ReplicaSets, 2 Pods and Service: %v", labels)
	}
	joined := strings.Join(labels, "\n")
	for _, text := range []string{"Deployment app", "ReplicaSet app-new [owner]", "ReplicaSet app-old [owner]", "Pod app-new-x", "Pod app-old-y", "Service web"} {
		if !strings.Contains(joined, text) {
			t.Fatal("missing", text, joined)
		}
	}
	if strings.Contains(joined, "Deployment app [owner]") {
		t.Fatal("matched ancestor marked as context")
	}
}

func TestSearchOwnershipControllerAndCycle(t *testing.T) {
	makeObj := func(name string, owners ...types.UID) *kube.Object {
		return &kube.Object{UID: types.UID(name), Name: name, Namespace: "ns", Kind: "Widget", GVR: schema.GroupVersionResource{Group: "test", Version: "v1", Resource: "widgets"}, Owners: owners}
	}
	first, controller, child := makeObj("a"), makeObj("z"), makeObj("child", "a", "z")
	child.ControllerOwner = "z"
	o := fsops.NewOperation("test")
	defer o.Cancel()
	s := &kube.Snapshot{Objects: map[types.UID]*kube.Object{"a": first, "z": controller, "child": child}, ByFolder: map[string][]*kube.Object{"widgets": {first, controller, child}}}
	b := &kubeBrowse{o: o, snapshots: map[string]*kube.Snapshot{"ns": s}}
	hits := []searchHit{{path: "/ns/widgets/child", entry: kubeObjectEntry(child, false)}}
	rows, labels, _, err := b.ownershipResults(hits)
	if err != nil || len(rows) != 2 || !strings.Contains(labels[0], "Widget z [owner]") {
		t.Fatal(labels, err)
	}
	controller.Owners = []types.UID{"child"}
	rows, labels, _, err = b.ownershipResults(hits)
	if err != nil || len(rows) != 2 {
		t.Fatal("cycle duplicated or lost objects", labels, err)
	}
	delete(s.Objects, "z")
	delete(s.Objects, "a")
	rows, _, _, err = b.ownershipResults(hits)
	if err != nil || len(rows) != 1 {
		t.Fatal("missing owner hid match", err)
	}
	o.Cancel()
	if _, _, _, err = b.ownershipResults(hits); err == nil {
		t.Fatal("cancel ignored")
	}
}
