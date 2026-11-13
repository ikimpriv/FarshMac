package kube

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

// fakeAPI is a hand-written API server: discovery, LIST (Table or plain),
// GET/PUT/PATCH/DELETE, namespaces and logs. Objects are stored by
// collection path; a "_columns" map on an object renders as Table columns.
type fakeAPI struct {
	mu        sync.Mutex
	server    *httptest.Server
	objects   map[string][]map[string]any // "/apis/apps/v1/namespaces/ns/deployments" -> items
	forbidden map[string]bool             // collection paths answering 403
	deletes   []string                    // "path?policy"
	puts      []string
	patches   []string
	logs      string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{objects: map[string][]map[string]any{}, forbidden: map[string]bool{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	f.add("/api/v1/namespaces", obj("Namespace", "", "default", "ns-default", nil, nil, map[string]string{"STATUS": "Active"}))
	f.add("/api/v1/namespaces", obj("Namespace", "", "shop", "ns-shop", nil, nil, map[string]string{"STATUS": "Active"}))
	return f
}

func (f *fakeAPI) add(collection string, items ...map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// apiVersion follows the collection: /apis/apps/v1/... -> apps/v1.
	parts := strings.Split(strings.TrimPrefix(collection, "/"), "/")
	apiVersion := "v1"
	if parts[0] == "apis" {
		apiVersion = parts[1] + "/" + parts[2]
	}
	for _, it := range items {
		it["apiVersion"] = apiVersion
	}
	f.objects[collection] = append(f.objects[collection], items...)
}

// obj builds an object; owners are UIDs, columns render in Table form.
func obj(kind, ns, name, uid string, owners []string, labels map[string]string, columns map[string]string) map[string]any {
	meta := map[string]any{"name": name, "uid": uid, "resourceVersion": "1", "creationTimestamp": "2024-01-02T03:04:05Z"}
	if ns != "" {
		meta["namespace"] = ns
	}
	if labels != nil {
		meta["labels"] = labels
	}
	if len(owners) > 0 {
		var refs []any
		for _, o := range owners {
			refs = append(refs, map[string]any{"uid": o, "kind": "Owner", "name": "owner", "apiVersion": "v1"})
		}
		meta["ownerReferences"] = refs
	}
	o := map[string]any{"kind": kind, "apiVersion": "v1", "metadata": meta}
	if columns != nil {
		o["_columns"] = columns
	}
	return o
}

func status(w http.ResponseWriter, code int, reason, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"kind": "Status", "apiVersion": "v1", "status": "Failure", "message": message, "reason": reason, "code": code})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func resources(groupVersion string, items ...map[string]any) map[string]any {
	return map[string]any{"kind": "APIResourceList", "apiVersion": "v1", "groupVersion": groupVersion, "resources": items}
}
func res(name, singular, kind string, namespaced bool, verbs ...string) map[string]any {
	if verbs == nil {
		verbs = []string{"get", "list", "create", "update", "patch", "delete"}
	}
	return map[string]any{"name": name, "singularName": singular, "kind": kind, "namespaced": namespaced, "verbs": verbs}
}
func group(name, version string) map[string]any {
	gv := map[string]any{"groupVersion": name + "/" + version, "version": version}
	return map[string]any{"name": name, "versions": []any{gv}, "preferredVersion": gv}
}

var discoveryRoutes = map[string]any{
	"/version": map[string]any{"gitVersion": "v1.30.0-fake", "major": "1", "minor": "30"},
	"/api":     map[string]any{"kind": "APIVersions", "versions": []string{"v1"}},
	"/api/v1": resources("v1",
		res("namespaces", "namespace", "Namespace", false),
		res("pods", "pod", "Pod", true),
		res("pods/log", "", "Pod", true, "get"),
		res("services", "service", "Service", true),
		res("configmaps", "configmap", "ConfigMap", true),
		res("secrets", "secret", "Secret", true),
		res("events", "event", "Event", true),
		res("bindings", "binding", "Binding", true, "create"),
	),
	"/apis": map[string]any{"kind": "APIGroupList", "groups": []any{group("apps", "v1"), group("batch", "v1"), group("metrics.k8s.io", "v1beta1"), group("example.com", "v1"), group("other.io", "v1")}},
	"/apis/apps/v1": resources("apps/v1",
		res("deployments", "deployment", "Deployment", true),
		res("replicasets", "replicaset", "ReplicaSet", true),
		res("statefulsets", "statefulset", "StatefulSet", true),
		res("controllerrevisions", "controllerrevision", "ControllerRevision", true),
		res("daemonsets", "daemonset", "DaemonSet", true),
	),
	"/apis/batch/v1":               resources("batch/v1", res("jobs", "job", "Job", true), res("cronjobs", "cronjob", "CronJob", true)),
	"/apis/metrics.k8s.io/v1beta1": resources("metrics.k8s.io/v1beta1", res("pods", "pod", "PodMetrics", true, "get", "list")),
	"/apis/example.com/v1":         resources("example.com/v1", res("widgets", "widget", "Widget", true), res("gadgets", "gadget", "Gadget", false)),
	"/apis/other.io/v1":            resources("other.io/v1", res("deployments", "deployment", "Deployment", true)),
}

func (f *fakeAPI) handle(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if v, ok := discoveryRoutes[path]; ok && r.Method == http.MethodGet {
		writeJSON(w, v)
		return
	}
	if strings.HasSuffix(path, "/log") && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/plain")
		f.mu.Lock()
		fmt.Fprint(w, f.logs)
		f.mu.Unlock()
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.forbidden[path] {
		status(w, 403, "Forbidden", "forbidden: User cannot list resource")
		return
	}
	// Collection?
	if items, ok := f.objects[path]; ok && r.Method == http.MethodGet {
		if strings.Contains(r.Header.Get("Accept"), "as=Table") {
			writeJSON(w, table(items, r.URL.Query().Get("includeObject")))
			return
		}
		out := make([]any, 0, len(items))
		for _, it := range items {
			out = append(out, stripColumns(it))
		}
		writeJSON(w, map[string]any{"kind": "List", "apiVersion": "v1", "items": out})
		return
	}
	// Single object.
	i := strings.LastIndex(path, "/")
	collection, name := path[:i], path[i+1:]
	items, ok := f.objects[collection]
	if !ok {
		if r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/namespaces/") {
			status(w, 404, "NotFound", "namespace not found")
			return
		}
		if r.Method == http.MethodPost && path == "/api/v1/namespaces" {
			f.objects[path] = append(f.objects[path], obj("Namespace", "", "created", "ns-created", nil, nil, nil))
			writeJSON(w, stripColumns(f.objects[path][len(f.objects[path])-1]))
			return
		}
		status(w, 404, "NotFound", "unknown path "+path)
		return
	}
	index := -1
	for j, it := range items {
		if it["metadata"].(map[string]any)["name"] == name {
			index = j
		}
	}
	switch r.Method {
	case http.MethodGet:
		if index < 0 {
			status(w, 404, "NotFound", name+" not found")
			return
		}
		writeJSON(w, stripColumns(items[index]))
	case http.MethodPut:
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		if index < 0 {
			status(w, 404, "NotFound", name+" not found")
			return
		}
		have := items[index]["metadata"].(map[string]any)["resourceVersion"]
		got, _ := in["metadata"].(map[string]any)["resourceVersion"]
		if got != have {
			status(w, 409, "Conflict", "the object has been modified")
			return
		}
		in["metadata"].(map[string]any)["resourceVersion"] = "2"
		items[index] = in
		f.puts = append(f.puts, path)
		writeJSON(w, in)
	case http.MethodPatch:
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		f.patches = append(f.patches, path+"?"+r.Header.Get("Content-Type")+"&fieldManager="+r.URL.Query().Get("fieldManager"))
		if index < 0 {
			f.objects[collection] = append(items, in)
		} else {
			items[index] = in
		}
		writeJSON(w, in)
	case http.MethodDelete:
		var opts map[string]any
		json.NewDecoder(r.Body).Decode(&opts)
		policy, _ := opts["propagationPolicy"].(string)
		f.deletes = append(f.deletes, path+"?"+policy)
		if index >= 0 {
			f.objects[collection] = append(items[:index], items[index+1:]...)
		}
		writeJSON(w, map[string]any{"kind": "Status", "status": "Success"})
	default:
		status(w, 405, "MethodNotAllowed", "no")
	}
}

func stripColumns(it map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range it {
		if k != "_columns" {
			out[k] = v
		}
	}
	return out
}

// table renders items as a metav1.Table: NAME, the items' columns, AGE.
func table(items []map[string]any, includeObject string) map[string]any {
	names := []string{}
	seen := map[string]bool{}
	for _, it := range items {
		if cols, ok := it["_columns"].(map[string]string); ok {
			for k := range cols {
				if !seen[k] {
					seen[k] = true
					names = append(names, k)
				}
			}
		}
	}
	sort.Strings(names)
	defs := []any{map[string]any{"name": "Name", "type": "string"}}
	for _, n := range names {
		defs = append(defs, map[string]any{"name": n, "type": "string"})
	}
	defs = append(defs, map[string]any{"name": "Age", "type": "string"})
	rows := []any{}
	for _, it := range items {
		meta := it["metadata"].(map[string]any)
		cells := []any{meta["name"]}
		cols, _ := it["_columns"].(map[string]string)
		for _, n := range names {
			if v, ok := cols[n]; ok {
				cells = append(cells, v)
			} else {
				cells = append(cells, nil)
			}
		}
		cells = append(cells, "5d")
		row := map[string]any{"cells": cells}
		if includeObject == "Metadata" {
			row["object"] = map[string]any{"kind": "PartialObjectMetadata", "apiVersion": "meta.k8s.io/v1", "metadata": meta}
		}
		rows = append(rows, row)
	}
	return map[string]any{"kind": "Table", "apiVersion": "meta.k8s.io/v1", "columnDefinitions": defs, "rows": rows}
}
