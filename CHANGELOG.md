# Changelog

## Unreleased

## 0.27.0 — Workload restarts and predictable batch operations

- Order the 15 recent connections strictly by last use, newest first regardless of connection type.

- Replace batch mutation completion reports with determinate progress and Retry / Skip / Skip all / Cancel error handling. Clear successful selection marks, retaining failed, skipped and unprocessed objects, including ephemeral attachment/update targets.
- Explicitly preserve the current replica count during workload restart and detect unexpected replica changes in the API response.

- Add selection-aware rollout restart for Deployments, StatefulSets and DaemonSets in F11 / Alt+Enter. Preserve update strategies, report per-object failures, reject paused/OnDelete workloads and cancel remaining requests with Esc.

## 0.26.0 — Remote connections and batch actions

- Show up to 15 recent connections and rename the saved configuration library to Custom kubeconfig files.

- Simplify Kubernetes connection setup to server and optional username, with automatic SSH authentication, fallback credential dialogs and remembered authentication methods. Browse local and remote config files and their contexts in a two-column master/details dialog.

- Add Kubernetes connections through system SSH, including loopback kind/k3s APIs, remote kubeconfig/context discovery and local ~/.kube discovery. Preserve TLS/SSH host verification and route shell/exec streams through SSH.
- Add a library of named pasted kubeconfigs with private credential storage, multiple context-specific connections, and direct or SSH access.

- Show Docker container Status and Age columns, including health and exit codes. Age uses creation time; the footer preserves Docker uptime/status details. Dim inactive containers and highlight failures without losing PodToast badges.

- Mark Docker containers and volumes with running PodToast toolboxes. Show full/partial coverage for Compose projects and a PodToast coverage summary in the panel footer; keep selection markers and object names unchanged.

- Add Create Context to Docker connections: SSH host/user/port, private-key picker and existing SSH aliases. Create standard Docker CLI contexts, preserve SSH configuration and keep the current default context unchanged.
- Fix PodToast target-root access on remote Docker hosts when the target uses a different AppArmor profile. Match the target profile for the toolbox, verify the agent before reporting success, and offer reconnect/repair for existing helpers. Prefer replacements over superseded helpers after refresh/reconnect.

- Make context menus selection-aware across files, Kubernetes and Docker. Add batch ephemeral attachment/PodToast updates, Docker lifecycle and diagnostics, image exports, volume backups, network membership changes and Compose recreation. Hide single-context interactive actions for multiple objects; preserve per-object results and cancellation.

- Preserve Kubernetes connection profiles in folder bookmarks and restore the saved path/cursor across panels and reconnections. Clear the Kubernetes backend when jumping to a local folder bookmark.

- Prevent Ctrl+Q and Ctrl+Shift+Q in the viewer/editor from quitting the application. Cmd+Q remains the application exit shortcut; panel metrics shortcuts do not escape the document context.

## 0.25.0 — Streaming search and Kubernetes node insights

- Scale node pod-usage charts to Allocatable without rounding up to unused headroom; show percentages against Allocatable and omit redundant Capacity lines. Expand only for actual usage above Allocatable.

- Show Quick Live Metrics for Nodes: sum pod CPU/working set across namespaces, compare with node capacity and allocatable resources, and display compact device reservation bars for GPUs and other extended resources. Keep device allocation available independently of Metrics Server.

- Keep short built-in resource names when custom API groups use the same plural; qualify other colliding types by API group and retain old built-in path aliases. Browse Nodes → Pods using a lazy server-side node filter and namespace-qualified pod names, with live updates and original API identities.

- Add Cmd+Shift+. to switch Kubernetes panels to cluster-level resources and back, with scope-specific discovery, live watches and manifest viewing.

- Fix namespace metrics disappearing when a pod has not started: exclude idle workloads while retaining running init containers and strict coverage checks for running workloads. Show the actual source error in Quick Live Metrics.

- Add shell output selection by character, word and line boundary; support rectangular mouse selection and forced selection over mouse-aware applications. Preserve Cmd+C/V and Ctrl+C semantics in every shell context, including bounded disk-history pages.

- Group recent remote connections together, ordered Docker, Kubernetes, then other connections.

- Exclude hidden subtrees from search when hidden-item display is disabled; select the initial name mask.
- Dim inactive Kubernetes resources, highlight failures, and add Age/State sorting.
- Add Quick Live Metrics aggregates for namespace/workload folders, following ownership chains with deduplication and fetching CPU/memory from Metrics Server.

- Stream search results into a usable flat list while traversal continues. View with F3, stop with F2 without losing results, or navigate with Enter and cancel remaining work. Make Kubernetes ownership trees optional via F5, built and refreshed in the background.

- Keep deep search-result trees readable by compressing excessive indentation, showing truncated labels with an ellipsis, and displaying row position, scroll progress and viewer/navigation shortcuts.

- Collect Kubernetes objects once, then group search matches by ownerReferences, merging common ancestors and removing duplicate rows. Prefer controller owners and keep context ancestors viewable with F3. F3 views a search result in place and returns to the same filtered list.

- Route file/object search and tree views through operation-scoped source strategies. Search Kubernetes and Docker catalogs without entering pod/container filesystems implicitly. Skip nested remote mounts on macOS and known network mounts reported by PodToast. Keep traversal bounded and cancellable.


## 0.24.0 — Markdown, PDF and responsive Quick Look

- Keep the current PDF page visible at full brightness while decoding the next page, then replace it without an intermediate blank or dimmed frame.

- Cancel Quick view work immediately when its source selection/context changes or the view is hidden, including directory scans, remote requests and Markdown image loading. Ignore stale completions without delaying the new preview.

- Browse every PDF page in F3 using Page Up/Down, first/last-page shortcuts and toolbar controls. Decode only one bounded page at a time and cancel superseded loads. Fix content shrinking inside high-resolution PDF previews.
- Give the F3 PDF viewer a viewport-based fit-page/fit-width layout, explicit zoom controls, scrolling and correct page dimensions independent of the text grid.

- Render Markdown in F3 and supported Quick view panels with theme-matched backgrounds, system fonts, tables, task lists, fenced code and footnotes.
- Cmd+Shift+M switches the viewer between formatted text and source; source search and editing retain existing controls.
- Parse bounded fragments asynchronously, preserve scrolling across redraws, and mask dialogs without hiding the whole document. Render local and relative raster images from the source filesystem with bounded helper-process decoding and cancellation; do not automatically load HTTP images or execute embedded HTML.

## 0.23.0 — Container workspaces and live diagnostics

### Quick Live Metrics

- Default to Working set instead of total cgroup memory. The M selector offers anonymous application memory, file cache and Total; missing data is never silently substituted. PodToast 0.8.2 exposes all components and retains their history.
- Keep charts updating behind dialogs and menus, masking only their bounds and shadows.
- Show agent versions, attach replacements explicitly, switch sessions after validation, and retire superseded agents only after active operations drain.
- Calculate container directory summaries inside PodToast with progress and cancellation; integrate them with Quick view and folder-size sorting.
- Read generated zero-size files such as /proc/cpuinfo in the viewer without false download-size errors.

- Ctrl+Shift+Q opens or closes Quick Live Metrics in the opposite panel for selected Kubernetes pods/containers or Docker containers. Theme-matched SVG charts show separate container lines, source-priority deduplication, bounded history and paged legends.
- New PodToast agents collect up to ten minutes of basic metric history while idle. Histories are target-process-specific, capped in memory/storage, and replayed before live samples in both quick and detached charts. Older agents remain compatible without historical data.

### cloud files

- Materialize macOS dataless files through iCloud/File Provider coordination before F3/F4 and Quick view; resolve legacy iCloud placeholder names. Download waits run in a cancellable helper, outside image/PDF decoding timeouts, without reading entire files into memory. Preserve provider error messages.

### persistent contexts and Docker

- Add state-aware Docker context actions: container controls/signals/resource limits, image history/tag/run/export, network membership, volume consumers/read-only backups, and Compose service operations with validated recreation. Stream archives to atomic local output and clean up temporary backup containers.

- Add structured Docker object reports to F3 and Quick view, including Compose service summaries; keep inspect reports read-only and retain F4 for container/volume files.

- Use nonselectable separators instead of redundant connection-menu headings.
- Fix Docker catalog crashes when a panel inherits date sorting; provide consistent metadata for virtual folders in every panel view.

- Reload renewed OIDC credentials instead of reusing a legacy client-go provider cached before an external kubectl login.
- Restore recent connections directly, without their settings dialogs.
- Retain shell processes, screens and independent disk histories across local, SSH, Kubernetes and Docker context switches; route background terminal replies to their originating session.
- Add Docker context connections, Compose/standalone container browsing, file operations, persistent shells, explicit PodToast and volume toolboxes, mount classification, logs/processes, live metrics and detached network monitoring.

### file actions and connections

- Open the current folder in Finder when Shift+Enter is pressed on the parent-directory entry.
- Add Finder-style email composition with attachments, compatible-application selection, and ZIP / tar.gz / tar.bz2 creation to file context menus, including multiple selections and staged remote files.
- Clear captured selections after successful file operations while preserving them on failure or cancellation.
- Add FTP/SFTP and Kubernetes connection entry points to panel and volume menus, with five recent connections and retained locations for older connections. Explicit folder-history paths take priority over location restoration.
- Isolate UI tests from the real user's connection profiles and settings.

### metrics and pod diagnostics

- Show storage/volume types directly in container file-panel columns, preserving manifest-declared mount points even when agent mount information is incomplete.

- Track kubelet ephemeral storage without metrics-server, distinguishing container writable layer/logs from shared pod volume usage. Preserve unknown/stale statistics and explain denied access.
- Highlight container mount points and identify volume types/PVCs using Kubernetes metadata plus PodToast 0.7.1 mount identity, with one mount-table read per listing.
- Route Cmd+O to the active container shell or attached toolbox; preserve live sessions, show isolated failure screens, and block local PTY/history fallback.

- Choose the destination chart and raw/delta/rate transform when adding metrics. Keep selected-series controls visible; prevent implicit reassignment to CPU and preserve restored layouts.
- Recognize known untyped cumulative counters, allow explicit gauge/counter overrides, handle resets and gaps, and warn about mixed units.
- Add passive pod socket/flow observation, incoming/outgoing port aggregates and multi-selection traffic charts using PodToast 0.7.0. Keep fault injection separate and explicit.
- Separate current process CPU percent from lifetime CPU seconds and prioritize basic container metrics when the catalog is full.
- Try compatible ephemeral toolboxes when an older preferred PodToast lacks a shell; offer an explicit upgrade only for missing shells.

### Graphical traffic monitor

- Add per-row graphical actions and a port-wide toolbar, show inherited conditions, and immediately re-evaluate queued delays when cleared.

- Open traffic monitoring in an independent macOS window with live rate/concurrency charts, bounded history, human-readable values, connection search and per-connection keyboard controls.
- Keep monitoring active until exit with renewable watchdog leases; apply and clear selected-connection delay, bandwidth, packet loss and resets without affecting peers. TLS remains opaque.
- Add kernel-level Docker and disposable Kubernetes traffic tests.

## Unreleased

- Automatically reconnect to existing PodToast agents without creating duplicates. Resolve the ephemeral execution context before commands, including after restarting FarshMac; report legacy toolbox limitations explicitly.

- Split live metrics into a compact per-container themed neighboring panel and independent chart windows for a container, pod or marked pods. Attribute PodToast series to targets, default to CPU-only comparison, hide detailed controls and add optional metrics-server CPU/memory sources.

- Run panel commands in PodToast or another integrated ephemeral shell at the target folder, with explicit target/debug execution labels and TARGET_ROOT. Add per-target attachment checkboxes and generic debug filesystem access.

- Select a sole PodToast target automatically; offer a container dropdown for isolated multi-container pods. Reuse one agent for shared PID namespaces with distinct target-root sessions. Discover metrics and run network diagnostics at pod scope.

- Add live pod metrics dashboards with annotation/PodMonitor/named-port discovery, PodToast reuse, counters and classic histogram quantiles, searchable series, chart normalization, and independent detached windows. Collection is cancellable and memory-bounded.

- Restore Alt+F9 window maximize/restore; context actions move to Alt+Enter and F11 without ordinary file commands.
- Shift+F4 opens or creates files on remote and container filesystems, with collision-safe saves.
- PodToast 0.2.0 adds Prometheus metrics over exec and an optional read-only HTTP listener. — PodToast

- Alt+Enter or F11 opens a context menu. Pods offer explicit ephemeral-container attachment: PodToast integration, saved launch presets or another image, with target, command, security profile, UID, TTY, pull policy and startup options. Preserve open container folders after attachment. Window zoom moves to Alt+F9.

- Add an explicitly started, shell-free Go agent in a Kubernetes ephemeral container for distroless filesystem access over a persistent multiplexed exec connection.
- Route container file operations through PodToast while preserving FarshMac transfer dialogs and collision handling; stream directory entries and file payloads, cancel individual requests, and copy within the target container.
- Add live CPU/memory/process/network monitoring, diagnostic snapshots with filesystem capacities, DNS/TCP/HTTP probes and bounded HTTP load tests.
- Add Linux protocol tests, isolated kind/distroless integration tests, an agent Dockerfile and Linux CI. See `podtoast/README.md` for deployment, capabilities and limitations.

## 0.22.1 — Kubernetes panels and remote workflows

- Add Kubernetes panels: Commands → Network connections → New Kubernetes cluster… connects through client-go (kubeconfig + context, exec auth plugins via the login shell PATH); profiles live in `kubernetes.json`.
- Show the cluster as folders: namespaces, resource types with objects and their `kubectl get` columns, owned objects with ReplicaSets and ControllerRevisions collapsed, Service pods by selector, pod containers and container file systems.
- F3/F4 open manifests (F4 saves with Update and reports conflicts), F5 exports manifests (recursively for folders, asking before Secrets), F7 creates a namespace, F8 deletes objects or a namespace (name typed), Shift+F8 keeps children, Shift+F3 logs, Alt+F3 follows logs in the terminal, Shift+F4 describes, Cmd+R refreshes.
- Container file systems work like server panels (view, edit, copy in both directions, rename, mkdir, delete); a command typed at the prompt runs inside the container with a TTY in the terminal area, and Kubernetes → Open shell in container starts an interactive shell.
- Check manifests in the editor against the kind's OpenAPI schema from the cluster (CRDs included): unknown fields, wrong types, enum values and missing required fields are marked, Ctrl+Space completes fields and values (required fields first and marked, object and array fields are inserted with their first child line or `- ` item ready), a dim hint after the cursor line shows the expected type, required fields, enum values and description, Alt+F2 jumps to the next problem.
- F5 into a cluster namespace applies manifests: local YAML/JSON files and folders of them (recursively), or objects from another cluster panel exported clean; every object gets the target namespace, Retry / Skip / Cancel per manifest, optional force over other field managers.
- Quick view scrolls (arrows, pages, Home/End when it is the active panel) and previews files inside containers; F3 shows manifests without managedFields; followed logs stop with Ctrl+C and no longer hit the request timeout (streams, watches and exec use clients without one); editing or deleting objects no longer re-reads a watched namespace.
- Container file systems work on BusyBox images (stat separators), images whose find rejects the GNU format (Harbor), and shell-only images (kube-proxy: names and types through shell builtins, files read with a shell loop when cat is missing); exec failures such as "container not found" are reported as such instead of "no shell".
- Ctrl+Q next to a Kubernetes panel shows kubectl describe of the current namespace, object or pod in the Quick view, with headers and keys highlighted.
- YAML documents always indent with two spaces: Tab, selection indent, Alt+I and pasted text never produce tabs, whatever the editor settings.
- Editor: Cmd+Left/Right go to line start/end, Cmd+Up/Down to document start/end, Option+Left/Right move by word.
- Highlight manifests as YAML; the editor strips status, managedFields, uid, resourceVersion, creationTimestamp and generation and keeps ownerReferences.
- Watch listed namespaces (Table watch, like `kubectl get -w`) and update the panel in place; show childless objects as files, kind/status/age columns, a visible reading indicator, and return to the panels with a key after a container command.
- Panel modes are now strategies behind one `panelBackend` interface with capabilities; the key bar shows Far's Shift/Ctrl/Alt rows while a modifier is held.

- Copy and move Kubernetes resources, including CRs, between namespace/cluster panels through a resource abstraction. Local copy/rename creates a new object; moves delete the original only after creation, with UID/resourceVersion preconditions and orphan propagation.
- F5/F6 target the other panel, including cross-filesystem moves; Shift+F5/F6 accept a new name in the current folder or namespace. Same-container copies keep file payloads inside the pod; moves use its filesystem rename.
- Cmd+= matches local, remote and Kubernetes locations without replacing Quick view; Alt+F1/F2 restores a visible filesystem panel when choosing a volume.
- Cancel discovery, describe, manifest and schema requests promptly. Invalidate cluster caches after connection failures; watcher-confirmed deletions update both panels, including the last object of a type.
- Keep container filesystem reads off the UI thread and out of watcher redraws, fixing stalls when the same pod is open in both panels.
- Suppress generic protocol metadata warnings for same-remote-filesystem copies and editor saves back to their source, while retaining actual metadata errors. Fix a race between saving connection profiles and background folder reads.

## 0.21.0 — File safety, links, and window bookmarks

- Improve editor permission checks, open/save errors, external-change detection, and explicit save-in-place recovery.
- Add Smart / As is / Copy contents handling for symbolic links and preserve hard links copied together.
- Improve local and remote operation recovery, cancellation, connection trust handling, and progress reporting.
- Unify folder and document bookmarks per window: Cmd+Option+0…9 saves a slot; Cmd+0…9 opens it.
- Enter opens .app bundles as folders; F3 opens supported images in the graphical viewer.
- Expand regression coverage for file operations, editor saves, remote sessions, terminal integration, and rendering.

## 0.20.0 — File-operation dialogs and recovery

- Adapt Far Manager file-operation dialogs: source/destination size and date, Overwrite / Skip / Rename / Append, Remember choice, and read-only confirmations.
- Add Retry / Skip / Skip all / Cancel for transfer and deletion failures, including individual children of folders and archive-operation failures.
- Add same-server FTP/SFTP move conflict handling and directory merging.
- Stage append output before replacement; retry source deletion separately after a committed move.
- Show full multiline errors with PageUp/PageDown scrolling and count skipped items.

- Merge into the prefilled existing destination folder instead of accidentally nesting another folder during copy/move. An explicit trailing slash still selects a destination container.
- Keep local file-operation errors visible in a confirmation dialog, including permission failures.
- Cancel safely when a local overwrite has no conflict handler.
- Add collision, partial-merge, permission-denial, and FTP/SFTP overwrite regression tests.

## 0.19.1 — First public preview

- Publish the project as FarshMac, inspired by Far Manager, with documented installation, contribution, and security-reporting workflows.
- Add signed GitHub Actions builds and tag-driven releases using a persistent certificate stored in environment secrets.

- Add Cmd+Option+F3 (Ctrl+Alt+F3 alias) to calculate all displayed folder sizes without changing selection.
- Retain sizes across navigation and refresh in a bounded per-panel memory cache. Mark reused values with `~`; preserve size sorting and completed results after cancellation.
- Add one-time self-signed local certificate setup and automatic reuse for stable macOS signing without a paid Apple account.

## 0.19.0

- Rename the application, executable, Go module, native bridge, build artifacts, and CI configuration to FarshMac.
- Translate all project documentation to English, retaining historical version notes and limitations.
- Import settings, connection profiles, and window sessions from earlier development builds when no current configuration exists; preserve the original files.
- Use the new `local.farshmac.app` bundle identifier and `FARSHMAC_` environment-variable prefix. macOS privacy permissions may need to be granted again.

## 0.18.7

- Automatically restart the parent terminal shell after exit, EOF (Ctrl+D), or a crash. Preserve terminal visibility, output history and the last known working directory; use a surviving ancestor if the directory was deleted.
- Keep nested shell exits normal, suppress restarts during application shutdown, and rate-limit retries when shell startup fails.

## 0.18.6

- Keep copy/move/delete operation failures involving remote panels visible in an OK dialog instead of only a transient status line. Do the same for remote F7 folder creation; cancellation remains quiet.
- Include source/destination paths in file-transfer failures and the target path in remote mkdir failures.
- Test F5 uploads of files and folders to SFTP in both panel directions, metadata confirmation, overwrite, and denied writes for F5/F7. The reported server-specific upload failure is not yet diagnosed.

## 0.18.5

- Returning to a parent directory focuses the folder just exited, even if the parent has a saved cursor on a different entry. Keep the parent's saved viewport and marked files. Cover synchronous and asynchronous loading.

## 0.18.4

- Fix native key-event JSON: modifier flags must be booleans, not numeric 0/1. Numeric Shift caused Go to reject the entire Option fast-find event. Correct the same issue in native keypad/selection shortcuts.
- Test native serialized key events through the Go panel handler, in addition to native modifier-type assertions.

## 0.18.3

- Route panel Option shortcuts before AppKit/WebKit handling; retain the current layout and Shift for fast find, including `.`, `_` and `-`.
- Give printable fast-find input precedence over selection punctuation while preserving explicit numeric-keypad selection shortcuts.
- Add native keyboard translation checks and panel input regression tests.

## 0.18.2

- Cmd+Shift+H now opens the interactive shell's command history (up to 500 entries), including commands entered directly in bash, sh and zsh. Select a command, edit it with F4, and press Enter to run; Shift+Enter inserts a newline and Esc cancels.
- Keep terminal output scrollback available separately under Window → Terminal Output History.
- Preserve unfinished shell input when browsing history, and retain previous Bash history entries when executing panel commands.

## 0.18.1

- Retry permanent directory removal when concurrent changes (including recreated Finder metadata) leave a directory nonempty. Retries are bounded and cancellable, and do not follow a replacement directory.
- Translate Option/Alt panel shortcuts using the current keyboard layout without Option transformations; retain the first search character even without a match.
- Calculate up to four local/archive folder roots concurrently with aggregate progress instead of retaining every file path. Remote sessions remain sequential.
- In a warm-cache local benchmark with 4096 files in 16 folders: 18.2 ms → 5.7 ms. Actual performance depends on storage and folder structure.

## 0.18.0

- Synchronize the active file panel after changing directory in the shell, including quick Cmd+O switching before the next prompt event.
- Calculate selected/current folders with F3 or all displayed folders through the Files menu. Store sizes in panels and use them in size sorting; support local, archive, and network panels.
- Preserve completed directory-size results when an operation is cancelled.
- Add a Far-style information panel with macOS disk, memory, swap, power, and folder-description data.
- Improve terminal-output batching and overlap history writes with rendering while keeping bounded queues.

## 0.17.2

- Synchronize the shell directory with the active local panel.
- Use a lightweight shell prompt by default; allow custom prompt hooks through Terminal settings.
- Switch between panels and terminal before waiting for output parsing.

## Earlier development

The original implementation notes and earlier changes are preserved in [Development history](docs/DEVELOPMENT_HISTORY.md). They include historical measurements and limitations that may have changed in subsequent versions.
