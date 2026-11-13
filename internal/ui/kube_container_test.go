package ui

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"farshmac/internal/kube/kubetest"
	"farshmac/internal/kube/localexec"
)

// podWithContainers adds a running pod whose containers the typed client
// can read; owners and labels are not needed for the container levels.
func podWithContainers(f *kubetest.FakeAPI, name string, containers ...string) {
	pod := kubetest.Obj("Pod", "shop", name, "pod-"+name, nil, nil, map[string]string{"STATUS": "Running", "READY": "1/1"})
	var spec, statuses []any
	for _, c := range containers {
		spec = append(spec, map[string]any{"name": c, "image": c + ":1"})
		statuses = append(statuses, map[string]any{"name": c, "ready": true, "state": map[string]any{"running": map[string]any{"startedAt": "2024-01-02T03:04:05Z"}}})
	}
	pod["spec"] = map[string]any{"containers": spec, "nodeName": "node-1"}
	pod["status"] = map[string]any{"phase": "Running", "containerStatuses": statuses}
	f.Add("/api/v1/namespaces/shop/pods", pod)
}

func TestKubeContainerFileSystem(t *testing.T) {
	a, f := kubeApp(t)
	p := &a.Panels[0]
	podWithContainers(f, "solo", "app")
	podWithContainers(f, "duo", "web", "sidecar")
	p.Kube.invalidate("")
	// The "container" is the local shell with a restricted PATH; its root is
	// the local file system, so the fixture is addressed by its absolute path.
	root := localexec.Fixture(t)
	p.Kube.executor = localexec.New(t, "", strings.Fields(localexec.BaseTools)...)
	// A pod with one container opens straight into its file system (C3).
	a.error(p.Load("/shop/pods/solo" + root))
	waitNetwork(t, a)
	if p.Err != "" || !strings.Contains(p.InfoText, "container app") {
		t.Fatal("container listing", p.Err, p.InfoText, a.Status)
	}
	names := strings.Join(entryNames(p), ",")
	if !strings.Contains(names, "plain.txt") {
		t.Fatal("fixture entries", names)
	}
	if !strings.Contains(gridLine(a.Draw(), a.Rows-2), "pod:shop/solo/app:"+root) {
		t.Fatal("prompt", gridLine(a.Draw(), a.Rows-2))
	}
	bar := gridLine(a.Draw(), a.Rows-1)
	if !strings.Contains(bar, "Copy") || !strings.Contains(bar, "MkFold") || !strings.Contains(bar, "Delete") {
		t.Fatal("container key bar", bar)
	}
	// F3 views a file through the container; the title shows the virtual path.
	selectEntry(t, p, "plain.txt")
	a.key(Message{Key: "F3"})
	waitNetwork(t, a)
	if a.Doc == nil || !strings.HasPrefix(a.Doc.displayPath(), "k8s:fake/shop/pods/solo"+root) {
		t.Fatal("view in container", a.Doc, a.Status)
	}
	content := a.Doc.Text
	closeDoc(t, a, "plain.txt")
	// F5 copies the file into the local panel through the container endpoint.
	a.key(Message{Key: "F5"})
	if a.Form == nil || a.Form.Title != "Copy" {
		t.Fatal("copy dialog", a.Form, a.Status)
	}
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	got, err := os.ReadFile(filepath.Join(a.Panels[1].Path, "plain.txt"))
	if err != nil || string(got) != content {
		t.Fatal("copied out", err, string(got), a.Status)
	}
	// A command typed at the prompt runs in the container, in the panel folder.
	a.Command = "printf hello > made-here.txt"
	a.key(Message{Key: "Enter"})
	if a.Foreign == nil || !a.TermVisible {
		t.Fatal("command must run as a foreign stream", a.Status)
	}
	deadline := time.Now().Add(10 * time.Second)
	for a.Foreign != nil && time.Now().Before(deadline) {
		a.Poll()
		time.Sleep(10 * time.Millisecond)
	}
	waitNetwork(t, a)
	if a.Foreign != nil {
		t.Fatal("stream did not end")
	}
	// The terminal keeps the output; the next key returns to the panels.
	if !a.TermVisible || !a.foreignEnded {
		t.Fatal("terminal should wait for a key", a.TermVisible, a.foreignEnded)
	}
	a.Handle(`{"Type":"input","Text":"x"}`)
	if a.TermVisible || a.foreignEnded {
		t.Fatal("key did not return to the panels")
	}
	if got, err := os.ReadFile(filepath.Join(root, "made-here.txt")); err != nil || string(got) != "hello" {
		t.Fatal("command output", err, string(got), a.Status)
	}
	if !strings.Contains(strings.Join(entryNames(p), ","), "made-here.txt") {
		t.Fatal("panel not refreshed after the command", entryNames(p))
	}
	// Following logs streams into the terminal area and Cmd+. stops it.
	f.SetLogs("first line\nsecond line\n")
	a.TermVisible = false
	a.error(p.Load("/shop/pods"))
	waitNetwork(t, a)
	selectEntry(t, p, "solo")
	a.key(Message{Key: "F3", Alt: true})
	if a.Foreign == nil {
		t.Fatal("follow logs", a.Status)
	}
	var shown []byte
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && a.Foreign != nil {
		if u := a.Poll(); u.Terminal != "" {
			chunk, _ := base64.StdEncoding.DecodeString(u.Terminal)
			shown = append(shown, chunk...)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if u := a.Poll(); u.Terminal != "" {
		chunk, _ := base64.StdEncoding.DecodeString(u.Terminal)
		shown = append(shown, chunk...)
	}
	if !strings.Contains(string(shown), "first line\r\nsecond line") {
		t.Fatal("log output", string(shown))
	}
	// A pod with two containers lists them; entering one gives its file system.
	a.error(p.Load("/shop/pods/duo"))
	waitNetwork(t, a)
	if got := strings.Join(entryNames(p), ","); got != "..,web,sidecar" && got != "..,sidecar,web" {
		t.Fatal("containers", got)
	}
	a.error(p.Load("/shop/pods/duo/sidecar" + root))
	waitNetwork(t, a)
	if p.Err != "" || !strings.Contains(p.InfoText, "container sidecar") {
		t.Fatal("second container", p.Err, p.InfoText)
	}
}

func TestKubeQuickViewShowsContainerFiles(t *testing.T) {
	a, f := kubeApp(t)
	p := &a.Panels[0]
	podWithContainers(f, "solo", "app")
	p.Kube.invalidate("")
	root := localexec.Fixture(t)
	p.Kube.executor = localexec.New(t, "", strings.Fields(localexec.BaseTools)...)
	a.error(p.Load("/shop/pods/solo" + root))
	waitNetwork(t, a)
	selectEntry(t, p, "plain.txt")
	want, _ := os.ReadFile(filepath.Join(root, "plain.txt"))
	a.key(Message{Key: "q", Ctrl: true})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && a.previewText != string(want) {
		a.Poll()
		time.Sleep(20 * time.Millisecond)
	}
	if a.previewText != string(want) {
		t.Fatalf("container file preview %q (want %q)", a.previewText, string(want))
	}
	// Scrolling: the Quick view panel scrolls with the arrows when active.
	a.previewText = "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nl12\nl13\nl14\nl15\nl16\nl17\nl18\nl19\nl20\nl21\nl22\nl23\nl24\nl25\nl26\nl27\nl28\nl29\nl30\nl31\nl32\nl33\nl34\nl35\nl36\nl37\nl38\nl39\nl40\nl41\nl42\nl43\nl44\nl45\nl46\nl47\nl48\nl49\nl50"
	a.Active = 1
	a.key(Message{Key: "ArrowDown"})
	a.key(Message{Key: "PageDown"})
	if a.previewOffset < 2 {
		t.Fatal("quick view did not scroll", a.previewOffset)
	}
	x, _ := a.panelRect(1)
	if line := gridLine(a.Draw(), 3); !strings.Contains(line[x:], "l"+fmt.Sprint(a.previewOffset+1)) {
		t.Fatal("scrolled view", a.previewOffset, line)
	}
	a.key(Message{Key: "Home"})
	if a.previewOffset != 0 {
		t.Fatal("Home")
	}
}
