package ui

import (
	"context"
	"encoding/json"
	"farshmac/internal/podtoast"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"farshmac/internal/kube"
)

func TestEphemeralContextMenuIsExplicit(t *testing.T) {
	a, api := kubeApp(t)
	podWithContainers(api, "debuggable", "app")
	p := &a.Panels[0]
	p.Kube.invalidate("")
	a.error(p.Load("/shop/pods"))
	waitNetwork(t, a)
	selectEntry(t, p, "debuggable")
	a.key(Message{Key: "Enter", Alt: true})
	if a.Zoom || a.Form == nil || a.Form.Title != "Context menu" {
		t.Fatal("context menu", a.Form)
	}
	attach := slices.Index(a.Form.Choice, "Attach ephemeral container…")
	if attach < 0 {
		t.Fatal("missing attach action")
	}
	a.Form.Select(attach)
	if a.Form == nil || a.Form.Title != "Ephemeral container" {
		t.Fatal("image chooser")
	}
	a.Form.Select(len(a.Form.Choice) - 1)
	waitNetwork(t, a)
	if a.Form == nil || a.Form.Title != "Attach ephemeral container" || a.Busy || len(p.Kube.toasts) != 0 {
		t.Fatal("opening options must not deploy or integrate")
	}
	if a.Form.Fields[3].Value != "Default" || a.Form.Fields[5].Value != "IfNotPresent" {
		t.Fatal("unexpected defaults")
	}
	a.key(Message{Key: "Escape"})
	if a.Form != nil || a.Busy {
		t.Fatal("Esc must leave pod untouched")
	}
}
func TestEphemeralHistoryKeepsRoleAndOptions(t *testing.T) {
	a, _ := kubeApp(t)
	uid := int64(1000)
	preset := EphemeralPreset{Options: kube.EphemeralOptions{Image: "example/debug:1", Command: []string{"/bin/sh", "-l"}, Profile: "Processes", RunAsUser: &uid, TTY: true, PullPolicy: "Always", TimeoutSeconds: 45}}
	a.rememberEphemeral(preset)
	a.rememberEphemeral(preset)
	if len(a.Settings.EphemeralHistory) != 1 {
		t.Fatal("history must deduplicate identical presets")
	}
	data, err := os.ReadFile(setupPath())
	if err != nil {
		t.Fatal(err)
	}
	var saved savedSetup
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Settings.EphemeralHistory[0], preset) {
		t.Fatal("lost launch options", saved.Settings.EphemeralHistory)
	}
	integrated := preset
	integrated.PodToast = true
	a.rememberEphemeral(integrated)
	if len(a.Settings.EphemeralHistory) != 2 {
		t.Fatal("image name must not determine integration role")
	}
}

func TestAddingEphemeralKeepsBothContainerFolders(t *testing.T) {
	a, api := kubeApp(t)
	podWithContainers(api, "changing", "app", "debug")
	p := &a.Panels[0]
	k := p.Kube
	k.invalidate("")
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "work"), 0755); err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	go podtoast.Serve(right, &podtoast.Server{Root: root})
	client, err := podtoast.Connect(context.Background(), left)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ref := kube.ContainerRef{Namespace: "shop", Pod: "changing", Container: "app"}
	k.toasts = map[string]*podtoast.Client{toastKey(ref): client}
	// Cached layout before the debugger was added: direct pod -> filesystem.
	k.pods["shop/changing"] = kube.PodSummary{Containers: []kube.ContainerState{{Name: "app"}}}
	a.error(p.Load("/shop/pods/changing/work"))
	waitNetwork(t, a)
	if p.Err != "" {
		t.Fatal(p.Err)
	}
	a.matchOtherPanel()
	waitNetwork(t, a)
	a.refreshEphemeralPod(k, ref, true)
	waitNetwork(t, a)
	for i := range a.Panels {
		p := &a.Panels[i]
		if p.Path != "/shop/pods/changing/app/work" || p.Err != "" {
			t.Fatalf("panel %d: %s %s", i, p.Path, p.Err)
		}
	}
}

func TestContextKeysAndPodToastNewFile(t *testing.T) {
	a, api := kubeApp(t)
	podWithContainers(api, "editable", "app")
	p := &a.Panels[0]
	k := p.Kube
	k.invalidate("")
	root := t.TempDir()
	left, right := net.Pipe()
	go podtoast.Serve(right, &podtoast.Server{Root: root})
	client, err := podtoast.Connect(context.Background(), left)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ref := kube.ContainerRef{Namespace: "shop", Pod: "editable", Container: "app"}
	k.toasts = map[string]*podtoast.Client{toastKey(ref): client}
	a.error(p.Load("/shop/pods/editable"))
	waitNetwork(t, a)
	for _, key := range []Message{{Key: "Enter", Alt: true}, {Key: "F11"}} {
		a.key(key)
		if a.Form == nil || a.Form.Title != "Context menu" {
			t.Fatal("context hotkey", key, a.Status)
		}
		for _, item := range a.Form.Choice {
			if item == "Copy / export…" || item == "Move resources…" || item == "Describe" || item == "Refresh snapshot" {
				t.Fatal("ordinary action", item)
			}
		}
		a.key(Message{Key: "Escape"})
	}
	a.key(Message{Key: "F4", Shift: true})
	if a.Dialog == nil {
		t.Fatal("no new file dialog", a.Status)
	}
	a.Dialog.Input = "created.txt"
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if a.Doc == nil || a.Doc.Source == nil {
		t.Fatal("new container editor", a.Status)
	}
	a.Doc.Insert("hello pod")
	a.Doc.modified()
	a.save()
	waitNetwork(t, a)
	b, err := os.ReadFile(filepath.Join(root, "created.txt"))
	if err != nil || string(b) != "hello pod" || a.Doc.Dirty {
		t.Fatal(string(b), err, a.Status)
	}
}
