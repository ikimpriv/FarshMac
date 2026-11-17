package kube

import (
	"context"
	"errors"
	"fmt"
	"io"
	corev1 "k8s.io/api/core/v1"
	"strings"
	"testing"
)

type toolboxExecutor struct {
	calls  []string
	denied bool
}

func (e *toolboxExecutor) Stream(ctx context.Context, ref ContainerRef, opts ExecOptions) error {
	e.calls = append(e.calls, ref.Container)
	if e.denied {
		return errors.New("forbidden: pods/exec")
	}
	if ref.Container == "old" {
		return fmt.Errorf("exec %s: no such file or directory", opts.Command[0])
	}
	output := "tar=1\ncat=1\nls=1\n"
	if strings.HasPrefix(opts.Command[2], "for f in") {
		output = "42\n"
	} else if strings.HasPrefix(opts.Command[2], "stat -Lc") {
		output = "7:123\n"
	}
	_, err := io.WriteString(opts.Stdout, output)
	return err
}
func TestDebugShellFallsBackFromPreferredShellFreeAgent(t *testing.T) {
	pod := &corev1.Pod{}
	pod.Spec.EphemeralContainers = []corev1.EphemeralContainer{
		{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "old", Image: "ghcr.io/ikimpriv/podtoast:0.1.0"}, TargetContainerName: "app"},
		{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "new", Image: "ghcr.io/ikimpriv/podtoast:0.7.0"}, TargetContainerName: "app"},
	}
	running := corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", ContainerID: "containerd://target-id", State: running}}
	pod.Status.EphemeralContainerStatuses = []corev1.ContainerStatus{{Name: "old", State: running}, {Name: "new", State: running}}
	executor := &toolboxExecutor{}
	ref := ContainerRef{Namespace: "ns", Pod: "pod", Container: "app"}
	result, err := resolveDebugShell(context.Background(), pod, ref, "old", executor)
	if err != nil {
		t.Fatal(err)
	}
	if result.Ref.Container != "new" || result.Root != "/proc/42/root" || executor.calls[0] != "old" {
		t.Fatalf("wrong execution target: %+v, %v", result, executor.calls)
	}
	pod.Spec.EphemeralContainers = pod.Spec.EphemeralContainers[:1]
	if _, err = resolveDebugShell(context.Background(), pod, ref, "old", executor); !errors.Is(err, ErrNoDebugToolbox) {
		t.Fatalf("expected explicit upgrade path: %v", err)
	}
	executor.denied = true
	if _, err = resolveDebugShell(context.Background(), pod, ref, "old", executor); err == nil || errors.Is(err, ErrNoDebugToolbox) {
		t.Fatalf("RBAC must not offer another attachment: %v", err)
	}
}
