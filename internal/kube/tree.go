package kube

import (
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

// Transparent types are owners the tree passes through: a Deployment shows
// the Pods of its ReplicaSets directly, a StatefulSet skips its
// ControllerRevisions. Keyed by "plural.group". Extending this is a code
// change with a test, not a setting, so every user sees the same tree.
var Transparent = map[string]bool{"replicasets.apps": true, "controllerrevisions.apps": true}

// folderPriority is the "Unsorted" order of type folders: the kinds a
// developer reaches for first, then the rest alphabetically.
var folderPriority = []string{"deployments", "statefulsets", "daemonsets", "cronjobs", "jobs", "pods", "services", "ingresses", "configmaps", "secrets", "persistentvolumeclaims", "serviceaccounts"}

// FolderOrder sorts type folders in priority order.
func FolderOrder(folders []string) []string {
	rank := map[string]int{}
	for i, f := range folderPriority {
		rank[f] = i
	}
	out := append([]string(nil), folders...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, oki := rank[FolderKind(out[i])]
		rj, okj := rank[FolderKind(out[j])]
		switch {
		case oki && okj:
			return ri < rj
		case oki != okj:
			return oki
		}
		return out[i] < out[j]
	})
	return out
}

// Folders lists the type folders that hold at least one object, in
// priority order.
func (s *Snapshot) Folders() []string {
	var folders []string
	for folder, objects := range s.ByFolder {
		if len(objects) > 0 {
			folders = append(folders, folder)
		}
	}
	return FolderOrder(folders)
}

// Lookup finds an object by its type folder and name.
func (s *Snapshot) Lookup(folder, name string) (*Object, bool) {
	for _, o := range s.ByFolder[folder] {
		if o.Name == name {
			return o, true
		}
	}
	return nil, false
}

func (s *Snapshot) folderOf(o *Object) string {
	for folder, objects := range s.ByFolder {
		for _, candidate := range objects {
			if candidate == o {
				return folder
			}
		}
	}
	return ""
}

func (s *Snapshot) isTransparent(o *Object) bool {
	return Transparent[resourceKey(o.GVR)]
}

func isPod(o *Object) bool { return o.GVR.Group == "" && o.GVR.Resource == "pods" }

// Children groups the objects owned by parent by type folder (5.2):
// ownerReferences decide, transparent owners are passed through, a Service
// gets the Pods its selector matches, and nothing is shown inside a Pod.
func (s *Snapshot) Children(parent *Object) map[string][]*Object {
	result := map[string][]*Object{}
	if parent == nil || isPod(parent) {
		return result
	}
	seen := map[types.UID]bool{}
	add := func(o *Object) {
		if seen[o.UID] {
			return
		}
		seen[o.UID] = true
		folder := s.folderOf(o)
		if folder == "" {
			folder = o.GVR.Resource
		}
		result[folder] = append(result[folder], o)
	}
	var collect func(owner types.UID, depth int)
	collect = func(owner types.UID, depth int) {
		if depth > 8 {
			return
		}
		for _, o := range s.Objects {
			if !hasOwner(o, owner) {
				continue
			}
			if s.isTransparent(o) {
				collect(o.UID, depth+1)
				continue
			}
			add(o)
		}
	}
	collect(parent.UID, 0)
	if parent.GVR.Group == "" && parent.GVR.Resource == "services" && len(parent.Selector) > 0 {
		for _, pod := range s.ByFolder["pods"] {
			if matchesSelector(pod.Labels, parent.Selector) {
				add(pod)
			}
		}
	}
	for folder := range result {
		sortObjects(result[folder])
	}
	return result
}

func hasOwner(o *Object, owner types.UID) bool {
	if owner == "" {
		return false
	}
	for _, u := range o.Owners {
		if u == owner {
			return true
		}
	}
	return false
}

func matchesSelector(labels, selector map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// HasPods reports whether the object is a Pod or owns Pods (directly or
// through transparent owners or a selector), which decides whether Logs
// apply to it.
func (s *Snapshot) HasPods(o *Object) bool {
	if isPod(o) {
		return true
	}
	return len(s.Children(o)["pods"]) > 0
}

// FolderKind is a human name for a type folder ("deployments.apps" -> "deployments").
func FolderKind(folder string) string {
	if i := strings.Index(folder, "."); i > 0 {
		return folder[:i]
	}
	return folder
}
