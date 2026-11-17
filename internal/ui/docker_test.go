package ui

import (
	"context"
	"io"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"farshmac/internal/docker"
)

func TestDockerCatalogRoutesAndCapabilities(t *testing.T) {
	d := &dockerPanel{client: docker.Client{Context: "fixture"}, containers: []docker.Container{
		{ID: "one", Names: "api", State: "running", Labels: "com.docker.compose.project=project"},
		{ID: "two", Names: "worker", State: "running"},
	}}
	p := Panel{Docker: d, Path: "/"}
	if p.can(capCopyIn) || p.can(capEdit) || p.endpoint().Opener != nil {
		t.Fatal("catalog offered local filesystem operations")
	}
	p.Path = "/Compose/project/api/tmp"
	c, prefix, cwd, ok := d.location(p.Path)
	if !ok || c.ID != "one" || prefix != "/Compose/project/api" || cwd != "/tmp" {
		t.Fatal(c, prefix, cwd, ok)
	}
	if !p.can(capEdit) || p.endpoint().Opener == nil {
		t.Fatal("container not writable")
	}
	if _, _, _, ok := d.location("/Containers/api"); ok {
		t.Fatal("compose container leaked into standalone catalog")
	}
}
func TestDockerLiveFilesystemAndPodToast(t *testing.T) {
	image := os.Getenv("FARSHMAC_DOCKER_TEST_IMAGE")
	if image == "" {
		t.Skip("set FARSHMAC_DOCKER_TEST_IMAGE for disposable Docker integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := docker.Client{}
	data, err := client.Run(ctx, "run", "-d", "--network=none", "--entrypoint=/bin/sh", image, "-c", "sleep 300")
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(string(data))
	defer client.Run(context.Background(), "rm", "-f", id)
	d := &dockerPanel{client: client, helpers: map[string]string{}}
	if err = d.refresh(ctx); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	name := ""
	for _, c := range d.containers {
		if c.ID == id {
			name = c.Names
		}
	}
	d.mu.Unlock()
	if name == "" {
		t.Fatal("container absent")
	}
	prefix := "/Containers/" + name
	opener := &dockerOpener{d: d, id: id, prefix: prefix}
	f, err := opener.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	file := path.Join(prefix, "tmp", "farshmac-docker-test")
	w, err := f.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte("round trip")); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Chmod(file, 0640); err != nil {
		t.Fatal(err)
	}
	if err = opener.CopyWithin(ctx, file, opener, file+"-copy", ""); err != nil {
		t.Fatal(err)
	}
	r, err := f.Open(file + "-copy")
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(b) != "round trip" {
		t.Fatal(string(b), err)
	}
	f.Close()
	helper, err := client.Run(ctx, "run", "-d", "--pid=container:"+id, "--network=container:"+id, "--cap-add=SYS_PTRACE", image, "idle")
	if err != nil {
		t.Fatal(err)
	}
	helperID := strings.TrimSpace(string(helper))
	defer client.Run(context.Background(), "rm", "-f", helperID)
	d.helpers[id] = helperID
	f, err = opener.Open(ctx)
	if err != nil {
		t.Fatal("PodToast target FS", err)
	}
	defer f.Close()
	r, err = f.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	b, err = io.ReadAll(r)
	r.Close()
	if err != nil || string(b) != "round trip" {
		t.Fatal("agent wrong target", string(b), err)
	}
	if err = f.Rename(file, file+"-renamed"); err != nil {
		t.Fatal(err)
	}
	if err = f.Remove(file+"-renamed", false); err != nil {
		t.Fatal(err)
	}
}

func TestDockerLiveDistrolessAndVolume(t *testing.T) {
	image := os.Getenv("FARSHMAC_DOCKER_TEST_IMAGE")
	if image == "" {
		t.Skip("set FARSHMAC_DOCKER_TEST_IMAGE")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client := docker.Client{}
	run := func(args ...string) string {
		t.Helper()
		b, e := client.Run(ctx, args...)
		if e != nil {
			t.Fatal(e)
		}
		return strings.TrimSpace(string(b))
	}
	source := run("run", "-d", "--network=none", image, "idle")
	defer client.Run(context.Background(), "rm", "-f", source)
	root := t.TempDir()
	run("cp", source+":/podtoast", path.Join(root, "podtoast"))
	if err := os.WriteFile(path.Join(root, "Dockerfile"), []byte("FROM scratch\nCOPY podtoast /app\nENTRYPOINT [\"/app\", \"idle\"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tag := "farshmac-test-distroless:" + source[:12]
	run("build", "--network=none", "-t", tag, root)
	defer client.Run(context.Background(), "image", "rm", "-f", tag)
	target := run("run", "-d", "--network=none", tag)
	defer client.Run(context.Background(), "rm", "-f", target)
	helper := run("run", "-d", "--network=container:"+target, "--pid=container:"+target, "--cap-add=SYS_PTRACE", image, "idle")
	defer client.Run(context.Background(), "rm", "-f", helper)
	d := &dockerPanel{client: client, helpers: map[string]string{target: helper}}
	opener := &dockerOpener{d: d, id: target, prefix: "/Containers/distroless"}
	f, err := opener.Open(ctx)
	if err != nil {
		t.Fatal("distroless PodToast", err)
	}
	entries, err := f.ReadDir("/Containers/distroless")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Name() == "app" {
			found = true
		}
	}
	if !found {
		t.Fatal("agent did not list distroless target root")
	}
	// Commands use the toolbox shell in the target folder, not a nonexistent target sh.
	if b, err := client.Run(ctx, "exec", helper, "/bin/sh", "-c", "cd /proc/1/root && test -f app && printf distroless-ok"); err != nil || string(b) != "distroless-ok" {
		t.Fatal(string(b), err)
	}
	volume := "farshmac-test-volume-" + source[:12]
	run("volume", "create", volume)
	defer client.Run(context.Background(), "volume", "rm", volume)
	volHelper := run("run", "-d", "--network=none", "--mount", "type=volume,source="+volume+",target=/volume", image, "idle")
	defer client.Run(context.Background(), "rm", "-f", volHelper)
	vol := &dockerOpener{d: d, id: volHelper, prefix: "/Volumes/" + volume}
	f, err = vol.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w, err := f.Create(vol.prefix + "/test")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("volume"))
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := f.Open(vol.prefix + "/test")
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(b) != "volume" {
		t.Fatal(string(b), err)
	}
}
