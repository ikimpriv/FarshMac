package ui

import (
	"farshmac/internal/shellsession"
	"fmt"
	"path"
	"time"
)

type foreignStream = shellsession.Stream
type deferredTerminal = shellsession.Deferred

var newDeferredTerminal = shellsession.NewDeferred

func (a *App) writeForeignInput(data []byte) error {
	if m := a.Shells.Remote(); m != nil {
		return m.Input(data)
	}
	return a.Foreign.Write(data)
}
func (a *App) acceptForeignOutput(data []byte) {
	if m := a.Shells.Remote(); m != nil {
		a.applyShellResult(m.Feed(data))
		return
	}
	a.pending = append(a.pending, data...)
}
func (a *App) applyShellResult(r shellsession.Result) {
	a.pending = append(a.pending, r.Output...)
	if r.ContextReported {
		a.Shells.ShellChanged(r.Context)
	}
	if r.Completed {
		a.TermVisible = false
		a.Status = "Command completed · ⌘O to view output"
		a.returnRemoteContext()
		if p := a.shellCommandPanel; p != nil && p != &a.Panels[a.Active] {
			a.error(p.Load(p.Path))
		}
	}
	if r.Started {
		a.Status = "Running command · ⌘. stops"
		a.TermVisible = true
	}
	if r.Err != nil {
		a.error(r.Err)
	}
	if a.shellContextReturn != "" && !a.TermVisible {
		if m := a.Shells.Remote(); m != nil && m.Ready() {
			a.returnRemoteContext()
		}
	}
}
func (a *App) submitContextCommand() {
	if m := a.Shells.Remote(); m != nil {
		a.applyShellResult(m.Dispatch())
	}
}
func (a *App) queueContextCommand(p *Panel, cwd, command string) {
	m := a.Shells.Remote()
	if m == nil {
		return
	}
	if err := m.Queue(shellsession.Command{Text: command, Path: cwd}); err != nil {
		a.error(err)
		return
	}
	a.shellCommandPanel = p
	a.submitContextCommand()
}
func (a *App) panelShellContext() shellsession.Context {
	p := &a.Panels[a.Active]
	if p.AI != nil {
		return a.aiShellContext(p)
	}
	if p.Docker != nil {
		c, _, cwd, ok := p.Docker.location(p.Path)
		if ok {
			return shellsession.Context{Kind: "docker", ID: "docker:" + p.Docker.client.Context + ":" + c.ID, Directory: cwd}
		}
		return shellsession.Context{Kind: "docker", ID: "docker:" + p.Docker.client.Context + ":catalog"}
	}
	if p.Remote != nil && p.backend().Kind() == backendRemote {
		return shellsession.Context{Kind: p.Remote.Protocol, ID: "remote:" + p.Remote.Label(), Directory: p.Path}
	}
	if p.Kube != nil {
		r := p.kubeLevel()
		if r.Level == kubeContainerFS && r.Object != nil {
			return shellsession.Context{Kind: "container", ID: fmt.Sprintf("kube:%s/%s/%s/%s/%s", kubeProfileKey(p.Kube.profile), r.Namespace, r.Object.Name, r.Object.UID, r.Container), Directory: r.FSPath}
		}
	}
	return shellsession.Context{Kind: "local", ID: "local", Directory: p.filesystemPath()}
}
func (a *App) returnRemoteContext() {
	m := a.Shells.Remote()
	if m == nil {
		return
	}
	a.shellContextReturn = m.Context().ID
	if !m.Ready() {
		return
	}
	a.shellContextReturn = ""
	c := m.Context()
	panel := a.panelShellContext()
	if c.ID != panel.ID || c.Directory == "" {
		return
	}
	p := &a.Panels[a.Active]
	target := c.Directory
	if c.Kind == "docker" {
		_, prefix, _, _ := p.Docker.location(p.Path)
		target = path.Join(prefix, c.Directory)
	}
	if c.Kind == "container" {
		target = path.Join(p.kubeLevel().Prefix, c.Directory)
	}
	a.error(p.Load(target))
}
func (a *App) pollShellContext() {
	a.Shells.PanelChanged(a.panelShellContext())
	if m := a.Shells.Remote(); m != nil {
		if m.Expire(time.Now()) {
			a.pending = append(a.pending, []byte("\r\nRemote shell did not become ready; the server may restrict interactive shell access.\r\n")...)
		}
		a.submitContextCommand()
	}
}
