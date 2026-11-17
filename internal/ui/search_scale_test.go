package ui

import (
	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"fmt"
	"github.com/mattn/go-runewidth"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"strings"
	"testing"
	"time"
)

func ownershipScaleFixture(count int, chain bool) (*kubeBrowse, []searchHit) {
	s := &kube.Snapshot{Taken: time.Now(), Objects: map[types.UID]*kube.Object{}, ByFolder: map[string][]*kube.Object{}}
	hits := make([]searchHit, 0, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("item-%06d", i)
		obj := &kube.Object{Name: name, UID: types.UID(name), Namespace: "scale", Kind: "Widget", GVR: schema.GroupVersionResource{Version: "v1", Resource: "widgets"}}
		if i > 0 {
			parent := 0
			if chain {
				parent = i - 1
			}
			obj.Owners = []types.UID{types.UID(fmt.Sprintf("item-%06d", parent))}
		}
		s.Objects[obj.UID] = obj
		s.ByFolder["widgets"] = append(s.ByFolder["widgets"], obj)
		hits = append(hits, searchHit{path: "/scale/widgets/" + name, entry: kubeObjectEntry(obj, true)})
	}
	o := fsops.NewOperation("scale")
	k := &kubePanel{snapshots: map[string]*kube.Snapshot{"scale": s}, ttl: time.Hour}
	return &kubeBrowse{o: o, k: k, snapshots: map[string]*kube.Snapshot{"scale": s}}, hits
}

func TestOwnershipLargeWideTree(t *testing.T) {
	b, hits := ownershipScaleFixture(100000, false)
	defer b.o.Cancel()
	selected := hits[90000:]
	start := time.Now()
	rows, labels, limited, err := b.ownershipResults(selected)
	if err != nil || limited || len(rows) != 10001 || len(labels) != len(rows) {
		t.Fatalf("wide tree: rows=%d limited=%v err=%v", len(rows), limited, err)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.path] {
			t.Fatal("duplicate", r.path)
		}
		seen[r.path] = true
	}
	t.Logf("100,000 objects / 10,000 matches: %d rows in %s", len(rows), time.Since(start))
}
func TestOwnershipDeepMatchedChain(t *testing.T) {
	b, hits := ownershipScaleFixture(2000, true)
	defer b.o.Cancel()
	rows, labels, limited, err := b.ownershipResults(hits)
	if err != nil || len(rows) != len(hits) {
		t.Fatal("lost matches", len(rows), err)
	}
	max := 0
	for _, label := range labels {
		if len(label) > max {
			max = len(label)
		}
	}
	if !limited || max > 1024 {
		t.Fatalf("unbounded presentation depth: max label=%d limited=%v", max, limited)
	}
}
func TestOwnershipLargeContextLimit(t *testing.T) {
	b, hits := ownershipScaleFixture(60000, true)
	defer b.o.Cancel()
	selected := []searchHit{}
	for i := 63; i < len(hits); i += 64 {
		selected = append(selected, hits[i])
	}
	rows, _, limited, err := b.ownershipResults(selected)
	if err != nil || !limited {
		t.Fatal("context budget", limited, err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.path] = true
	}
	for _, hit := range selected {
		if !seen[hit.path] {
			t.Fatal("budget hid a match", hit.path)
		}
	}
	if len(rows) > 50000+len(selected) {
		t.Fatal("unbounded context", len(rows))
	}
}
func TestOwnershipLargeCancellation(t *testing.T) {
	b, hits := ownershipScaleFixture(100000, false)
	defer b.o.Cancel()
	done := make(chan error, 1)
	go func() { _, _, _, err := b.ownershipResults(hits[len(hits)-10000:]); done <- err }()
	time.Sleep(time.Millisecond)
	start := time.Now()
	b.o.Cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ignored cancellation")
		}
		t.Logf("cancel latency: %s", time.Since(start))
	case <-time.After(time.Second):
		t.Fatal("cancel too slow")
	}
}
func BenchmarkOwnershipLarge(b *testing.B) {
	for _, n := range []int{10000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			source, hits := ownershipScaleFixture(n, false)
			defer source.o.Cancel()
			hits = hits[len(hits)-10000:]
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _, _, err := source.ownershipResults(hits)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
func BenchmarkKubeCatalogLarge(b *testing.B) {
	for _, n := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			source, _ := ownershipScaleFixture(n, false)
			defer source.o.Cancel()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				source.children = nil
				source.seen = nil
				source.objects = nil
				count := 0
				err := source.ReadDir("/scale/widgets", func(e Entry) error { count++; return nil })
				if err != nil || count != n {
					b.Fatal(err, count)
				}
			}
		})
	}
}

func TestOwnershipLargeResultListNavigation(t *testing.T) {
	source, hits := ownershipScaleFixture(100000, false)
	defer source.o.Cancel()
	_, labels, _, err := source.ownershipResults(hits[90000:])
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Cols, a.Rows = 120, 40
	a.choose("Scale results", labels, func(int) {})
	a.key(Message{Key: "End"})
	a.Draw()
	if a.Form.Focus != len(labels)-1 || a.Form.Scroll == 0 {
		t.Fatal("large result list did not scroll to end")
	}
	if !a.setListFilter("item-099999") || len(a.Form.Choice) != 1 {
		t.Fatal("large result filter failed")
	}
	if a.Form.choiceIndex(0) != len(labels)-1 {
		t.Fatal("filter lost original result index")
	}
	a.setListFilter("")
	a.key(Message{Key: "Home"})
	a.Draw()
	if a.Form.Focus != 0 || a.Form.Scroll != 0 {
		t.Fatal("large result list did not return to start")
	}
}
func BenchmarkOwnershipResultListDraw(b *testing.B) {
	source, hits := ownershipScaleFixture(100000, false)
	defer source.o.Cancel()
	_, labels, _, err := source.ownershipResults(hits[90000:])
	if err != nil {
		b.Fatal(err)
	}
	root := b.TempDir()
	a, err := New(root, root)
	if err != nil {
		b.Fatal(err)
	}
	defer a.Close()
	a.Cols, a.Rows = 120, 40
	a.choose("Scale results", labels, func(int) {})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Form.Focus = i % len(labels)
		a.Draw()
	}
}

func TestSearchResultDeepLabelsRemainVisible(t *testing.T) {
	for _, width := range []int{32, 60, 80, 120} {
		for depth := 1; depth < 64; depth++ {
			label := strings.Repeat("   ", depth) + "└─ Widget item-000199"
			got := searchResultLabel(label, width, true)
			if !strings.Contains(got, "Widget") || runewidth.StringWidth(got) > width {
				t.Fatalf("width %d depth %d: %q", width, depth, got)
			}
		}
	}
}
