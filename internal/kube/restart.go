package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// CanRestart identifies controllers that roll out changes to their pod template.
func CanRestart(o *Object) bool {
	if o == nil || o.Deleting || o.GVR.Group != "apps" {
		return false
	}
	switch o.GVR.Resource {
	case "deployments", "statefulsets", "daemonsets":
		return true
	}
	return false
}

// Restart requests a rollout without deleting pods or changing the update strategy.
func (c *Client) Restart(ctx context.Context, target Object) error {
	if !CanRestart(&target) {
		return fmt.Errorf("resource does not support rollout restart")
	}
	ref := Ref{GVR: target.GVR, Namespace: target.Namespace, Name: target.Name}
	obj, err := c.Get(ctx, ref)
	if err != nil {
		return err
	}
	if target.UID != "" && obj.GetUID() != target.UID {
		return fmt.Errorf("object was replaced; refresh the panel and try again")
	}
	if obj.GetDeletionTimestamp() != nil {
		return fmt.Errorf("object is being deleted")
	}
	paused, _, _ := unstructured.NestedBool(obj.Object, "spec", "paused")
	if paused {
		return fmt.Errorf("deployment is paused; resume it before requesting a restart")
	}
	strategy, _, _ := unstructured.NestedString(obj.Object, "spec", "updateStrategy", "type")
	if strategy == "OnDelete" {
		return fmt.Errorf("OnDelete update strategy does not automatically restart pods")
	}
	// Pin replicas to the freshly read value as well as resourceVersion. This
	// preserves scale explicitly even in API paths that default omitted fields.
	spec := map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]string{"kubectl.kubernetes.io/restartedAt": time.Now().UTC().Format(time.RFC3339Nano)}}}}
	replicas, hasReplicas, err := unstructured.NestedInt64(obj.Object, "spec", "replicas")
	if err != nil {
		return err
	}
	if hasReplicas {
		spec["replicas"] = replicas
	}
	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"uid": obj.GetUID(), "resourceVersion": obj.GetResourceVersion()},
		"spec":     spec,
	})
	if err != nil {
		return err
	}
	updated, err := c.dyn.Resource(ref.GVR).Namespace(ref.Namespace).Patch(ctx, ref.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return err
	}
	if hasReplicas {
		actual, found, e := unstructured.NestedInt64(updated.Object, "spec", "replicas")
		if e != nil || !found || actual != replicas {
			return fmt.Errorf("API changed replicas during restart: expected %d, received %d; inspect admission policies before retrying", replicas, actual)
		}
	}
	return nil
}
