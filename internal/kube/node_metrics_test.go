package kube

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNodeMetricsAcrossNamespaces(t *testing.T) {
	gvr := schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}
	dyn := dfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: "PodMetricsList"})
	core := fake.NewSimpleClientset()
	for _, ns := range []string{"a", "b", "other"} {
		node := "worker"
		if ns == "other" {
			node = "elsewhere"
		}
		p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "same", Namespace: ns}, Spec: corev1.PodSpec{NodeName: node, Containers: []corev1.Container{{Name: "app"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
		if _, err := core.CoreV1().Pods(ns).Create(context.Background(), p, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		m := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "metrics.k8s.io/v1beta1", "kind": "PodMetrics", "metadata": map[string]any{"name": "same", "namespace": ns}, "containers": []any{map[string]any{"name": "app", "usage": map[string]any{"cpu": "500m", "memory": "64Mi"}}, map[string]any{"name": "agent", "usage": map[string]any{"cpu": "100", "memory": "100Gi"}}}}}
		if err := dyn.Tracker().Create(gvr, m, ns); err != nil {
			t.Fatal(err)
		}
	}
	c := &Client{core: core, dyn: dyn}
	body, err := c.NodeResourceMetrics(context.Background(), "worker")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "usage_cores 1\n") || !strings.Contains(string(body), "usage_bytes 1.34217728e+08\n") {
		t.Fatal(string(body))
	}
	if len(dyn.Actions()) != 1 || dyn.Actions()[0].GetNamespace() != "" {
		t.Fatal("must list pod metrics once across namespaces", dyn.Actions())
	}
	if err := dyn.Tracker().Delete(gvr, "b", "same"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.NodeResourceMetrics(context.Background(), "worker"); err == nil {
		t.Fatal("must not silently undercount missing running pod metrics")
	}
}

func TestNodeDeviceAllocation(t *testing.T) {
	gpu := corev1.ResourceName("nvidia.com/gpu")
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker"}, Status: corev1.NodeStatus{Capacity: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("16"), corev1.ResourceMemory: resource.MustParse("32Gi"), gpu: resource.MustParse("8")}, Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("15"), corev1.ResourceMemory: resource.MustParse("30Gi"), gpu: resource.MustParse("6")}}}
	ctr := func(name, count string) corev1.Container {
		return corev1.Container{Name: name, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{gpu: resource.MustParse(count)}}}
	}
	restart := corev1.ContainerRestartPolicyAlways
	sidecar := ctr("sidecar", "1")
	sidecar.RestartPolicy = &restart
	pod := corev1.Pod{Spec: corev1.PodSpec{NodeName: "worker", Containers: []corev1.Container{ctr("app", "2")}, InitContainers: []corev1.Container{sidecar, ctr("init", "4")}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	done := *pod.DeepCopy()
	done.Status.Phase = corev1.PodSucceeded
	other := *pod.DeepCopy()
	other.Spec.NodeName = "elsewhere"
	body := string(nodeResourceBounds(node, []corev1.Pod{pod, done, other}))
	for _, want := range []string{"farshmac_node_capacity_cpu_cores 16", "farshmac_node_allocatable_cpu_cores 15", `farshmac_node_allocated_device{device="nvidia.com/gpu"} 5`, `farshmac_node_allocatable_device{device="nvidia.com/gpu"} 6`, `farshmac_node_capacity_device{device="nvidia.com/gpu"} 8`} {
		if !strings.Contains(body, want) {
			t.Fatal(want, body)
		}
	}
	empty := string(nodeResourceBounds(node, nil))
	if !strings.Contains(empty, `farshmac_node_allocated_device{device="nvidia.com/gpu"} 0`) {
		t.Fatal(empty)
	}
}
