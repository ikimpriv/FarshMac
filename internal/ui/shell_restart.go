package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (a *App) scheduleShellRestart(err error) {
	cwd := a.ShellCwd
	if a.pendingShellCwd != "" {
		cwd = a.pendingShellCwd
	}
	if cwd == "" {
		cwd = a.Panels[a.Active].filesystemPath()
	}
	if a.Term != nil {
		a.Term.Close()
		a.Term = nil
	}
	a.ShellReady = false
	a.shellStarted = false
	a.shellCommands = nil
	a.dirty = true
	if a.closed || a.Quit {
		return
	}
	a.shellRestartCwd = cwd
	// A bad startup script must not cause an unbounded process-spawn loop.
	a.shellRestartAt = time.Now().Add(time.Second)
	a.Status = "Shell exited; restarting…"
	if err != nil {
		a.Status = fmt.Sprintf("Shell stopped: %v; restarting…", err)
	}
	// Restore terminal input/display modes after an exited shell or exec'ed app,
	// retaining the normal screen and its scrollback.
	a.pending = append(a.pending, []byte("\x1b[?1049l\x1b[0m\x1b[?25h\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?2004l\r\n[Restarting shell]\r\n")...)
}

func (a *App) restartShellIfDue() {
	if a.closed || a.Quit || a.Term != nil || a.shellRestartAt.IsZero() || time.Now().Before(a.shellRestartAt) {
		return
	}
	cwd := a.shellRestartCwd
	if a.pendingShellCwd != "" {
		cwd = a.pendingShellCwd
	}
	// If the working directory was deleted, use its nearest surviving parent.
	for cwd != "" {
		if info, err := os.Stat(cwd); err == nil && info.IsDir() {
			break
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			cwd = ""
			break
		}
		cwd = parent
	}
	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	if err := a.startTerminalAt(cwd); err != nil {
		a.shellRestartAt = time.Now().Add(3 * time.Second)
		a.Status = fmt.Sprintf("Cannot restart shell: %v; retrying…", err)
	} else {
		a.Status = "Shell restarting…"
	}
	a.dirty = true
}
