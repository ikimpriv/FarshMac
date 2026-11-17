# Shell session management

**internal/shellsession** is the shared session and context layer. Each application
window has one Manager, one local PTY session and a retained session per remote
context. Switching local / SSH / Kubernetes / Docker contexts preserves the
process, environment, screen and output. Explicit cancellation or closing the
window closes transports; returning to an ended session starts a new shell.

Every context has a bounded terminal screen and its own disk-backed output
history. Inactive streams are drained with bounded queues and renderer budgets.
Terminal-generated replies carry the originating context, so background DSR
responses cannot enter the foreground shell. Panel-directory notifications are
remembered per context; simply selecting another context does not reset its cwd.

## Responsibilities

- **Manager**: ownership and shutdown of local/remote sessions, active context,
  panel-originated context changes, and shell-originated cwd changes.
- **Session**: command queue, readiness, busy/completed state, directory requests,
  prompt protocol, cancellation, startup timeout, and bounded protocol buffering.
  The local PTY feeds already-decoded readiness events into the same command
  lifecycle; SSH and container sessions use the shared remote prompt protocol.
- **Deferred**: asynchronous connection resolution, bounded input/output queues,
  stream forwarding and cancellation. No worker mutates UI state.
- **Transport adapters**: local PTY in internal/terminal, SSH in internal/remotefs,
  Kubernetes exec in internal/kube. Adapters provide bytes, input, resize,
  completion and close; they do not decide when to show panels.
- **UI**: selects a backend/context, resolves credentials or a container binding,
  supplies a transport, renders output, reloads panels, and applies completion
  events. Local shell configuration, restart presentation and filesystem fallback
  remain in the existing local adapter (ui/shell_restart.go).

The package has no dependency on UI, Kubernetes or remote filesystem packages.
The UI must not inspect or mutate private session state.

## Context contract

A context contains:

- **Kind**: local, SFTP/FTP/FTPS, Kubernetes container, or Docker container.
- **ID**: the connection/target identity within the window, without credentials.
- **Directory**: the path in the panel's filesystem.
- **ExecutionDirectory**: the actual cwd reported by the executing shell.

**PanelChanged(context)** receives panel navigation and active-context changes.
The current panel snapshot is remembered independently from the shell's latest
cwd. Repeated snapshots are idempotent: a stationary panel cannot undo cd
inside a shell. A directory request waits until the shell is ready; it cannot
be injected into a foreground program's stdin.

**ShellChanged(context)** receives a context reported by the shell. A notification
from a different session identity cannot change the active context. On return
from the terminal, the UI applies the reported directory only if the active
panel still belongs to that session. A return before the prompt arrives waits
for readiness.

For a PodToast/toolbox shell, Directory is relative to the target container's
root, while ExecutionDirectory may be below /proc/PID/root. Outside that
root, Directory is empty: the manager still knows the execution cwd, but the
UI must not interpret an ephemeral-container path as a target-container path.

## Command lifecycle

A panel command waits for a prompt, switches to the requested directory, then
executes in the persistent session. The next parent-shell prompt emits completion;
the UI returns to panels and keeps output accessible with Cmd+O. Terminal-entered
commands do not request automatic panel return. A nested shell's prompt must not
complete the parent command.

The private prompt protocol carries logical and execution cwd, separated by NUL,
with a per-session random marker. It tolerates arbitrary transport chunk
boundaries and caps incomplete frames at 32 KiB. Invalid paths do not generate a
context change. The marker is emitted separately from PS1 so line-editor redraws
cannot be mistaken for completion.

Local PTY restarts preserve a command queued during startup. Remote sessions are
explicitly reconnected; FTP/FTPS or an SSH server denying PTY/exec returns an error.
There is no implicit fallback from a remote context to the local shell.

## Threading and tests

Manager and session state is confined to the window's serial engine queue.
Transport workers own blocking network/process I/O; cancellation must unblock
both reads and queued writes. Output remains subject to terminal backpressure and
the existing scrollback spool.

Unit tests cover split markers, bounded parsing, busy-shell queues, directory
synchronization, context feedback suppression, session isolation, cancellation,
and shutdown ownership. UI tests cover real local PTYs, an isolated SSH server,
and container exec fixtures, including panel-to-shell and shell-to-panel cwd
changes. Run ./scripts/check.sh --race for the complete suite.
