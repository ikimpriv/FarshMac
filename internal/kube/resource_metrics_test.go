package kube

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	"strings"
	"testing"
)

func TestResourceMetricsQuantitiesAndIdentity(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns", UID: "uid"}}
	metric := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "metrics.k8s.io/v1beta1", "kind": "PodMetrics", "metadata": map[string]any{"name": "pod", "namespace": "ns"}, "containers": []any{map[string]any{"name": "app", "usage": map[string]any{"cpu": "250m", "memory": "16Mi"}}}}}
	pod.Spec.EphemeralContainers = []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "agent", Command: []string{"/podtoast", "idle"}}}}
	metric.Object["containers"] = append(metric.Object["containers"].([]any), map[string]any{"name": "agent", "usage": map[string]any{"cpu": "500m", "memory": "32Mi"}})
	dyn := dfake.NewSimpleDynamicClient(runtime.NewScheme())
	if err := dyn.Tracker().Create(schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}, metric, "ns"); err != nil {
		t.Fatal(err)
	}
	c := &Client{core: fake.NewSimpleClientset(pod), dyn: dyn}
	ref := ContainerRef{Namespace: "ns", Pod: "pod"}
	data, err := c.ResourceMetrics(context.Background(), ref, "uid")
	if err != nil || !strings.Contains(string(data), "0.25") || !strings.Contains(string(data), "1.6777216e+07") {
		t.Fatal(string(data), err)
	}
	if strings.Contains(string(data), `container="agent"`) {
		t.Fatal("agent exposed as target metrics", string(data))
	}
	if _, err = c.ResourceMetrics(context.Background(), ref, "old"); err == nil {
		t.Fatal("replacement pod accepted")
	}
}

func TestResourceBoundsWithoutMetricsServer(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns", UID: "uid"}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi"), corev1.ResourceEphemeralStorage: resource.MustParse("1Gi")}}}}}}
	c := &Client{core: fake.NewSimpleClientset(pod)}
	data, err := c.ResourceBounds(context.Background(), ContainerRef{Namespace: "ns", Pod: "pod"}, "uid")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`farshmac_container_request_cpu_cores{container="app"} 0.25`, `farshmac_container_limit_memory_bytes{container="app"} 6.7108864e+07`, `farshmac_container_limit_ephemeral_storage_bytes{container="app"} 1.073741824e+09`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %s: %s", want, data)
		}
	}
	if strings.Contains(string(data), "limit_cpu") {
		t.Fatal("invented CPU limit")
	}
	if _, err = c.ResourceBounds(context.Background(), ContainerRef{Namespace: "ns", Pod: "pod"}, "old"); err == nil {
		t.Fatal("replacement accepted")
	}
}
