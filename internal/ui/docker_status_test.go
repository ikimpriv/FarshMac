package ui

import (
	"farshmac/internal/docker"
	"strings"
	"testing"
	"time"
)

func TestDockerContainerStateAgeAndColumns(t *testing.T) {
	created := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	stamp := created.Format("2006-01-02 15:04:05 -0700 MST")
	d := &dockerPanel{containers: []docker.Container{{ID: "api", Names: "api", State: "running", Status: "Up 10 minutes (healthy)", CreatedAt: stamp, Labels: "com.docker.compose.project=demo"}, {ID: "helper", State: "running", Labels: "io.farshmac.podtoast.target=api"}}, helpers: map[string]string{"api": "helper"}}
	entries := []Entry{dockerCatalogEntry("api", true)}
	d.markPodToastEntries("/Compose/demo", entries)
	info := entries[0].Info.(dockerCatalogInfo)
	if info.containerAge() != "3h" || info.containerStatus() != "Running (healthy)" || !info.ModTime().Equal(created) || info.toastMarker() != "◆" {
		t.Fatal(info)
	}
	t.Setenv("HOME", t.TempDir())
	a, e := New(t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	p := &a.Panels[0]
	p.Docker = d
	p.Path = "/Compose/demo"
	p.Entries = entries
	p.Cursor = 0
	for _, width := range []int{32, 60, 120} {
		g := NewGrid(width, 20)
		a.drawPanel(g, 0, 0, 0, width, 20)
		var b strings.Builder
		for _, cell := range g.Cells {
			b.WriteString(cell.Text)
		}
		text := b.String()
		if !strings.Contains(text, "Status") || !strings.Contains(text, "Age") || !strings.Contains(text, "3h") || !strings.Contains(text, "◆api") {
			t.Fatal(width, text)
		}
	}
	p.Path = "/Compose/demo/api/tmp"
	if p.dockerContainerCatalog() {
		t.Fatal("container files got catalog columns")
	}
}
func TestDockerContainerStateColors(t *testing.T) {
	for _, tc := range []struct {
		state, status string
		color         uint32
	}{{"running", "Up 5 minutes", White}, {"running", "Up 1 minute (unhealthy)", 0xff5555}, {"paused", "Up 1 hour (Paused)", Yellow}, {"exited", "Exited (0) 1 hour ago", 0x888899}, {"exited", "Exited (137) 1 minute ago", 0xff5555}, {"dead", "Dead", 0xff5555}, {"restarting", "Restarting (1) 2 seconds ago", 0xff5555}} {
		info := dockerCatalogInfo{state: tc.state, status: tc.status}
		if info.stateColor() != tc.color {
			t.Fatal(tc, info.stateColor())
		}
	}
	if !(dockerCatalogInfo{}).created.IsZero() || (dockerCatalogInfo{}).containerAge() != "—" {
		t.Fatal("missing age fabricated")
	}
}
