package ui

import (
	"context"
	"errors"
	"farshmac/internal/kube"
	"farshmac/internal/shellsession"
	"farshmac/internal/terminal"
	"fmt"
	"path"
	"time"
)

func (a *App) showContainerTerminal() bool {
	p := &a.Panels[a.Active]
	if p.Kube == nil {
		return false
	}
	r := p.kubeLevel()
	if r.Level != kubeContainerFS || r.Object == nil {
		return false
	}
	k := p.Kube
	ref := kube.ContainerRef{Namespace: r.Namespace, Pod: r.Object.Name, Container: r.Container}
	key := a.panelShellContext().ID
	a.activateTerminal(key)
	if a.Shells.RemoteKey() == key && a.Foreign != nil {
		a.TermVisible = true
		return true
	}
	managed, err := shellsession.NewRemote(a.panelShellContext(), time.Now())
	if err != nil {
		a.error(err)
		return true
	}
	preserveOutput := a.Shells.RemoteKey() == key
	cols, rows := a.Cols, max(1, a.Rows-1)
	binding, _ := k.debugShell(ref)
	stream := newDeferredTerminal(cols, rows, func(ctx context.Context) (foreignStream, error) {
		// Resolve/probe is cancellable and bounded; the interactive stream has no deadline.
		probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		probe, err := k.probe(probeCtx, ref)
		target := ref
		label := "target " + ref.Container + " · native shell"
		argv := []string{}
		if err == nil {
			script := "cd " + terminal.Quote(path.Clean("/"+r.FSPath)) + " || exit; unset TARGET_ROOT; " + managed.Launch(probe.Shell, ref.Container)
			argv = []string{probe.Shell, "-c", script}
		} else {
			if !errors.Is(err, kube.ErrNoShell) {
				return nil, err
			}
			d, e := k.client.ResolveDebugShell(probeCtx, ref, binding.Ref.Container)
			if e != nil {
				return nil, fmt.Errorf("container %s has no usable shell or compatible ephemeral toolbox: %w", ref.Container, e)
			}
			target = d.Ref
			argv = d.Command(r.FSPath, managed.Launch(d.Probe.Shell, ref.Container+" | ephemeral "+d.Ref.Container), false)
			label = "target " + ref.Container + " · ephemeral " + d.Ref.Container
		}
		inner, e := kube.StartExec(ctx, k.exec(), target, argv, cols, rows)
		if e != nil {
			return nil, e
		}
		// Emit context before shell output; no UI state is mutated by this worker.
		return shellsession.WithPrefix(inner, []byte(label+"\r\n")), nil
	})
	a.installForeign(stream, "Container "+ref.Namespace+"/"+ref.Pod+"/"+ref.Container, nil)
	a.Shells.SetRemote(managed, stream)
	if preserveOutput {
		a.pending = append(a.pending, "\r\nReconnecting to container shell…\r\n"...)
	} else {
		a.pending = []byte("Connecting to container shell…\r\n")
	}
	a.foreignEnded = false
	return true
}
