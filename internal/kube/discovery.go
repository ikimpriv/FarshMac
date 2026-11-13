package kube

import (
	"context"
	"sort"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// Resource is a namespaced, listable resource type of the cluster.
type Resource struct {
	GVR        schema.GroupVersionResource
	Kind       string
	Singular   string
	Namespaced bool
	Verbs      []string
	// Folder is the type-folder name in the panel: the plural, or
	// plural.group when the same plural exists in several groups (C2).
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
	discoveryMu.Lock()
	defer discoveryMu.Unlock()
	if c.resources != nil {
		return c.resources.list, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lists, err := c.disc.ServerPreferredNamespacedResources()
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
			if strings.Contains(r.Name, "/") || !r.Namespaced || strings.HasSuffix(gv.Group, "metrics.k8s.io") {
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
			all = append(all, Resource{GVR: schema.GroupVersionResource{Group: group, Version: version, Resource: r.Name}, Kind: r.Kind, Singular: r.SingularName, Namespaced: true, Verbs: append([]string(nil), r.Verbs...)})
		}
	}
	c.resources = buildResourceCache(all)
	return c.resources.list, nil
}

// buildResourceCache names type folders: the plural alone, unless several
// groups share it.
func buildResourceCache(all []Resource) *resourceCache {
	count := map[string]int{}
	for _, r := range all {
		count[r.GVR.Resource]++
	}
	cache := &resourceCache{byFolder: map[string]Resource{}}
	for _, r := range all {
		r.Folder = r.GVR.Resource
		if count[r.GVR.Resource] > 1 {
			r.Folder = resourceKey(r.GVR)
		}
		if _, dup := cache.byFolder[r.Folder]; dup {
			continue
		}
		cache.byFolder[r.Folder] = r
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
