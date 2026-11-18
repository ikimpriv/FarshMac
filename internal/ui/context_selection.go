package ui

import (
	"farshmac/internal/kube"
	"path/filepath"
)

// Resolve marked objects, never silently substitute the cursor for a selection.
func kubeContextTargets(p *Panel) []kube.ContainerRef {
	if p.Kube == nil {
		return nil
	}
	r := p.kubeLevel()
	selected := p.Selected()
	if r.Level == kubeTypeFolder {
		var refs []kube.ContainerRef
		for _, selectedPath := range selected {
			found := false
			for _, e := range p.Entries {
				if e.Name == filepath.Base(selectedPath) {
					ke, ok := e.Info.(*kubeEntry)
					if !ok || !isPodObject(ke.object) {
						return nil
					}
					refs = append(refs, kube.ContainerRef{Namespace: ke.object.Namespace, Pod: ke.object.Name})
					found = true
					break
				}
			}
			if !found {
				return nil
			}
		}
		return refs
	}
	if r.Level == kubeContainers && len(selected) > 0 {
		var refs []kube.ContainerRef
		for _, name := range selected {
			refs = append(refs, kube.ContainerRef{Namespace: r.Namespace, Pod: r.Object.Name, Container: containerName(filepath.Base(name))})
		}
		return refs
	}
	if len(selected) > 1 {
		return nil
	} // Files are not a selection of pod targets.
	if ref, ok := podToastTarget(p); ok {
		return []kube.ContainerRef{ref}
	}
	return nil
}
