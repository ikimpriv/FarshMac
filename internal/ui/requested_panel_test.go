package ui

import (
	"context"
	"farshmac/internal/kube"
	"k8s.io/apimachinery/pkg/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSearchSkipsHiddenSubtreesAndSelectsMask(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, ".hidden"), 0700)
	os.WriteFile(filepath.Join(root, ".hidden", "inside.txt"), nil, 0600)
	os.WriteFile(filepath.Join(root, "visible.txt"), nil, 0600)
	os.WriteFile(filepath.Join(root, ".secret.txt"), nil, 0600)
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Settings.ShowHidden = false
	a.findFiles(false)
	if !a.Form.Selected {
		t.Fatal("mask is not selected")
	}
	a.key(Message{Key: "v"})
	if a.Form.Fields[0].Value != "v" {
		t.Fatal("typing did not replace mask", a.Form.Fields[0].Value)
	}
	a.Form.Fields[0].Value = "*"
	a.key(Message{Key: "Enter"})
	waitWork(t, a)
	if len(a.searchSession.hits) != 1 || a.searchSession.hits[0].entry.Name != "visible.txt" {
		t.Fatal("hidden subtree leaked into search", a.Form.Choice)
	}
	a.closeSearch()
	a.Settings.ShowHidden = true
	a.findFiles(false)
	a.key(Message{Key: "Enter"})
	waitWork(t, a)
	if len(a.searchSession.hits) != 4 {
		t.Fatal("show hidden should include hidden results", a.Form.Choice)
	}
}
func TestKubeLifecycleColorsAndSorting(t *testing.T) {
	for state, want := range map[string]int{"Running": 0, "Pending": 2, "Completed": 1, "Terminating": 1, "Succeeded": 1, "terminated:0": 1, "terminated:137": 2, "waiting:ImagePullBackOff": 2, "CrashLoopBackOff": 2, "Failed": 2, "OOMKilled": 2} {
		if got := kubeStateTone(state); got != want {
			t.Fatalf("%s: %d", state, got)
		}
	}
	old := &kube.Object{Name: "old", Created: time.Now().Add(-time.Hour), Columns: []kube.TableColumn{{Name: "STATUS", Value: "Completed"}}}
	young := &kube.Object{Name: "young", Created: time.Now(), Columns: []kube.TableColumn{{Name: "STATUS", Value: "Running"}}}
	if kubeStatusFolderEntry("pods", []*kube.Object{old}).Info.(*kubeEntry).stateTone != 1 {
		t.Fatal("terminal folder not dimmed")
	}
	if kubeStatusFolderEntry("pods", []*kube.Object{old, young}).Info.(*kubeEntry).stateTone != 0 {
		t.Fatal("mixed folder dimmed")
	}
	p := Panel{}
	p.Sort = "Age"
	entries := []Entry{kubeObjectEntry(old, true), kubeObjectEntry(young, true)}
	p.sortEntries(entries)
	if entries[0].Name != "young" {
		t.Fatal("age ordering")
	}
	p.Reverse = true
	p.sortEntries(entries)
	if entries[0].Name != "old" {
		t.Fatal("reverse age")
	}
	p.Sort = "State"
	p.Reverse = false
	p.sortEntries(entries)
	if entries[0].Name != "old" {
		t.Fatal("state ordering")
	}
}
func TestMetricOwnershipScope(t *testing.T) {
	a, _ := kubeApp(t)
	k := a.Panels[0].Kube
	groups, err := resolveMetricGroups(context.Background(), k, []string{"/shop/deployments.apps/app", "/shop/pods"})
	if err != nil {
		t.Fatal(err)
	}
	// Deployment -> ReplicaSet -> Pod, plus overlapping direct pod scope.
	if len(groups["shop"]) < 2 {
		t.Fatal("missing transitive pods", groups)
	}
	group, err := resolveMetricGroups(context.Background(), k, []string{"/shop/deployments.apps/app"})
	if err != nil {
		t.Fatal(err)
	}
	for name := range group["shop"] {
		if !strings.HasPrefix(name, "app-") && name != "shared" {
			t.Fatal("foreign pod", name)
		}
	}
	if len(group["shop"]) != 3 {
		t.Fatal("expected both ReplicaSet pods plus shared pod", group)
	}
	b, hits := ownershipScaleFixture(1000, true)
	defer b.o.Cancel()
	s := b.snapshots["scale"]
	pod := hits[999].entry.Info.(*kubeEntry).object
	pod.GVR.Resource = "pods"
	pod.Kind = "Pod"
	roots := []*kube.Object{s.Objects[types.UID("item-000000")], s.Objects[types.UID("item-000005")]}
	pods, err := ownedMetricPods(context.Background(), s, roots)
	if err != nil || len(pods) != 1 {
		t.Fatal("deep/overlapping ownership", pods, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ownedMetricPods(ctx, s, roots); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestQuickMetricsNamespaceStartsAggregate(t *testing.T) {
	a, _ := kubeApp(t)
	selectEntry(t, &a.Panels[0], "shop")
	a.toggleQuickMetrics()
	waitWork(t, a)
	if a.compactMetrics == nil || !strings.Contains(a.compactMetrics.title, "/shop") || a.compactMetrics.enabled[2] {
		t.Fatal("namespace did not open CPU/memory aggregate", a.Status)
	}
	a.stopCompactMetrics(true)
	enterKube(t, a, "shop")
	a.sortMenu(0)
	labels := strings.Join(a.Form.Choice, "|")
	if !strings.Contains(labels, "Age") || !strings.Contains(labels, "State") {
		t.Fatal("missing Kubernetes sort modes", labels)
	}
}

func TestKubeLifecycleColorsReachGrid(t *testing.T) {
	a := formTestApp(t)
	a.Panels[0].Entries = []Entry{
		kubeObjectEntry(&kube.Object{Name: "active", Columns: []kube.TableColumn{{Name: "STATUS", Value: "Running"}}}, true),
		kubeObjectEntry(&kube.Object{Name: "dimmed-pod", Columns: []kube.TableColumn{{Name: "STATUS", Value: "Completed"}}}, true),
		kubeObjectEntry(&kube.Object{Name: "failed-pod", Columns: []kube.TableColumn{{Name: "STATUS", Value: "CrashLoopBackOff"}}}, true),
	}
	a.Panels[0].Entries = append(a.Panels[0].Entries, kubeObjectEntry(&kube.Object{Name: "pending-pod", Columns: []kube.TableColumn{{Name: "STATUS", Value: "Pending"}}}, true))
	a.Panels[0].Cursor = 0
	g := a.Draw()
	for name, want := range map[string]uint32{"dimmed-pod": 0x888899, "failed-pod": 0xff5555, "pending-pod": 0xff5555} {
		found := false
		for y := 0; y < g.H; y++ {
			line := gridLine(g, y)
			if x := strings.Index(line, name); x >= 0 {
				found = true
				if g.Cells[y*g.W+x].FG != want {
					t.Fatalf("%s was not colored", name)
				}
			}
		}
		if !found {
			t.Fatal("missing row", name)
		}
	}
}
