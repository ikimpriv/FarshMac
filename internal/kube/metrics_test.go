package kube

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func TestMetricsDiscoverySelectorsAndNoDeployment(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "apps", UID: "one", Labels: map[string]string{"app": "demo"}, Annotations: map[string]string{"prometheus.io/scrape": "true", "prometheus.io/port": "metrics"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Ports: []corev1.ContainerPort{{Name: "metrics", ContainerPort: 8080}}}}}}
	monitor := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "monitoring.coreos.com/v1", "kind": "PodMonitor", "metadata": map[string]any{"name": "test", "namespace": "monitoring"}, "spec": map[string]any{"namespaceSelector": map[string]any{"matchNames": []any{"apps"}}, "selector": map[string]any{"matchLabels": map[string]any{"app": "demo"}}, "podMetricsEndpoints": []any{map[string]any{"port": "metrics", "path": "/custom", "params": map[string]any{"format": []any{"prometheus"}}}}}}}
	gvr := schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "podmonitors"}
	c := &Client{core: fake.NewSimpleClientset(pod), dyn: dfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: "PodMonitorList"}, monitor)}
	d, err := c.DiscoverMetrics(context.Background(), ContainerRef{Namespace: "apps", Pod: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if d.PodUID != "one" || len(d.Endpoints) != 4 {
		t.Fatal(d)
	}
	found := false
	for _, ep := range d.Endpoints {
		if ep.Path == "/custom" {
			found = true
			if ep.Params.Get("format") != "prometheus" {
				t.Fatal(ep)
			}
		}
	}
	if !found {
		t.Fatal("monitor not matched")
	}
	for _, a := range c.core.(*fake.Clientset).Actions() {
		if a.GetVerb() != "get" {
			t.Fatal("discovery mutated cluster", a)
		}
	}
	monitor.Object["spec"].(map[string]any)["selector"] = map[string]any{"matchLabels": map[string]any{"app": "other"}}
	c.dyn = dfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: "PodMonitorList"}, monitor)
	d, err = c.DiscoverMetrics(context.Background(), ContainerRef{Namespace: "apps", Pod: "app"})
	if err != nil || len(d.Endpoints) != 3 {
		t.Fatal(d, err)
	}
}
func TestMetricsPathBoundaries(t *testing.T) {
	for _, p := range []string{"https://other/metrics", "//other/metrics", "/../secrets", "/metrics?token=x"} {
		if metricPath(p) {
			t.Fatal(p)
		}
	}
	if !metricPath("/actuator/prometheus") {
		t.Fatal("valid path")
	}
}

func TestSharedAgentMetricsCoverAllContainers(t *testing.T) {
	shared := true
	running := corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "test"}, Spec: corev1.PodSpec{ShareProcessNamespace: &shared, EphemeralContainers: []corev1.EphemeralContainer{{TargetContainerName: "app", EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "toast", Command: []string{"/podtoast", "idle"}}}}}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "app", ContainerID: "containerd://aaa", State: running}, {Name: "sidecar", ContainerID: "containerd://bbb", State: running}}, EphemeralContainerStatuses: []corev1.ContainerStatus{{Name: "toast", State: running}}}}
	pod.Spec.EphemeralContainers[0].SecurityContext = fullManagementSecurityContext()
	gvr := schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "podmonitors"}
	c := &Client{core: fake.NewSimpleClientset(pod), dyn: dfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: "PodMonitorList"})}
	d, err := c.DiscoverMetrics(context.Background(), ContainerRef{Namespace: "test", Pod: "app"})
	if err != nil || len(d.Endpoints) != 2 {
		t.Fatal(d, err)
	}
	if d.Endpoints[0].ID == d.Endpoints[1].ID {
		t.Fatal("container series collapsed")
	}
	newer := pod.Spec.EphemeralContainers[0].DeepCopy()
	newer.Name = "toast-new"
	newer.Image = "toast:0.8.0"
	pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, *newer)
	pod.Status.EphemeralContainerStatuses = append(pod.Status.EphemeralContainerStatuses, corev1.ContainerStatus{Name: newer.Name, State: running})
	if _, err = c.core.CoreV1().Pods("test").Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	d, err = c.DiscoverMetrics(context.Background(), ContainerRef{Namespace: "test", Pod: "app"})
	if err != nil || len(d.Endpoints) != 2 {
		t.Fatal(d, err)
	}
	for _, ep := range d.Endpoints {
		if ep.Container != "toast-new" {
			t.Fatal("metrics still use superseded agent", ep)
		}
	}

}
