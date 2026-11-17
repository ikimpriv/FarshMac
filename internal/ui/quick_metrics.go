package ui

import (
	"sort"
	"strings"

	"farshmac/internal/docker"
	"farshmac/internal/kube"
	"farshmac/internal/metrics"
)

// Discover each pod once even when multiple containers of it are selected.
// A whole-pod selection subsumes its individual container selections.
func quickMetricRefs(refs []kube.ContainerRef) ([]kube.ContainerRef, map[string][]string) {
	var out []kube.ContainerRef
	filters := map[string][]string{}
	seen := map[string]bool{}
	whole := map[string]bool{}
	for _, ref := range refs {
		key := ref.Namespace + "/" + ref.Pod
		if !seen[key] {
			seen[key] = true
			r := ref
			r.Container = ""
			out = append(out, r)
		}
		if ref.Container == "" {
			whole[key] = true
		} else {
			filters[key] = append(filters[key], ref.Container)
		}
	}
	for key := range whole {
		delete(filters, key)
	}
	return out, filters
}
func dockerMetricSelection(p *Panel) []docker.Container {
	if p.Docker == nil || strings.HasPrefix(p.Path, "/Volumes") {
		return nil
	}
	var out []docker.Container
	seen := map[string]bool{}
	add := func(name string) {
		if c, _, _, ok := p.Docker.location(name); ok && c.State == "running" && !seen[c.ID] {
			seen[c.ID] = true
			out = append(out, c)
		}
	}
	if _, _, _, ok := p.Docker.location(p.Path); ok {
		add(p.Path)
	} else {
		for _, name := range p.Selected() {
			add(name)
		}
	}
	return out
}
func (a *App) toggleQuickMetrics() {
	if a.compactMetrics != nil {
		a.stopCompactMetrics(true)
		return
	}
	p := &a.Panels[a.Active]
	if p.Kube != nil {
		refs := metricSelection(p)
		if len(refs) == 0 {
			a.Status = "Select pods or containers for Quick Live Metrics"
			return
		}
		if len(refs) > 32 {
			a.Status = "Select at most 32 pods/containers"
			return
		}
		a.openMetrics(metricsLaunch{Config: p.Kube.client.Config(), Profile: p.Kube.profile, Ref: refs[0], Refs: refs, Text: true})
		return
	}
	if p.Docker != nil {
		containers := dockerMetricSelection(p)
		if len(containers) > 0 {
			if len(containers) > 32 {
				a.Status = "Select at most 32 containers"
				return
			}
			a.dockerMetrics(p.Docker, containers, true)
			return
		}
	}
	a.Status = "Quick Live Metrics: select Kubernetes pods/containers or running Docker containers"
}
func metricScope(d metrics.Descriptor) string {
	labels := d.Labels
	parts := []string{}
	for _, key := range []string{"farshmac_namespace", "farshmac_pod", "farshmac_container"} {
		if labels[key] != "" {
			parts = append(parts, labels[key])
		}
	}
	if len(parts) == 0 {
		return d.Target
	}
	return strings.Join(parts, "/")
}
func quickSeriesIDs(catalog []metrics.Descriptor) []string {
	return quickSeriesIDsForMemory(catalog, 0)
}
func quickSeriesIDsForMemory(catalog []metrics.Descriptor, memoryMode int) []string {
	chosen := map[string]metrics.Descriptor{}
	priority := map[string]int{}
	for _, d := range catalog {
		group := basicMetric(d.Name)
		if group < 0 || group == 1 && !quickMemoryMatches(d.Name, memoryMode) {
			continue
		}
		key := string(rune('0'+group)) + "/" + metricScope(d)
		p := 1
		if strings.HasPrefix(d.Name, "podtoast_") {
			p = 3
		} else if strings.HasPrefix(d.Name, "container_") {
			p = 2
		}
		if old, ok := chosen[key]; !ok || p > priority[key] || p == priority[key] && d.ID < old.ID {
			chosen[key] = d
			priority[key] = p
		}
	}
	keys := make([]string, 0, len(chosen))
	for key := range chosen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ids := make([]string, 0, len(keys))
	for _, key := range keys {
		ids = append(ids, chosen[key].ID)
	}
	return ids
}

func quickMemoryMatches(name string, mode int) bool {
	switch mode {
	case 0:
		return name == "podtoast_memory_working_set_bytes" || name == "container_memory_working_set_bytes" || name == "kubernetes_container_memory_usage_bytes"
	case 1:
		return name == "podtoast_memory_anonymous_bytes"
	case 2:
		return name == "podtoast_memory_file_cache_bytes" || name == "container_memory_cache"
	case 3:
		return name == "podtoast_memory_usage_bytes" || name == "container_memory_usage_bytes"
	}
	return false
}
