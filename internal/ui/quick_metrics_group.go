package ui

import (
	"context"
	"fmt"
	"strings"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"farshmac/internal/metrics"
	"k8s.io/apimachinery/pkg/types"
)

// Walk actual ownerReferences once, including transparent owners. UIDs deduplicate
// shared descendants and terminate cycles; this never enters container filesystems.
func ownedMetricPods(ctx context.Context, s *kube.Snapshot, roots []*kube.Object) (map[string]types.UID, error) {
	children := map[types.UID][]*kube.Object{}
	for _, o := range s.Objects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, uid := range o.Owners {
			children[uid] = append(children[uid], o)
		}
	}
	seen := map[types.UID]bool{}
	pods := map[string]types.UID{}
	queue := append([]*kube.Object(nil), roots...)
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		o := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if o == nil || seen[o.UID] {
			continue
		}
		seen[o.UID] = true
		if isPodObject(o) {
			pods[o.Name] = o.UID
			continue
		}
		queue = append(queue, children[o.UID]...)
	}
	return pods, nil
}
func resolveMetricGroups(ctx context.Context, k *kubePanel, paths []string) (map[string]map[string]types.UID, error) {
	groups := map[string]map[string]types.UID{}
	var add func(string) error
	add = func(p string) error {
		r, err := k.resolve(ctx, p)
		if err != nil {
			return err
		}
		if r.Level == kubeRoot {
			namespaces, err := k.namespaceList(ctx)
			if err != nil {
				return err
			}
			for _, ns := range namespaces {
				if err = add("/" + ns.Name); err != nil {
					return err
				}
			}
			return nil
		}
		if r.Snapshot == nil {
			return fmt.Errorf("no resource snapshot for %s", p)
		}
		roots := r.Objects
		switch r.Level {
		case kubeNamespace:
			roots = nil
			for _, o := range r.Snapshot.Objects {
				if isPodObject(o) {
					roots = append(roots, o)
				}
			}
		case kubeObject, kubeContainers, kubeContainerFS:
			roots = []*kube.Object{r.Object}
		}
		pods, err := ownedMetricPods(ctx, r.Snapshot, roots)
		if err != nil {
			return err
		}
		if groups[r.Namespace] == nil {
			groups[r.Namespace] = map[string]types.UID{}
		}
		for name, uid := range pods {
			groups[r.Namespace][name] = uid
		}
		return nil
	}
	for _, p := range paths {
		if err := add(p); err != nil {
			return nil, err
		}
	}
	return groups, nil
}
func (a *App) openQuickMetricsGroup(p *Panel) {
	paths := p.Selected()
	if len(paths) == 0 {
		paths = []string{p.Path}
	}
	config, profile, settings := p.Kube.client.Config(), p.Kube.profile, a.Settings
	title := strings.Join(paths, ", ") + " · total"
	a.backgroundTask("Resolving metric scope", func(o *fsops.Operation) (func(), func(), error) {
		client, err := kube.NewFromConfig(o.Context(), config, profile)
		if err != nil {
			return nil, nil, err
		}
		k := newKubePanel(profile, client, nil, settings)
		k.hidden = map[string]bool{} // Hidden presentation types must not break ownership chains.
		var nodes []string
		seenNodes := map[string]bool{}
		allNodes := true
		for _, selectedPath := range paths {
			r, e := k.resolve(o.Context(), selectedPath)
			if e != nil {
				k.close()
				return nil, nil, e
			}
			nodeObject := r.Object
			if r.Level == kubeTypeFolder && isNodeObject(r.Parent) {
				nodeObject = r.Parent
			}
			if !isNodeObject(nodeObject) {
				allNodes = false
				break
			}
			if !seenNodes[nodeObject.Name] {
				nodes = append(nodes, nodeObject.Name)
				seenNodes[nodeObject.Name] = true
			}
		}
		if allNodes && len(nodes) > 0 {
			var targets []metrics.Target
			for _, node := range nodes {
				labels := map[string]string{"farshmac_pod": "Node " + node + " · pods"}
				targets = append(targets, metrics.Target{ID: "node-usage/" + node, Name: node + " pod usage", Labels: labels, Fetch: func(ctx context.Context) ([]byte, error) { return client.NodeResourceMetrics(ctx, node) }})
				targets = append(targets, metrics.Target{ID: "node-resources/" + node, Name: node + " resources", Labels: labels, Fetch: func(ctx context.Context) ([]byte, error) { return client.NodeResourceBounds(ctx, node) }})
			}
			session := metrics.NewSession("Nodes · pod usage", targets, nil, k.close)
			session.SetReferenceVisibility(true)
			adopted := false
			return func() {
					adopted = true
					a.startCompactMetrics(session, "Nodes · pod usage")
					a.compactMetrics.enabled[2] = false
				}, func() {
					if !adopted {
						session.Close()
					}
				}, nil
		}
		groups, err := resolveMetricGroups(o.Context(), k, paths)
		if err != nil {
			k.close()
			return nil, nil, err
		}
		count := 0
		for _, pods := range groups {
			count += len(pods)
		}
		if count == 0 {
			k.close()
			return nil, nil, fmt.Errorf("selected scope contains no owned pods")
		}
		target := metrics.Target{ID: "kubernetes-group", Name: title, Labels: map[string]string{"farshmac_pod": title}, Fetch: func(ctx context.Context) ([]byte, error) {
			groups, err := resolveMetricGroups(ctx, k, paths)
			if err != nil {
				return nil, err
			}
			total := kube.ResourceUsage{}
			for ns, pods := range groups {
				v, err := client.ResourceMetricsGroup(ctx, ns, pods)
				if err != nil {
					return nil, err
				}
				total.CPU += v.CPU
				total.Memory += v.Memory
			}
			return []byte(fmt.Sprintf("# TYPE kubernetes_container_cpu_usage_cores gauge\n# TYPE kubernetes_container_memory_usage_bytes gauge\nkubernetes_container_cpu_usage_cores %g\nkubernetes_container_memory_usage_bytes %g\n", total.CPU, total.Memory)), nil
		}}
		session := metrics.NewSession(title, []metrics.Target{target}, nil, k.close)
		session.SetReferenceVisibility(false)
		adopted := false
		return func() { adopted = true; a.startCompactMetrics(session, title); a.compactMetrics.enabled[2] = false }, func() {
			if !adopted {
				session.Close()
			}
		}, nil
	})
}
