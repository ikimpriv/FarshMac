//go:build visualqa

package ui

import (
	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"fmt"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"os"
	"path/filepath"
	"time"
)

func visualOwnershipFixture(count int, chain bool) (*kubeBrowse, []searchHit) {
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

// NewSearchVisualQA uses production ownership formatting and list rendering.
func NewSearchVisualQA(root string) (*App, error) {
	a, err := New(root, root)
	if err != nil {
		return nil, err
	}
	var menu func()
	menu = func() {
		a.choose("Search visual QA", []string{"Deep hierarchy: 200 levels", "Many results: 10,001 rows", "Streaming results: 1,000 files"}, func(i int) {
			if i == 2 {
				dir, err := os.MkdirTemp(root, "stream-")
				if err != nil {
					panic(err)
				}
				for n := 0; n < 1000; n++ {
					if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("result-%05d.txt", n)), []byte(fmt.Sprintf("Streaming search result %d\n", n)), 0600); err != nil {
						panic(err)
					}
				}
				a.Panels[0].Load(dir)
				a.startSearch(a.Panels[0], 0, "Find file", searchOptions{mask: "*.txt"}, func(o *fsops.Operation) (browseStrategy, error) {
					b, err := newLocalBrowse(dir, o)
					return &visualSlowBrowse{browseStrategy: b, o: o}, err
				})
				return
			}
			count := 200
			if i == 1 {
				count = 10001
			}
			b, hits := visualOwnershipFixture(count, i == 0)
			_, labels, _, err := b.ownershipResults(hits)
			b.o.Cancel()
			if err != nil {
				panic(err)
			}
			a.choose(fmt.Sprintf("Found: %d", count), labels, func(int) { menu() })
			a.Form.SearchResults = true
			a.Form.OwnershipTree = true
			a.Form.Message = "F3 View · Enter Go to · Esc Close"
			a.Form.Cancel = menu
		})
		a.dirty = true
	}
	menu()
	return a, nil
}

type visualSlowBrowse struct {
	browseStrategy
	o *fsops.Operation
}

func (b *visualSlowBrowse) ReadDir(path string, visit func(Entry) error) error {
	return b.browseStrategy.ReadDir(path, func(e Entry) error {
		timer := time.NewTimer(40 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-b.o.Context().Done():
			return b.o.Check()
		}
		return visit(e)
	})
}
