package ui

import (
	"farshmac/internal/fsops"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"farshmac/internal/kube"
	"farshmac/internal/kube/kubetest"
)

// kubeApp connects panel 0 to a fake cluster seeded with the "shop" namespace.
func kubeApp(t *testing.T) (*App, *kubetest.FakeAPI) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	f := kubetest.New(t)
	kubetest.SeedShop(f)
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	a.Settings.ShowKeys = true
	a.connectKube(0, kube.Profile{Kubeconfig: f.Kubeconfig(t), Context: "fake"})
	waitNetwork(t, a)
	if a.Panels[0].Kube == nil || a.Panels[0].Path != "/" {
		t.Fatal("not connected", a.Status, a.Panels[0].Path)
	}
	return a, f
}

func entryNames(p *Panel) []string {
	var names []string
	for _, e := range p.Entries {
		names = append(names, e.Name)
	}
	return names
}

func enterKube(t *testing.T, a *App, name string) {
	t.Helper()
	p := &a.Panels[0]
	selectEntry(t, p, name)
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if p.Err != "" {
		t.Fatal("enter", name, p.Err, a.Status)
	}
}

// closeDoc leaves a document screen and waits for the panel reload it triggers.
func closeDoc(t *testing.T, a *App, name string) {
	t.Helper()
	a.key(Message{Key: "Escape"})
	waitNetwork(t, a)
	if a.Doc != nil {
		t.Fatal("document still open")
	}
	selectEntry(t, &a.Panels[0], name)
}

func TestKubePanelLevels(t *testing.T) {
	a, _ := kubeApp(t)
	p := &a.Panels[0]
	if !strings.HasPrefix(a.Status, "Connected: k8s:fake") {
		t.Fatal(a.Status)
	}
	if got := entryNames(p); strings.Join(got, ",") != "default,shop" {
		t.Fatal("namespaces", got)
	}
	// Root: namespaces with their STATUS in the size column and no ".." entry.
	bar := gridLine(a.Draw(), a.Rows-1)
	if !strings.Contains(bar, "NewNS") || !strings.Contains(bar, "Export") || strings.Contains(bar, "Trash") {
		t.Fatal("root key bar", bar)
	}
	g := a.Draw()
	rows := gridLine(g, 2) + gridLine(g, 3) + gridLine(g, 4)
	if !strings.Contains(rows, "shop") || !strings.Contains(rows, "Active") {
		t.Fatal("namespace rows", rows)
	}
	p.Sort = "Unsorted"
	enterKube(t, a, "shop")
	folders := entryNames(p)
	// deployments exists in two groups, so the folder carries the group (C2).
	if folders[0] != ".." || folders[1] != "deployments.apps" || folders[2] != "statefulsets" {
		t.Fatal("type folders in priority order", folders)
	}
	p.Sort = "Name"
	a.error(p.Load(p.Path))
	waitNetwork(t, a)
	if folders = entryNames(p); folders[1] != "configmaps" {
		t.Fatal("name sort is alphabetical", folders)
	}
	if strings.Contains(strings.Join(folders, ","), "secrets") {
		t.Fatal("forbidden type listed", folders)
	}
	if !strings.Contains(p.InfoText, "skipped") {
		t.Fatal("skipped types not reported", p.InfoText)
	}
	enterKube(t, a, "deployments.apps")
	if got := entryNames(p); strings.Join(got, ",") != "..,app" {
		t.Fatal("deployments", got)
	}
	line := gridLine(a.Draw(), 3) + gridLine(a.Draw(), 4)
	if !strings.Contains(line, "app") || !strings.Contains(line, "2/3") || !strings.Contains(line, "Deployment") {
		t.Fatal("deployment kind and READY columns", line)
	}
	if head := gridLine(a.Draw(), 2); !strings.Contains(head, "Name / Kind / Status / Age") {
		t.Fatal("kube heading", head)
	}
	// An object that owns nothing is a file: Enter shows its manifest.
	a.error(p.Load("/shop/configmaps"))
	waitNetwork(t, a)
	selectEntry(t, p, "settings")
	if p.Entries[p.Cursor].Dir {
		t.Fatal("a childless object must be a file")
	}
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if a.Doc == nil || !strings.Contains(a.Doc.Text, "kind: ConfigMap") {
		t.Fatal("Enter on a file object must show the manifest", a.Doc, a.Status)
	}
	closeDoc(t, a, "settings")
	a.error(p.Load("/shop/deployments.apps"))
	waitNetwork(t, a)
	if !p.Entries[1].Dir {
		t.Fatal("an owner must stay a folder")
	}
	// While a listing is being read the panel says so.
	p.load = &panelLoad{started: time.Now().Add(-2 * time.Second), done: make(chan panelResult, 1), o: fsops.NewOperation("x")}
	if body := gridLine(a.Draw(), 3+(a.Rows-9)/2); !strings.Contains(body, "Reading cluster") || !strings.Contains(body, "Esc stops") {
		t.Fatal("reading indicator", body)
	}
	p.load = nil
	bar = gridLine(a.Draw(), a.Rows-1)
	if !strings.Contains(bar, "Manif") || !strings.Contains(bar, "Delete") || !strings.Contains(bar, "Copy") {
		t.Fatal("type folder key bar", bar)
	}
	selectEntry(t, p, "app")
	a.Handle(`{"Type":"modifiers","Shift":true}`)
	if bar = gridLine(a.Draw(), a.Rows-1); !strings.Contains(bar, "Logs") || !strings.Contains(bar, "Orphan") {
		t.Fatal("shift row for an owner of pods", bar)
	}
	a.Handle(`{"Type":"modifiers"}`)
	enterKube(t, a, "app")
	if got := entryNames(p); strings.Join(got, ",") != "..,pods,widgets" {
		t.Fatal("owned folders, ReplicaSets collapsed", got)
	}
	enterKube(t, a, "pods")
	if got := entryNames(p); strings.Join(got, ",") != "..,app-new-x,app-old-y,shared" {
		t.Fatal("pods of both ReplicaSets and the two-owner pod", got)
	}
	if p.Path != "/shop/deployments.apps/app/pods" {
		t.Fatal(p.Path)
	}
	// Up again through the virtual tree.
	selectEntry(t, p, "..")
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if p.Path != "/shop/deployments.apps/app" || p.Entries[p.Cursor].Name != "pods" {
		t.Fatal("parent", p.Path, p.Entries[p.Cursor].Name)
	}
	// Services list pods by selector and say so.
	a.error(p.Load("/shop/services/web/pods"))
	waitNetwork(t, a)
	if got := entryNames(p); strings.Join(got, ",") != "..,app-new-x,app-old-y" || !strings.Contains(p.InfoText, "selector") {
		t.Fatal("service pods", got, p.InfoText)
	}
	// Bad paths keep the panel where it was.
	before := p.Path
	a.error(p.Load("/shop/nothing"))
	waitNetwork(t, a)
	if p.Path != before || !strings.Contains(p.Err+a.Status, "no such folder") {
		t.Fatal("bad path", p.Path, p.Err, a.Status)
	}
	// The terminal stays local and F9 gets a Kubernetes section.
	if !strings.Contains(gridLine(a.Draw(), a.Rows-2), "local:") {
		t.Fatal("prompt must show the local folder")
	}
	names := []string{}
	for _, s := range a.menuSections() {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "Left,Files,Commands,Options,Kubernetes,Right" {
		t.Fatal("menu sections", names)
	}
	a.disconnectRemote(0)
	if p.Kube != nil || p.Path == before {
		t.Fatal("disconnect", p.Path)
	}
}

func TestKubeManifestViewEditAndDelete(t *testing.T) {
	a, f := kubeApp(t)
	p := &a.Panels[0]
	enterKube(t, a, "shop")
	enterKube(t, a, "deployments.apps")
	selectEntry(t, p, "app")
	a.key(Message{Key: "F3"})
	waitNetwork(t, a)
	if a.Doc == nil || !strings.Contains(a.Doc.Text, "kind: Deployment") || !strings.Contains(a.Doc.Text, "name: app") || a.Doc.Edit {
		t.Fatal("manifest view", a.Doc, a.Status)
	}
	if a.Doc.displayPath() != "k8s:fake/shop/deployments/app.yaml" {
		t.Fatal("display path must end in .yaml for highlighting", a.Doc.displayPath())
	}
	if !strings.Contains(a.Doc.Text, "resourceVersion:") {
		t.Fatal("the viewer shows the full manifest", a.Doc.Text)
	}
	closeDoc(t, a, "app")
	a.key(Message{Key: "F4"})
	waitNetwork(t, a)
	d := a.Doc
	if d == nil || !d.Edit {
		t.Fatal("manifest editor", a.Status)
	}
	if strings.Contains(d.Text, "resourceVersion:") || strings.Contains(d.Text, "creationTimestamp:") || strings.Contains(d.Text, "uid:") || !strings.Contains(d.Text, "name: app") {
		t.Fatal("the editor must strip server-managed fields", d.Text)
	}
	d.Cursor = len(d.Text)
	d.Insert("  replicas-note: edited\n")
	d.modified()
	a.save()
	waitNetwork(t, a)
	if d.Dirty || len(f.Puts()) != 1 || !strings.Contains(f.Puts()[0], "/deployments/app") {
		t.Fatal("manifest update", d.Dirty, f.Puts(), a.Status)
	}
	closeDoc(t, a, "app")
	// Describe and logs open read-only text screens.
	a.key(Message{Key: "F4", Shift: true})
	waitNetwork(t, a)
	if a.Doc == nil || !a.Doc.ReadOnly || !strings.Contains(a.Doc.Text, "Name:") {
		t.Fatal("describe", a.Doc, a.Status)
	}
	closeDoc(t, a, "app")
	f.SetLogs("line one\nline two\n")
	a.key(Message{Key: "F3", Shift: true})
	if a.Form == nil || a.Form.Title != "Pod" {
		t.Fatal("logs of a deployment must ask which pod", a.Form, a.Status)
	}
	a.Form.Focus = 0
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if a.Doc == nil || a.Doc.Text != "line one\nline two\n" {
		t.Fatal("logs", a.Doc, a.Status)
	}
	closeDoc(t, a, "app")
	// F8 deletes with a confirmation that names the objects; Shift+F8 orphans.
	a.key(Message{Key: "F8"})
	if a.Form == nil || a.Form.Title != "Delete objects" || !strings.Contains(a.Form.Message, "Deployment app") {
		t.Fatal("delete confirmation", a.Form)
	}
	chooseButton(t, a, "Delete")
	waitNetwork(t, a)
	deletes := f.Deletes()
	if len(deletes) != 1 || !strings.Contains(deletes[0], "/deployments/app") || !strings.Contains(deletes[0], "Background") {
		t.Fatal("delete", deletes, a.Status)
	}
	// Root: namespaces need their name typed; F7 creates one.
	a.error(p.Load("/"))
	waitNetwork(t, a)
	selectEntry(t, p, "shop")
	a.key(Message{Key: "F8"})
	if a.Dialog == nil || !strings.Contains(a.Dialog.Title, "shop") {
		t.Fatal("namespace delete prompt", a.Dialog)
	}
	a.Dialog.Input = "wrong"
	a.key(Message{Key: "Enter"})
	if !strings.Contains(a.Status, "did not match") || len(f.Deletes()) != 1 {
		t.Fatal("namespace must not be deleted on a wrong name", a.Status, f.Deletes())
	}
	a.key(Message{Key: "F7"})
	if a.Dialog == nil || a.Dialog.Title != "New namespace" {
		t.Fatal("F7 at root", a.Dialog)
	}
	a.Dialog.Input = "fresh"
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if !strings.Contains(strings.Join(entryNames(p), ","), "fresh") {
		t.Fatal("namespace not created", entryNames(p), a.Status)
	}
}

func TestKubeExportManifests(t *testing.T) {
	a, _ := kubeApp(t)
	p := &a.Panels[0]
	out := a.Panels[1].Path
	enterKube(t, a, "shop")
	enterKube(t, a, "deployments.apps")
	selectEntry(t, p, "app")
	a.key(Message{Key: "F5"})
	if a.Form == nil || a.Form.Title != "Export manifests" || a.Form.Fields[0].Value != out {
		t.Fatal("export dialog", a.Form)
	}
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	data, err := os.ReadFile(filepath.Join(out, "app.yaml"))
	if err != nil || !strings.Contains(string(data), "kind: Deployment") || strings.Contains(string(data), "resourceVersion") {
		t.Fatal("clean export", err, string(data))
	}
	// A whole namespace exports folder by folder; events are left out.
	a.error(p.Load("/"))
	waitNetwork(t, a)
	selectEntry(t, p, "shop")
	a.key(Message{Key: "F5"})
	if a.Form == nil {
		t.Fatal("namespace export dialog", a.Status)
	}
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if _, err := os.Stat(filepath.Join(out, "shop", "services", "web.yaml")); err != nil {
		t.Fatal("recursive export", err)
	}
	if _, err := os.Stat(filepath.Join(out, "shop", "pods", "loner.yaml")); err != nil {
		t.Fatal("pods exported", err)
	}
	// Moving and copying into the cluster are refused with a status, not a dialog.
	a.key(Message{Key: "F6"})
	if a.Form != nil || !strings.Contains(a.Status, "cannot be renamed") {
		t.Fatal("F6", a.Form, a.Status)
	}
	a.Active = 1
	a.key(Message{Key: "F5"})
	if a.Form != nil || !strings.Contains(a.Status, "destination") {
		t.Fatal("copy into the cluster", a.Form, a.Status)
	}
}

func TestKubeWatchUpdatesPanel(t *testing.T) {
	a, f := kubeApp(t)
	p := &a.Panels[0]
	enterKube(t, a, "shop")
	enterKube(t, a, "deployments.apps")
	if !p.Kube.watching("shop") || !p.Kube.watching("") {
		t.Fatal("watches must start after listing", p.Kube.watches)
	}
	f.Emit("/apis/apps/v1/namespaces/shop/deployments", "ADDED", kubetest.Obj("Deployment", "shop", "late", "dep-late", nil, nil, map[string]string{"READY": "0/1"}))
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(strings.Join(entryNames(p), ","), "late") {
		a.Poll()
		// Updates come from the cache: no reading indicator, no emptied list.
		if p.load != nil || len(p.Entries) < 2 || p.Err != "" {
			t.Fatal("watch update must not re-read the cluster", p.load != nil, len(p.Entries), p.Err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := strings.Join(entryNames(p), ","); !strings.Contains(got, "late") {
		t.Fatal("added object not shown", got)
	}
	// An expired watch reloads the namespace in the background and resumes.
	f.ExpireWatch("/apis/apps/v1/namespaces/shop/deployments")
	f.DropWatches("/apis/apps/v1/namespaces/shop/deployments")
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && p.Kube.watching("shop") {
		a.Poll()
		time.Sleep(20 * time.Millisecond)
	}
	for time.Now().Before(deadline) && !p.Kube.watching("shop") {
		a.Poll()
		time.Sleep(20 * time.Millisecond)
	}
	if !p.Kube.watching("shop") || len(p.Entries) < 2 || p.load != nil {
		t.Fatal("watch did not resume after expiry", p.Kube.watching("shop"), len(p.Entries), p.load != nil)
	}
	f.Emit("/apis/apps/v1/namespaces/shop/deployments", "DELETED", kubetest.Obj("Deployment", "shop", "late", "dep-late", nil, nil, nil))
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && strings.Contains(strings.Join(entryNames(p), ","), "late") {
		a.Poll()
		time.Sleep(20 * time.Millisecond)
	}
	if got := strings.Join(entryNames(p), ","); strings.Contains(got, "late") {
		t.Fatal("deleted object still shown", got)
	}
	// Namespaces are watched too.
	a.error(p.Load("/"))
	waitNetwork(t, a)
	f.Emit("/api/v1/namespaces", "ADDED", kubetest.Obj("Namespace", "", "watched", "ns-watched", nil, nil, map[string]string{"STATUS": "Active"}))
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(strings.Join(entryNames(p), ","), "watched") {
		a.Poll()
		time.Sleep(20 * time.Millisecond)
	}
	if got := strings.Join(entryNames(p), ","); !strings.Contains(got, "watched") {
		t.Fatal("added namespace not shown", got)
	}
	a.disconnectRemote(0)
}
