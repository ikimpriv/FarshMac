package kube

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type ResourceUsage struct{ CPU, Memory float64 }

// ResourceMetricsGroup uses namespace LISTs, not one request per pod. Only
// workload containers count; ephemeral helpers cannot inflate the aggregate.
// Incomplete coverage is an error (a graph gap), never a misleading low total.
func (c *Client) ResourceMetricsGroup(ctx context.Context, ns string, selected map[string]types.UID) (ResourceUsage, error) {
	total := ResourceUsage{}
	if len(selected) == 0 {
		return total, nil
	}
	pods, err := c.core.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return total, err
	}
	var included []corev1.Pod
	for _, p := range pods.Items {
		uid, ok := selected[p.Name]
		if !ok {
			continue
		}
		if p.UID != uid {
			return total, fmt.Errorf("pod %s/%s was replaced; waiting for ownership refresh", ns, p.Name)
		}
		included = append(included, p)
	}
	return c.aggregatePodMetrics(ctx, ns, included)
}

// Namespace-qualified keys prevent pods with the same name on a node from colliding.
func (c *Client) aggregatePodMetrics(ctx context.Context, ns string, pods []corev1.Pod) (ResourceUsage, error) {
	total := ResourceUsage{}
	expected := map[string]map[string]bool{}
	required := map[string]map[string]bool{}
	for _, p := range pods {
		if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		names := map[string]bool{}
		running := map[string]bool{}
		for _, ctr := range p.Spec.Containers {
			names[ctr.Name] = true
			running[ctr.Name] = true
		}
		for _, ctr := range p.Spec.InitContainers {
			names[ctr.Name] = true
		}
		for _, st := range p.Status.InitContainerStatuses {
			if st.State.Running != nil {
				running[st.Name] = true
			}
		}
		for _, st := range p.Status.ContainerStatuses {
			if st.State.Waiting != nil || st.State.Terminated != nil {
				delete(running, st.Name)
			}
		}
		// Pending pods may not have a node or any container statuses yet.
		// They consume no workload resources, so Metrics Server legitimately
		// omits them. A running init container still needs to be measured.
		if p.Status.Phase == corev1.PodPending {
			running = map[string]bool{}
			for _, st := range append(append([]corev1.ContainerStatus{}, p.Status.ContainerStatuses...), p.Status.InitContainerStatuses...) {
				if st.State.Running != nil {
					running[st.Name] = true
				}
			}
		}
		if len(running) == 0 {
			continue
		}
		expected[p.Namespace+"/"+p.Name] = names
		required[p.Namespace+"/"+p.Name] = running
	}
	if len(expected) == 0 {
		return total, nil
	}
	list, err := c.dyn.Resource(schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return total, err
	}
	measured := map[string]bool{}
	for _, obj := range list.Items {
		key := obj.GetNamespace() + "/" + obj.GetName()
		names, ok := expected[key]
		if !ok {
			continue
		}
		containers, _, err := unstructured.NestedSlice(obj.Object, "containers")
		if err != nil {
			return total, err
		}
		found := false
		seen := map[string]bool{}
		for _, raw := range containers {
			ctr, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name, _ := ctr["name"].(string)
			if !names[name] {
				continue
			}
			cpu, _, _ := unstructured.NestedString(ctr, "usage", "cpu")
			memory, _, _ := unstructured.NestedString(ctr, "usage", "memory")
			cq, ce := resource.ParseQuantity(cpu)
			mq, me := resource.ParseQuantity(memory)
			if ce != nil || me != nil {
				return total, fmt.Errorf("invalid resource metrics for %s/%s", obj.GetName(), name)
			}
			total.CPU += cq.AsApproximateFloat64()
			total.Memory += mq.AsApproximateFloat64()
			found = true
			seen[name] = true
		}
		for name := range required[key] {
			if !seen[name] {
				return ResourceUsage{}, fmt.Errorf("Metrics Server: missing container %s/%s/%s", ns, obj.GetName(), name)
			}
		}
		if found {
			measured[key] = true
		}
	}
	if len(measured) < len(expected) {
		return ResourceUsage{}, fmt.Errorf("Metrics Server: %d of %d active pods have no metrics in %s", len(expected)-len(measured), len(expected), ns)
	}
	return total, nil
}
