package kube

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ResourceStore is independent of panel paths and the resource kind. CRs and
// built-in resources use the same transfer protocol, including across clusters.
type ResourceStore interface {
	Identity() string
	Get(context.Context, Ref) (*unstructured.Unstructured, error)
	ResolveTransfer(context.Context, *unstructured.Unstructured, string, string) (Ref, error)
	WriteTransfer(context.Context, Ref, *unstructured.Unstructured, bool, bool) error
	DeleteTransfer(context.Context, Ref, *unstructured.Unstructured) error
}

type ResourceTransferOptions struct {
	Namespace, Name      string
	Move, Replace, Force bool
}

func TransferResource(ctx context.Context, source, target ResourceStore, ref Ref, options ResourceTransferOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if options.Move && ref.GVR.Group == "" && ref.GVR.Resource == "namespaces" {
		return fmt.Errorf("namespace rename/move is not supported: deleting a namespace would delete its contents")
	}
	live, err := source.Get(ctx, ref)
	if err != nil {
		return err
	}
	name := options.Name
	if name == "" {
		name = live.GetName()
	}
	destination, err := target.ResolveTransfer(ctx, live, options.Namespace, name)
	if err != nil {
		return err
	}
	if source.Identity() == target.Identity() && destination == ref {
		return fmt.Errorf("source and destination are the same resource")
	}
	data, err := ExportYAMLMode(live, ExportClean)
	if err != nil {
		return err
	}
	docs, err := Decode(data)
	if err != nil {
		return err
	}
	if len(docs) != 1 {
		return fmt.Errorf("expected one resource")
	}
	obj := docs[0]
	obj.SetName(destination.Name)
	obj.SetNamespace(destination.Namespace)
	obj.SetFinalizers(nil) // Controller lifecycle state must not follow a copy.
	unstructured.RemoveNestedField(obj.Object, "metadata", "deletionTimestamp")
	unstructured.RemoveNestedField(obj.Object, "metadata", "deletionGracePeriodSeconds")
	obj.SetGenerateName("")
	if err = target.WriteTransfer(ctx, destination, obj, options.Replace, options.Force); err != nil {
		return err
	}
	if options.Move {
		if err = ctx.Err(); err != nil {
			return fmt.Errorf("destination created; source retained: %w", err)
		}
		if err = source.DeleteTransfer(ctx, ref, live); err != nil {
			return fmt.Errorf("destination created; could not delete source %s: %w", ref, err)
		}
	}
	return nil
}

func (c *Client) Identity() string { return c.config.Host }
func (c *Client) ResolveTransfer(ctx context.Context, obj *unstructured.Unstructured, namespace, name string) (Ref, error) {
	mapper, err := c.contextualMapper(ctx)
	if err != nil {
		return Ref{}, err
	}
	gvk := obj.GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return Ref{}, err
	}
	if mapping.Scope.Name() != meta.RESTScopeNameNamespace {
		namespace = ""
	} else if namespace == "" {
		return Ref{}, fmt.Errorf("choose a destination namespace")
	}
	return Ref{GVR: mapping.Resource, Namespace: namespace, Name: name}, nil
}
func (c *Client) WriteTransfer(ctx context.Context, ref Ref, obj *unstructured.Unstructured, replace, force bool) error {
	if replace {
		_, err := c.ApplyWith(ctx, obj, ref.Namespace, force)
		return err
	}
	_, err := c.dyn.Resource(ref.GVR).Namespace(ref.Namespace).Create(ctx, obj, metav1.CreateOptions{FieldManager: "farshmac"})
	return err
}
func (c *Client) DeleteTransfer(ctx context.Context, ref Ref, original *unstructured.Unstructured) error {
	uid, version := original.GetUID(), original.GetResourceVersion()
	if uid == "" || version == "" {
		return fmt.Errorf("cannot safely delete a resource without UID and resourceVersion")
	}
	// Orphan children: moving an owner must not cascade-delete workloads. Owner
	// references in other resources are not silently rewritten.
	policy := metav1.DeletePropagationOrphan
	return c.dyn.Resource(ref.GVR).Namespace(ref.Namespace).Delete(ctx, ref.Name, metav1.DeleteOptions{PropagationPolicy: &policy, Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}})
}
