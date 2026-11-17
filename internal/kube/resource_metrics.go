package kube

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ResourceMetrics needs metrics-server and pods.metrics.k8s.io read access.
// It does not fabricate ephemeral usage from filesystem-wide free space.
func (c *Client) ResourceMetrics(ctx context.Context, ref ContainerRef, uid string) ([]byte, error) {
	pod, err := c.Pod(ctx, ref.Namespace, ref.Pod)
	if err != nil {
		return nil, err
	}
	if string(pod.UID) != uid {
		return nil, fmt.Errorf("pod was replaced")
	}
	obj, err := c.dyn.Resource(schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}).Namespace(ref.Namespace).Get(ctx, ref.Pod, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	containers, _, err := unstructured.NestedSlice(obj.Object, "containers")
	if err != nil {
		return nil, err
	}
	agents := map[string]bool{}
	for _, ec := range pod.Spec.EphemeralContainers {
		if isPodToastCandidate(ec) {
			agents[ec.Name] = true
		}
	}
	var b strings.Builder
	b.WriteString("# TYPE kubernetes_container_cpu_usage_cores gauge\n# TYPE kubernetes_container_memory_usage_bytes gauge\n")
	for _, raw := range containers {
		ctr, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := ctr["name"].(string)
		if agents[name] {
			continue
		}
		for key, metric := range map[string]string{"cpu": "kubernetes_container_cpu_usage_cores", "memory": "kubernetes_container_memory_usage_bytes"} {
			value, _, _ := unstructured.NestedString(ctr, "usage", key)
			q, e := resource.ParseQuantity(value)
			if e != nil {
				continue
			}
			fmt.Fprintf(&b, "%s{container=%q} %g\n", metric, name, q.AsApproximateFloat64())
		}
	}
	return []byte(b.String()), nil
}

// ResourceBounds reads declared budgets independently of metrics-server.
func (c *Client) ResourceBounds(ctx context.Context, ref ContainerRef, uid string) ([]byte, error) {
	pod, err := c.Pod(ctx, ref.Namespace, ref.Pod)
	if err != nil {
		return nil, err
	}
	if string(pod.UID) != uid {
		return nil, fmt.Errorf("pod was replaced")
	}
	var b strings.Builder
	containers := append(append([]corev1.Container(nil), pod.Spec.Containers...), pod.Spec.InitContainers...)
	for _, ctr := range containers {
		for kind, values := range map[string]corev1.ResourceList{"request": ctr.Resources.Requests, "limit": ctr.Resources.Limits} {
			for resource, suffix := range map[corev1.ResourceName]string{corev1.ResourceCPU: "cpu_cores", corev1.ResourceMemory: "memory_bytes", corev1.ResourceEphemeralStorage: "ephemeral_storage_bytes"} {
				q, ok := values[resource]
				if !ok || q.Sign() <= 0 {
					continue
				}
				name := "farshmac_container_" + kind + "_" + suffix
				fmt.Fprintf(&b, "# TYPE %s gauge\n%s{container=%q} %g\n", name, name, ctr.Name, q.AsApproximateFloat64())
			}
		}
	}
	return []byte(b.String()), nil
}
