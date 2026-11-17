package ui

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"k8s.io/apimachinery/pkg/types"
)

type kubeBrowse struct {
	k                    *kubePanel
	o                    *fsops.Operation
	objects              map[string]*kube.Object
	snapshots            map[string]*kube.Snapshot
	children             map[*kube.Snapshot]map[types.UID]bool
	seen                 map[string]bool
	initialized, catalog bool
}

func (b *kubeBrowse) ReadDir(dir string, visit func(Entry) error) error {
	if err := b.o.Check(); err != nil {
		return err
	}
	// Check the virtual boundary BEFORE resolve: resolving a pod may fetch its
	// summary or transparently enter its single container.
	segs := kubeSegments(dir)
	for i := 1; i+1 < len(segs); i += 2 {
		if segs[i] == "pods" {
			return nil
		}
	}
	r, err := b.k.resolve(b.o.Context(), dir)
	if err != nil {
		return err
	}
	if r.Snapshot != nil {
		if b.children == nil {
			b.children = map[*kube.Snapshot]map[types.UID]bool{}
		}
		if b.children[r.Snapshot] == nil {
			owners := map[types.UID]bool{}
			for _, obj := range r.Snapshot.Objects {
				if err := b.o.Check(); err != nil {
					return err
				}
				for _, uid := range obj.Owners {
					owners[uid] = true
				}
			}
			b.children[r.Snapshot] = owners
		}
	}
	if r.Snapshot != nil {
		if b.snapshots == nil {
			b.snapshots = map[string]*kube.Snapshot{}
		}
		b.snapshots[r.Namespace] = r.Snapshot
	}
	if !b.initialized {
		b.initialized = true
		b.catalog = r.Level == kubeRoot || r.Level == kubeNamespace || r.Level == kubeTypeFolder
	}
	emit := func(e Entry) error {
		if err := b.o.Check(); err != nil {
			return err
		}
		if ke, ok := e.Info.(*kubeEntry); ok && ke.object != nil {
			identity := string(ke.object.UID)
			if identity == "" {
				identity = ke.object.Ref().String()
			}
			if b.seen == nil {
				b.seen = map[string]bool{}
			}
			if b.seen[identity] {
				return nil
			}
			b.seen[identity] = true
			if b.objects == nil {
				b.objects = map[string]*kube.Object{}
			}
			b.objects[filepath.Join(dir, e.Name)] = ke.object
		}
		return visit(e)
	}
	switch r.Level {
	case kubeRoot:
		list, err := b.k.namespaceList(b.o.Context())
		if err != nil {
			return err
		}
		for _, ns := range list {
			if err := emit(kubeObjectEntry(ns, true)); err != nil {
				return err
			}
		}
	case kubeNamespace:
		for _, folder := range r.Snapshot.Folders() {
			if err := emit(kubeFolderEntry(folder, len(r.Snapshot.ByFolder[folder]))); err != nil {
				return err
			}
		}
	case kubeTypeFolder:
		for _, obj := range r.Objects {
			if err := emit(kubeObjectEntry(obj, isNodeObject(obj) || isPodObject(obj) || b.children[r.Snapshot][obj.UID] || obj.GVR.Resource == "services" && len(obj.Selector) > 0)); err != nil {
				return err
			}
		}
	case kubeObject:
		children := r.Snapshot.Children(r.Object)
		var names []string
		for name := range children {
			names = append(names, name)
		}
		for _, name := range kube.FolderOrder(names) {
			if err := emit(kubeFolderEntry(name, len(children[name]))); err != nil {
				return err
			}
		}
	}
	return nil
}
func (b *kubeBrowse) Descend(_ string, e Entry) bool {
	if ke, ok := e.Info.(*kubeEntry); ok && ke.object != nil {
		if isPodObject(ke.object) || isNodeObject(ke.object) {
			return false
		}
		if b.catalog && ke.object.GVR.Resource != "namespaces" {
			return false
		}
	}
	return e.Dir
}
func (b *kubeBrowse) Open(path string) (io.ReadCloser, error) {
	obj := b.objects[path]
	if obj == nil {
		return nil, fmt.Errorf("not a resource object")
	}
	text, err := b.k.client.Describe(b.o.Context(), obj.Ref())
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(text)), nil
}
func (b *kubeBrowse) Close() error { return nil }

type dockerBrowse struct {
	d *dockerPanel
	o *fsops.Operation
}

func (b *dockerBrowse) ReadDir(dir string, visit func(Entry) error) error {
	// The catalog strategy never invokes a container or attaches a toolbox.
	parts := strings.Split(strings.Trim(dir, "/"), "/")
	allowed := dir == "/" || len(parts) == 1 || len(parts) == 2 && parts[0] == "Compose"
	if !allowed {
		return nil
	}
	p := Panel{Docker: b.d, Path: dir, readOperation: b.o}
	if err := p.loadDocker(dir); err != nil {
		return err
	}
	for _, e := range p.Entries {
		if e.Name == ".." {
			continue
		}
		if err := b.o.Check(); err != nil {
			return err
		}
		if err := visit(e); err != nil {
			return err
		}
	}
	return nil
}
func (b *dockerBrowse) Descend(path string, e Entry) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return e.Dir && (len(parts) == 1 || len(parts) == 2 && parts[0] == "Compose")
}
func (b *dockerBrowse) Open(path string) (io.ReadCloser, error) {
	text, err := b.d.describe(b.o.Context(), path)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(text)), nil
}
func (b *dockerBrowse) Close() error { return nil }
