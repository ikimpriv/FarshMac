package kube

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"path"
	"strings"
	"time"

	"farshmac/internal/podtoast"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StartPodToast is explicitly invoked by the user, never by a directory read.
// It creates/reuses an ephemeral container and opens one multiplexed exec.
// Cancellation stops the connection/wait, not an already-created container.
func (c *Client) StartPodToast(ctx context.Context, ref ContainerRef, image string) (*podtoast.Client, error) {
	return c.StartPodToastOptions(ctx, ref, image, "IfNotPresent", 90)
}
func (c *Client) StartPodToastOptions(ctx context.Context, ref ContainerRef, image, pullPolicy string, timeout int) (*podtoast.Client, error) {
	return c.startPodToastImage(ctx, ref, image, pullPolicy, timeout, true)
}

// EnsurePodToastImage is only called after explicit user approval to add a toolbox.
// It may reuse that exact image, but must not reuse a legacy shell-free image.
func (c *Client) EnsurePodToastImage(ctx context.Context, ref ContainerRef, image string) (*podtoast.Client, error) {
	return c.startPodToastImage(ctx, ref, image, "IfNotPresent", 90, true)
}
func (c *Client) startPodToastImage(ctx context.Context, ref ContainerRef, image, pullPolicy string, timeout int, exactImage bool) (*podtoast.Client, error) {
	if timeout < 5 || timeout > 600 {
		return nil, fmt.Errorf("startup timeout must be 5–600 seconds")
	}
	switch pullPolicy {
	case "Always", "IfNotPresent", "Never":
	default:
		return nil, fmt.Errorf("invalid pull policy")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	if strings.TrimSpace(image) == "" {
		return nil, fmt.Errorf("configure a published PodToast image or a locally loaded image first")
	}
	if !exactImage {
		if existing, e := c.ConnectExistingPodToast(ctx, ref); e == nil {
			return existing, nil
		} else if !errors.Is(e, ErrNoPodToast) {
			return nil, e
		}
	}

	pod, err := c.Pod(ctx, ref.Namespace, ref.Pod)
	if err != nil {
		return nil, err
	}
	if pod.Spec.HostPID || pod.Spec.HostNetwork {
		return nil, fmt.Errorf("PodToast does not attach to hostPID/hostNetwork pods")
	}
	cid := ""
	statuses := append(append(append([]corev1.ContainerStatus{}, pod.Status.ContainerStatuses...), pod.Status.InitContainerStatuses...), pod.Status.EphemeralContainerStatuses...)
	for _, st := range statuses {
		if st.Name == ref.Container && st.State.Running != nil {
			_, cid, _ = strings.Cut(st.ContainerID, "://")
		}
	}
	if cid == "" {
		return nil, fmt.Errorf("target must be a running container")
	}
	targetUID := pod.UID
	hash := sha256.Sum256([]byte(cid + "\x00" + image))
	prefix := fmt.Sprintf("podtoast-%x-", hash[:5])
	name := ""
	for _, e := range pod.Spec.EphemeralContainers {
		if reusablePodToast(pod, e, ref.Container, image, prefix) {
			ended := false
			for _, st := range pod.Status.EphemeralContainerStatuses {
				if st.Name == e.Name && st.State.Terminated != nil {
					ended = true
				}
			}
			if !ended {
				name = e.Name
				break
			}
		}
	}
	if name == "" {
		var suffix [4]byte
		if _, err = rand.Read(suffix[:]); err != nil {
			return nil, err
		}
		name = fmt.Sprintf("%s%x", prefix, suffix)
		zero := int64(0)
		no := false
		ec := corev1.EphemeralContainer{TargetContainerName: ref.Container, EphemeralContainerCommon: corev1.EphemeralContainerCommon{
			Name: name, Image: image, ImagePullPolicy: corev1.PullPolicy(pullPolicy), Command: []string{"/podtoast", "idle"},
			SecurityContext: &corev1.SecurityContext{RunAsUser: &zero, RunAsNonRoot: &no, AllowPrivilegeEscalation: &no, ReadOnlyRootFilesystem: &no,
				Capabilities:   &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}, Add: []corev1.Capability{"SYS_PTRACE", "SYS_CHROOT", "DAC_OVERRIDE", "FOWNER", "CHOWN", "FSETID"}},
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
		}}
		for attempt := 0; attempt < 3; attempt++ {
			next := pod.DeepCopy()
			next.Spec.EphemeralContainers = append(next.Spec.EphemeralContainers, ec)
			_, err = c.core.CoreV1().Pods(ref.Namespace).UpdateEphemeralContainers(ctx, ref.Pod, next, metav1.UpdateOptions{})
			if !apierrors.IsConflict(err) {
				break
			}
			updateErr := err
			pod, err = c.Pod(ctx, ref.Namespace, ref.Pod)
			if err != nil {
				return nil, err
			}
			err = updateErr
			if pod.UID != targetUID {
				return nil, fmt.Errorf("pod was replaced")
			}
		}
		if err != nil {
			return nil, fmt.Errorf("create PodToast ephemeral container: %w", err)
		}
	}
	waitCtx, cancelWait := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancelWait()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		pod, err = c.Pod(waitCtx, ref.Namespace, ref.Pod)
		if err != nil {
			return nil, err
		}
		if pod.UID != targetUID {
			return nil, fmt.Errorf("pod was replaced")
		}
		running := false
		for _, st := range pod.Status.EphemeralContainerStatuses {
			if st.Name == name {
				if st.State.Running != nil {
					running = true
				}
				if st.State.Terminated != nil {
					return nil, fmt.Errorf("PodToast exited: %s", st.State.Terminated.Message)
				}
				if st.State.Waiting != nil && (st.State.Waiting.Reason == "ImagePullBackOff" || st.State.Waiting.Reason == "ErrImagePull" || st.State.Waiting.Reason == "CreateContainerConfigError") {
					return nil, fmt.Errorf("PodToast %s: %s", st.State.Waiting.Reason, st.State.Waiting.Message)
				}
			}
		}
		if running {
			break
		}
		select {
		case <-waitCtx.Done():
			return nil, waitCtx.Err()
		case <-ticker.C:
		}
	}
	client, err := c.connectPodToast(ctx, ref, name, cid, pod.Spec.ShareProcessNamespace != nil && *pod.Spec.ShareProcessNamespace)
	if err == nil {
		client.AgentName = name
		client.ImageRef = image
	}
	return client, err
}

// podToastSharedManagement is conservative: PID visibility alone does not grant
// access to another container's root, ownership or permissions. This matches
// the capabilities requested by FarshMac's full filesystem integration.
func podToastSharedManagement(pod *corev1.Pod, ec corev1.EphemeralContainer) bool {
	if pod.Spec.ShareProcessNamespace == nil || !*pod.Spec.ShareProcessNamespace || pod.Spec.HostPID || pod.Spec.HostNetwork {
		return false
	}
	sc := ec.SecurityContext
	if sc == nil {
		return false
	}
	uid := sc.RunAsUser
	if uid == nil && pod.Spec.SecurityContext != nil {
		uid = pod.Spec.SecurityContext.RunAsUser
	}
	if uid == nil || *uid != 0 {
		return false
	}
	if sc.Privileged != nil && *sc.Privileged {
		return true
	}
	if sc.Capabilities == nil {
		return false
	}
	caps := map[corev1.Capability]bool{}
	for _, c := range sc.Capabilities.Add {
		caps[c] = true
	}
	for _, c := range []corev1.Capability{"SYS_PTRACE", "SYS_CHROOT", "DAC_OVERRIDE", "FOWNER", "CHOWN", "FSETID"} {
		if !caps[c] {
			return false
		}
	}
	return true
}

func reusablePodToast(pod *corev1.Pod, e corev1.EphemeralContainer, target, image, prefix string) bool {
	if e.Image != image || len(e.Command) != 2 || e.Command[0] != "/podtoast" || e.Command[1] != "idle" {
		return false
	}
	if podToastSharedManagement(pod, e) {
		return true
	}
	return strings.HasPrefix(e.Name, prefix) && e.TargetContainerName == target
}

// connectPodToast opens a separate exec session without creating a container.
func (c *Client) connectPodToast(ctx context.Context, ref ContainerRef, name, cid string, shared bool) (*podtoast.Client, error) {
	command := []string{"/podtoast", "serve", "--target-id", cid}
	if shared {
		command = append(command, "--shared-processes")
	}
	// Lifetime belongs to the client, not the startup operation. Only the
	// handshake inherits ctx; subsequent FS handles carry their own contexts.
	sessionCtx, cancel := context.WithCancel(context.Background())
	clientConn, execConn := net.Pipe()
	conn := &podtoast.StreamConn{Reader: clientConn, Writer: clientConn, CloseFunc: func() error { cancel(); _ = execConn.Close(); return clientConn.Close() }}
	streamDone := make(chan string, 1)
	go func() {
		defer execConn.Close()
		defer cancel()
		var stderr bytes.Buffer
		streamErr := c.Executor().Stream(sessionCtx, ContainerRef{Namespace: ref.Namespace, Pod: ref.Pod, Container: name}, ExecOptions{Command: command, Stdin: execConn, Stdout: execConn, Stderr: &limitedWriter{w: &stderr, limit: 4096}})
		message := strings.TrimSpace(stderr.String())
		if message == "" && streamErr != nil {
			message = streamErr.Error()
		}
		streamDone <- message
	}()
	handshake, cancelHandshake := context.WithTimeout(ctx, 20*time.Second)
	defer cancelHandshake()
	client, err := podtoast.Connect(handshake, conn)
	if err != nil {
		conn.Close()
		select {
		case detail := <-streamDone:
			if detail != "" {
				return nil, fmt.Errorf("PodToast: %s", detail)
			}
		case <-time.After(time.Second):
		}
		return nil, fmt.Errorf("PodToast handshake (target namespace, capabilities and runtime must permit access): %w", err)
	}
	return client, nil
}

var ErrNoPodToast = errors.New("no running PodToast for this target")

// ConnectExistingPodToast never creates or updates a Kubernetes resource. It
// reconnects across FarshMac restarts and accepts compatible custom image names.
func (c *Client) ConnectExistingPodToast(ctx context.Context, ref ContainerRef) (*podtoast.Client, error) {
	pod, err := c.Pod(ctx, ref.Namespace, ref.Pod)
	if err != nil {
		return nil, err
	}
	if pod.Spec.HostPID || pod.Spec.HostNetwork {
		return nil, ErrNoPodToast
	}
	shared := pod.Spec.ShareProcessNamespace != nil && *pod.Spec.ShareProcessNamespace
	cid := ""
	statuses := append(append([]corev1.ContainerStatus{}, pod.Status.ContainerStatuses...), pod.Status.InitContainerStatuses...)
	for _, st := range statuses {
		if st.Name == ref.Container && st.State.Running != nil {
			_, cid, _ = strings.Cut(st.ContainerID, "://")
		}
	}
	if ec, ok := RunningPodToast(pod, ref.Container); ok {
		if cid == "" {
			return nil, fmt.Errorf("PodToast target %s is not running", ref.Container)
		}
		client, err := c.connectPodToast(ctx, ref, ec.Name, cid, shared)
		if err == nil {
			client.AgentName = ec.Name
			client.ImageRef = ec.Image
		}
		return client, err
	}
	return nil, ErrNoPodToast
}

// Image/name hints identify candidates only; the HTTP/2 handshake validates
// the agent before a filesystem is adopted. This also supports default ENTRYPOINT.
func isPodToastCandidate(ec corev1.EphemeralContainer) bool {
	if len(ec.Command) == 2 && ec.Command[0] == "/podtoast" && ec.Command[1] == "idle" {
		return true
	}
	image, _, _ := strings.Cut(ec.Image, "@")
	image, _, _ = strings.Cut(path.Base(image), ":")
	return len(ec.Command) == 0 && (image == "podtoast" || strings.HasPrefix(ec.Name, "podtoast-"))
}

// RunningPodToast selects the most recently attached running agent covering the
// target. A failed/waiting replacement never displaces a working agent.
func RunningPodToast(pod *corev1.Pod, target string) (corev1.EphemeralContainer, bool) {
	if pod == nil || pod.Spec.HostPID || pod.Spec.HostNetwork {
		return corev1.EphemeralContainer{}, false
	}
	for i := len(pod.Spec.EphemeralContainers) - 1; i >= 0; i-- {
		ec := pod.Spec.EphemeralContainers[i]
		if !isPodToastCandidate(ec) || (!podToastSharedManagement(pod, ec) && ec.TargetContainerName != target) {
			continue
		}
		for _, st := range pod.Status.EphemeralContainerStatuses {
			if st.Name == ec.Name && st.State.Running != nil {
				return ec, true
			}
		}
	}
	return corev1.EphemeralContainer{}, false
}
