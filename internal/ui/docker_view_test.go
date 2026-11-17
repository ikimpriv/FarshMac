package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"farshmac/internal/docker"
)

func TestDockerObjectViews(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "docker")
	script := "#!/bin/sh\nprintf '%s' '[{\"Name\":\"demo\",\"Id\":\"abc\",\"State\":{\"Status\":\"running\"},\"Mounts\":[{\"Destination\":\"/data\",\"Type\":\"volume\"}],\"NetworkSettings\":{\"Ports\":{\"80/tcp\":[{\"HostPort\":\"8080\"}]}},\"Size\":1048576}]'\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	d := &dockerPanel{client: docker.Client{Binary: binary, Context: "fixture"}, containers: []docker.Container{{ID: "abc", Names: "demo"}}}
	for _, target := range []string{"/Containers/demo", "/Volumes/data", "/Networks/bridge", "/Images/abc"} {
		text, err := d.describe(context.Background(), target)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Read-only Docker object", "Identity", "State", "Storage", "Network", "8080", "1048576 bytes"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s: missing %s in %s", target, want, text)
			}
		}
	}
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	p := &a.Panels[0]
	p.Docker = d
	p.Path = "/Containers"
	p.Entries = []Entry{dockerCatalogEntry("demo", true)}
	p.Cursor = 0
	a.function(3)
	waitNetwork(t, a)
	if a.Doc == nil || !a.Doc.ReadOnly {
		t.Fatal("F3 did not open a read-only report")
	}
	a.Doc = nil
	a.function(4)
	waitNetwork(t, a)
	if a.Doc == nil || !a.Doc.ReadOnly {
		t.Fatal("F4 must not edit inspect as a manifest")
	}
	a.Doc = nil
	a.updateDockerPreview(p)
	deadline := time.Now().Add(3 * time.Second)
	for a.previewPending && time.Now().Before(deadline) {
		select {
		case job := <-a.jobs:
			job()
		case <-time.After(time.Millisecond):
		}
	}
	if a.previewPending || !strings.Contains(a.previewText, "8080") {
		t.Fatal("Quick view failed", a.previewText)
	}
}

func TestDockerInspectMalformedResponse(t *testing.T) {
	if _, err := dockerInspectReport("test", "fixture", []byte("not json")); err == nil {
		t.Fatal("malformed inspect accepted")
	}
}
