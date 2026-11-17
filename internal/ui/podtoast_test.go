package ui

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"farshmac/internal/podtoast"
	corev1 "k8s.io/api/core/v1"
)

func TestPodToastPanelAndFileOperations(t *testing.T) {
	a, api := kubeApp(t)
	p := &a.Panels[0]
	podWithContainers(api, "toast", "app")
	p.Kube.invalidate("")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("from PodToast"), 0640); err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	go podtoast.Serve(right, &podtoast.Server{Root: root})
	c, err := podtoast.Connect(context.Background(), left)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ref := kube.ContainerRef{Namespace: "shop", Pod: "toast", Container: "app"}
	p.Kube.toasts = map[string]*podtoast.Client{toastKey(ref): c}
	// No shell executor is installed; any accidental shell fallback fails.
	a.error(p.Load("/shop/pods/toast"))
	waitNetwork(t, a)
	if p.Err != "" || !strings.Contains(p.InfoText, "container app") || strings.Contains(p.InfoText, "PodToast") {
		t.Fatal(p.Err, p.InfoText, a.Status)
	}
	selectEntry(t, p, "hello.txt")
	a.key(Message{Key: "F3"})
	waitNetwork(t, a)
	if a.Doc == nil || a.Doc.Text != "from PodToast" {
		t.Fatal("viewer did not use agent", a.Status)
	}
	closeDoc(t, a, "hello.txt")
	opener := p.kubeLevel().container(p.Kube)
	fs, err := opener.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	prefix := "/shop/pods/toast"
	if err = fs.Mkdir(prefix + "/folder"); err != nil {
		t.Fatal(err)
	}
	if err = opener.CopyWithin(context.Background(), prefix+"/hello.txt", opener, prefix+"/folder/copy", ""); err != nil {
		t.Fatal(err)
	}
	if err = fs.Rename(prefix+"/folder/copy", prefix+"/folder/renamed"); err != nil {
		t.Fatal(err)
	}
	r, err := fs.Open(prefix + "/folder/renamed")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(data) != "from PodToast" {
		t.Fatal(string(data), err)
	}
	if err = fs.Remove(prefix+"/folder/renamed", false); err != nil {
		t.Fatal(err)
	}
	if err = fs.Remove(prefix+"/folder", true); err != nil {
		t.Fatal(err)
	}
	// Exercise the editor's real staging/metadata/atomic-save path.
	staged := filepath.Join(t.TempDir(), "edited.txt")
	if err = os.WriteFile(staged, []byte("edited through PodToast"), 0600); err != nil {
		t.Fatal(err)
	}
	expected, err := fs.Lstat(prefix + "/hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	operation := fsops.NewOperation("Save PodToast document")
	defer operation.Cancel()
	source := &remoteSource{Opener: opener, Path: prefix + "/hello.txt"}
	saved, err := source.Store(operation, staged, expected, false)
	if err != nil || saved.Mode().Perm() != 0640 {
		t.Fatal("editor save", saved, err)
	}
	contents, err := os.ReadFile(filepath.Join(root, "hello.txt"))
	if err != nil || string(contents) != "edited through PodToast" {
		t.Fatal(string(contents), err)
	}
	c.Close()
	if _, err = opener.Open(context.Background()); err == nil || !strings.Contains(err.Error(), "reconnect") {
		t.Fatal("disconnected agent silently fell back", err)
	}
}

func TestPodToastContainerChoice(t *testing.T) {
	a, _ := kubeApp(t)
	pod := &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "app", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	preset := EphemeralPreset{Options: kube.EphemeralOptions{Image: "podtoast:test", PullPolicy: "IfNotPresent", TimeoutSeconds: 90}}
	ref := kube.ContainerRef{Namespace: "test", Pod: "pod"}
	a.podToastForm(&a.Panels[0], preset, ref, pod)
	if len(a.Form.Fields) != 3 {
		t.Fatal("single container should not require a choice")
	}
	pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: "sidecar", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}})
	a.podToastForm(&a.Panels[0], preset, ref, pod)
	if len(a.Form.Fields) != 5 || !a.Form.Fields[3].Toggle || !a.Form.Fields[4].Toggle || a.Form.Fields[3].Combo {
		t.Fatal("expected per-target checkboxes")
	}
	a.Form.Focus = 3
	a.formKey(Message{Key: " "})
	if a.Form.Fields[3].Checked || !a.Form.Fields[4].Checked {
		t.Fatal("target selection not independent")
	}
	a.Form.Fields[4].Checked = false
	a.formKey(Message{Key: "Enter"})
	if a.Form == nil || a.Busy {
		t.Fatal("empty target selection must be rejected")
	}
	shared := true
	pod.Spec.ShareProcessNamespace = &shared
	a.podToastForm(&a.Panels[0], preset, ref, pod)
	if len(a.Form.Fields) != 5 {
		t.Fatal("shared targets must also be selectable")
	}
}

func TestDefaultPodToastImageUpgrade(t *testing.T) {
	for _, old := range []string{"", "ghcr.io/ikimpriv/podtoast:0.1.0", "ghcr.io/ikimpriv/podtoast:0.3.1", "ghcr.io/ikimpriv/podtoast:0.6.0", "ghcr.io/ikimpriv/podtoast:0.7.0", "ghcr.io/ikimpriv/podtoast:0.7.1"} {
		if got := preferredPodToastImage(old); got != defaultPodToastImage {
			t.Fatal(got)
		}
	}
	for _, custom := range []string{"registry.example/agent:custom", "ghcr.io/ikimpriv/podtoast@sha256:pinned", "ghcr.io/ikimpriv/podtoast:0.9.0", "ghcr.io/ikimpriv/podtoast:edge", defaultPodToastImage} {
		if preferredPodToastImage(custom) != custom {
			t.Fatal("custom image overridden")
		}
	}
}

func TestPodToastVersionPrompt(t *testing.T) {
	a, _ := kubeApp(t)
	defer a.Close()
	running := corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	pod := &corev1.Pod{Spec: corev1.PodSpec{EphemeralContainers: []corev1.EphemeralContainer{{TargetContainerName: "app", EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "podtoast-old", Image: "ghcr.io/ikimpriv/podtoast:0.7.1", Command: []string{"/podtoast", "idle"}}}}}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "app", State: running}}, EphemeralContainerStatuses: []corev1.ContainerStatus{{Name: "podtoast-old", State: running}}}}
	preset := EphemeralPreset{PodToast: true, Options: kube.EphemeralOptions{Image: defaultPodToastImage, PullPolicy: "IfNotPresent", TimeoutSeconds: 90}}
	a.podToastVersionPrompt(&a.Panels[0], preset, kube.ContainerRef{Container: "app"}, pod)
	if !a.Form.Buttons || !strings.Contains(a.Form.Message, "0.7.1") || !strings.Contains(a.Form.Message, defaultPodToastImage) || a.Form.Choice[0] != "Attach new agent" {
		t.Fatalf("%+v", a.Form)
	}
	a.Form.Select(0)
	if a.Form.Buttons || a.Form.Fields[0].Value != defaultPodToastImage {
		t.Fatal("upgrade did not open new-image options")
	}
}
