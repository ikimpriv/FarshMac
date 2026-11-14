package kube

import (
	"context"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ContainerState is one container of a pod as the containers level shows it.
// Kind is "" for an ordinary container, "init" or "ephemeral" otherwise.
// State is "running", "waiting:<reason>" or "terminated:<exit code>".
type ContainerState struct {
	Name, Kind string
	State      string
	Started    time.Time
	Ready      bool
}

// PodSummary is what the panel needs from a pod: its phase, node and
// containers in manifest order (init, regular, ephemeral).
type PodSummary struct {
	Phase, Node string
	Containers  []ContainerState
}

// PodSummaryOf reads the container list and statuses of a pod. Containers
// without a status yet (pending pod) are reported as "waiting".
func PodSummaryOf(pod *corev1.Pod) PodSummary {
	s := PodSummary{Phase: string(pod.Status.Phase), Node: pod.Spec.NodeName}
	add := func(name, kind string, statuses []corev1.ContainerStatus) {
		c := ContainerState{Name: name, Kind: kind, State: "waiting"}
		for _, st := range statuses {
			if st.Name != name {
				continue
			}
			c.Ready = st.Ready
			switch {
			case st.State.Running != nil:
				c.State = "running"
				c.Started = st.State.Running.StartedAt.Time
			case st.State.Terminated != nil:
				c.State = fmt.Sprintf("terminated:%d", st.State.Terminated.ExitCode)
				c.Started = st.State.Terminated.StartedAt.Time
			case st.State.Waiting != nil:
				c.State = "waiting"
				if st.State.Waiting.Reason != "" {
					c.State += ":" + st.State.Waiting.Reason
				}
			}
			break
		}
		s.Containers = append(s.Containers, c)
	}
	for _, c := range pod.Spec.InitContainers {
		add(c.Name, "init", pod.Status.InitContainerStatuses)
	}
	for _, c := range pod.Spec.Containers {
		add(c.Name, "", pod.Status.ContainerStatuses)
	}
	for _, c := range pod.Spec.EphemeralContainers {
		add(c.Name, "ephemeral", pod.Status.EphemeralContainerStatuses)
	}
	return s
}

// Regular returns the ordinary (non-init, non-ephemeral) containers.
func (s PodSummary) Regular() []ContainerState {
	var out []ContainerState
	for _, c := range s.Containers {
		if c.Kind == "" {
			out = append(out, c)
		}
	}
	return out
}

// SingleContainer reports the one container a pod has when entering the pod
// should show its file system directly (C3): exactly one regular container
// and no init or ephemeral containers.
func (s PodSummary) SingleContainer() (string, bool) {
	if len(s.Containers) != 1 || s.Containers[0].Kind != "" {
		return "", false
	}
	return s.Containers[0].Name, true
}

// Names returns the container names in a stable order for menus.
func (s PodSummary) Names() []string {
	names := make([]string, 0, len(s.Containers))
	for _, c := range s.Containers {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	return names
}

// Pod fetches one pod, for its container list and status.
func (c *Client) Pod(ctx context.Context, namespace, name string) (*corev1.Pod, error) {
	return c.core.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
}
