package kube

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	sigyaml "sigs.k8s.io/yaml"
)

// Ref addresses one object for API calls.
type Ref struct {
	GVR       schema.GroupVersionResource
	Namespace string
	Name      string
}

func (r Ref) String() string {
	if r.Namespace == "" {
		return r.GVR.Resource + "/" + r.Name
	}
	return r.Namespace + "/" + r.GVR.Resource + "/" + r.Name
}

// Get reads the full manifest of an object.
func (c *Client) Get(ctx context.Context, ref Ref) (*unstructured.Unstructured, error) {
	return c.dyn.Resource(ref.GVR).Namespace(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
}

// ExportYAML renders a manifest. With clean, the server-managed fields that
// would make the document unusable elsewhere are removed: managedFields,
// uid, resourceVersion, creationTimestamp, generation, selfLink, status and
// ownerReferences. The namespace is kept.
func ExportYAML(obj *unstructured.Unstructured, clean bool) ([]byte, error) {
	if clean {
		return ExportYAMLMode(obj, ExportClean)
	}
	return ExportYAMLMode(obj, ExportFull)
}

// ExportMode selects which server-managed fields an export keeps.
type ExportMode int

const (
	ExportFull  ExportMode = iota // Everything, as the server returns it (viewer).
	ExportEdit                    // Without status, managedFields, uid, resourceVersion, creationTimestamp, generation, selfLink; ownerReferences kept (editor: Update replaces the whole object).
	ExportClean                   // ExportEdit without ownerReferences (copies to another namespace or cluster).
)

// ExportYAMLMode renders an object as YAML, dropping fields by mode.
func ExportYAMLMode(obj *unstructured.Unstructured, mode ExportMode) ([]byte, error) {
	if obj == nil {
		return nil, fmt.Errorf("no object")
	}
	out := obj.DeepCopy()
	if mode != ExportFull {
		unstructured.RemoveNestedField(out.Object, "status")
		for _, field := range []string{"managedFields", "uid", "resourceVersion", "creationTimestamp", "generation", "selfLink"} {
			unstructured.RemoveNestedField(out.Object, "metadata", field)
		}
	}
	if mode == ExportClean {
		unstructured.RemoveNestedField(out.Object, "metadata", "ownerReferences")
	}
	return sigyaml.Marshal(out.Object)
}

// Decode parses one or more YAML documents, or a JSON document, into
// objects. Empty documents are skipped.
func Decode(data []byte) ([]*unstructured.Unstructured, error) {
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var objects []*unstructured.Unstructured
	for {
		var raw map[string]any
		err := decoder.Decode(&raw)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			continue
		}
		u := &unstructured.Unstructured{Object: raw}
		if u.GetKind() == "" || u.GetAPIVersion() == "" {
			return nil, fmt.Errorf("document %d has no kind/apiVersion", len(objects)+1)
		}
		objects = append(objects, u)
	}
	return objects, nil
}

// ResourceFor maps a manifest's apiVersion/kind to its resource.
func (c *Client) ResourceFor(obj *unstructured.Unstructured) (schema.GroupVersionResource, bool) {
	gvk := obj.GroupVersionKind()
	mapping, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return schema.GroupVersionResource{}, false
	}
	return mapping.Resource, true
}

// Apply creates or updates the object in ns by server-side apply with field
// manager "farshmac" and no force: conflicting managers fail.
func (c *Client) Apply(ctx context.Context, obj *unstructured.Unstructured, ns string) (*unstructured.Unstructured, error) {
	gvr, ok := c.ResourceFor(obj)
	if !ok {
		return nil, fmt.Errorf("unknown kind %s", obj.GroupVersionKind())
	}
	obj = obj.DeepCopy()
	obj.SetNamespace(ns)
	data, err := obj.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return c.dyn.Resource(gvr).Namespace(ns).Patch(ctx, obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{FieldManager: "farshmac"})
}

// Update replaces the object; the manifest must carry the resourceVersion it
// was read with, so a concurrent change comes back as errors.IsConflict.
func (c *Client) Update(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	gvr, ok := c.ResourceFor(obj)
	if !ok {
		return nil, fmt.Errorf("unknown kind %s", obj.GroupVersionKind())
	}
	return c.dyn.Resource(gvr).Namespace(obj.GetNamespace()).Update(ctx, obj, metav1.UpdateOptions{FieldManager: "farshmac"})
}

// Delete removes the object; dependents are deleted in the background, or
// kept when orphan is set.
func (c *Client) Delete(ctx context.Context, ref Ref, orphan bool) error {
	policy := metav1.DeletePropagationBackground
	if orphan {
		policy = metav1.DeletePropagationOrphan
	}
	return c.dyn.Resource(ref.GVR).Namespace(ref.Namespace).Delete(ctx, ref.Name, metav1.DeleteOptions{PropagationPolicy: &policy})
}

var namespacesGVR = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}

// CreateNamespace creates an empty namespace. The dynamic client speaks JSON,
// which every server and proxy accepts (the typed client prefers protobuf).
func (c *Client) CreateNamespace(ctx context.Context, name string) error {
	ns := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": name}}}
	_, err := c.dyn.Resource(namespacesGVR).Create(ctx, ns, metav1.CreateOptions{FieldManager: "farshmac"})
	return err
}

// DeleteNamespace deletes a namespace and everything in it.
func (c *Client) DeleteNamespace(ctx context.Context, name string) error {
	return c.dyn.Resource(namespacesGVR).Delete(ctx, name, metav1.DeleteOptions{})
}

// Mapping resolves the REST mapping of a resource, for describers.
func (c *Client) mapping(gvr schema.GroupVersionResource) (*meta.RESTMapping, error) {
	kinds, err := c.mapper.KindsFor(gvr)
	if err != nil {
		return nil, err
	}
	if len(kinds) == 0 {
		return nil, fmt.Errorf("no kind for %s", gvr)
	}
	return c.mapper.RESTMapping(kinds[0].GroupKind(), kinds[0].Version)
}
