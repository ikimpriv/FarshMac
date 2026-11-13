package kube

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"farshmac/internal/kube/kubetest"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

const ns = "shop"

var (
	gvrDeploy = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	gvrPods   = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
)

func testClient(t *testing.T, f *kubetest.FakeAPI) *Client {
	t.Helper()
	c, err := NewFromConfig(context.Background(), &rest.Config{Host: f.URL()}, Profile{Context: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func names(objects []*Object) []string {
	var out []string
	for _, o := range objects {
		out = append(out, o.Name)
	}
	return out
}

func TestConnectAndDiscovery(t *testing.T) {
	f := kubetest.New(t)
	c := testClient(t, f)
	if c.ServerVersion() != "v1.30.0-fake" || c.Profile.Label() != "k8s:fake" {
		t.Fatal(c.ServerVersion(), c.Profile.Label())
	}
	resources, err := c.Resources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	folders := map[string]Resource{}
	for _, r := range resources {
		folders[r.Folder] = r
	}
	for _, want := range []string{"pods", "services", "configmaps", "secrets", "events", "deployments.apps", "deployments.other.io", "replicasets", "jobs", "cronjobs", "widgets"} {
		if _, ok := folders[want]; !ok {
			t.Fatal("missing folder", want, folders)
		}
	}
	for _, unwanted := range []string{"namespaces", "gadgets", "bindings", "pods/log", "deployments"} {
		if _, ok := folders[unwanted]; ok {
			t.Fatal("unexpected folder", unwanted)
		}
	}
	if r, ok := c.ResourceByFolder("deployments.apps"); !ok || r.GVR != gvrDeploy || r.Kind != "Deployment" || r.Key() != "deployments.apps" {
		t.Fatalf("deployments.apps %+v %v", r, ok)
	}
	// Metrics pods must not shadow core pods.
	if r, _ := c.ResourceByFolder("pods"); r.GVR.Group != "" {
		t.Fatal("pods folder must be core", r.GVR)
	}
	// Discovery is cached: the same slice comes back.
	again, _ := c.Resources(context.Background())
	if len(again) != len(resources) {
		t.Fatal("cache")
	}
}

func TestConnectFailure(t *testing.T) {
	_, err := NewFromConfig(context.Background(), &rest.Config{Host: "http://127.0.0.1:1"}, Profile{})
	if err == nil || !strings.Contains(err.Error(), "cannot reach") {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	os.WriteFile(cfg, []byte("apiVersion: v1\nkind: Config\ncurrent-context: b\nclusters:\n- name: c\n  cluster:\n    server: http://127.0.0.1:1\ncontexts:\n- name: b\n  context:\n    cluster: c\n    user: u\n- name: a\n  context:\n    cluster: c\n    user: u\nusers:\n- name: u\n  user:\n    token: x\n"), 0600)
	list, current, err := Contexts(cfg)
	if err != nil || current != "b" || strings.Join(list, ",") != "a,b" {
		t.Fatal(list, current, err)
	}
	if _, err := Connect(context.Background(), Profile{Kubeconfig: cfg, Context: "a"}); err == nil {
		t.Fatal("connect to a closed port must fail")
	}
	if _, err := Connect(context.Background(), Profile{Kubeconfig: cfg, Context: "missing"}); err == nil || !strings.Contains(err.Error(), "kubeconfig") {
		t.Fatal("missing context", err)
	}
	t.Setenv("KUBECONFIG", cfg+string(os.PathListSeparator)+"/other")
	if DefaultKubeconfig() != cfg {
		t.Fatal(DefaultKubeconfig())
	}
	t.Setenv("KUBECONFIG", "")
	if !strings.HasSuffix(DefaultKubeconfig(), filepath.Join(".kube", "config")) {
		t.Fatal(DefaultKubeconfig())
	}
}

func TestEnsureLoginPathAddsExtra(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("SHELL", "/bin/sh")
	EnsureLoginPath("/opt/plugins")
	path := os.Getenv("PATH")
	if !strings.HasPrefix(path, "/opt/plugins"+string(os.PathListSeparator)) || !strings.Contains(path, "/usr/bin") {
		t.Fatal(path)
	}
	if joinPaths("/a:/b", "/b:/c", "") != "/a:/b:/c" {
		t.Fatal(joinPaths("/a:/b", "/b:/c", ""))
	}
}

func TestNamespacesAndSnapshot(t *testing.T) {
	f := kubetest.New(t)
	kubetest.SeedShop(f)
	c := testClient(t, f)
	ctx := context.Background()
	namespaces, err := c.Namespaces(ctx)
	if err != nil || strings.Join(names(namespaces), ",") != "default,shop" || namespaces[1].ShortStatus() != "Active" {
		t.Fatal(names(namespaces), err)
	}
	if namespaces[0].Created.IsZero() || namespaces[0].UID != "ns-default" {
		t.Fatal("metadata from Table rows", namespaces[0])
	}
	if _, err := c.LoadSnapshot(ctx, "nope", nil); !apierrors.IsNotFound(err) {
		t.Fatal("missing namespace", err)
	}
	s, err := c.LoadSnapshot(ctx, ns, map[string]bool{"events": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Skipped) != 1 || s.Skipped[0].Resource.Folder != "secrets" || s.Skipped[0].Reason != "forbidden" {
		t.Fatalf("skipped %+v", s.Skipped)
	}
	if _, listed := s.ByFolder["events"]; listed {
		t.Fatal("hidden folder listed")
	}
	if got := strings.Join(s.Folders(), ","); got != "deployments.apps,statefulsets,cronjobs,jobs,pods,services,configmaps,controllerrevisions,replicasets,widgets" {
		t.Fatal(got)
	}
	app, ok := s.Lookup("deployments.apps", "app")
	if !ok || app.UID != "dep-app" || app.ShortStatus() != "2/3" || app.Kind != "Deployment" {
		t.Fatalf("lookup %+v %v", app, ok)
	}
	pod, _ := s.Lookup("pods", "app-new-x")
	if pod.ShortStatus() != "Running" || pod.Labels["app"] != "shop" || len(pod.Owners) != 1 || pod.Owners[0] != "rs-new" {
		t.Fatalf("pod %+v", pod)
	}
	web, _ := s.Lookup("services", "web")
	if web.ShortStatus() != "ClusterIP" || web.Selector["app"] != "shop" {
		t.Fatalf("service %+v", web)
	}
	cj, _ := s.Lookup("cronjobs", "backup")
	if cj.ShortStatus() != "0 * * * *" {
		t.Fatal(cj.ShortStatus())
	}
	cm, _ := s.Lookup("configmaps", "settings")
	if cm.ShortStatus() != "3" {
		t.Fatal("fallback column", cm.ShortStatus())
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.LoadSnapshot(cancelled, ns, nil); err == nil {
		t.Fatal("cancelled context must fail")
	}
}

func TestTreeRules(t *testing.T) {
	f := kubetest.New(t)
	kubetest.SeedShop(f)
	c := testClient(t, f)
	s, err := c.LoadSnapshot(context.Background(), ns, nil)
	if err != nil {
		t.Fatal(err)
	}
	app, _ := s.Lookup("deployments.apps", "app")
	children := s.Children(app)
	if got := strings.Join(names(children["pods"]), ","); got != "app-new-x,app-old-y,shared" {
		t.Fatal("deployment pods through old and new replicasets", got)
	}
	if _, rs := children["replicasets"]; rs {
		t.Fatal("transparent replicasets must not appear")
	}
	if got := strings.Join(names(children["widgets"]), ","); got != "w1" {
		t.Fatal("CRD child by ownerReference", got)
	}
	db, _ := s.Lookup("statefulsets", "db")
	children = s.Children(db)
	if got := strings.Join(names(children["pods"]), ","); got != "db-0,shared" || children["controllerrevisions"] != nil {
		t.Fatalf("statefulset children %v", children)
	}
	web, _ := s.Lookup("services", "web")
	if got := strings.Join(names(s.Children(web)["pods"]), ","); got != "app-new-x,app-old-y" {
		t.Fatal("service pods by selector", got)
	}
	headless, _ := s.Lookup("services", "headless")
	if len(s.Children(headless)) != 0 {
		t.Fatal("empty selector matches nothing")
	}
	cj, _ := s.Lookup("cronjobs", "backup")
	jobs := s.Children(cj)["jobs"]
	if strings.Join(names(jobs), ",") != "backup-1" || strings.Join(names(s.Children(jobs[0])["pods"]), ",") != "backup-1-z" {
		t.Fatal("cronjob -> job -> pod", names(jobs))
	}
	pod, _ := s.Lookup("pods", "app-new-x")
	if len(s.Children(pod)) != 0 {
		t.Fatal("nothing inside a pod")
	}
	loner, _ := s.Lookup("pods", "loner")
	if len(s.Children(loner)) != 0 || !s.HasPods(loner) || s.HasPods(headless) || !s.HasPods(app) {
		t.Fatal("HasPods")
	}
	if got := strings.Join(FolderOrder([]string{"zoo", "pods", "alpha", "deployments"}), ","); got != "deployments,pods,alpha,zoo" {
		t.Fatal(got)
	}
	if FolderKind("deployments.apps") != "deployments" || FolderKind("pods") != "pods" {
		t.Fatal("FolderKind")
	}
}

func TestManifests(t *testing.T) {
	f := kubetest.New(t)
	kubetest.SeedShop(f)
	c := testClient(t, f)
	ctx := context.Background()
	ref := Ref{GVR: gvrDeploy, Namespace: ns, Name: "app"}
	if ref.String() != "shop/deployments/app" {
		t.Fatal(ref.String())
	}
	obj, err := c.Get(ctx, ref)
	if err != nil || obj.GetName() != "app" || obj.GetResourceVersion() != "1" {
		t.Fatal(obj, err)
	}
	obj.Object["status"] = map[string]any{"readyReplicas": int64(2)}
	full, _ := ExportYAML(obj, false)
	clean, _ := ExportYAML(obj, true)
	for _, field := range []string{"status:", "uid:", "resourceVersion:", "creationTimestamp:"} {
		if !strings.Contains(string(full), field) || strings.Contains(string(clean), field) {
			t.Fatal("clean export", field, string(clean))
		}
	}
	if !strings.Contains(string(clean), "namespace: shop") || !strings.Contains(string(clean), "kind: Deployment") {
		t.Fatal(string(clean))
	}
	docs, err := Decode([]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n---\n\n---\n{\"apiVersion\":\"v1\",\"kind\":\"Secret\",\"metadata\":{\"name\":\"b\"}}\n"))
	if err != nil || len(docs) != 2 || docs[0].GetKind() != "ConfigMap" || docs[1].GetName() != "b" {
		t.Fatal(docs, err)
	}
	if _, err := Decode([]byte("metadata:\n  name: nokind\n")); err == nil {
		t.Fatal("kind required")
	}
	if _, err := Decode([]byte("{not yaml")); err == nil {
		t.Fatal("syntax error")
	}
	if gvr, ok := c.ResourceFor(docs[0]); !ok || gvr.Resource != "configmaps" {
		t.Fatal(gvr, ok)
	}
	// Update with a stale resourceVersion conflicts; with the read one succeeds.
	stale := obj.DeepCopy()
	stale.SetResourceVersion("0")
	if _, err := c.Update(ctx, stale); !apierrors.IsConflict(err) {
		t.Fatal("stale update", err)
	}
	updated, err := c.Update(ctx, obj)
	if err != nil || updated.GetResourceVersion() != "2" || len(f.Puts()) != 1 {
		t.Fatal(err, f.Puts())
	}
	// Apply is a server-side apply patch with our field manager into the target namespace.
	applied, err := c.Apply(ctx, docs[0], ns)
	if err != nil || applied.GetName() != "a" || len(f.Patches()) != 1 || !strings.Contains(f.Patches()[0], "application/apply-patch+yaml") || !strings.Contains(f.Patches()[0], "fieldManager=farshmac") {
		t.Fatal(err, f.Patches())
	}
	if !strings.HasPrefix(f.Patches()[0], "/api/v1/namespaces/shop/configmaps/a") {
		t.Fatal(f.Patches()[0])
	}
	unknown := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "nope/v1", "kind": "Nope", "metadata": map[string]any{"name": "x"}}}
	if _, err := c.Apply(ctx, unknown, ns); err == nil {
		t.Fatal("unknown kind must fail")
	}
	// Delete propagation.
	if err := c.Delete(ctx, ref, false); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, Ref{GVR: gvrPods, Namespace: ns, Name: "loner"}, true); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.Deletes(), " ") != "/apis/apps/v1/namespaces/shop/deployments/app?Background /api/v1/namespaces/shop/pods/loner?Orphan" {
		t.Fatal(f.Deletes())
	}
	if err := c.CreateNamespace(ctx, "created"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteNamespace(ctx, "default"); err != nil || !strings.HasSuffix(f.Deletes()[len(f.Deletes())-1], "/api/v1/namespaces/default?") {
		t.Fatal(err, f.Deletes())
	}
}

func TestLogsAndDescribe(t *testing.T) {
	f := kubetest.New(t)
	kubetest.SeedShop(f)
	f.SetLogs("line one\nline two\n")
	c := testClient(t, f)
	ctx := context.Background()
	r, err := c.Logs(ctx, ns, "app-new-x", LogOptions{Container: "app", TailLines: 10})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(r)
	r.Close()
	if string(data) != f.Logs() {
		t.Fatal(string(data))
	}
	text, err := c.Describe(ctx, Ref{GVR: gvrDeploy, Namespace: ns, Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Name:") || !strings.Contains(text, "app") {
		t.Fatal(text)
	}
	widget, err := c.Describe(ctx, Ref{GVR: schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: "widgets"}, Namespace: ns, Name: "w1"})
	if err != nil || !strings.Contains(widget, "w1") {
		t.Fatal(widget, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.Describe(cancelled, Ref{GVR: gvrDeploy, Namespace: ns, Name: "app"}); err == nil {
		t.Fatal("cancelled describe")
	}
}

func TestShortStatusAndObjectInfo(t *testing.T) {
	pod := &Object{GVR: gvrPods, Name: "p", Columns: []TableColumn{{"NAME", "p"}, {"READY", "1/1"}, {"STATUS", "Running"}, {"AGE", "1d"}}}
	if pod.ShortStatus() != "Running" {
		t.Fatal(pod.ShortStatus())
	}
	dep := &Object{GVR: gvrDeploy, Columns: []TableColumn{{"Name", "d"}, {"Ready", "1/2"}, {"Age", "1d"}}}
	if dep.ShortStatus() != "1/2" {
		t.Fatal(dep.ShortStatus())
	}
	svc := &Object{GVR: schema.GroupVersionResource{Version: "v1", Resource: "services"}, Columns: []TableColumn{{"NAME", "s"}, {"TYPE", "NodePort"}}}
	if svc.ShortStatus() != "NodePort" {
		t.Fatal(svc.ShortStatus())
	}
	crd := &Object{GVR: schema.GroupVersionResource{Group: "x", Version: "v1", Resource: "things"}, Columns: []TableColumn{{"NAME", "t"}, {"AGE", "1d"}, {"PHASE", "Ok"}}}
	if crd.ShortStatus() != "Ok" {
		t.Fatal(crd.ShortStatus())
	}
	if (&Object{Columns: []TableColumn{{"NAME", "x"}}}).ShortStatus() != "" {
		t.Fatal("no status")
	}
	var info os.FileInfo = ObjectInfo{pod}
	if info.Name() != "p" || !info.IsDir() || info.Mode()&os.ModeDir == 0 || info.Sys() != pod || info.Size() != 0 {
		t.Fatal("ObjectInfo")
	}
}

func TestTableDecodeFallsBackToPlainList(t *testing.T) {
	r := Resource{GVR: gvrPods, Kind: "Pod"}
	objects, err := decodeTable([]byte(`{"kind":"PodList","items":[{"metadata":{"name":"a","uid":"u1","ownerReferences":[{"uid":"o"}]},"status":{"phase":"Running"}}]}`), r, ns)
	if err != nil || len(objects) != 1 || objects[0].ShortStatus() != "Running" || objects[0].Owners[0] != "o" || objects[0].Namespace != ns {
		t.Fatal(objects, err)
	}
	// Table rows without metadata still name the object from the NAME column.
	objects, err = decodeTable([]byte(`{"kind":"Table","columnDefinitions":[{"name":"Name"},{"name":"Status"}],"rows":[{"cells":["b","Pending"]},{"cells":[null,null]}]}`), r, ns)
	if err != nil || len(objects) != 1 || objects[0].Name != "b" || objects[0].ShortStatus() != "Pending" {
		t.Fatal(objects, err)
	}
	if resourcePath(gvrDeploy, ns) != "/apis/apps/v1/namespaces/shop/deployments" || resourcePath(gvrPods, "") != "/api/v1/pods" {
		t.Fatal(resourcePath(gvrDeploy, ns))
	}
}

func TestConnectThroughKubeconfig(t *testing.T) {
	f := kubetest.New(t)
	c, err := Connect(context.Background(), Profile{Kubeconfig: f.Kubeconfig(t), Context: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.ServerVersion() == "" {
		t.Fatal("no server version")
	}
}
