package kube

import (
	"context"
	"sort"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// Resource is a listable resource type of the cluster.
type Resource struct {
	GVR        schema.GroupVersionResource
	Kind       string
	Singular   string
	Namespaced bool
	Verbs      []string
	// Folder is the type-folder name in the panel: the plural, or
	// plural.group on collisions, except the preferred built-in API type.
	Folder string
}

// Key identifies the resource as "plural.group" ("pods", "deployments.apps").
func (r Resource) Key() string { return resourceKey(r.GVR) }

func resourceKey(gvr schema.GroupVersionResource) string {
	if gvr.Group == "" {
		return gvr.Resource
	}
	return gvr.Resource + "." + gvr.Group
}

type resourceCache struct {
	list     []Resource
	byFolder map[string]Resource
}

var discoveryMu sync.Mutex

// Resources lists the namespaced resources that can be listed and read,
// without subresources and metrics. The answer is cached per client.
func (c *Client) Resources(ctx context.Context) ([]Resource, error) {
	return c.resourcesForScope(ctx, true)
}

func (c *Client) resourcesForScope(ctx context.Context, namespaced bool) ([]Resource, error) {
	all, err := c.discoverResources(ctx)
	if err != nil {
		return nil, err
	}
	var result []Resource
	for _, r := range all {
		if r.Namespaced == namespaced {
			result = append(result, r)
		}
	}
	return result, nil
}

func (c *Client) discoverResources(ctx context.Context) ([]Resource, error) {
	discoveryMu.Lock()
	epoch := c.ConnectionEpoch()
	if c.resources != nil && c.resourcesEpoch == epoch {
		list := c.resources.list
		discoveryMu.Unlock()
		return list, nil
	}
	discoveryMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	disc, err := discovery.NewDiscoveryClientForConfig(contextualConfig(c.config, ctx))
	if err != nil {
		return nil, err
	}
	lists, err := disc.ServerPreferredResources()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil && !discovery.IsGroupDiscoveryFailedError(err) {
		return nil, err
	}
	var all []Resource
	for _, list := range lists {
		gv, e := schema.ParseGroupVersion(list.GroupVersion)
		if e != nil {
			continue
		}
		for _, r := range list.APIResources {
			if strings.Contains(r.Name, "/") || strings.HasSuffix(gv.Group, "metrics.k8s.io") {
				continue
			}
			if !hasVerb(r.Verbs, "list") || !hasVerb(r.Verbs, "get") {
				continue
			}
			group := r.Group
			if group == "" {
				group = gv.Group
			}
			version := r.Version
			if version == "" {
				version = gv.Version
			}
			all = append(all, Resource{GVR: schema.GroupVersionResource{Group: group, Version: version, Resource: r.Name}, Kind: r.Kind, Singular: r.SingularName, Namespaced: r.Namespaced, Verbs: append([]string(nil), r.Verbs...)})
		}
	}
	discoveryMu.Lock()
	defer discoveryMu.Unlock()
	c.resourcesEpoch = epoch
	c.resources = buildResourceCache(all)
	return c.resources.list, nil
}

// buildResourceCache keeps built-in short names and qualifies colliding extensions.
func buildResourceCache(all []Resource) *resourceCache {
	count := map[string]int{}
	preferred := map[string]string{}
	// Built-in API groups retain familiar kubectl names when CRDs collide.
	groups := []string{"", "apps", "batch", "autoscaling", "networking.k8s.io", "policy", "rbac.authorization.k8s.io", "storage.k8s.io", "scheduling.k8s.io", "apiextensions.k8s.io", "admissionregistration.k8s.io", "coordination.k8s.io", "discovery.k8s.io", "certificates.k8s.io"}
	for _, group := range groups {
		for _, r := range all {
			if r.GVR.Group == group {
				if _, ok := preferred[r.GVR.Resource]; !ok {
					preferred[r.GVR.Resource] = group
				}
			}
		}
	}
	for _, r := range all {
		count[r.GVR.Resource]++
	}
	cache := &resourceCache{byFolder: map[string]Resource{}}
	for _, r := range all {
		r.Folder = r.GVR.Resource
		if group, ok := preferred[r.GVR.Resource]; count[r.GVR.Resource] > 1 && (!ok || group != r.GVR.Group) {
			r.Folder = resourceKey(r.GVR)
		}
		if _, dup := cache.byFolder[r.Folder]; dup {
			continue
		}
		cache.byFolder[r.Folder] = r
		cache.byFolder[r.Key()] = r
		cache.list = append(cache.list, r)
	}
	sort.Slice(cache.list, func(i, j int) bool { return cache.list[i].Folder < cache.list[j].Folder })
	return cache
}

// ResourceByFolder finds the resource behind a type-folder name. Resources
// must have been called first.
func (c *Client) ResourceByFolder(folder string) (Resource, bool) {
	discoveryMu.Lock()
	defer discoveryMu.Unlock()
	if c.resources == nil {
		return Resource{}, false
	}
	r, ok := c.resources.byFolder[folder]
	return r, ok
}

func hasVerb(verbs []string, verb string) bool {
	for _, v := range verbs {
		if v == verb {
			return true
		}
	}
	return false
}
