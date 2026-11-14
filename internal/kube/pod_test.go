package kube

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// samplePod has an init container, one running container and one
// crash-looping ephemeral debug container.
func samplePod() *corev1.Pod {
	started := metav1.NewTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "app-1", Namespace: "demo"},
		Spec: corev1.PodSpec{
			NodeName:            "node-1",
			InitContainers:      []corev1.Container{{Name: "setup"}},
			Containers:          []corev1.Container{{Name: "app"}},
			EphemeralContainers: []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "debug"}}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			InitContainerStatuses: []corev1.ContainerStatus{{Name: "setup", State: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, StartedAt: started},
			}}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "app", Ready: true, State: corev1.ContainerState{
				Running: &corev1.ContainerStateRunning{StartedAt: started},
			}}},
			EphemeralContainerStatuses: []corev1.ContainerStatus{{Name: "debug", State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
			}}},
		},
	}
}
