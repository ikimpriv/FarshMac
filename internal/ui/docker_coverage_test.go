package ui

import (
	"farshmac/internal/docker"
	"strings"
	"testing"
)

func TestDockerPodToastCoverage(t *testing.T) {
	d := &dockerPanel{helpers: map[string]string{"api": "helper", "worker": "stopped-helper", "volume:data": "volume-helper"}, containers: []docker.Container{
		{ID: "api", Names: "api", State: "running", Labels: "com.docker.compose.project=demo"},
		{ID: "worker", Names: "worker", State: "running", Labels: "com.docker.compose.project=demo"},
		{ID: "single", Names: "single", State: "running"},
		{ID: "helper", Names: "toolbox", State: "running", Labels: "io.farshmac.podtoast.target=api"},
		{ID: "stopped-helper", State: "exited", Labels: "io.farshmac.podtoast.target=worker"},
		{ID: "volume-helper", State: "running", Labels: "io.farshmac.podtoast.volume=data"},
		{ID: "network-helper", State: "running", Labels: "io.farshmac.podtoast.network=single"},
	}}
	check := func(folder, name, marker string, managed, total int) {
		t.Helper()
		e := []Entry{dockerCatalogEntry(name, true), dockerCatalogEntry("..", true)}
		d.markPodToastEntries(folder, e)
		info := e[0].Info.(dockerCatalogInfo)
		if e[0].Name != name || info.Name() != name || info.toastMarker() != marker || info.toastCount != managed || info.toastTotal != total {
			t.Fatal(folder, info)
		}
		if e[1].Info.(dockerCatalogInfo).toastCount != 0 {
			t.Fatal("parent marked")
		}
	}
	check("/Compose", "demo", "◇", 1, 2)
	check("/Compose/demo", "api", "◆", 1, 1)
	check("/Compose/demo", "worker", " ", 0, 1)
	check("/Containers", "single", " ", 0, 1)
	check("/Volumes", "data", "◆", 1, 1)
	check("/Networks", "single", " ", 0, 0)
	d.containers[4].State = "running"
	check("/Compose", "demo", "◆", 2, 2)
	d.containers[0].State = "exited"
	check("/Compose/demo", "api", " ", 0, 1)
	d.containers[5].State = "exited"
	check("/Volumes", "data", " ", 0, 0)
}
func TestDockerPodToastMarkerDrawAndSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	p := &a.Panels[0]
	p.Path = "/Compose"
	p.Docker = &dockerPanel{}
	p.Entries = []Entry{{Name: "demo", Dir: true, Info: dockerCatalogInfo{name: "demo", dir: true, toastCount: 1, toastTotal: 2}}}
	p.Cursor = 0
	p.ItemCount = 1
	render := func() string {
		g := NewGrid(100, 20)
		a.drawPanel(g, 0, 0, 0, 100, 20)
		var b strings.Builder
		for _, cell := range g.Cells {
			b.WriteString(cell.Text)
		}
		return b.String()
	}
	if text := render(); !strings.Contains(text, "◇demo") || !strings.Contains(text, "PodToast 1/2 containers") {
		t.Fatal(text)
	}
	p.Marked = map[string]bool{"demo": true}
	if text := render(); !strings.Contains(text, "√demo") || !strings.Contains(text, "PodToast 1/2 containers") {
		t.Fatal(text)
	}
}
