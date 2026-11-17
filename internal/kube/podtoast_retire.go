package kube

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// RetireSupersededPodToast runs only after replacement connections are adopted.
// Every target covered by the old agent must now have a running replacement;
// otherwise retiring a shared agent could strand another container.
func (c *Client) RetireSupersededPodToast(ctx context.Context, ref ContainerRef, oldNames []string, replacement string) error {
	var failures []string
	for _, old := range oldNames {
		pod, err := c.Pod(ctx, ref.Namespace, ref.Pod)
		if err != nil {
			return err
		}
		if !CanRetirePodToast(pod, old, replacement) {
			failures = append(failures, old+": still covers targets without a replacement")
			continue
		}
		// Verify each replacement handshake, including shared-PID targets
		// that were not selected in the attach dialog.
		verified := true
		var previous corev1.EphemeralContainer
		for _, ec := range pod.Spec.EphemeralContainers {
			if ec.Name == old {
				previous = ec
			}
		}
		statuses := append(append([]corev1.ContainerStatus{}, pod.Status.ContainerStatuses...), pod.Status.InitContainerStatuses...)
		for _, st := range statuses {
			if st.State.Running == nil || (!podToastSharedManagement(pod, previous) && previous.TargetContainerName != st.Name) {
				continue
			}
			target := ref
			target.Container = st.Name
			probe, e := c.ConnectExistingPodToast(ctx, target)
			if e != nil {
				failures = append(failures, old+": replacement verification for "+st.Name+": "+e.Error())
				verified = false
				break
			}
			same := probe.AgentName == old
			probe.Close()
			if same {
				failures = append(failures, old+": still selected for "+st.Name)
				verified = false
				break
			}
		}
		if !verified {
			continue
		}
		err = retirePodToastUsing(ctx, c.Executor(), ref, old, replacement)
		// Container shutdown can end the exec stream before it acknowledges
		// success; the Pod status is authoritative.
		stopped := false
		for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline) && ctx.Err() == nil; {
			current, e := c.Pod(ctx, ref.Namespace, ref.Pod)
			if e != nil || current.UID != pod.UID {
				break
			}
			for _, status := range current.Status.EphemeralContainerStatuses {
				if status.Name == old && status.State.Terminated != nil {
					stopped = true
				}
			}
			if stopped {
				break
			}
			select {
			case <-ctx.Done():
			case <-time.After(250 * time.Millisecond):
			}
		}
		if stopped {
			continue
		}
		if err == nil {
			err = fmt.Errorf("termination not confirmed; agent may still be stopping")
		}
		if err != nil {
			failures = append(failures, old+": "+err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

func CanRetirePodToast(pod *corev1.Pod, old, replacement string) bool {
	if old == replacement || pod.Spec.HostPID || pod.Spec.HostNetwork {
		return false
	}
	var candidate *corev1.EphemeralContainer
	replacementRunning := false
	for i := range pod.Spec.EphemeralContainers {
		ec := &pod.Spec.EphemeralContainers[i]
		if ec.Name == old && isPodToastCandidate(*ec) {
			candidate = ec
		}
		if ec.Name == replacement && isPodToastCandidate(*ec) {
			for _, st := range pod.Status.EphemeralContainerStatuses {
				if st.Name == replacement && st.State.Running != nil {
					replacementRunning = true
				}
			}
		}
	}
	if candidate == nil || !replacementRunning {
		return false
	}
	covered := false
	for _, st := range append(append([]corev1.ContainerStatus{}, pod.Status.ContainerStatuses...), pod.Status.InitContainerStatuses...) {
		if st.State.Running == nil || (!podToastSharedManagement(pod, *candidate) && st.Name != candidate.TargetContainerName) {
			continue
		}
		newest, ok := RunningPodToast(pod, st.Name)
		if !ok || newest.Name == old {
			return false
		}
		covered = true
	}
	return covered
}

// Copy the verified replacement's helper into the OLD toolbox's private /tmp.
// This supports legacy agents without a stop command. Never write into the
// application's filesystem and never execute a command in an application.
func retirePodToastUsing(ctx context.Context, executor Executor, ref ContainerRef, old, replacement string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	done := make(chan error, 1)
	source := ref
	source.Container = replacement
	go func() {
		err := executor.Stream(ctx, source, ExecOptions{Command: []string{"/podtoast", "export-self"}, Stdout: writer})
		writer.CloseWithError(err)
		done <- err
	}()
	target := ref
	target.Container = old
	// exec replaces the staging shell, so it cannot keep the old toolbox busy.
	command := `set -eu; umask 077; d=$(mktemp -d /tmp/podtoast-retire.XXXXXX); trap 'rm -f "$d/helper"; rmdir "$d" 2>/dev/null || true' EXIT; cat > "$d/helper"; chmod 700 "$d/helper"; exec "$d/helper" retire --temporary`
	var stderr limitedRetireError
	err := executor.Stream(ctx, target, ExecOptions{Command: []string{"/bin/sh", "-c", command}, Stdin: io.LimitReader(reader, 32<<20), Stderr: &stderr})
	reader.Close()
	cancel()
	sourceErr := <-done
	if err != nil {
		return fmt.Errorf("safe retirement unavailable: %v %s", err, stderr.text)
	}
	return sourceErr
}

type limitedRetireError struct{ text string }

func (b *limitedRetireError) Write(p []byte) (int, error) {
	n := len(p)
	if len(b.text) < 2048 {
		b.text += string(p[:min(len(p), 2048-len(b.text))])
	}
	return n, nil
}
