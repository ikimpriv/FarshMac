package kube

import (
	"encoding/json"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"testing"
)

func TestPodToastPresentationAndCoverage(t *testing.T) {
	pod := samplePod()
	pod.Spec.InitContainers = nil
	pod.Spec.EphemeralContainers[0].Command = []string{"/podtoast", "idle"}
	pod.Spec.EphemeralContainers[0].TargetContainerName = "app"
	pod.Status.EphemeralContainerStatuses[0].State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	s := PodSummaryOf(pod)
	if name, ok := s.SingleContainer(); !ok || name != "app" || !s.Covered() {
		t.Fatalf("agent should be transparent: %+v", s)
	}
	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: "sidecar"})
	pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: "sidecar", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}})
	if PodSummaryOf(pod).Containers[1].PodToast {
		t.Fatal("isolated agent covers wrong target")
	}
	shared := true
	pod.Spec.ShareProcessNamespace = &shared
	if PodSummaryOf(pod).Containers[1].PodToast {
		t.Fatal("shared PID without management rights must not mark other targets")
	}
	pod.Spec.EphemeralContainers[0].SecurityContext = fullManagementSecurityContext()
	if !PodSummaryOf(pod).Containers[1].PodToast {
		t.Fatal("shared agent should cover sidecar")
	}
	raw, _ := json.Marshal(pod)
	r := Resource{GVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}, Kind: "Pod"}
	objects, _, err := decodeWatchObject(raw, r, "demo", nil)
	if err != nil || len(objects) != 1 || objects[0].Pod == nil || !objects[0].Pod.Covered() {
		t.Fatalf("watch coverage: %v %+v", err, objects)
	}
	pod.Status.EphemeralContainerStatuses[0].State = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{}}
	if PodSummaryOf(pod).Covered() {
		t.Fatal("stopped agent still covers target")
	}
	pod.Spec.EphemeralContainers[0].Command = []string{"sh"}
	if len(PodSummaryOf(pod).Containers) != 3 {
		t.Fatal("unrelated debugger hidden")
	}
}

func fullManagementSecurityContext() *corev1.SecurityContext {
	zero := int64(0)
	return &corev1.SecurityContext{RunAsUser: &zero, Capabilities: &corev1.Capabilities{Add: []corev1.Capability{"SYS_PTRACE", "SYS_CHROOT", "DAC_OVERRIDE", "FOWNER", "CHOWN", "FSETID"}}}
}

func TestSharedPIDWithoutManagementRights(t *testing.T) {
	pod := samplePod()
	shared := true
	pod.Spec.ShareProcessNamespace = &shared
	ec := pod.Spec.EphemeralContainers[0]
	if podToastSharedManagement(pod, ec) {
		t.Fatal("PID sharing alone grants no management rights")
	}
	ec.SecurityContext = fullManagementSecurityContext()
	if !podToastSharedManagement(pod, ec) {
		t.Fatal("full management agent not recognized")
	}
	ec.SecurityContext.Capabilities.Add = []corev1.Capability{"SYS_PTRACE"}
	if podToastSharedManagement(pod, ec) {
		t.Fatal("process access alone is not full management")
	}
}
