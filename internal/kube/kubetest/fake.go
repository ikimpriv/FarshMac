// Package kubetest is a hand-written fake Kubernetes API server for tests of
// package kube and of the panel: discovery, LIST (Table or plain),
// GET/PUT/PATCH/DELETE, namespaces and logs. It imports nothing from kube.
package kubetest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// FakeAPI serves a small cluster. Objects are stored by collection path; a
// "_columns" map on an object renders as Table columns.
type FakeAPI struct {
	mu        sync.Mutex
	server    *httptest.Server
	objects   map[string][]map[string]any // "/apis/apps/v1/namespaces/ns/deployments" -> items
	forbidden map[string]bool             // collection paths answering 403
	deletes   []string                    // "path?policy"
	puts      []string
	patches   []string
	logs      string
	rv        int                   // Cluster resourceVersion, bumped by every change.
	watchers  map[string][]*watcher // Open watch streams per collection.
	expire    map[string]bool       // Collections whose next watch answers 410 Gone.
}

// watcher is one open watch request: events are JSON lines; a Table watch
// sends columnDefinitions in its first event only, like a real server.
type watcher struct {
	events chan []byte
	drop   chan struct{}
	table  bool
	meta   bool
	names  []string // Table column names fixed when the watch started.
	first  bool
}

// New starts a server with the "default" and "shop" namespaces.
func New(t testing.TB) *FakeAPI {
	t.Helper()
	f := &FakeAPI{objects: map[string][]map[string]any{}, forbidden: map[string]bool{}, watchers: map[string][]*watcher{}, expire: map[string]bool{}, rv: 1}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	f.Add("/api/v1/namespaces", Obj("Namespace", "", "default", "ns-default", nil, nil, map[string]string{"STATUS": "Active"}))
	f.Add("/api/v1/namespaces", Obj("Namespace", "", "shop", "ns-shop", nil, nil, map[string]string{"STATUS": "Active"}))
	return f
}

// Add stores items under a collection path such as
// "/apis/apps/v1/namespaces/ns/deployments".
func (f *FakeAPI) Add(collection string, items ...map[string]any) {
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
	f.rv++
	f.objects[collection] = append(f.objects[collection], items...)
}

// Emit applies a change to the stored objects (ADDED appends, MODIFIED
// replaces by name, DELETED removes) and pushes the event to every open
// watch of the collection.
func (f *FakeAPI) Emit(collection string, eventType string, obj map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rv++
	meta := obj["metadata"].(map[string]any)
	meta["resourceVersion"] = fmt.Sprint(f.rv)
	if _, ok := obj["apiVersion"]; !ok {
		obj["apiVersion"] = "v1"
	}
	name := meta["name"]
	items := f.objects[collection]
	index := -1
	for i, it := range items {
		if it["metadata"].(map[string]any)["name"] == name {
			index = i
		}
	}
	switch eventType {
	case "DELETED":
		if index >= 0 {
			f.objects[collection] = append(items[:index], items[index+1:]...)
		}
	default:
		if index >= 0 {
			items[index] = obj
		} else {
			f.objects[collection] = append(items, obj)
		}
	}
	f.push(collection, eventType, obj)
}

// push sends an event to the open watches of a collection; callers hold f.mu.
// Mutating requests (PUT, PATCH, POST, DELETE) push too, as a real server does.
func (f *FakeAPI) push(collection, eventType string, obj map[string]any) {
	for _, w := range f.watchers[collection] {
		var object any
		if w.table {
			object = tableWith(w.names, []map[string]any{obj}, w.meta, !w.first, fmt.Sprint(f.rv))
			w.first = true
		} else {
			object = stripColumns(obj)
		}
		line, _ := json.Marshal(map[string]any{"type": eventType, "object": object})
		select {
		case w.events <- append(line, '\n'):
		default:
		}
	}
}

// ExpireWatch makes the next watch request of the collection answer 410
// Gone, as a server does when the resourceVersion is too old.
func (f *FakeAPI) ExpireWatch(collection string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expire[collection] = true
}

// DropWatches closes every open watch stream of the collection without an
// error, as a server does after its timeout; clients reconnect.
func (f *FakeAPI) DropWatches(collection string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.watchers[collection] {
		close(w.drop)
	}
	f.watchers[collection] = nil
}

// Watchers counts the open watch streams of the collection.
func (f *FakeAPI) Watchers(collection string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.watchers[collection])
}

// serveWatch streams events for a collection until the client goes away or
// the stream is dropped.
func (f *FakeAPI) serveWatch(w http.ResponseWriter, r *http.Request, path string) {
	f.mu.Lock()
	if f.expire[path] {
		delete(f.expire, path)
		f.mu.Unlock()
		status(w, 410, "Expired", "too old resource version")
		return
	}
	items := f.objects[path]
	wt := &watcher{events: make(chan []byte, 64), drop: make(chan struct{}), table: strings.Contains(r.Header.Get("Accept"), "as=Table"), meta: r.URL.Query().Get("includeObject") == "Metadata", names: columnNames(items)}
	f.watchers[path] = append(f.watchers[path], wt)
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		list := f.watchers[path]
		for i, x := range list {
			if x == wt {
				f.watchers[path] = append(list[:i], list[i+1:]...)
				break
			}
		}
		f.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.WriteHeader(200)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	for {
		select {
		case line := <-wt.events:
			if _, err := w.Write(line); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		case <-wt.drop:
			return
		case <-r.Context().Done():
			return
		}
	}
}

// Obj builds an object; owners are UIDs, columns render in Table form.
func Obj(kind, ns, name, uid string, owners []string, labels map[string]string, columns map[string]string) map[string]any {
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

// openAPIRoutes serve /openapi/v3 like a real server: an index of
// group/version documents and the documents with components.schemas.
var openAPIRoutes = map[string]any{
	"/openapi/v3": map[string]any{"paths": map[string]any{
		"apis/example.com/v1": map[string]any{"serverRelativeURL": "/openapi/v3/apis/example.com/v1?hash=x"},
		"apis/apps/v1":        map[string]any{"serverRelativeURL": "/openapi/v3/apis/apps/v1?hash=y"},
		"api/v1":              map[string]any{"serverRelativeURL": "/openapi/v3/api/v1?hash=z"},
	}},
	"/openapi/v3/apis/example.com/v1": map[string]any{"openapi": "3.0.0", "components": map[string]any{"schemas": map[string]any{
		"io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta": objectMetaSchema,
		"com.example.v1.Widget": map[string]any{
			"type":                            "object",
			"description":                     "Widget is a sample custom resource. It has a spec and a status.",
			"x-kubernetes-group-version-kind": []any{map[string]any{"group": "example.com", "version": "v1", "kind": "Widget"}},
			"properties": map[string]any{
				"apiVersion": map[string]any{"type": "string", "description": "APIVersion defines the versioned schema."},
				"kind":       map[string]any{"type": "string", "description": "Kind is a string value representing the REST resource."},
				"metadata":   map[string]any{"allOf": []any{map[string]any{"$ref": "#/components/schemas/io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta"}}},
				"spec": map[string]any{
					"type":        "object",
					"description": "Desired state of the widget. Everything else is derived.",
					"required":    []any{"size"},
					"properties": map[string]any{
						"size":     map[string]any{"type": "integer", "description": "Size in units."},
						"mode":     map[string]any{"type": "string", "enum": []any{"fast", "slow"}, "description": "Operating mode."},
						"replicas": map[string]any{"x-kubernetes-int-or-string": true, "description": "Replica count or percentage."},
						"options":  map[string]any{"type": "object", "x-kubernetes-preserve-unknown-fields": true},
						"items": map[string]any{"type": "array", "items": map[string]any{
							"type": "object", "required": []any{"name"},
							"properties": map[string]any{"name": map[string]any{"type": "string"}, "weight": map[string]any{"type": "number"}},
						}},
						"enabled": map[string]any{"type": "boolean"},
					},
				},
				"status": map[string]any{"type": "object", "x-kubernetes-preserve-unknown-fields": true},
			},
		},
	}}},
	"/openapi/v3/apis/apps/v1": map[string]any{"openapi": "3.0.0", "components": map[string]any{"schemas": map[string]any{
		"io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta": objectMetaSchema,
		"io.k8s.api.apps.v1.Deployment": map[string]any{
			"type":                            "object",
			"x-kubernetes-group-version-kind": []any{map[string]any{"group": "apps", "version": "v1", "kind": "Deployment"}},
			"properties": map[string]any{
				"apiVersion": map[string]any{"type": "string"},
				"kind":       map[string]any{"type": "string"},
				"metadata":   map[string]any{"$ref": "#/components/schemas/io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta"},
				"spec":       map[string]any{"$ref": "#/components/schemas/io.k8s.api.apps.v1.DeploymentSpec"},
			},
		},
		"io.k8s.api.apps.v1.DeploymentSpec": map[string]any{
			"type":     "object",
			"required": []any{"selector", "template"},
			"properties": map[string]any{
				"replicas": map[string]any{"type": "integer", "format": "int32", "description": "Number of desired pods. Defaults to 1."},
				"selector": map[string]any{"type": "object", "properties": map[string]any{"matchLabels": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}}},
				"template": map[string]any{"$ref": "#/components/schemas/io.k8s.api.core.v1.PodTemplateSpec"},
			},
		},
		"io.k8s.api.core.v1.PodTemplateSpec": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"metadata": map[string]any{"$ref": "#/components/schemas/io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta"},
				"spec": map[string]any{"type": "object", "properties": map[string]any{
					"containers": map[string]any{"type": "array", "items": map[string]any{"type": "object", "required": []any{"name"}, "properties": map[string]any{"name": map[string]any{"type": "string"}, "image": map[string]any{"type": "string"}}}},
				}},
			},
		},
	}}},
	"/openapi/v3/api/v1": map[string]any{"openapi": "3.0.0", "components": map[string]any{"schemas": map[string]any{
		"io.k8s.apimachinery.pkg.apis.meta.v1.ObjectMeta": objectMetaSchema,
	}}},
}

var objectMetaSchema = map[string]any{
	"type":        "object",
	"description": "ObjectMeta is metadata that all persisted resources must have.",
	"properties": map[string]any{
		"name":        map[string]any{"type": "string", "description": "Name must be unique within a namespace."},
		"namespace":   map[string]any{"type": "string"},
		"labels":      map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
		"annotations": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
		// Server-managed fields the real ObjectMeta also declares.
		"uid":               map[string]any{"type": "string"},
		"resourceVersion":   map[string]any{"type": "string"},
		"generation":        map[string]any{"type": "integer"},
		"creationTimestamp": map[string]any{"type": "string"},
		"finalizers":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"ownerReferences": map[string]any{"type": "array", "items": map[string]any{"type": "object", "required": []any{"apiVersion", "kind", "name", "uid"}, "properties": map[string]any{
			"apiVersion": map[string]any{"type": "string"}, "kind": map[string]any{"type": "string"}, "name": map[string]any{"type": "string"}, "uid": map[string]any{"type": "string"},
			"controller": map[string]any{"type": "boolean"}, "blockOwnerDeletion": map[string]any{"type": "boolean"},
		}}},
	},
}

func (f *FakeAPI) handle(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if v, ok := discoveryRoutes[path]; ok && r.Method == http.MethodGet {
		writeJSON(w, v)
		return
	}
	if v, ok := openAPIRoutes[path]; ok && r.Method == http.MethodGet {
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
		if r.URL.Query().Get("watch") == "true" {
			f.mu.Unlock()
			f.serveWatch(w, r, path)
			f.mu.Lock() // The deferred Unlock expects it held.
			return
		}
		rv := fmt.Sprint(f.rv)
		if strings.Contains(r.Header.Get("Accept"), "as=Table") {
			writeJSON(w, tableWith(columnNames(items), items, r.URL.Query().Get("includeObject") == "Metadata", true, rv))
			return
		}
		out := make([]any, 0, len(items))
		for _, it := range items {
			out = append(out, stripColumns(it))
		}
		writeJSON(w, map[string]any{"kind": "List", "apiVersion": "v1", "metadata": map[string]any{"resourceVersion": rv}, "items": out})
		return
	}
	// Single object.
	i := strings.LastIndex(path, "/")
	collection, name := path[:i], path[i+1:]
	items, ok := f.objects[collection]
	if !ok && r.Method == http.MethodPatch && strings.Contains(collection, "/namespaces/") {
		// Server-side apply creates the first object of a type in a namespace.
		items, ok = nil, true
	}
	if !ok {
		if r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v1/namespaces/") {
			status(w, 404, "NotFound", "namespace not found")
			return
		}
		if r.Method == http.MethodPost && path == "/api/v1/namespaces" {
			// The created namespace takes the name from the request body.
			name := "created"
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			if json.Unmarshal(raw, &body) == nil {
				if meta, ok := body["metadata"].(map[string]any); ok {
					if n, ok := meta["name"].(string); ok && n != "" {
						name = n
					}
				}
			}
			created := Obj("Namespace", "", name, "ns-"+name, nil, nil, map[string]string{"STATUS": "Active"})
			f.rv++
			created["metadata"].(map[string]any)["resourceVersion"] = fmt.Sprint(f.rv)
			f.objects[path] = append(f.objects[path], created)
			f.push(path, "ADDED", created)
			writeJSON(w, stripColumns(created))
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
		// Like a real server: identity and server-managed fields survive an update.
		oldMeta, newMeta := items[index]["metadata"].(map[string]any), in["metadata"].(map[string]any)
		for _, field := range []string{"uid", "creationTimestamp", "managedFields"} {
			if _, ok := newMeta[field]; !ok {
				if v, ok := oldMeta[field]; ok {
					newMeta[field] = v
				}
			}
		}
		if _, ok := in["_columns"]; !ok {
			if v, ok := items[index]["_columns"]; ok {
				in["_columns"] = v
			}
		}
		f.rv++
		newMeta["resourceVersion"] = fmt.Sprint(f.rv)
		items[index] = in
		f.puts = append(f.puts, path)
		f.push(collection, "MODIFIED", in)
		writeJSON(w, in)
	case http.MethodPatch:
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		f.patches = append(f.patches, path+"?"+r.Header.Get("Content-Type")+"&fieldManager="+r.URL.Query().Get("fieldManager"))
		f.rv++
		if meta, ok := in["metadata"].(map[string]any); ok {
			meta["resourceVersion"] = fmt.Sprint(f.rv)
		}
		if index < 0 {
			f.objects[collection] = append(items, in)
			f.push(collection, "ADDED", in)
		} else {
			items[index] = in
			f.push(collection, "MODIFIED", in)
		}
		writeJSON(w, in)
	case http.MethodDelete:
		var opts map[string]any
		json.NewDecoder(r.Body).Decode(&opts)
		policy, _ := opts["propagationPolicy"].(string)
		f.deletes = append(f.deletes, path+"?"+policy)
		if index >= 0 {
			gone := items[index]
			f.objects[collection] = append(items[:index], items[index+1:]...)
			f.rv++
			f.push(collection, "DELETED", gone)
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

// columnNames collects the custom column names of items, sorted.
func columnNames(items []map[string]any) []string {
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
	return names
}

// tableWith renders items as a metav1.Table: NAME, the given columns, AGE.
// withDefs includes columnDefinitions (a watch sends them once).
func tableWith(names []string, items []map[string]any, includeMeta, withDefs bool, rv string) map[string]any {
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
		if includeMeta {
			row["object"] = map[string]any{"kind": "PartialObjectMetadata", "apiVersion": "meta.k8s.io/v1", "metadata": meta}
		}
		rows = append(rows, row)
	}
	t := map[string]any{"kind": "Table", "apiVersion": "meta.k8s.io/v1", "metadata": map[string]any{"resourceVersion": rv}, "rows": rows}
	if withDefs {
		defs := []any{map[string]any{"name": "Name", "type": "string"}}
		for _, n := range names {
			defs = append(defs, map[string]any{"name": n, "type": "string"})
		}
		defs = append(defs, map[string]any{"name": "Age", "type": "string"})
		t["columnDefinitions"] = defs
	}
	return t
}

// URL is the server address.
func (f *FakeAPI) URL() string { return f.server.URL }

// Deletes lists DELETE requests as "path?propagationPolicy".
func (f *FakeAPI) Deletes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deletes...)
}

// Puts lists PUT request paths.
func (f *FakeAPI) Puts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.puts...)
}

// Patches lists PATCH requests as "path?contentType&fieldManager=...".
func (f *FakeAPI) Patches() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.patches...)
}

// SetLogs sets the text every pod log request returns.
func (f *FakeAPI) SetLogs(text string) { f.mu.Lock(); defer f.mu.Unlock(); f.logs = text }

// Logs returns the text pod log requests return.
func (f *FakeAPI) Logs() string { f.mu.Lock(); defer f.mu.Unlock(); return f.logs }

// Forbid makes a collection path answer 403.
func (f *FakeAPI) Forbid(collection string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forbidden[collection] = true
}

// Kubeconfig writes a kubeconfig for this server into t.TempDir(): one
// cluster, a user without credentials, and a current context named "fake".
func (f *FakeAPI) Kubeconfig(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	text := "apiVersion: v1\nkind: Config\ncurrent-context: fake\nclusters:\n- name: fake\n  cluster:\n    server: " + f.server.URL + "\ncontexts:\n- name: fake\n  context:\n    cluster: fake\n    user: fake\nusers:\n- name: fake\n  user: {}\n"
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

const shopNS = "shop"

// SeedShop fills the "shop" namespace with a Deployment -> ReplicaSets ->
// Pods chain (old and new ReplicaSet), a StatefulSet with ControllerRevisions,
// a CronJob -> Job -> Pod chain, Services with and without selectors, a Pod
// with two owners, a CRD object, and a forbidden Secret collection.
func SeedShop(f *FakeAPI) {
	p := func(resource string) string { return "/api/v1/namespaces/" + shopNS + "/" + resource }
	apps := func(resource string) string { return "/apis/apps/v1/namespaces/" + shopNS + "/" + resource }
	batch := func(resource string) string { return "/apis/batch/v1/namespaces/" + shopNS + "/" + resource }
	dep := Obj("Deployment", shopNS, "app", "dep-app", nil, nil, map[string]string{"READY": "2/3", "UP-TO-DATE": "3"})
	dep["spec"] = map[string]any{"replicas": 3, "selector": map[string]any{"matchLabels": map[string]any{"app": "shop"}}, "template": map[string]any{"metadata": map[string]any{"labels": map[string]any{"app": "shop"}}, "spec": map[string]any{"containers": []any{map[string]any{"name": "app", "image": "shop:1"}}}}}
	f.Add(apps("deployments"), dep)
	f.Add(apps("replicasets"), Obj("ReplicaSet", shopNS, "app-new", "rs-new", []string{"dep-app"}, nil, map[string]string{"READY": "2"}),
		Obj("ReplicaSet", shopNS, "app-old", "rs-old", []string{"dep-app"}, nil, map[string]string{"READY": "0"}))
	f.Add(apps("statefulsets"), Obj("StatefulSet", shopNS, "db", "sts-db", nil, nil, map[string]string{"READY": "1/1"}))
	f.Add(apps("controllerrevisions"), Obj("ControllerRevision", shopNS, "db-1", "cr-1", []string{"sts-db"}, nil, nil))
	f.Add(batch("cronjobs"), Obj("CronJob", shopNS, "backup", "cj-backup", nil, nil, map[string]string{"SCHEDULE": "0 * * * *"}))
	f.Add(batch("jobs"), Obj("Job", shopNS, "backup-1", "job-1", []string{"cj-backup"}, nil, map[string]string{"STATUS": "Complete"}))
	f.Add(p("pods"),
		Obj("Pod", shopNS, "app-new-x", "pod-new-x", []string{"rs-new"}, map[string]string{"app": "shop"}, map[string]string{"STATUS": "Running", "READY": "1/1"}),
		Obj("Pod", shopNS, "app-old-y", "pod-old-y", []string{"rs-old"}, map[string]string{"app": "shop"}, map[string]string{"STATUS": "Terminating", "READY": "0/1"}),
		Obj("Pod", shopNS, "db-0", "pod-db-0", []string{"sts-db"}, map[string]string{"app": "db"}, map[string]string{"STATUS": "Running", "READY": "1/1"}),
		Obj("Pod", shopNS, "backup-1-z", "pod-backup", []string{"job-1"}, nil, map[string]string{"STATUS": "Succeeded", "READY": "0/1"}),
		Obj("Pod", shopNS, "shared", "pod-shared", []string{"dep-app", "sts-db"}, map[string]string{"app": "shared"}, map[string]string{"STATUS": "Running", "READY": "1/1"}),
		Obj("Pod", shopNS, "loner", "pod-loner", nil, map[string]string{"app": "loner"}, map[string]string{"STATUS": "Pending", "READY": "0/1"}),
	)
	svc := Obj("Service", shopNS, "web", "svc-web", nil, nil, map[string]string{"TYPE": "ClusterIP"})
	svc["spec"] = map[string]any{"selector": map[string]any{"app": "shop"}}
	empty := Obj("Service", shopNS, "headless", "svc-headless", nil, nil, map[string]string{"TYPE": "ClusterIP"})
	empty["spec"] = map[string]any{}
	f.Add(p("services"), svc, empty)
	f.Add(p("configmaps"), Obj("ConfigMap", shopNS, "settings", "cm-settings", nil, nil, map[string]string{"DATA": "3"}))
	f.Add(p("events"))
	f.Add("/apis/example.com/v1/namespaces/"+shopNS+"/widgets", Obj("Widget", shopNS, "w1", "widget-1", []string{"dep-app"}, nil, map[string]string{"PHASE": "Ready"}))
	f.Add("/apis/other.io/v1/namespaces/" + shopNS + "/deployments")
	f.Add(p("secrets"))
	f.forbidden[p("secrets")] = true
	f.Add(apps("daemonsets"))
}
