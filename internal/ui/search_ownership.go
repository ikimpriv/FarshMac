package ui

import (
	"fmt"
	"path/filepath"
	"sort"

	"farshmac/internal/kube"
)

type ownershipNode struct {
	object   *kube.Object
	hit      searchHit
	matched  bool
	parent   string
	children []string
	order    string
}

func ownershipID(o *kube.Object) string {
	if o.UID != "" {
		return o.Namespace + ":" + string(o.UID)
	}
	return o.Ref().String()
}

// Build the presentation AFTER matching, so grouping neither broadens the
// query nor hides matches. Ancestors come only from the already-read snapshots.
// Each object has one primary parent and is emitted once, including matches
// that are also ancestors of other matches.
func (b *kubeBrowse) ownershipResults(hits []searchHit) ([]searchHit, []string, bool, error) {
	const maxNodes = 50000
	nodes := map[string]*ownershipNode{}
	matched := map[string]searchHit{}
	var extra []searchHit
	for _, h := range hits {
		if e, ok := h.entry.Info.(*kubeEntry); ok && e.object != nil {
			matched[ownershipID(e.object)] = h
		} else {
			extra = append(extra, h)
		}
	}
	// Canonical navigation destinations are independent of the displayed tree.
	paths := map[string]string{}
	for ns, s := range b.snapshots {
		for folder, objects := range s.ByFolder {
			for _, obj := range objects {
				if err := b.o.Check(); err != nil {
					return nil, nil, false, err
				}
				paths[ownershipID(obj)] = filepath.Join("/", ns, folder, obj.Name)
			}
		}
	}
	keys := make([]string, 0, len(matched))
	for id := range matched {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	active := map[string]bool{}
	limited := false
	var add func(*kube.Object, int) string
	add = func(obj *kube.Object, depth int) string {
		id := ownershipID(obj)
		if nodes[id] != nil {
			return id
		}
		_, isMatch := matched[id]
		if len(nodes) >= maxNodes && !isMatch {
			limited = true
			return ""
		}
		hit, found := matched[id]
		if !found {
			hit = searchHit{path: paths[id], entry: kubeObjectEntry(obj, true)}
		}
		if hit.path == "" {
			return ""
		}
		node := &ownershipNode{object: obj, hit: hit, matched: found, order: obj.Namespace + "/" + obj.Kind + "/" + obj.Name + "/" + id}
		nodes[id] = node
		if depth >= 64 {
			limited = true
			return id
		}
		snapshot := b.snapshots[obj.Namespace]
		if snapshot == nil {
			return id
		}
		owners := make([]string, 0, len(obj.Owners))
		for _, uid := range obj.Owners {
			if owner := snapshot.Objects[uid]; owner != nil && owner.Namespace == obj.Namespace && owner.UID != obj.UID {
				owners = append(owners, string(uid))
			}
		}
		sort.Slice(owners, func(i, j int) bool {
			ci, cj := owners[i] == string(obj.ControllerOwner), owners[j] == string(obj.ControllerOwner)
			if ci != cj {
				return ci
			}
			return owners[i] < owners[j]
		})
		active[id] = true
		defer delete(active, id)
		for _, uid := range owners {
			var owner *kube.Object
			for _, candidate := range obj.Owners {
				if string(candidate) == uid {
					owner = snapshot.Objects[candidate]
					break
				}
			}
			if owner == nil || active[ownershipID(owner)] {
				continue
			}
			node.parent = add(owner, depth+1)
			break
		}
		return id
	}
	for _, id := range keys {
		if err := b.o.Check(); err != nil {
			return nil, nil, false, err
		}
		obj := matched[id].entry.Info.(*kubeEntry).object
		add(obj, 0)
	}
	var roots []string
	for id, node := range nodes {
		if node.parent != "" && nodes[node.parent] != nil {
			nodes[node.parent].children = append(nodes[node.parent].children, id)
		} else {
			roots = append(roots, id)
		}
	}
	less := func(a, c string) bool { return nodes[a].order < nodes[c].order }
	sort.Slice(roots, func(i, j int) bool { return less(roots[i], roots[j]) })
	out := make([]searchHit, 0, len(nodes)+len(extra))
	labels := make([]string, 0, len(nodes)+len(extra))
	type frame struct {
		id, prefix, branch string
		depth              int
	}
	stack := make([]frame, 0, len(roots))
	for i := len(roots) - 1; i >= 0; i-- {
		stack = append(stack, frame{id: roots[i]})
	}
	for len(stack) > 0 {
		if err := b.o.Check(); err != nil {
			return nil, nil, false, err
		}
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		continuation := f.depth >= 64
		if continuation {
			limited = true
			f.prefix = ""
			f.branch = ""
			f.depth = 0
		}
		n := nodes[f.id]
		label := n.object.Kind + " " + n.object.Name
		if f.branch == "" {
			label = n.object.Namespace + " / " + label
		}
		if !n.matched {
			label += " [owner]"
		}
		if continuation {
			label += " [continued]"
		}
		out = append(out, n.hit)
		labels = append(labels, f.prefix+f.branch+label)
		children := n.children
		sort.Slice(children, func(i, j int) bool { return less(children[i], children[j]) })
		next := f.prefix
		if f.branch == "├─ " {
			next += "│  "
		} else if f.branch != "" {
			next += "   "
		}
		for i := len(children) - 1; i >= 0; i-- {
			edge := "├─ "
			if i == len(children)-1 {
				edge = "└─ "
			}
			stack = append(stack, frame{id: children[i], prefix: next, branch: edge, depth: f.depth + 1})
		}
	}
	for _, h := range extra {
		out = append(out, h)
		labels = append(labels, fmt.Sprintf("%s [folder]", h.path))
	}
	return out, labels, limited, nil
}
