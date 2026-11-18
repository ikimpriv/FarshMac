package ui

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"farshmac/internal/docker"
	"farshmac/internal/fsops"
	"farshmac/internal/kube"
)

func TestKubeContextMultipleTargets(t *testing.T) {
	a, api := kubeApp(t)
	podWithContainers(api, "first", "app", "sidecar")
	podWithContainers(api, "second", "app")
	p := &a.Panels[0]
	p.Kube.invalidate("")
	a.error(p.Load("/shop/pods"))
	waitNetwork(t, a)
	p.Marked = map[string]bool{"first": true, "second": true}
	p.Cursor = 0
	refs := kubeContextTargets(p)
	if len(refs) != 2 || refs[0].Pod != "first" || refs[1].Pod != "second" {
		t.Fatal(refs)
	}
	a.panelContextMenu()
	labels := strings.Join(a.Form.Choice, "\n")
	if strings.Contains(labels, "Open shell") || strings.Contains(labels, "Traffic monitoring") || !strings.Contains(labels, "Attach ephemeral") || !strings.Contains(labels, "version / update") {
		t.Fatal(labels)
	}
	a.Form = nil
	a.ephemeralForm(p, EphemeralPreset{Options: kube.EphemeralOptions{Profile: "Default", PullPolicy: "IfNotPresent", TimeoutSeconds: 90}})
	waitNetwork(t, a)
	if a.Form == nil || a.Form.Title != "Attach ephemeral containers" || len(a.Form.Fields) != 12 {
		t.Fatalf("%+v", a.Form)
	}
	for i, want := range []string{"shop/first/app", "shop/first/sidecar", "shop/second/app"} {
		if a.Form.Fields[9+i].Label != want {
			t.Fatal(a.Form.Fields)
		}
	}
	a.formKey(Message{Key: "Escape"})
	if a.Busy || len(p.Kube.toasts) != 0 {
		t.Fatal("opening/cancelling options changed cluster")
	}
	a.startPodToast(p)
	waitNetwork(t, a)
	if a.Form == nil || len(a.Form.Fields) != 6 || !strings.Contains(a.Form.Title, "selected targets") {
		t.Fatal(a.Form)
	}
	a.formKey(Message{Key: "Escape"})
	// One marked item must win over an unrelated cursor.
	p.Marked = map[string]bool{"second": true}
	selectEntry(t, p, "first")
	a.panelContextMenu()
	if p.Entries[p.Cursor].Name != "second" {
		t.Fatal("cursor used instead of mark")
	}
}

func TestDockerBatchMenus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, e := New(t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	p := &a.Panels[0]
	p.Path = "/Containers"
	p.Entries = []Entry{dockerCatalogEntry("..", true), dockerCatalogEntry("run", true), dockerCatalogEntry("off", true)}
	p.Marked = map[string]bool{"run": true, "off": true}
	p.Docker = &dockerPanel{containers: []docker.Container{{ID: "r", Names: "run", State: "running"}, {ID: "o", Names: "off", State: "exited"}}}
	var labels []string
	for _, item := range a.dockerContextItems(p) {
		labels = append(labels, item.Label)
		if strings.HasPrefix(item.Label, "Stop containers") {
			item.Run()
			if a.Form == nil || strings.Contains(a.Form.Message, "off") || !strings.Contains(a.Form.Message, "run") {
				t.Fatal(a.Form)
			}
			a.formKey(Message{Key: "Escape"})
		}
	}
	joined := strings.Join(labels, "\n")
	for _, want := range []string{"Start containers · 1/2", "Stop containers · 1/2", "Logs · 2", "Processes · 1", "Attach / repair PodToast toolbox · 1"} {
		if !strings.Contains(joined, want) {
			t.Fatal(want, joined)
		}
	}
	if strings.Contains(joined, "Traffic monitoring") {
		t.Fatal(joined)
	}
	for _, folder := range []string{"/Images", "/Networks", "/Volumes"} {
		p.Path = folder
		var labels []string
		for _, item := range a.dockerContextItems(p) {
			labels = append(labels, item.Label)
		}
		text := strings.Join(labels, "\n")
		if text == "" || strings.Contains(text, "Run container") || strings.Contains(text, "Tag image") || strings.Contains(text, "Attach volume toolbox") {
			t.Fatal(folder, text)
		}
	}
	p.Path = "/Containers/run/tmp"
	if items := a.dockerContextItems(p); len(items) != 0 {
		t.Fatal("file selection treated as containers", items)
	}
}

func TestContextBatchPartialFailureAndCancellation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "failure", true: "cancel"}[cancel], func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			a, e := New(t.TempDir(), t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defer a.Close()
			var calls []string
			jobs := []contextJob{
				{"first", func(o *fsops.Operation) ([]byte, error) {
					calls = append(calls, "first")
					return []byte("first result"), nil
				}},
				{"second", func(o *fsops.Operation) ([]byte, error) {
					calls = append(calls, "second")
					if cancel {
						o.Cancel()
						return nil, o.Check()
					}
					return nil, errors.New("permission denied")
				}},
				{"third", func(o *fsops.Operation) ([]byte, error) {
					calls = append(calls, "third")
					return []byte("third result"), nil
				}},
			}
			a.contextBatch(&a.Panels[0], "Batch", jobs, false)
			waitNetwork(t, a)
			if a.Doc == nil {
				t.Fatal("missing partial report", a.Status)
			}
			if !strings.Contains(a.Doc.Text, "first result") {
				t.Fatal(a.Doc.Text)
			}
			if cancel && !strings.Contains(a.Doc.Text, "Cancelled") {
				t.Fatal(a.Doc.Text)
			}
			if !cancel && (!strings.Contains(a.Doc.Text, "permission denied") || !strings.Contains(a.Doc.Text, "third result")) {
				t.Fatal(a.Doc.Text)
			}
			want := 3
			if cancel {
				want = 2
			}
			if len(calls) != want {
				t.Fatal(calls)
			}
		})
	}
}

func TestKubeQualifiedPodContextTargets(t *testing.T) {
	a, api := kubeApp(t)
	podWithContainers(api, "first", "app")
	podWithContainers(api, "second", "app")
	p := &a.Panels[0]
	p.Kube.invalidate("")
	a.error(p.Load("/shop/pods"))
	waitNetwork(t, a)
	// Node -> Pods uses qualified display names, but mutations need API names.
	for i := range p.Entries {
		if p.Entries[i].Name == "first" || p.Entries[i].Name == "second" {
			p.Entries[i].Name = "shop:" + p.Entries[i].Name
		}
	}
	p.Marked = map[string]bool{"shop:first": true, "shop:second": true}
	refs := kubeContextTargets(p)
	if len(refs) != 2 || refs[0].Namespace != "shop" || refs[0].Pod != "first" || refs[1].Pod != "second" {
		t.Fatal(refs)
	}
	if len(metricSelection(p)) != 2 {
		t.Fatal("qualified selection lost in metrics")
	}
}

func TestFileContextMenuRemainsBatchOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	p := &a.Panels[0]
	p.Entries = []Entry{dockerCatalogEntry("one", false), dockerCatalogEntry("two", false)}
	p.Marked = map[string]bool{"one": true, "two": true}
	a.panelContextMenu()
	want := []string{"Send by Email…", "Create archive…", "Open In…"}
	if a.Form == nil || !slices.Equal(a.Form.Choice, want) {
		t.Fatal(a.Form)
	}
}

func TestDockerBatchLogsUseAllMarkedIDs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, e := New(t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	binary := filepath.Join(t.TempDir(), "docker")
	if e = os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0700); e != nil {
		t.Fatal(e)
	}
	p := &a.Panels[0]
	p.Path = "/Containers"
	p.Entries = []Entry{dockerCatalogEntry("cursor", true), dockerCatalogEntry("first", true), dockerCatalogEntry("second", true)}
	p.Marked = map[string]bool{"first": true, "second": true}
	p.Docker = &dockerPanel{client: docker.Client{Binary: binary}, containers: []docker.Container{{ID: "id-cursor", Names: "cursor"}, {ID: "id-first", Names: "first"}, {ID: "id-second", Names: "second"}}}
	for _, item := range a.dockerContextItems(p) {
		if strings.HasPrefix(item.Label, "Logs ·") {
			item.Run()
			waitNetwork(t, a)
			break
		}
	}
	if a.Doc == nil || !strings.Contains(a.Doc.Text, "id-first") || !strings.Contains(a.Doc.Text, "id-second") || strings.Contains(a.Doc.Text, "id-cursor") {
		t.Fatal("wrong command scope", a.Doc)
	}
}

func TestEphemeralBatchRejectsReplacedPodAndContinues(t *testing.T) {
	a, api := kubeApp(t)
	podWithContainers(api, "first", "app")
	podWithContainers(api, "second", "app")
	p := &a.Panels[0]
	a.runEphemeralBatch(p, p.Kube, EphemeralPreset{PodToast: true}, []ephemeralBatchTarget{
		{Ref: kube.ContainerRef{Namespace: "shop", Pod: "first", Container: "app"}, UID: "old-first"},
		{Ref: kube.ContainerRef{Namespace: "shop", Pod: "second", Container: "app"}, UID: "old-second"},
	})
	waitNetwork(t, a)
	if a.Doc == nil || strings.Count(a.Doc.Text, "pod was replaced") != 2 || len(p.Kube.toasts) != 0 {
		t.Fatal("replacement protection/report", a.Doc)
	}
}
