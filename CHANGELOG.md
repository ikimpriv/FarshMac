# Changelog

## Unreleased — Kubernetes panel

- Add Kubernetes panels: Commands → Network connections → New Kubernetes cluster… connects through client-go (kubeconfig + context, exec auth plugins via the login shell PATH); profiles live in `kubernetes.json`.
- Show the cluster as folders: namespaces, resource types with objects and their `kubectl get` columns, owned objects with ReplicaSets and ControllerRevisions collapsed, Service pods by selector, pod containers and container file systems.
- F3/F4 open manifests (F4 saves with Update and reports conflicts), F5 exports manifests (recursively for folders, asking before Secrets), F7 creates a namespace, F8 deletes objects or a namespace (name typed), Shift+F8 keeps children, Shift+F3 logs, Alt+F3 follows logs in the terminal, Shift+F4 describes, Cmd+R refreshes.
- Container file systems work like server panels (view, edit, copy in both directions, rename, mkdir, delete); a command typed at the prompt runs inside the container with a TTY in the terminal area, and Kubernetes → Open shell in container starts an interactive shell.
- Check manifests in the editor against the kind's OpenAPI schema from the cluster (CRDs included): unknown fields, wrong types, enum values and missing required fields are marked, Ctrl+Space completes fields and values (required fields first and marked, object and array fields are inserted with their first child line or `- ` item ready), a dim hint after the cursor line shows the expected type, required fields, enum values and description, Alt+F2 jumps to the next problem.
- Quick view scrolls (arrows, pages, Home/End when it is the active panel) and previews files inside containers; F3 shows manifests without managedFields; followed logs stop with Ctrl+C and no longer hit the request timeout (streams, watches and exec use clients without one); editing or deleting objects no longer re-reads a watched namespace.
- Container file systems work on BusyBox images (stat separators), images whose find rejects the GNU format (Harbor), and shell-only images (kube-proxy: names and types through shell builtins, files read with a shell loop when cat is missing); exec failures such as "container not found" are reported as such instead of "no shell".
- Ctrl+Q next to a Kubernetes panel shows kubectl describe of the current namespace, object or pod in the Quick view, with headers and keys highlighted.
- YAML documents always indent with two spaces: Tab, selection indent, Alt+I and pasted text never produce tabs, whatever the editor settings.
- Editor: Cmd+Left/Right go to line start/end, Cmd+Up/Down to document start/end, Option+Left/Right move by word.
- Highlight manifests as YAML; the editor strips status, managedFields, uid, resourceVersion, creationTimestamp and generation and keeps ownerReferences.
- Watch listed namespaces (Table watch, like `kubectl get -w`) and update the panel in place; show childless objects as files, kind/status/age columns, a visible reading indicator, and return to the panels with a key after a container command.
- Panel modes are now strategies behind one `panelBackend` interface with capabilities; the key bar shows Far's Shift/Ctrl/Alt rows while a modifier is held.

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
