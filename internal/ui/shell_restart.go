package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// A shell exiting this soon after start without ever reaching a prompt
	// counts as a startup failure for restart backoff.
	shellStartupWindow    = 5 * time.Second
	maxShellStartFailures = 5
)

// shellIntegrationTimeout bounds how long a sent integration key may go
// without any Running/Ready marker before it is considered lost.
var shellIntegrationTimeout = 5 * time.Second

// shellState is engine-queue state for shell supervision: restart backoff and
// the lost-integration-key watchdog.
type shellState struct {
	shellStartedAt     time.Time
	shellFailures      int
	shellStopped       string
	integrationSentAt  time.Time
	integrationCommand string
	integrationHistory bool
	commandsError      string
}

func shellBackoff(failures int) time.Duration {
	return time.Second << min(3, max(0, failures-1))
}

// StartTerminalOrRetry starts the window's shell; on failure it schedules the
// regular restart mechanism instead of leaving the window without a shell.
func (a *App) StartTerminalOrRetry() {
	if a.Term != nil || !a.shellRestartAt.IsZero() || a.shellStopped != "" {
		return
	}
	if err := a.StartTerminal(); err != nil {
		a.shellRestartCwd = a.Panels[a.Active].filesystemPath()
		a.shellStartFailed(err)
		if a.shellStopped == "" {
			a.Status = fmt.Sprintf("Shell: %v; retrying…", err)
		}
	}
}

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
	fast := !a.shellStarted && !a.shellStartedAt.IsZero() && time.Since(a.shellStartedAt) < shellStartupWindow
	a.ShellReady = false
	a.shellStarted = false
	a.integrationSentAt = time.Time{}
	// An open command-history overlay must not keep waiting for a dead shell.
	a.shellCommands = &[]string{}
	a.commandsError = "Shell exited; history unavailable"
	a.dirty = true
	if a.closed || a.Quit {
		return
	}
	a.shellRestartCwd = cwd
	// Restore terminal input/display modes after an exited shell or exec'ed app,
	// retaining the normal screen and its scrollback.
	a.pending = append(a.pending, []byte("\x1b[?1049l\x1b[0m\x1b[?25h\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?2004l")...)
	if fast {
		a.shellFailures++
	} else {
		a.shellFailures = 0
	}
	if a.shellFailures >= maxShellStartFailures {
		a.stopShell(fmt.Sprintf("exited %d times during startup", a.shellFailures))
		a.Status = fmt.Sprintf("Shell keeps exiting during startup (%d attempts). Check your shell startup files; press Enter in the terminal or ⌘O to retry", a.shellFailures)
		return
	}
	a.pending = append(a.pending, "\r\n[Restarting shell]\r\n"...)
	// A bad startup script must not cause an unbounded process-spawn loop.
	a.shellRestartAt = time.Now().Add(shellBackoff(a.shellFailures))
	a.Status = "Shell exited; restarting…"
	if err != nil {
		a.Status = fmt.Sprintf("Shell stopped: %v; restarting…", err)
	}
}

// stopShell ends automatic restarts until the user explicitly retries.
func (a *App) stopShell(reason string) {
	a.shellRestartAt = time.Time{}
	a.shellStopped = reason
	if strings.HasPrefix(reason, "exited ") {
		a.pending = append(a.pending, fmt.Sprintf("\r\n[Shell exited %d times during startup; press Enter to retry]\r\n", a.shellFailures)...)
	} else {
		a.pending = append(a.pending, fmt.Sprintf("\r\n[Shell stopped: %s; press Enter to retry]\r\n", reason)...)
	}
	a.dirty = true
}

// retryShell makes one restart attempt after automatic restarts stopped.
func (a *App) retryShell() bool {
	if a.Term != nil || a.shellStopped == "" || a.closed || a.Quit {
		return false
	}
	a.shellStopped = ""
	// One attempt: another startup failure stops again immediately.
	a.shellFailures = maxShellStartFailures - 1
	a.shellRestartAt = time.Now()
	a.Status = "Shell restarting…"
	a.dirty = true
	return true
}

func (a *App) shellStartFailed(err error) {
	a.shellFailures++
	if a.shellFailures >= maxShellStartFailures {
		a.stopShell(err.Error())
		a.Status = fmt.Sprintf("Cannot start shell (%d attempts): %v; press Enter in the terminal or ⌘O to retry", a.shellFailures, err)
		return
	}
	a.shellRestartAt = time.Now().Add(max(3*time.Second, shellBackoff(a.shellFailures)))
	a.Status = fmt.Sprintf("Cannot restart shell: %v; retrying…", err)
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
	home, _ := os.UserHomeDir()
	if cwd == "" {
		cwd = home
	}
	a.dirty = true
	err := a.startTerminalAt(cwd)
	if err == nil {
		a.Status = "Shell restarting…"
		return
	}
	// The folder may exist but refuse chdir (no search permission); do not
	// retry the same folder forever.
	for _, fallback := range []string{home, "/"} {
		if fallback == "" || fallback == cwd {
			continue
		}
		if a.startTerminalAt(fallback) == nil {
			a.Status = fmt.Sprintf("Cannot start shell in %s: %v; using %s", cwd, err, fallback)
			return
		}
	}
	a.shellStartFailed(err)
}

func (a *App) integrationSent(command string, history bool) {
	a.integrationSentAt = time.Now()
	a.integrationCommand = command
	a.integrationHistory = history
}

// checkIntegrationWatchdog recovers from an integration key the shell never
// handled, e.g. after a plugin replaced the keymap. A request file consumed by
// the shell means a program is running, so the shell stays reserved.
func (a *App) checkIntegrationWatchdog() {
	if a.integrationSentAt.IsZero() || time.Since(a.integrationSentAt) < shellIntegrationTimeout {
		return
	}
	if a.Term != nil && len(a.Term.Events) > 0 {
		return // Undrained shell output may still hold the marker.
	}
	a.integrationSentAt = time.Time{}
	if a.Term == nil || !a.Term.CancelRequest() {
		return
	}
	a.ShellReady = true
	a.Status = "Shell integration did not respond; command not sent — check shell key bindings"
	if a.integrationCommand != "" && a.Command == "" {
		a.Command = a.integrationCommand
	}
	if a.integrationHistory {
		a.shellCommands = &[]string{}
		a.commandsError = a.Status
	}
	a.integrationCommand = ""
	a.integrationHistory = false
	a.dirty = true
}
