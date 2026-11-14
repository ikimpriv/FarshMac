package ui

import (
	"context"
	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"farshmac/internal/kube/localexec"
	"farshmac/internal/remotefs"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type internalOnlyExec struct {
	kube.Executor
	copies atomic.Int32
}

func (x *internalOnlyExec) Stream(ctx context.Context, ref kube.ContainerRef, opts kube.ExecOptions) error {
	script := strings.Join(opts.Command, " ")
	if strings.Contains(script, "cat -- ") {
		if !strings.Contains(script, " > ") {
			return fmt.Errorf("file download forbidden in this test")
		}
		x.copies.Add(1)
	}
	return x.Executor.Stream(ctx, ref, opts)
}

func TestContainerCopyAndMoveStayInsidePod(t *testing.T) {
	a, f := kubeApp(t)
	p := &a.Panels[0]
	podWithContainers(f, "solo", "app")
	p.Kube.invalidate("")
	root := localexec.Fixture(t)
	x := &internalOnlyExec{Executor: localexec.New(t, "", strings.Fields(localexec.BaseTools)...)}
	p.Kube.executor = x
	if err := os.Mkdir(filepath.Join(root, "destination"), 0755); err != nil {
		t.Fatal(err)
	}
	sourcePath := "/shop/pods/solo" + root
	a.error(p.Load(sourcePath))
	waitNetwork(t, a)
	// The same pod/container in the other panel.
	a.key(Message{Key: "=", Meta: true})
	waitNetwork(t, a)
	a.error(a.Panels[1].Load(sourcePath + "/destination"))
	waitNetwork(t, a)
	selectEntry(t, p, "plain.txt")
	a.key(Message{Key: "F5"})
	if a.Form == nil {
		t.Fatal(a.Status)
	}
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if _, err := os.Stat(filepath.Join(root, "destination", "plain.txt")); err != nil {
		t.Fatal(err, a.Status)
	}
	if x.copies.Load() != 1 {
		t.Fatal("copy did not run inside container", x.copies.Load())
	}
	// Shift+F5 defaults to the source directory even when the other panel differs.
	selectEntry(t, p, "plain.txt")
	a.key(Message{Key: "F5", Shift: true})
	if a.Form == nil || a.Form.Fields[0].Value != "plain.txt" {
		t.Fatal("local copy target", a.Form, a.Status)
	}
	a.Form.Fields[0].Value = "local-copy.txt"
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if _, err := os.Stat(filepath.Join(root, "local-copy.txt")); err != nil {
		t.Fatal(err, a.Status)
	}
	selectEntry(t, p, "local-copy.txt")
	a.key(Message{Key: "F6", Shift: true})
	if a.Dialog == nil {
		t.Fatal("local rename prompt", a.Status)
	}
	a.Dialog.Input = "renamed.txt"
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); err != nil {
		t.Fatal(err, a.Status)
	}
	if _, err := os.Stat(filepath.Join(root, "local-copy.txt")); !os.IsNotExist(err) {
		t.Fatal("rename left source", err)
	}
	selectEntry(t, p, "renamed.txt")
	a.key(Message{Key: "F6"})
	if a.Dialog == nil {
		t.Fatal(a.Status)
	}
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if _, err := os.Stat(filepath.Join(root, "destination", "renamed.txt")); err != nil {
		t.Fatal(err, a.Status)
	}
	if x.copies.Load() != 2 {
		t.Fatal("move streamed file contents", x.copies.Load())
	}

	// Two resource-tree routes may expose different virtual prefixes for the
	// same container. Only the paths inside the container identify files.
	from := p.endpoint().Opener.(*containerOpener)
	alias := *from
	alias.prefix = "/another/resource/path/to/solo"
	op := fsops.NewOperation("copy through alternate route")
	defer op.Cancel()
	err := remotefs.Copy(remotefs.Endpoint{Opener: from}, remotefs.Endpoint{Opener: &alias}, []remotefs.Pair{{Source: sourcePath + "/plain.txt", Destination: alias.prefix + root + "/aliased.txt"}}, remotefs.CopyOptions{}, op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "aliased.txt")); err != nil {
		t.Fatal(err)
	}
	op2 := fsops.NewOperation("self copy")
	defer op2.Cancel()
	err = remotefs.Copy(remotefs.Endpoint{Opener: from}, remotefs.Endpoint{Opener: &alias}, []remotefs.Pair{{Source: sourcePath + "/plain.txt", Destination: alias.prefix + root + "/plain.txt"}}, remotefs.CopyOptions{}, op2)
	if err == nil {
		t.Fatal("self copy through an alternate route was allowed")
	}
}

func TestLocalResourceCopyRenameAndMove(t *testing.T) {
	a, f := kubeApp(t)
	p := &a.Panels[0]
	enterKube(t, a, "shop")
	enterKube(t, a, "widgets")
	selectEntry(t, p, "w1")
	a.key(Message{Key: "F5", Shift: true})
	if a.Form == nil {
		t.Fatal(a.Status)
	}
	a.Form.Fields[0].Value = "copied"
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	selectEntry(t, p, "copied")
	a.key(Message{Key: "F6", Shift: true})
	if a.Form == nil {
		t.Fatal(a.Status)
	}
	a.Form.Fields[0].Value = "renamed"
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	selectEntry(t, p, "renamed")
	a.connectKube(1, kube.Profile{Kubeconfig: f.Kubeconfig(t), Context: "fake"})
	waitNetwork(t, a)
	a.error(a.Panels[1].Load("/default"))
	waitNetwork(t, a)
	a.Active = 0
	a.key(Message{Key: "F6"})
	if a.Form == nil {
		t.Fatal(a.Status)
	}
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if strings.Contains(strings.Join(entryNames(p), ","), "renamed") {
		t.Fatal("move left source", a.Status)
	}
	a.error(a.Panels[1].Load("/default/widgets"))
	waitNetwork(t, a)
	if !strings.Contains(strings.Join(entryNames(&a.Panels[1]), ","), "renamed") {
		t.Fatal("destination missing", a.Status, a.Panels[1].Err)
	}
}

func TestF6LocalManifestTargetsKubePanel(t *testing.T) {
	a, _ := kubeApp(t)
	enterKube(t, a, "shop")
	local := a.Panels[1].Path
	filename := filepath.Join(local, "moved.yaml")
	if err := os.WriteFile(filename, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: moved\ndata:\n  value: ok\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a.error(a.Panels[1].Load(local))
	waitNetwork(t, a)
	a.Active = 1
	selectEntry(t, &a.Panels[1], "moved.yaml")
	a.key(Message{Key: "F6"})
	if a.Form == nil || a.Form.Primary != "Move" || a.Form.Fields[0].Value != "shop" {
		t.Fatal("manifest destination", a.Form, a.Status)
	}
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Fatal("source not moved", err, a.Status)
	}
	a.error(a.Panels[0].Load("/shop/configmaps"))
	waitNetwork(t, a)
	selectEntry(t, &a.Panels[0], "moved")
}
