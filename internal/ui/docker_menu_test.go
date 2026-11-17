package ui

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"farshmac/internal/docker"
	"farshmac/internal/fsops"
)

func TestDockerMenusByState(t *testing.T) {
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	p := &a.Panels[0]
	p.Path = "/Containers"
	p.Entries = []Entry{dockerCatalogEntry("demo", true)}
	p.Cursor = 0
	for _, state := range []string{"running", "paused", "exited", "created", "restarting", "dead", "removing"} {
		p.Docker = &dockerPanel{containers: []docker.Container{{ID: "id", Names: "demo", State: state}}}
		items := a.dockerContextItems(p)
		labels := map[string]bool{}
		for _, item := range items {
			labels[item.Label] = true
			if strings.Contains(item.Label, "details") || strings.Contains(item.Label, "Copy") || strings.Contains(item.Label, "Rename") {
				t.Fatal("duplicate standard action", item.Label)
			}
		}
		for _, verb := range []string{"start", "stop", "restart", "pause", "unpause"} {
			want := false
			for _, allowed := range dockerStateActions(state) {
				want = want || verb == allowed
			}
			if labels[dockerActionLabel(verb)+" container…"] != want {
				t.Fatal(state, verb, labels)
			}
		}
		if labels["Processes"] != (state == "running") {
			t.Fatal("process state", state)
		}
		if labels["PodToast diagnostics"] {
			t.Fatal("diagnostics without toolbox")
		}
	}
	for location, label := range map[string]string{"/Images": "Layer history", "/Volumes": "Back up volume…", "/Networks": "Connect container…"} {
		p.Path = location
		p.Entries = []Entry{dockerCatalogEntry("object", false)}
		found := false
		for _, item := range a.dockerContextItems(p) {
			found = found || item.Label == label
		}
		if !found {
			t.Fatal(location, label)
		}
	}
	p.Path = "/Networks"
	p.Entries = []Entry{dockerCatalogEntry("host", false)}
	if len(a.dockerContextItems(p)) != 1 {
		t.Fatal("host network offered connect")
	}
}
func TestDockerActionArguments(t *testing.T) {
	fields := []Field{{Value: "demo"}, {Value: "127.0.0.1:8080:80,9090:90"}, {Value: `["echo","$(touch /tmp/not-executed)"]`}, {Value: "no"}}
	args, err := dockerRunArgs("image:id", fields)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"run", "--detach", "--restart", "no", "--name", "demo", "--publish", "127.0.0.1:8080:80", "--publish", "9090:90", "image:id", "echo", "$(touch /tmp/not-executed)"}
	if !reflect.DeepEqual(args, want) {
		t.Fatal(args)
	}
	fields[2].Value = "echo test"
	if _, err = dockerRunArgs("id", fields); err == nil {
		t.Fatal("invalid command accepted")
	}
	for _, cpu := range []string{"NaN", "Inf", "-1", "oops"} {
		if _, err = dockerUpdateArgs("id", cpu, "", "Keep unchanged"); err == nil {
			t.Fatal(cpu)
		}
	}
	args, err = dockerUpdateArgs("id", "0.5", "256m", "unless-stopped")
	if err != nil || !reflect.DeepEqual(args, []string{"update", "--cpus", "0.5", "--memory", "256m", "--restart", "unless-stopped", "id"}) {
		t.Fatal(args, err)
	}
}
func TestDockerActionRequiresConfirmation(t *testing.T) {
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Panels[0].Docker = &dockerPanel{client: docker.Client{Binary: "must-not-run"}}
	a.dockerConfirm(&a.Panels[0], "Stop demo", []string{"stop", "demo"})
	if a.Form == nil || !a.Form.Buttons || a.Busy {
		t.Fatal("action did not wait for confirmation")
	}
	a.formKey(Message{Key: "Escape"})
	if a.Busy {
		t.Fatal("cancel executed command")
	}
}
func TestDockerExportFailureAndCollision(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "docker")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf partial\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	client := docker.Client{Binary: binary}
	dest := filepath.Join(dir, "output.tar")
	op := fsops.NewOperation("test")
	defer op.Cancel()
	if err := dockerExportArchive(client, op, dest, []string{"image", "save", "id"}, "", ""); err == nil {
		t.Fatal("failure hidden")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("partial output published")
	}
	if err := os.WriteFile(dest, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := dockerExportArchive(client, op, dest, nil, "", ""); err == nil {
		t.Fatal("collision accepted")
	}
	b, _ := os.ReadFile(dest)
	if string(b) != "keep" {
		t.Fatal("existing file changed")
	}
	temps, _ := filepath.Glob(filepath.Join(dir, ".farshmac-export-*"))
	if len(temps) != 0 {
		t.Fatal("temporary files leaked", temps)
	}
}
func TestDockerMenuLiveOperations(t *testing.T) {
	image := os.Getenv("FARSHMAC_DOCKER_TEST_IMAGE")
	if image == "" {
		t.Skip("opt-in disposable Docker test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client := docker.Client{}
	run := func(args ...string) string {
		t.Helper()
		b, err := client.Run(ctx, args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(b))
	}
	network := run("network", "create", "farshmac-menu-"+strings.ToLower(rand.Text()))
	defer client.Run(context.Background(), "network", "rm", network)
	volume := run("volume", "create")
	defer client.Run(context.Background(), "volume", "rm", volume)
	id := run("run", "-d", "--entrypoint", "/bin/sh", "--mount", "type=volume,source="+volume+",target=/fixture", image, "-c", "echo archive-content > /fixture/check; sleep 300")
	defer client.Run(context.Background(), "rm", "--force", id)
	for _, args := range [][]string{{"pause", id}, {"unpause", id}, {"update", "--cpus", "0.5", "--restart", "unless-stopped", id}, {"network", "connect", network, id}} {
		run(args...)
	}
	members, err := dockerNetworkMembers(ctx, client, network)
	if err != nil || len(members) != 1 {
		t.Fatal(members, err)
	}
	run("network", "disconnect", network, id)
	op := fsops.NewOperation("archive")
	defer op.Cancel()
	dest := filepath.Join(t.TempDir(), "volume.tar.gz")
	if err = dockerExportArchive(client, op, dest, nil, volume, image); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := false
	for {
		h, e := tr.Next()
		if e != nil {
			break
		}
		if strings.HasSuffix(h.Name, "/check") || h.Name == "check" {
			found = true
		}
	}
	if !found {
		t.Fatal("volume archive missing fixture")
	}
	for _, args := range [][]string{{"stop", "--time", "1", id}, {"start", id}, {"restart", "--time", "1", id}, {"kill", "--signal", "TERM", id}} {
		run(args...)
	}
}

func TestDockerComposeStateScope(t *testing.T) {
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	p := &a.Panels[0]
	p.Docker = &dockerPanel{containers: []docker.Container{
		{ID: "running", Names: "api", State: "running", Labels: "com.docker.compose.project=demo"},
		{ID: "paused", Names: "worker", State: "paused", Labels: "com.docker.compose.project=demo"},
		{ID: "unrelated", Names: "other", State: "running", Labels: "com.docker.compose.project=elsewhere"},
	}}
	items := a.dockerComposeItems(p, "demo")
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.Label] = true
		if item.Label == "Stop services…" {
			item.Run()
			if a.Form == nil || !strings.Contains(a.Form.Message, "2 services in demo") {
				t.Fatal("wrong project scope", a.Form)
			}
		}
	}
	if seen["Start services…"] || !seen["Unpause services…"] || !seen["Pause services…"] {
		t.Fatal(seen)
	}
}
