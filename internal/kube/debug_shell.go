package kube

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

var ErrNoDebugToolbox = errors.New("no compatible ephemeral shell/toolbox")

var ErrNoDebugShell = errors.New("no running ephemeral container for this target")

// DebugShell keeps tools in the debugger root; only cwd enters the target root.
// Absolute paths and absolute symlinks retain debug-container semantics.
type DebugShell struct {
	PodToast     bool
	Ref          ContainerRef
	Target       ContainerRef
	Probe        Probe
	Root         string
	RootIdentity string
}

func (c *Client) ResolveDebugShell(ctx context.Context, target ContainerRef, preferred string) (DebugShell, error) {
	pod, err := c.Pod(ctx, target.Namespace, target.Pod)
	if err != nil {
		return DebugShell{}, err
	}
	return resolveDebugShell(ctx, pod, target, preferred, c.Executor())
}

func resolveDebugShell(ctx context.Context, pod *corev1.Pod, target ContainerRef, preferred string, executor Executor) (DebugShell, error) {
	if pod.Spec.HostPID || pod.Spec.HostNetwork {
		return DebugShell{}, fmt.Errorf("debug filesystem integration does not support host namespaces")
	}
	cid := ""
	statuses := append(pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses...)
	for _, st := range statuses {
		if st.Name == target.Container && st.State.Running != nil {
			_, cid, _ = strings.Cut(st.ContainerID, "://")
		}
	}
	shared := pod.Spec.ShareProcessNamespace != nil && *pod.Spec.ShareProcessNamespace
	var candidateError error
	order := []int{}
	for i := len(pod.Spec.EphemeralContainers) - 1; i >= 0; i-- {
		if pod.Spec.EphemeralContainers[i].Name == preferred {
			order = append(order, i)
		}
	}
	for i := len(pod.Spec.EphemeralContainers) - 1; i >= 0; i-- {
		if pod.Spec.EphemeralContainers[i].Name != preferred {
			order = append(order, i)
		}
	}
	for _, i := range order {
		ec := pod.Spec.EphemeralContainers[i]
		if isPodToastCandidate(ec) && ec.TargetContainerName != target.Container && !podToastSharedManagement(pod, ec) {
			continue
		}
		if !shared && ec.TargetContainerName != target.Container {
			continue
		}
		running := false
		for _, st := range pod.Status.EphemeralContainerStatuses {
			if st.Name == ec.Name && st.State.Running != nil {
				running = true
			}
		}
		if !running {
			continue
		}
		if cid == "" {
			return DebugShell{}, fmt.Errorf("target container is not running")
		}
		ref := target
		ref.Container = ec.Name
		probe, e := ProbeContainer(ctx, executor, ref)
		if e != nil {
			candidateError = fmt.Errorf("ephemeral %s (%s) has no usable shell/toolbox; target %s is not the execution container. Existing shell-free PodToast can still serve files; use a toolbox image for commands: %w", ec.Name, ec.Image, target.Container, e)
			if ctx.Err() != nil {
				return DebugShell{}, ctx.Err()
			}
			if !errors.Is(e, ErrNoShell) {
				return DebugShell{}, e
			}
			continue
		}
		script := "for f in /proc/[0-9]*/cgroup; do if grep -F -q -- " + shQuote(cid) + " \"$f\" 2>/dev/null; then p=${f#/proc/}; p=${p%/cgroup}; printf '%s\\n' \"$p\"; exit; fi; done; "
		if shared {
			script += "exit 3"
		} else {
			script += "a=$(stat -Lc '%d:%i' /); b=$(stat -Lc '%d:%i' /proc/1/root) || exit; [ \"$a\" != \"$b\" ] || exit 3; echo 1"
		}
		var out bytes.Buffer
		e = executor.Stream(ctx, ref, ExecOptions{Command: []string{probe.Shell, "-c", script}, Stdout: &limitedWriter{w: &out, limit: 128}})
		pid, parse := strconv.Atoi(strings.TrimSpace(out.String()))
		if e != nil || parse != nil || pid < 1 {
			return DebugShell{}, fmt.Errorf("ephemeral %s cannot identify target process/root", ec.Name)
		}
		root := fmt.Sprintf("/proc/%d/root", pid)
		out.Reset()
		e = executor.Stream(ctx, ref, ExecOptions{Command: []string{probe.Shell, "-c", "stat -Lc '%d:%i' " + shQuote(root)}, Stdout: &limitedWriter{w: &out, limit: 128}})
		if e != nil || strings.TrimSpace(out.String()) == "" {
			return DebugShell{}, fmt.Errorf("ephemeral %s cannot access target root: %v", ec.Name, e)
		}
		return DebugShell{PodToast: isPodToastCandidate(ec), Ref: ref, Target: target, Probe: probe, Root: root, RootIdentity: strings.TrimSpace(out.String())}, nil
	}
	if candidateError != nil {
		return DebugShell{}, errors.Join(ErrNoDebugToolbox, candidateError)
	}
	return DebugShell{}, ErrNoDebugShell
}
func (d DebugShell) Command(cwd, command string, interactive bool) []string {
	label := d.Target.Container + " | ephemeral " + d.Ref.Container
	if d.PodToast {
		label = d.Target.Container + " | PodToast"
	}
	script := "export TARGET_ROOT=" + shQuote(d.Root) + "; [ \"$(stat -Lc '%d:%i' \"$TARGET_ROOT\")\" = " + shQuote(d.RootIdentity) + " ] || { echo 'Target root changed; reconnect' >&2; exit 1; }; cd -- " + shQuote(path.Join(d.Root, path.Clean("/"+cwd))) + " || exit; "
	script += "printf '%s\\n' " + shQuote(label+" · relative paths: target cwd; /: ephemeral root; $TARGET_ROOT: target root") + "; export PS1=" + shQuote("["+label+"] "+cwd+" $ ") + "; "
	if interactive {
		script += "exec " + shQuote(d.Probe.Shell) + " -i"
	} else {
		script += command
	}
	return []string{d.Probe.Shell, "-c", script}
}
