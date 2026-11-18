# Context actions and multiple selection

Alt+Enter / F11 acts on the marked objects. With no marks it uses the cursor.
A single marked object takes precedence over a cursor elsewhere. Standard file
operations and F3/F4 are not duplicated in this menu.

| Context | Multiple-selection actions | Single-object actions |
| --- | --- | --- |
| Files, including remote/container files | Email attachments, create an archive, Open In (applications compatible with every file) | None |
| Kubernetes pods/containers | Attach ephemeral containers, reuse/update PodToast, live charts and Quick Live Metrics, available PodToast snapshots and network probes | Shell, terminal live monitor, interactive traffic monitoring/faults |
| Docker containers | State-appropriate start/stop/restart/pause/unpause, signals, common limits/restart policy, attach PodToast, diagnostics, processes/logs/filesystem changes/ports, metrics | Interactive traffic monitoring/faults |
| Docker Compose projects | The same container actions across project members; recreate projects with verified local Compose configurations | Individual service log chooser (batch selection gets combined logs) |
| Docker images | Layer histories; save all selected images into one TAR | Tag and Run, which require an individual destination/configuration |
| Docker volumes | Consumer reports; separate backup archives in one selected directory | Open a volume toolbox filesystem |
| Docker networks | Membership reports; connect/disconnect a chosen container to/from all eligible selected networks | None |

Container lifecycle actions show eligible/selected counts. Host and none networks
are excluded from attachment actions. File selections inside a container are not
reinterpreted as container selections. Clipboard menus in the terminal/viewer
continue to operate on the text selection, not panel objects.

Batch operations execute sequentially with cancellation and per-object results.
An error on one object does not abort independent remaining work. Escape cancels
the active request and prevents starting subsequent targets; successful earlier
changes are retained. Reports cap individual output at 128 KiB and combined output
at 2 MiB (plus per-object status). Docker clears successful directly corresponding
marks; skipped and failed objects remain marked. Kubernetes target checkboxes do
not clear panel marks, since one marked pod can contain several selected targets.

Kubernetes attachment first reads the selected pods, presents target checkboxes
and existing agent images, and verifies pod UIDs again before mutation. Matching
agents are reused. Replacement agents become active after connection succeeds;
retirement failures are reported without undoing a working replacement. No shell
is opened automatically for a batch. Network probes run once per pod, even when
multiple containers from that pod are selected.

Compose recreation validates configuration paths and the Docker endpoint before
confirmation. Configuration files must be available locally; a remote daemon does
not authorize reading paths copied from its labels.
