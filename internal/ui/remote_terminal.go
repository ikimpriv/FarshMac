package ui

import (
	"context"
	"time"

	"farshmac/internal/remotefs"
	"farshmac/internal/shellsession"
	"farshmac/internal/terminal"
)

// An SSH transport uses the same prompt protocol and command lifecycle as a
// container shell. Unsupported protocols get an isolated error screen.
func (a *App) showRemoteTerminal() bool {
	p := &a.Panels[a.Active]
	if p.Remote == nil || p.backend().Kind() != backendRemote {
		return false
	}
	c := *p.Remote
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
	cwd := p.Path
	cols, rows := a.Cols, max(1, a.Rows-1)
	stream := newDeferredTerminal(cols, rows, func(ctx context.Context) (foreignStream, error) {
		script := "cd -- " + terminal.Quote(cwd) + " || exit; unset TARGET_ROOT; " + "if [ -x /bin/bash ]; then " + managed.Launch("/bin/bash", "SSH") + "; else " + managed.Launch("/bin/sh", "SSH") + "; fi"
		s, err := c.StartTerminal(ctx, "/bin/sh -c "+terminal.Quote(script), cols, rows)
		if err != nil {
			return nil, err
		}
		return shellsession.WithPrefix(&sshTerminal{s}, []byte("SSH "+c.Label()+"\r\n")), nil
	})
	a.installForeign(stream, "SSH "+c.Label(), nil)
	a.Shells.SetRemote(managed, stream)
	if preserveOutput {
		a.pending = append(a.pending, "\r\nReconnecting to remote shell…\r\n"...)
	} else {
		a.pending = []byte("Connecting to remote shell…\r\n")
	}
	a.foreignEnded = false
	return true
}

type sshTerminal struct{ *remotefs.SSHStream }

func (s *sshTerminal) Write(data []byte) error { return s.Input(data) }

func remoteShellDescription(c remotefs.Config) string {
	if c.Protocol == "sftp" {
		return "Cmd+O: SSH shell on this server"
	}
	return "Shell unavailable over FTP/FTPS; use SFTP/SSH"
}
