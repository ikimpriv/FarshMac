package kube

import (
	"context"
	"farshmac/internal/podtoast"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"net"
	"strings"
	"time"
)

func trafficCapable(ec corev1.EphemeralContainer) bool {
	if !isPodToastCandidate(ec) || ec.SecurityContext == nil {
		return false
	}
	sc := ec.SecurityContext
	if sc.Privileged != nil && *sc.Privileged {
		return true
	}
	if sc.Capabilities != nil {
		for _, c := range sc.Capabilities.Add {
			if c == "NET_ADMIN" {
				return true
			}
		}
	}
	return false
}

// OpenTraffic only runs after an explicit Start action. No target/PID selection
// is involved: the rules belong to the whole pod network namespace.
func (c *Client) OpenTraffic(ctx context.Context, ref ContainerRef, image string, allowAttach bool) (*podtoast.Client, error) {
	pod, err := c.Pod(ctx, ref.Namespace, ref.Pod)
	if err != nil {
		return nil, err
	}
	if pod.Spec.HostNetwork || pod.Spec.HostPID {
		return nil, fmt.Errorf("traffic control is disabled for host namespaces")
	}
	name := ""
	for i := len(pod.Spec.EphemeralContainers) - 1; i >= 0; i-- {
		ec := pod.Spec.EphemeralContainers[i]
		if !trafficCapable(ec) || (strings.TrimSpace(image) != "" && ec.Image != strings.TrimSpace(image)) {
			continue
		}
		for _, st := range pod.Status.EphemeralContainerStatuses {
			if st.Name == ec.Name && st.State.Running != nil {
				name = ec.Name
				break
			}
		}
		if name != "" {
			break
		}
	}
	if name == "" {
		if !allowAttach {
			return nil, fmt.Errorf("no running PodToast with NET_ADMIN; enable explicit network-agent attachment")
		}
		zero := int64(0)
		network := ref
		network.Container = ""
		created, e := c.StartEphemeral(ctx, network, EphemeralOptions{Image: strings.TrimSpace(image), Command: []string{"/podtoast", "idle"}, Profile: "Network", PullPolicy: "IfNotPresent", RunAsUser: &zero, TimeoutSeconds: 90})
		if e != nil {
			return nil, e
		}
		name = created.Container
	}
	current, err := c.Pod(ctx, ref.Namespace, ref.Pod)
	if err != nil {
		return nil, err
	}
	if current.UID != pod.UID {
		return nil, fmt.Errorf("pod was replaced")
	}
	ref.Container = name
	session, cancel := context.WithCancel(context.Background())
	clientSide, execSide := net.Pipe()
	conn := &podtoast.StreamConn{Reader: clientSide, Writer: clientSide, CloseFunc: func() error { cancel(); execSide.Close(); return clientSide.Close() }}
	go func() {
		defer execSide.Close()
		_ = c.Executor().Stream(session, ref, ExecOptions{Command: []string{"/podtoast", "traffic"}, Stdin: execSide, Stdout: execSide})
	}()
	handshake, stop := context.WithTimeout(ctx, 15*time.Second)
	defer stop()
	client, err := podtoast.Connect(handshake, conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("PodToast traffic handshake: use an image with traffic support: %w", err)
	}
	var cap podtoast.Capability
	if err = client.JSON(handshake, "GET", "/v1/hello", nil, &cap); err != nil || !cap.Traffic {
		client.Close()
		return nil, fmt.Errorf("PodToast image does not support traffic control")
	}
	return client, nil
}
