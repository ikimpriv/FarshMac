package kube

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func TestRetireSharedAgentRequiresFullReplacement(t *testing.T) {
	running := corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	shared := true
	old := corev1.EphemeralContainer{TargetContainerName: "a", EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "old", Command: []string{"/podtoast", "idle"}, SecurityContext: fullManagementSecurityContext()}}
	replacement := corev1.EphemeralContainer{TargetContainerName: "a", EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "new", Command: []string{"/podtoast", "idle"}}}
	pod := &corev1.Pod{Spec: corev1.PodSpec{ShareProcessNamespace: &shared, EphemeralContainers: []corev1.EphemeralContainer{old, replacement}}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "a", State: running}, {Name: "b", State: running}}, EphemeralContainerStatuses: []corev1.ContainerStatus{{Name: "old", State: running}, {Name: "new", State: running}}}}
	if CanRetirePodToast(pod, "old", "new") {
		t.Fatal("retired agent still covering b")
	}
	pod.Spec.EphemeralContainers[1].SecurityContext = fullManagementSecurityContext()
	if !CanRetirePodToast(pod, "old", "new") {
		t.Fatal("fully replaced agent retained")
	}
	if CanRetirePodToast(pod, "new", "new") {
		t.Fatal("retired replacement")
	}
}

type retireDockerExec struct{}

func (retireDockerExec) Stream(ctx context.Context, ref ContainerRef, o ExecOptions) error {
	args := append([]string{"exec", "-i", ref.Container}, o.Command...)
	c := exec.CommandContext(ctx, "docker", args...)
	c.Stdin = o.Stdin
	c.Stdout = o.Stdout
	c.Stderr = o.Stderr
	return c.Run()
}
func TestDockerLegacyAgentRetirement(t *testing.T) {
	image := os.Getenv("PODTOAST_RETIRE_TEST_IMAGE")
	if image == "" {
		t.Skip("opt-in isolated Docker lifecycle test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, "docker", args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		b, e := cmd.Output()
		if e != nil {
			t.Fatalf("%v: %s %s", e, b, stderr.String())
		}
		return strings.TrimSpace(string(b))
	}
	target := run("run", "-d", "--network=none", "--entrypoint=/bin/sh", image, "-c", "sleep 120")
	defer exec.Command("docker", "rm", "-f", target).Run()
	old := run("run", "-d", "--pid=container:"+target, "--network=container:"+target, "--cap-add=SYS_PTRACE", "ghcr.io/ikimpriv/podtoast:0.8.0", "idle")
	defer exec.Command("docker", "rm", "-f", old).Run()
	newAgent := run("run", "-d", "--pid=container:"+target, "--network=container:"+target, "--cap-add=SYS_PTRACE", image, "idle")
	defer exec.Command("docker", "rm", "-f", newAgent).Run()
	run("exec", "-d", old, "/bin/sh", "-c", "sleep 3")
	started := time.Now()
	err := retirePodToastUsing(ctx, retireDockerExec{}, ContainerRef{}, old, newAgent)
	// Termination may cut off the helper exec before it can report success.
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if run("inspect", "-f", "{{.State.Running}}", old) == "false" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if run("inspect", "-f", "{{.State.Running}}", old) != "false" {
		t.Fatal("legacy agent still running", err)
	}
	if time.Since(started) < 2*time.Second {
		t.Fatal("active shell was not drained")
	}
	for _, id := range []string{target, newAgent} {
		if run("inspect", "-f", "{{.State.Running}}", id) != "true" {
			t.Fatal("terminated another container")
		}
	}
}
