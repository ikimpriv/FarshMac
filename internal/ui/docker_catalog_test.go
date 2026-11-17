package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"farshmac/internal/docker"
)

func TestDockerCatalogAllSortsAndViews(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "docker-fixture")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	p := &a.Panels[0]
	p.Docker = &dockerPanel{client: docker.Client{Binary: binary}}
	for _, order := range []string{"Modified", "Name", "Size", "Created", "Accessed", "Owner", "Extension", "Unsorted"} {
		p.Sort = order
		if err := p.loadDocker("/"); err != nil {
			t.Fatal(err)
		}
		for _, view := range panelViews {
			p.View = view
			a.Draw()
		}
	}
}

func TestDockerContextConnectAndDraw(t *testing.T) {
	name := os.Getenv("FARSHMAC_DOCKER_CONTEXT")
	if name == "" {
		t.Skip("opt-in read-only Docker context test")
	}
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Panels[0].Sort = "Modified"
	a.connectDocker(0, name)
	waitNetwork(t, a)
	if a.Panels[0].Docker == nil || a.Panels[0].Err != "" {
		t.Fatal("Docker connection failed", a.Status, a.Panels[0].Err)
	}
	for _, location := range []string{"/", "/Compose", "/Containers", "/Volumes", "/Networks", "/Images"} {
		if err := a.Panels[0].Load(location); err != nil {
			t.Fatal(err)
		}
		waitNetwork(t, a)
		if a.Panels[0].Err != "" {
			t.Fatal(location, a.Panels[0].Err)
		}
		a.Draw()
		if location != "/" {
			for _, e := range a.Panels[0].Entries {
				if e.Name == ".." {
					continue
				}
				target := filepath.Join(location, e.Name)
				if _, err := a.Panels[0].Docker.describe(context.Background(), target); err != nil {
					t.Fatal(target, err)
				}
				break
			}
		}
	}
}
