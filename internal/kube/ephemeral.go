package kube

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EphemeralOptions describes an explicit debug-container launch. Command is an
// argv array, never a shell expression. Empty means the image's entrypoint.
type EphemeralOptions struct {
	Image          string
	Command        []string
	Profile        string
	PullPolicy     string
	RunAsUser      *int64
	TTY            bool
	ReadOnlyRoot   bool
	TimeoutSeconds int
}

func (o EphemeralOptions) Validate() error {
	if strings.TrimSpace(o.Image) == "" || strings.ContainsAny(o.Image, " \t\r\n") {
		return fmt.Errorf("enter an image reference without whitespace")
	}
	switch o.Profile {
	case "Default", "Restricted", "Network", "Processes", "Files and shell", "Privileged":
	default:
		return fmt.Errorf("unknown access profile")
	}
	switch o.PullPolicy {
	case "IfNotPresent", "Always", "Never":
	default:
		return fmt.Errorf("unknown image pull policy")
	}
	if o.TimeoutSeconds < 5 || o.TimeoutSeconds > 600 {
		return fmt.Errorf("startup timeout must be 5–600 seconds")
	}
	if o.RunAsUser != nil && *o.RunAsUser < 0 {
		return fmt.Errorf("UID must be nonnegative")
	}
	if o.Profile == "Restricted" && o.RunAsUser != nil && *o.RunAsUser == 0 {
		return fmt.Errorf("Restricted requires a non-root UID")
	}
	for _, arg := range o.Command {
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("command contains NUL")
		}
	}
	if len(o.Command) > 0 && o.Command[0] == "" {
		return fmt.Errorf("command executable must not be empty")
	}
	return nil
}
func (o EphemeralOptions) securityContext() *corev1.SecurityContext {
	no, yes := false, true
	s := &corev1.SecurityContext{RunAsUser: o.RunAsUser, ReadOnlyRootFilesystem: &o.ReadOnlyRoot}
	if o.RunAsUser != nil {
		nonroot := *o.RunAsUser != 0
		s.RunAsNonRoot = &nonroot
	}
	if o.Profile == "Default" {
		return s
	}
	s.AllowPrivilegeEscalation = &no
	s.Capabilities = &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}
	s.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
	switch o.Profile {
	case "Restricted":
		s.RunAsNonRoot = &yes
	case "Network":
		s.Capabilities.Add = []corev1.Capability{"NET_RAW", "NET_ADMIN"}
	case "Files and shell":
		s.Capabilities.Add = []corev1.Capability{"SYS_PTRACE", "DAC_OVERRIDE", "FOWNER", "CHOWN", "FSETID"}
	case "Processes":
		s.Capabilities.Add = []corev1.Capability{"SYS_PTRACE"}
	case "Privileged":
		s.Privileged = &yes
		s.AllowPrivilegeEscalation = &yes
		s.Capabilities = nil
		s.SeccompProfile = nil
	}
	return s
}

// StartEphemeral creates a new user-requested container. It never connects the
// PodToast protocol and never silently retries with broader privileges.
func (c *Client) StartEphemeral(ctx context.Context, ref ContainerRef, o EphemeralOptions) (ContainerRef, error) {
	if err := o.Validate(); err != nil {
		return ContainerRef{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(o.TimeoutSeconds)*time.Second)
	defer cancel()
	pod, err := c.Pod(ctx, ref.Namespace, ref.Pod)
	if err != nil {
		return ContainerRef{}, err
	}
	uid := pod.UID
	if ref.Container != "" {
		found := false
		for _, list := range [][]corev1.ContainerStatus{pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses, pod.Status.EphemeralContainerStatuses} {
			for _, s := range list {
				if s.Name == ref.Container && s.State.Running != nil {
					found = true
				}
			}
		}
		if !found {
			return ContainerRef{}, fmt.Errorf("target container is not running")
		}
	}
	var suffix [6]byte
	if _, err = rand.Read(suffix[:]); err != nil {
		return ContainerRef{}, err
	}
	name := fmt.Sprintf("debug-%x", suffix)
	ec := corev1.EphemeralContainer{TargetContainerName: ref.Container, EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: name, Image: o.Image, Command: o.Command, ImagePullPolicy: corev1.PullPolicy(o.PullPolicy), Stdin: true, TTY: o.TTY, SecurityContext: o.securityContext()}}
	for i := 0; i < 3; i++ {
		next := pod.DeepCopy()
		next.Spec.EphemeralContainers = append(next.Spec.EphemeralContainers, ec)
		_, err = c.core.CoreV1().Pods(ref.Namespace).UpdateEphemeralContainers(ctx, ref.Pod, next, metav1.UpdateOptions{})
		if !apierrors.IsConflict(err) {
			break
		}
		updateErr := err
		pod, err = c.Pod(ctx, ref.Namespace, ref.Pod)
		if err != nil {
			return ContainerRef{}, err
		}
		if pod.UID != uid {
			return ContainerRef{}, fmt.Errorf("pod was replaced")
		}
		err = updateErr
	}
	if err != nil {
		return ContainerRef{}, err
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		pod, err = c.Pod(ctx, ref.Namespace, ref.Pod)
		if err != nil {
			return ContainerRef{}, err
		}
		if pod.UID != uid {
			return ContainerRef{}, fmt.Errorf("pod was replaced")
		}
		for _, s := range pod.Status.EphemeralContainerStatuses {
			if s.Name != name {
				continue
			}
			if s.State.Running != nil || s.State.Terminated != nil {
				return ContainerRef{Namespace: ref.Namespace, Pod: ref.Pod, Container: name}, nil
			}
			if s.State.Waiting != nil {
				switch s.State.Waiting.Reason {
				case "ErrImagePull", "ImagePullBackOff", "CreateContainerConfigError", "CreateContainerError":
					return ContainerRef{}, fmt.Errorf("%s: %s", s.State.Waiting.Reason, s.State.Waiting.Message)
				}
			}
		}
		select {
		case <-ctx.Done():
			return ContainerRef{}, fmt.Errorf("%s was created but startup wait ended: %w", name, ctx.Err())
		case <-ticker.C:
		}
	}
}
