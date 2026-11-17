package kube

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
)

func TestPodToastExplicitCreationPolicy(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "test", UID: "uid-1", ResourceVersion: "7"}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "app", ContainerID: "containerd://0123456789abcdef", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	core := fake.NewSimpleClientset(pod)
	c := &Client{core: core}
	ref := ContainerRef{Namespace: "test", Pod: "app", Container: "app"}
	if _, err := c.StartPodToast(context.Background(), ref, ""); err == nil {
		t.Fatal("blank image must not deploy")
	}
	called := false
	core.PrependReactor("update", "pods", func(a kt.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "ephemeralcontainers" {
			t.Fatal(a.GetSubresource())
		}
		next := a.(kt.UpdateAction).GetObject().(*corev1.Pod)
		if next.UID != pod.UID || next.ResourceVersion != "7" || len(next.Spec.EphemeralContainers) != 1 {
			t.Fatal("lost optimistic concurrency or target identity")
		}
		e := next.Spec.EphemeralContainers[0]
		if e.TargetContainerName != "app" || e.Image != "podtoast:test" || strings.Join(e.Command, " ") != "/podtoast idle" {
			t.Fatal(e)
		}
		sc := e.SecurityContext
		if sc == nil || sc.Privileged != nil && *sc.Privileged || *sc.AllowPrivilegeEscalation || *sc.ReadOnlyRootFilesystem {
			t.Fatal("unexpected broad privileges")
		}
		if next.Spec.HostPID || next.Spec.HostNetwork {
			t.Fatal("host namespace requested")
		}
		called = true
		return true, nil, errors.New("creation denied by fixture")
	})
	_, err := c.StartPodToast(context.Background(), ref, "podtoast:test")
	if !called || err == nil || !strings.Contains(err.Error(), "creation denied") {
		t.Fatal(called, err)
	}
}

func TestPodToastReuseNamespaceBoundary(t *testing.T) {
	pod := &corev1.Pod{}
	ec := corev1.EphemeralContainer{TargetContainerName: "app", EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "podtoast-old-123", Image: "toast:v1", Command: []string{"/podtoast", "idle"}}}
	if reusablePodToast(pod, ec, "sidecar", "toast:v1", "podtoast-other-") {
		t.Fatal("crossed private PID namespace")
	}
	if !reusablePodToast(pod, ec, "app", "toast:v1", "podtoast-old-") {
		t.Fatal("lost existing target reuse")
	}
	shared := true
	pod.Spec.ShareProcessNamespace = &shared
	ec.SecurityContext = fullManagementSecurityContext()
	if !reusablePodToast(pod, ec, "sidecar", "toast:v1", "podtoast-other-") {
		t.Fatal("shared agent not reused")
	}
	if reusablePodToast(pod, ec, "sidecar", "toast:v2", "") {
		t.Fatal("wrong image reused")
	}
	ec.Command = []string{"sh"}
	if reusablePodToast(pod, ec, "sidecar", "toast:v1", "") {
		t.Fatal("unrelated debug container reused")
	}
}

func TestPodToastUpgradeCreatesSelectedImage(t *testing.T) {
	running := corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns", UID: "uid"},
		Spec:   corev1.PodSpec{EphemeralContainers: []corev1.EphemeralContainer{{TargetContainerName: "app", EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "podtoast-old", Image: "toast:old", Command: []string{"/podtoast", "idle"}}}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "app", ContainerID: "containerd://0123456789abcdef", State: running}}, EphemeralContainerStatuses: []corev1.ContainerStatus{{Name: "podtoast-old", State: running}}}}
	core := fake.NewSimpleClientset(pod)
	called := false
	core.PrependReactor("update", "pods", func(a kt.Action) (bool, runtime.Object, error) {
		next := a.(kt.UpdateAction).GetObject().(*corev1.Pod)
		if len(next.Spec.EphemeralContainers) != 2 || next.Spec.EphemeralContainers[1].Image != "toast:new" {
			t.Fatal(next.Spec.EphemeralContainers)
		}
		called = true
		return true, nil, errors.New("fixture stopped creation")
	})
	c := &Client{core: core}
	_, err := c.StartPodToastOptions(context.Background(), ContainerRef{Namespace: "ns", Pod: "pod", Container: "app"}, "toast:new", "IfNotPresent", 5)
	if !called || err == nil {
		t.Fatal(called, err)
	}
	pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, corev1.EphemeralContainer{TargetContainerName: "app", EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "podtoast-new", Image: "toast:new", Command: []string{"/podtoast", "idle"}}})
	if ec, _ := RunningPodToast(pod, "app"); ec.Name != "podtoast-old" {
		t.Fatal("pending agent displaced working one")
	}
	pod.Status.EphemeralContainerStatuses = append(pod.Status.EphemeralContainerStatuses, corev1.ContainerStatus{Name: "podtoast-new", State: running})
	if ec, _ := RunningPodToast(pod, "app"); ec.Name != "podtoast-new" {
		t.Fatal("old agent still selected")
	}
	if _, ok := RunningPodToast(pod, "other"); ok {
		t.Fatal("crossed target namespace")
	}
}
