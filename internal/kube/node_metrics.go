package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	resourcehelper "k8s.io/component-helpers/resource"
)

// NodeResourceMetrics sums workload containers across namespaces. It is not
// host CPU/memory: kubelet, kernel and other non-pod processes are excluded.
func (c *Client) NodeResourceMetrics(ctx context.Context, node string) ([]byte, error) {
	pods, err := c.core.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + node})
	if err != nil {
		return nil, err
	}
	// Also filter locally, so no unrelated pod can inflate a node aggregate.
	var assigned []corev1.Pod
	for _, p := range pods.Items {
		if p.Spec.NodeName == node {
			assigned = append(assigned, p)
		}
	}
	v, err := c.aggregatePodMetrics(ctx, "", assigned)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("# TYPE kubernetes_container_cpu_usage_cores gauge\n# TYPE kubernetes_container_memory_usage_bytes gauge\nkubernetes_container_cpu_usage_cores %g\nkubernetes_container_memory_usage_bytes %g\n", v.CPU, v.Memory)), nil
}

// NodeResourceBounds is independent of metrics-server. Device values describe
// scheduler reservations, not hardware utilization or unique physical GPUs.
func (c *Client) NodeResourceBounds(ctx context.Context, name string) ([]byte, error) {
	node, err := c.core.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	pods, err := c.core.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + name})
	if err != nil {
		return nil, err
	}
	return nodeResourceBounds(node, pods.Items), nil
}
func nodeResourceBounds(node *corev1.Node, pods []corev1.Pod) []byte {
	allocated := map[corev1.ResourceName]float64{}
	for i := range pods {
		p := &pods[i]
		if p.Spec.NodeName != node.Name || p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		for name, q := range resourcehelper.PodRequests(p, resourcehelper.PodResourcesOptions{}) {
			allocated[name] += q.AsApproximateFloat64()
		}
	}
	var b strings.Builder
	write := func(name, labels string, value float64) {
		fmt.Fprintf(&b, "# TYPE %s gauge\n%s%s %g\n", name, name, labels, value)
	}
	for _, item := range []struct {
		name   corev1.ResourceName
		suffix string
	}{{corev1.ResourceCPU, "cpu_cores"}, {corev1.ResourceMemory, "memory_bytes"}} {
		for _, bound := range []struct {
			name   string
			values corev1.ResourceList
		}{{"capacity", node.Status.Capacity}, {"allocatable", node.Status.Allocatable}} {
			if q, ok := bound.values[item.name]; ok {
				write("farshmac_node_"+bound.name+"_"+item.suffix, "", q.AsApproximateFloat64())
			}
		}
	}
	var devices []string
	for name := range node.Status.Capacity {
		// Extended resources, including device-plugin GPU/MIG/RDMA resources.
		if strings.Contains(string(name), "/") && !strings.Contains(string(name), "kubernetes.io/") {
			devices = append(devices, string(name))
		}
	}
	sort.Strings(devices)
	for _, name := range devices {
		key := corev1.ResourceName(name)
		labels := fmt.Sprintf("{device=%q}", name)
		for _, bound := range []struct {
			name   string
			values corev1.ResourceList
		}{{"capacity", node.Status.Capacity}, {"allocatable", node.Status.Allocatable}} {
			if q, ok := bound.values[key]; ok {
				write("farshmac_node_"+bound.name+"_device", labels, q.AsApproximateFloat64())
			}
		}
		write("farshmac_node_allocated_device", labels, allocated[key])
	}
	return []byte(b.String())
}
