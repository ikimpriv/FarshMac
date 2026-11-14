# FarshMac follow-up implementation backlog

## Status

All 85 items have been implemented on the `backlog-fixes` branch (2026-11-11); the "docs" decisions from the "Decisions on discrepancies" section have been added to the F1 help, README and the parity documents. The tasks from `TODO.md` (permission checks and save errors in the editor) are closed by items P3-01…P3-04.

Require verification on a device (the automated tests cover only the model):

- P4-09: ⌘ shortcuts in non-Latin keyboard layouts;
- P4-12: minimum window size at font size 20–24; delivery of the font-size keys (⌘+/⌘−/⌘0) to the page;
- P4-06: a minimized busy window on ⌘Q;
- re-binding of the zsh precmd hook with zsh-vi-mode;
- P1-07: the Full Disk Access hint on real protected folders;
- handling of symbolic links on a real FTP server;
- P3-01: permission prediction on SMB and read-only volumes;
- preservation of Finder tags after writing in the editor;
- the light theme (visually);
- document bookmarks: Cmd+Option+0..9 and Ctrl+Shift+0..9 (Cmd+Shift+3/4/5 is taken by macOS screenshots, so bookmarks were moved to Cmd+Option);
- cross-volume F6 on a real second volume;
- a rename that changes only the case on case-sensitive APFS;
- reconnection after a real network outage;
- an SFTP server without `posix-rename`;
- P5-09: the dialog on quit via ⌘Q and the Dock;
- the message of a second application instance;
- font size in several windows;
- greyed-out menu items in both themes.

Deviations from what the items proposed:

- P5-09: an in-app dialog instead of a native alert, without `os_log`;
- P1-05: no separate Cyan color for links to folders;
- P2-07: no "do not show again" checkbox;
- P3-04: other open copies of the same file are not synchronized after saving.

Result of the gap analysis against `docs/FUNCTIONAL_SPEC.md`: for each function the variants (panel type, selection, filesystem and network errors, cancellation, busy state, settings, window size, persistence, mouse and keyboard) were checked and compared with the code. Only the cases that the code **does not handle or handles badly** are included here; each one is confirmed by reading the sources, and references to files and functions are given in the items. Cases that the code handles but the specification did not describe have been added to the appendices of `docs/FUNCTIONAL_SPEC.md`.

The task from `TODO.md` (permission checks when opening and saving in the editor) is detailed in items P3-01…P3-04; once the backlog is accepted, the `TODO.md` file can be deleted or replaced with a link to this document.

Priorities: P0 — crash, data loss or secret leak; P1 — a function does not work in a real scenario; P2 — inconvenience or inconsistency; P3 — cosmetics. Estimate: S — up to a day, M — several days, L — a week or more.

Compiled 2026-11-11 against the `main` branch (commit `b91313a`).

## Summary table

| Priority | Items |
|---|---|
| P0 | 1 |
| P1 | 15 |
| P2 | 35 |
| P3 | 34 |
| Total | 85 |

| ID | Title | Priority | Estimate | Part |
|---|---|---|---|---|
| P5-01 | Crash on ⌘C/⌘X on a form button | P0 | S | 5 |
| P1-01 | Keys and operations act on invisible entries (Quick view / Info panel / hidden panel) | P1 | M | 1 |
| P1-02 | Changing a panel's source before an asynchronous load completes: the rollback leaves the panel in an inconsistent state | P1 | M | 1 |
| P2-01 | Destination is a symbolic link to a directory (`/tmp`, `~/link`) | P1 | M | 2 |
| P2-02 | Cross-volume move: Retry/Skip/metadata act on the whole tree rather than on a file | P1 | M | 2 |
| P2-03 | Remote: Retry after a connection drop uses the dead session | P1 | M | 2 |
| P3-01 | Preliminary permission check when opening for editing (TODO, items 1–3) | P1 | M | 3 |
| P3-02 | Open/save errors: readable message, buffer preservation, retry and Save as (TODO, items 4–5) | P1 | M | 3 |
| P3-03 | "In place" save strategy when the directory is not writable | P1 | M | 3 |
| P3-04 | File changed/deleted on disk (or on the server) by the time of saving — a choice instead of a dead end | P1 | M | 3 |
| P4-01 | Lost integration key: `ShellReady` hangs, stale `panel-command` | P1 | M | 4 |
| P4-02 | Endless shell restart loop without backoff | P1 | M | 4 |
| P4-03 | First shell start error is not retried; restart with an unavailable cwd loops | P1 | S | 4 |
| P5-02 | ⌘C/⌘X take the password out of a Secret field into the system clipboard | P1 | S | 5 |
| P5-03 | Host key change (MITM) is shown as a truncated raw error | P1 | M | 5 |
| P5-04 | A corrupted setup.json is silently replaced by defaults and then overwritten | P1 | M | 5 |
| P1-03 | Returning from Quick view/Info panel does not re-read the panel; an interrupted load stays at "Reading folder… Esc stops" | P2 | S | 1 |
| P1-04 | Folder history mixes local, remote and archive paths without a source marker | P2 | M | 1 |
| P1-05 | Remote symbolic links to folders are shown as files and cannot be opened | P2 | M | 1 |
| P1-06 | Several windows: bookmarks and settings of one window are invisible in another and are overwritten on Save setup | P2 | M | 1 |
| P1-07 | No hint about Full Disk Access on EPERM on protected folders | P2 | S | 1 |
| P2-04 | `Skip` on a preparatory step (`connect`, `open destination`, `read destination`) finishes the operation as "Done — skipped" | P2 | S | 2 |
| P2-05 | Progress of remote delete / remote move: eternal "Working / calculating… 0 items" | P2 | S | 2 |
| P2-06 | A case-only rename (`File` → `file`) on a case-insensitive volume is rejected | P2 | S | 2 |
| P2-07 | Remote copy with default settings asks `Cannot preserve metadata` for every file | P2 | M | 2 |
| P2-08 | SFTP without `posix-rename@openssh.com`: `Overwrite` always fails with an unclear error | P2 | S | 2 |
| P2-09 | Archive commands do not check for a remote opposite panel | P2 | S | 2 |
| P2-10 | User menu and "Open current file with macOS default app" on an archive/remote panel work with the virtual path | P2 | S | 2 |
| P2-11 | Shift+F8 with `Confirm deletion` disabled deletes without confirmation; on remote the confirmation is always shown | P2 | S | 2 |
| P3-05 | Save as over a symbolic link or a directory — wrong message | P2 | S | 3 |
| P3-06 | The viewer does not open a UTF-16 file of odd length | P2 | S | 3 |
| P3-07 | `+`/`-` in the viewer with a remote active panel + TempDir leak | P2 | S | 3 |
| P3-08 | A remote symbolic link to a file cannot be opened | P2 | S | 3 |
| P3-09 | A remote save interrupted/cancelled after `Rename` leaves the document in a dead end | P2 | S | 3 |
| P3-10 | Saving loses xattrs (Finder tags/comments), ACL | P2 | S | 3 |
| P3-14 | Bookmarks Cmd+Shift+0..9 are not saved (confirmed) | P2 | S | 3 |
| P4-04 | Returning from the terminal throws the panel out of the archive | P2 | S | 4 |
| P4-05 | Autocompletion in a remote/archive panel reads the virtual path locally | P2 | S | 4 |
| P4-06 | ⌘Q is silently aborted by a busy window in the background | P2 | S | 4 |
| P4-07 | Inability to write session.json overwrites the status line every 500 ms | P2 | S | 4 |
| P4-08 | A new window (⌘N) starts with an empty command history; AutoSave on window close overwrites setup.json | P2 | M | 4 |
| P4-09 | Shortcuts are compared by `e.key`: non-Latin layouts (requires verification on a device) | P2 | S | 4 |
| P5-05 | Connection errors are visible only in the truncated status line | P2 | M | 5 |
| P5-06 | No keyboard-interactive password authentication | P2 | S | 5 |
| P5-07 | An encrypted key / wrong passphrase do not return the user to the passphrase field | P2 | S | 5 |
| P5-08 | "Trust once" cannot be saved — the unknown host question repeats on every new connection | P2 | S | 5 |
| P5-09 | AutoSave error on window close is invisible | P2 | M | 5 |
| P5-10 | Several windows/instances: "last writer wins" | P2 | M | 5 |
| P5-11 | Pasting into form fields does not filter control characters and line breaks | P2 | S | 5 |
| P5-12 | Selection lists without PageUp/PageDown and type-ahead search | P2 | M | 5 |
| P5-13 | Connection profiles: no deletion, one profile per Label, Directory is not synchronized | P2 | M | 5 |
| P1-08 | Tree panel does not roll back on a root error (unlike the file mode) | P3 | S | 1 |
| P1-09 | Fast find: `[` cannot be searched, non-ASCII names in NFD do not match | P3 | S | 1 |
| P1-10 | Folder sizes (F3) in Tree panel and multi-column modes are calculated but not displayed | P3 | S | 1 |
| P1-11 | Quick view: empty source panel and special files without meaningful text | P3 | S | 1 |
| P1-12 | Enter on `.app` in Tree panel launches the application instead of navigating the tree | P3 | S | 1 |
| P1-13 | Filter/masks referring to a deleted mask group silently hide all files | P3 | S | 1 |
| P1-14 | Key bar does not reflect the read-only archive panel | P3 | S | 1 |
| P1-15 | ⌘C and Ctrl+Insert copy different things with the cursor on `..` / an empty panel | P3 | S | 1 |
| P2-12 | The delete confirmation dialog lists all names | P3 | S | 2 |
| P2-13 | `~` in the destination for a remote panel expands to the local home directory | P3 | S | 2 |
| P2-14 | A destination with a trailing `/` that does not exist creates a file instead of a directory | P3 | S | 2 |
| P2-15 | An archive open error is visible only in the status line | P3 | S | 2 |
| P2-16 | Shift+F1 onto an existing name: Retry/Skip dialog, `Skip` reports `Done` | P3 | S | 2 |
| P2-17 | Encrypted ZIP: the index opens, a member fails with `zip: checksum error` | P3 | S | 2 |
| P2-18 | Message when copying a file onto itself: `destination is inside source` | P3 | S | 2 |
| P3-11 | `Hex bytes` in the editor searches stale bytes and breaks the mode | P3 | S | 3 |
| P3-12 | Silence when editing in Read-only: step-by-step replace and paste from the clipboard | P3 | S | 3 |
| P3-13 | Paged viewing silently shows emptiness on a read error | P3 | S | 3 |
| P3-15 | F8 in the viewer is silent for Dirty and paged documents (discrepancy 10) | P3 | S | 3 |
| P4-10 | Shell command history overlay: swallows ⌘O, hangs in `Loading…` after a shell restart or a request error | P3 | S | 4 |
| P4-11 | ⌘W/⌘Q with an open form replace it with a confirmation dialog; unsaved form edits are lost | P3 | S | 4 |
| P4-12 | Minimum window size is not consistent with the 60×15 lower bound at a large font | P3 | S | 4 |
| P4-13 | A command consisting only of spaces is executed and gets into the history; a too-long command gives a misleading message | P3 | S | 4 |
| P4-14 | Two application instances: race for session.json and setup.json | P3 | M | 4 |
| P5-14 | Menu items unavailable for a remote/archive panel are not marked | P3 | S | 5 |
| P5-15 | A validation error in the settings form closes the form and loses the other changes | P3 | S | 5 |
| P5-16 | Edit user menu / File associations: editing moves the entry to the end, an empty Label deletes silently | P3 | S | 5 |
| P5-17 | Groups of file masks: a name with ` = ` breaks list parsing; validation of description file names | P3 | S | 5 |
| P5-18 | Sort modes… on a remote panel allows sort orders forbidden by the hotkeys | P3 | S | 5 |
| P5-19 | A corrupted connections.json is not recovered | P3 | S | 5 |
| P5-20 | IDN hosts and non-UTF-8 FTP file names | P3 | M | 5 |
| P5-21 | FTPS with a self-signed certificate and plain FTP without a warning | P3 | M | 5 |
| P5-22 | Status line: overwriting and truncation of messages | P3 | S | 5 |
| P5-23 | Dialog (prompt): no cursor, no Delete and no insertion in the middle | P3 | M | 5 |

## Recommended order

1. P0 and P1 in full: these are the crash, the password leak into the clipboard, loss of settings, dead ends in the shell and the editor, actions on invisible panel entries.
2. Items P3-01…P3-04 (TODO.md) are done as a single change: a common document error dialog instead of the truncated status line.
3. P2 in parts, starting with file operations (symlink destination, Retry on cross-volume move and on remote panels).
4. The "fix the code" discrepancies from the "Decisions on discrepancies" section can be closed with small commits in parallel.


## Part 1. File panels and navigation

#### P1-01. Keys and operations act on invisible entries (Quick view / Info panel / hidden panel)
- **Function and variant:** Tab, Enter, Ctrl+PgDn, Shift+Enter, Insert/Space, F3–F8, Ctrl+Insert/⌘C, fast find (Alt+character), Alt+F6, Describe files — when the active panel is in `Quick view` or `Info panel` mode or is hidden (`Hidden`).
- **Current behavior:** `Tab` switches `Active = 1 − Active` without checking the mode and `Hidden` (`app.go:key`). A panel in Quick view/Info keeps the `Entries` and `Cursor` of the previous file mode (`toggleOtherView` does not clear them). `infoPanelKey` intercepts only arrows/PgUp/PgDn/Home/End/F3/F4/Ctrl+F12 (`info_panel.go:infoPanelKey`); for Quick view there is no interception at all. Further, `enterFile` (`launch.go`), `transferDialog` (`transfers.go`), `deleteSelected` (`delete.go`), `openSystemSelected`, `mark` (`app.go`), `fastFindKey` (`fastfind.go`), `createLink`, `describeFiles` operate through `Panels[Active].Entries/Selected()` without checking `View`/`Hidden`. As a result: in an active Quick view, Enter launches the executable under the invisible cursor, F8 offers to delete an invisible file (the dialog names it, but the user does not see the context), Alt+letter opens the `Find` window and moves the invisible cursor, Enter in it enters the folder and the Quick view "drifts away". `volumes()` (`panel_options.go`) activates even a hidden panel. Tree panel is fine: its entries are visible.
- **Required behavior:**
  - Tab skips a hidden panel: if `Panels[1−Active].Hidden`, Tab does nothing and shows the status `Other panel is hidden (⌘F1/⌘F2 shows it)`; if both are hidden, Tab shows the active one (`Hidden = false`). Alt+F1/F2 and `Left/Right → Change volume…` on a hidden panel clear `Hidden` before loading.
  - For an active Quick view/Info panel: Enter, Ctrl+PgDn, Shift+Enter, Insert, Space (with an empty command line), F3 (except the description in Info), F4 (except the description in Info), F5, F6, F7, F8, Delete, Shift+F8, Alt+F6, Ctrl+Insert/⌘C, `Files → Describe files…`, Ctrl+F/Ctrl+; and ⌘Enter are ignored with the status `Switch to a file panel first (Tab)`. Alt+character does not open fast find. Ctrl+PgUp, Cmd+L, Alt+F12 and bookmarks are allowed (they change the source `Path`, which is meaningful for the Info panel).
  - The cursor/marks of the Quick view/Info panel do not change; `Hidden` of the panel does not change.
  - Mouse: a double click on the Quick view does nothing (this is already the case), but a single click activates the panel, as with the Info panel (symmetrically).
- **Edge cases:** both panels in Quick view/Info (Tab still switches focus); archive/remote in Quick view via Ctrl+Q; a full-screen Detailed active panel with the second one hidden; `..` under the invisible cursor.
- **Tests:** `TestQuickViewPanelIgnoresFileKeys` (Quick view active: Enter/F8/Insert/Alt+a do not change Entries/Marked/Dialog, the status is set); `TestInfoPanelIgnoresEnterAndTransfers`; `TestTabSkipsHiddenPanel` (⌘F2, Tab, Active stays 0; both hidden → Tab clears Hidden); `TestVolumesUnhidesTargetPanel`.
- **Priority:** P1
- **Estimate:** M

#### P1-02. Changing a panel's source before an asynchronous load completes: the rollback leaves the panel in an inconsistent state
- **Function and variant:** ⌘=/⌘Shift+\ (`matchOtherPanel`) onto a remote/archive folder on a network timeout; Alt+F1/F2 (`volumes`) from a remote panel onto an unmounted volume; `Network connections → Disconnect active panel` (`disconnectRemote`) when `RemoteLocalPath` has been deleted.
- **Current behavior:** `matchOtherPanel` (`archive_panel.go`) assigns `dst.Remote/RemoteLocalPath/Archive` **before** `dst.Load`; with `Async`, `Load` always returns nil (`loadPanel`), so the recovery branch `dst.Archive = previous` is unreachable. `loadPanel` (`panel_load.go`) takes `previous := *p` only after the mutation; on error `pollPanelLoad` restores `*p = job.previous` → a panel with `Remote = src.Remote` but the `Path`/`Entries` of the local folder. Enter on such an entry goes to the server. Similarly, `volumes()` sets `Remote = nil` before `Load` (a panel with `Remote == nil` but remote `Path`/`Entries`); `disconnectRemote` — the same.
- **Required behavior:** `loadPanel` receives a snapshot of the panel from the caller (a `previous Panel` parameter), or `matchOtherPanel`/`volumes`/`disconnectRemote` save `{Remote, RemoteLocalPath, Archive, View, Path}` and register in `panelLoad` a rollback callback that restores these fields. The user sees: the previous listing, the error text in the footer (`dial tcp …: i/o timeout`), the status `Other panel kept: <error>` for ⌘=. For `volumes` onto an unavailable volume: an `os.Stat`/`ReadDir(1)` check **before** changing the source (as in `goFolderShortcut`), the error goes to the status, the panel is not touched. For `disconnectRemote`: if `RemoteLocalPath` is unavailable — `accessibleFolder(local, home)`.
- **Edge cases:** Esc during such a load (rollback on `ErrCancelled` — the same fields); dst in Tree panel (`View` is forced to Full — roll back as well); both panels on the same server (shared `*remotefs.Config` pointer).
- **Tests:** `TestMatchOtherPanelRemoteFailureRestoresLocalSource` (loopback SFTP with a closed port: after `pollPanelLoad`, `dst.Remote == nil`, `dst.Entries` are local); `TestVolumesFromRemotePanelKeepsRemoteOnError`; `TestDisconnectWithMissingLocalPathFallsBack`.
- **Priority:** P1
- **Estimate:** M

#### P1-03. Returning from Quick view/Info panel does not re-read the panel; an interrupted load stays at "Reading folder… Esc stops"
- **Function and variant:** Ctrl+Q / Ctrl+L (`toggleOtherView`) on the inactive panel that has an asynchronous read in progress (slow volume, remote), then Ctrl+Q/Ctrl+L again; also the ordinary return from Quick view/Info to file mode after the folder has changed.
- **Current behavior:** `toggleOtherView` (`far_keys.go`) calls `p.cancelPanelLoad()` (the load result is discarded) and reloads the panel only if the new mode is `Tree panel`. During the load, `loadPanel` has already set `Entries = [..]`, `Err = "Reading folder… Esc stops"`. After returning to Full, the panel shows only `..` and this footer indefinitely; `Esc` does not help (`p.load == nil`), ⌘R acts only on the active panel. Without an interrupted load the listing is simply stale (`refresh()` is not called).
- **Required behavior:** when returning from Quick view/Info to file mode, `toggleOtherView` calls `a.error(p.Load(p.Path))` (as for Tree panel); the cursor/marks are restored by names via `states`/`snapshot()` (already the case). If a load was in progress at the moment of switching **into** Quick view/Info, cancellation is acceptable, but `Err` is reset to `""` and `Entries` are not touched. The same for Hidden panels: `togglePanel`, when showing a panel with `Err == "Reading folder… Esc stops"` and `p.load == nil`, re-reads it.
- **Edge cases:** a remote panel (re-reading is a new dial, acceptable); a panel in an archive; `viewReturns` is empty; the load fails again → rollback per P1-02.
- **Tests:** `TestQuickViewToggleBackReloadsPanel` (create a file while in Quick view, after Ctrl+Q the file is visible); `TestQuickViewDuringPendingLoadDoesNotStrandPanel` (slow load via a blocking `fsops.Operation`, Ctrl+Q twice, wait for `pollPanelLoad`, Entries > 1, Err == "").
- **Priority:** P2
- **Estimate:** S

#### P1-04. Folder history mixes local, remote and archive paths without a source marker
- **Function and variant:** Alt+F12 / `Commands → Folder history` after working in a remote panel or inside an archive; restoring `Folders` from setup.json.
- **Current behavior:** `beginLoad` (`app.go`) adds any `abs` to `p.History`, including a remote path (`/home/user`) and a path inside an archive (`/x/a.zip/dir`). `historyMenu("Folders")` (`commands.go`) shows bare strings and does `Panels[Active].Load(path)`: a remote path in a local panel → `open /home/user: no such file or directory`; a local path chosen in a remote panel goes to the server; a path inside an archive in an ordinary panel → `not a directory`. `saveSetup` saves all of this into `Folders`.
- **Required behavior:** a history element stores its source: `{Path, Remote *remotefs.Config(without secrets), Archive string}`; the `Folder history` list shows lines like `sftp://user@host/home/user`, `[Archive] /x/a.zip/dir`, `/Users/x`. Selection: local → `Remote=nil, Archive=nil, Load`; archive → `openArchiveAt(active, archive, path)` (an error if the archive has disappeared: `Archive not found: <path>`); remote → if any panel is connected to the same server — `connectRemote` with its live configuration, otherwise `connectionDialog` with pre-filled fields (as with bookmarks). The `Folders` format in setup.json is extended with backward compatibility (strings are treated as local).
- **Edge cases:** `HistoryLimit` applies to the combined list; duplicates by (source, path); the panel history (`p.History`) is also used in `saveSetup`.
- **Tests:** `TestFolderHistoryKeepsRemoteAndArchiveSources` (after navigating over loopback SFTP and an archive, selecting elements leads to the correct source); `TestFolderHistoryLegacyStringsAreLocal` (old setup.json).
- **Priority:** P2
- **Estimate:** M

#### P1-05. Remote symbolic links to folders are shown as files and cannot be opened
- **Function and variant:** Enter / Ctrl+PgDn / F3 (size) on a symlinked directory in an SFTP/FTP/FTPS panel.
- **Current behavior:** `loadRemote` (`remote.go`) sets `Dir: info.IsDir()`; SFTP `ReadDirContext` returns lstat attributes (`ModeSymlink`, `IsDir()==false`), FTP `EntryTypeLink` → `ModeSymlink|0777`. Unlike the local `Load` (`os.Stat` of the target), the target is not resolved. In the panel the entry has size `<LINK>`, Enter → `openRemoteDocument` tries to download the directory → the error `Downloading remote file` in the status; `walkRemoteSizes` does not traverse links.
- **Required behavior:** for entries with `ModeSymlink` in SFTP, perform `client.Stat(path)` (follow) in the same session and set `Dir` by the target (limit by the number of links, for example 500 per listing, so as not to slow down large directories; the rest — a lazy check on Enter). For FTP, on Enter on a link try `ReadDir(path)`: success → enter, otherwise open as a file. In the size column show `<LINK>` for links to directories; Cyan color with `Highlight`. Size calculation over a link to a directory — do not traverse (as locally), show `0`.
- **Edge cases:** a broken link (Stat error → file, Enter puts the server error in the status); a link cycle (the SFTP server will return an error); a timeout on Stat (cancellation via `o.Context()`).
- **Tests:** in `internal/ui/remote_test.go` with the `testnet` SFTP: `TestRemoteSymlinkedDirectoryEntersAsFolder`; the FTP variant `TestFTPLinkEntryEnterTriesDirectory`.
- **Priority:** P2
- **Estimate:** M

#### P1-06. Several windows: bookmarks and settings of one window are invisible in another and are overwritten on Save setup
- **Function and variant:** Ctrl+Shift+N in window A, Ctrl+N in window B; Shift+F9/AutoSave in different windows; `Options → …` in one window.
- **Current behavior:** `NewWindow` (`windows.go`) makes a JSON copy of `Settings` at the moment of creation. `saveSetup` (`settings.go`) writes the **whole** setup.json from the current window's copy: a bookmark saved in A (`saveFolderShortcut` calls `saveSetup` immediately) is overwritten when B presses Shift+F9 or closes with `AutoSave` (`Close()` → `saveSetup`). Window B never sees A's bookmark. There is no message at all.
- **Required behavior:** after a successful `saveSetup` in any window, `WindowGroup` distributes the updated `Settings` to the other windows (except `panelOptions`, which are per-window), via `a.jobs`; `configurePanel` + `refresh()` in the windows where `ShowHidden/IgnoreDSStore/SortGroups/MaskGroups/DescriptionFile` changed. Minimum alternative: `saveSetup` re-reads setup.json before writing and merges `Shortcuts/ShortcutRemotes/ShortcutArchives/UserMenu/Associations/RemoteConnections` (the last write wins per slot, an empty slot does not overwrite). Status in the other windows: `Settings updated from another window`.
- **Edge cases:** window B closed with AutoSave while a dialog is open in A; setup.json is not writable (both windows show the error); application shutdown (`CloseAll`).
- **Tests:** `TestShortcutSavedInOneWindowVisibleInOther` (WindowGroup with two Apps, `saveFolderShortcut(3)` in A, `Settings.Shortcuts[3]` in B); `TestSecondWindowSaveSetupKeepsFirstWindowShortcuts`.
- **Priority:** P2
- **Estimate:** M

#### P1-07. No hint about Full Disk Access on EPERM on protected folders
- **Function and variant:** Enter/Cmd+L/bookmark into `~/Library/Mail`, `~/Desktop`, `~/Documents` and the like without Full Disk Access granted; the same for Tree panel and Quick view of a folder.
- **Current behavior:** the footer shows the raw `open /Users/x/Library/Mail: operation not permitted` (`panel_load.go:pollPanelLoad`, `tree.go:status`, `commands.go:updatePreview`). The `Options → Full Disk Access…` item exists (`menu.go`), but nothing leads to it from the error; `native.PrepareDiskAccess` asks only on the first launch.
- **Required behavior:** a common function `describeReadError(path, err)`: if `errors.Is(err, os.ErrPermission)` and `syscall.EPERM` (not EACCES) on a local path → footer `Access denied by macOS privacy protection` and status `Grant Full Disk Access: Options → Full Disk Access…`. On EACCES — `Permission denied: <path>`. Apply in `pollPanelLoad`, `pollTree`, `updatePreview` (folder), `calculateFolderSizes` (`Unreadable` + a status with the same hint if all entries are EPERM).
- **Edge cases:** remote/archive — no hint; system folders (`/private/var/db`) — the same hint; a test cannot reproduce TCC → test the error-mapping function.
- **Tests:** `TestDescribeReadErrorMapsEPERMToDiskAccessHint`; `TestPanelLoadPermissionDeniedShowsHint` (a `chmod 000` folder → the EACCES variant of the message).
- **Priority:** P2
- **Estimate:** S

#### P1-08. Tree panel does not roll back on a root error (unlike the file mode)
- **Function and variant:** Cmd+L / bookmark / Folder history / Enter on a node that has managed to disappear, in an active Tree panel; a root without permissions.
- **Current behavior:** `startTree` calls `beginLoad(abs)` (the path is already changed), `pollTree` always does `installEntries(result.entries)` and `Err = result.status()` — the panel ends up on a non-existent path with a list of `..` and the error text (`tree.go:pollTree`). `loadPanel` in the same situation returns to the previous state. The existing `TestTreeMissingDirectory` pins the current behavior.
- **Required behavior:** `treeLoad` stores `previous Panel` (like `panelLoad`); when `result.err != nil && len(result.entries) == 0 && !errors.Is(err, context.Canceled)` → `*p = previous`, `p.Err = <error>`; on cancellation — as now (`Stopped · partial tree`). Status: `Cannot read <path>: <err>`.
- **Edge cases:** the first launch (previous.Path == "") — keep the current behavior; a root error with partially read subfolders is impossible (the root is read first).
- **Tests:** update `TestTreeMissingDirectory` (expect a path rollback when a previous one exists), add `TestTreeRootErrorRestoresPreviousRoot`.
- **Priority:** P3
- **Estimate:** S

#### P1-09. Fast find: `[` cannot be searched, non-ASCII names in NFD do not match
- **Function and variant:** Alt+`[` for files like `[Unreleased].md`; Alt+`é` on HFS+/external volumes with NFD names or names created by other programs in NFD; pasting from the clipboard.
- **Current behavior:** `fastMatch` (`fastfind.go`) calls `filepath.Match(lower(query)+"*", lower(name))`; `[` opens a character class → Match error → `false`; the first character is accepted anyway (the window opens, the cursor does not move), subsequent characters are rejected. There is no Unicode normalization (`grep norm` over `internal/ui` is empty): `é` (U+00E9) ≠ `e`+U+0301.
- **Required behavior:** escape `[` and `\` in the query before `Match` (keeping `*`/`?` as wildcards — this is a documented feature); compare `norm.NFC` of both strings after `ToLower` (add `golang.org/x/text/unicode/norm`; if the dependency is undesirable — limit to escaping). The same escaping is not needed for filter masks (there the brackets are syntax).
- **Edge cases:** a query consisting of a single `\`; names with combining marks in the middle; a search with `*` as the first character.
- **Tests:** `TestFastFindLiteralBracket`, `TestFastFindMatchesNFDNames` (create a file with an NFD name via `os.WriteFile`).
- **Priority:** P3
- **Estimate:** S

#### P1-10. Folder sizes (F3) in Tree panel and multi-column modes are calculated but not displayed
- **Function and variant:** F3/⌘⌥F3 with `View ∈ {Brief, Medium, Tree panel}`.
- **Current behavior:** `drawPanel` draws the size column only when `columns == 1 && View != "Tree panel" && width >= 12` (`panel_draw.go`). `calculateFolderSizes` runs, the status is `Calculated N folders`, the results only affect the `Size` sort order. The user does not see a single number.
- **Required behavior:** in Brief/Medium/Tree panel, after the calculation the status is extended with the size of the current folder: `Calculated 3 folders · <name>: 1.5M (12 files, 3 dirs)`; in the panel footer, for the folder under the cursor that has a calculated size, show `<size> in <name>` instead of the base line (in Tree panel — instead of the tree status line while the cursor is on such a folder). The `>=`/`~` prefixes are kept.
- **Edge cases:** `..` (the size of the current folder) — show as `<size> in .`; the footer with an `Err` error — the error takes priority.
- **Tests:** `TestFolderSizeVisibleInBriefFooter`, `TestTreePanelFolderSizeInFooter`.
- **Priority:** P3
- **Estimate:** S

#### P1-11. Quick view: empty source panel and special files without meaningful text
- **Function and variant:** the source is an empty filtered panel in `/` (not even `..`), an empty panel after a load error; the cursor on a FIFO/socket/device (`/dev`).
- **Current behavior:** `updatePreview` (`commands.go`): `source.Current()` returns `""` → `os.Stat("")` → the text `stat : no such file or directory`. For non-regular files no branch fires → `text == ""`, the panel is empty without an explanation.
- **Required behavior:** empty source → `No file selected`; special file → `"<name>\n<Type> (no preview)"`, where Type ∈ `Named pipe`, `Socket`, `Character device`, `Block device`, `Symbolic link` (for broken ones — `Broken symbolic link → <target>` via `os.Readlink`).
- **Edge cases:** an archive source panel — a separate message (see Discrepancies, item 13); `..` at the root of an archive.
- **Tests:** `TestQuickViewEmptySourceAndSpecialFiles` (a FIFO via `syscall.Mkfifo`, a broken symlink).
- **Priority:** P3
- **Estimate:** S

#### P1-12. Enter on `.app` in Tree panel launches the application instead of navigating the tree
- **Function and variant:** Tree panel, a `Foo.app` node (bundles get into the tree as directories), Enter / Ctrl+PgDn / double click.
- **Current behavior:** `enterFile` (`launch.go`) checks the `.app` extension before the panel mode → `/usr/bin/open Foo.app`. In file mode this is expected, in the tree it is not (the tree shows the bundle contents as subfolders).
- **Required behavior:** with `p.View == "Tree panel"`, Enter always does `Load(Current())` (rebuilding the tree with the new root); launching a bundle from the tree — only via Shift+Enter.
- **Edge cases:** an `.app` that is a symbolic link (does not get into the tree).
- **Tests:** `TestTreeEnterOnAppBundleReroots`.
- **Priority:** P3
- **Estimate:** S

#### P1-13. Filter/masks referring to a deleted mask group silently hide all files
- **Function and variant:** `File panel filter` = `<src>`, then the `src` group is deleted in `Groups of file masks`; the same via manual editing of setup.json; `SortGroups` with `<group>`.
- **Current behavior:** `validateMasks` is called only on input. `matchesMask` (`panel_options.go`) after `expandMasks` leaves `<src>` as a literal → `filepath.Match("<src>", name)` never matches → all files disappear, the footer shows `[<src>] 0 bytes in N items`, folders are visible. The user does not understand why the panel is empty.
- **Required behavior:** on `configurePanel`/`Load` the filter is checked with `validateMasks`; an invalid filter is not applied, the footer shows `[<src>] invalid filter — showing all`, the status `Panel filter refers to unknown mask group; edit it in Commands → File panel filter…`. Deleting a group in `Groups of file masks` while it is referenced from filters/SortGroups/Associations — a confirmation `Group <src> is used by a panel filter. Delete anyway?` (`Delete`/`Cancel`).
- **Edge cases:** a group is renamed (the old name is deleted — the same confirmation); a `filepath.Match` syntax error in a saved filter.
- **Tests:** `TestInvalidPanelFilterShowsAllAndWarns`, `TestDeleteMaskGroupInUseAsksConfirmation`.
- **Priority:** P3
- **Estimate:** S

#### P1-14. Key bar does not reflect the read-only archive panel
- **Function and variant:** the active panel is inside an archive; clicking the `4 Edit`, `6 RenMov`, `7 MkFold`, `8 Trash` cells.
- **Current behavior:** `Draw` (`draw.go`) changes the label only for remote (`Delete`); for an archive the labels are the usual ones, but `function(4/6/7/8)` answers `Archive is read-only. Use F5 to copy files out.` (`app.go:function`, `archiveReadOnly`).
- **Required behavior:** for an active archive panel the labels are: `3 View`, `5 Extract`, `4`/`6`/`7`/`8` — empty (like the 10th); on hover/click — the existing status.
- **Edge cases:** a remote archive is not supported; the Shift variants are not displayed (labels do not change with modifiers — as now).
- **Tests:** extend `TestDrawSmallAndLarge`/`panel_draw_test.go`: `TestKeyBarLabelsForArchivePanel`.
- **Priority:** P3
- **Estimate:** S

#### P1-15. ⌘C and Ctrl+Insert copy different things with the cursor on `..` / an empty panel
- **Function and variant:** ⌘C with an empty command line (`copyPanelCommand`) versus Ctrl+Insert (`farPanelKey`).
- **Current behavior:** `copyPanelCommand` (`mouse.go`) takes `Entries[Cursor].Name` even for `..` → the string `..` gets into the clipboard; with an empty panel the clipboard is not changed. `farPanelKey` uses `Selected()` (excludes `..`) and, when there are no names, copies `p.Path`.
- **Required behavior:** ⌘C uses the same logic as Ctrl+Insert: names from `Selected()` joined with `\n`; if empty — the panel path; status `Copied N names` / `Copied panel path`.
- **Edge cases:** Tree panel (relative node paths — copy as is); remote — names only.
- **Tests:** `TestCopyCommandMatchesControlInsert`.
- **Priority:** P3
- **Estimate:** S

## Part 2. File operations, archives, launching programs

#### P2-01. Destination is a symbolic link to a directory (`/tmp`, `~/link`)
- **Function and variant:** F5/F6 (local and remote), the destination is entered/suggested as a path whose last component is a symlink to a directory (`/tmp` → `/private/tmp`, user-created links).
- **Current behavior:** `transfers.go:transferDialog` checks the destination via `destination.Lstat(dst)` → a symlink is not `IsDir()`. Several items → `destination must be an existing directory`. One item → `target = dst` → `fsops.transfer`: `os.Lstat(dst)` exists, not a directory → the collision dialog `The file already exists\n/tmp … Existing: N bytes`; `Overwrite` → a staging copy and `os.Rename(tmp, dst)` **replaces the link itself** with the file (for move — `os.Rename(src, dst)` likewise). In `remotefs.Copy.walk` for a directory source: `dest.Lstat` → `destination is not a directory`. The same code for a remote destination (`sftpFS.Lstat`).
- **Required behavior:** to determine "the destination is a container", use `Stat` (follow the link) both in the UI and in `remotefs.Copy.walk` (`create folder`) and in `fsops.transfer` (`di`). Far Manager semantics: a link to a directory behaves like a directory — items are placed inside, directories are merged. A link to a *file* remains a collision object (as now). `Overwrite` over a symlink to a directory is not offered. In the collision dialog for a link to a file, add the line `Existing: symbolic link → <target>`.
- **Edge cases:** a broken link in the destination (Stat error ENOENT → treat as an existing non-directory, a collision with `Existing: broken link`); a link to a directory inside the source (the `destination is inside source` check is already via `EvalSymlinks` — keep it); remote: `Lstat` on SFTP + `Stat` — a `Stat` method is needed in the `remotefs.FS` interface (FTP: `ReadDir(parent)` does not give the link target — treat a symlink as a directory only if `ReadDir(dst)` succeeds).
- **Tests:** `TestTransferIntoSymlinkedDirectory` (fsops: file→link-to-dir places inside, dir→link-to-dir merges, the link is intact); `TestCopyDialogAcceptsSymlinkedDirectory` (ui: two marked files into a `/tmp`-like link — no error); `TestMoveIntoSymlinkedDirectoryKeepsLink`; `TestCollisionWithFileSymlinkStillAsks`.
- **Priority:** P1 (unexpected replacement of the link, blocking of the standard `/tmp`).
- **Estimate:** M.

#### P2-02. Cross-volume move: Retry/Skip/metadata act on the whole tree rather than on a file
- **Function and variant:** F6, the destination on another volume (EXDEV), a directory with an error on one of the files (no permissions, ENOSPC, disappeared) or a refusal in the `Cannot preserve metadata` dialog for one file.
- **Current behavior:** `fsops/transfer.go:moveAcrossVolumes` → `Copy(src, dst, o)` → a recursive `copyEntry` **without** `Attempt` at the file level. An error on any file bubbles up to `Transfer` → `Attempt("move", <root>)`; `Retry` repeats the copy of the **whole** tree from scratch (the deferred `RemoveAll(dst)` has already deleted the partial copy), `Skip` skips the **whole** root item. `Skip` in `metadataQuestions` for a single file returns `ErrSkipped` from `copyEntry` → the parent `copyEntry` deletes the whole directory → in the end "Done — 1 item(s) skipped", the directory is not moved, although the user expected one file to be skipped. This contradicts `docs/FILE_OPERATIONS.md` ("Retry repeats the failed file or step, not the whole batch") and the text of the F6 spec ("Skip → the item is skipped, the source remains").
- **Required behavior:** perform the cross-volume move item by item, like F5: walk the tree, for each file `Attempt("move", file, dstFile)` (copy + metadata + `delete source` of the file), create directories with the walker, apply directory metadata in reverse order, delete the empty source directory via `Attempt("delete source folder")`; with skips, the source directory remains with the skipped files (as with a same-volume merge). The `Error` dialog: `Cannot move\n<file>\nTo: <dstFile>\n\n<err>`. The metadata dialog — per file; `Skip` leaves only this file in the source, the copy of the file is deleted. Progress: `o.Scan` already exists — the percentages are preserved.
- **Edge cases:** hard links (`hardlinks.go` must work item by item — `rememberHardlink`/`copyHardlink` are already per-file); cancellation between files — the moved files remain in the destination, the rest in the source (as same-volume); `.DS_Store` is transferred (`o.moving`); an error on `delete source` of a file after publication — Retry only the deletion (the `Attempt` semantics already exist).
- **Tests:** `TestCrossVolumeMoveSkipsSingleFileAndKeepsRest` (one file without read permission → Skip → the rest are moved, the source contains only the skipped one); `TestCrossVolumeMoveRetryRepeatsOnlyFailedFile` (a call counter on `metadataCopier`); `TestCrossVolumeMoveMetadataSkipKeepsOnlyThatFile`.
- **Priority:** P1 (re-copying gigabytes, loss of progress, non-compliance with the policy).
- **Estimate:** M–L.

#### P2-03. Remote: Retry after a connection drop uses the dead session
- **Function and variant:** F5 involving a remote panel, remote F8, remote F6 — a network drop/timeout in the middle of a transfer.
- **Current behavior:** `remotefs/transfer.go:Copy` opens `s`, `d` once per worker; `child.Attempt("copy", …, func() { copyFile(s, d, …) })` on `Retry` calls `copyFile` on the same closed connections → guaranteed the same error; the walker's `scan`/`dest` (`Attempt("read folder")`, `Attempt("create folder")`) — likewise. `remoteDelete`/`remoteMoveDialog` hold a single `f := config.Dial` for the whole operation. The user is left with Skip/Cancel.
- **Required behavior:** in the `Attempt` closures of remote steps, reopen the session before retrying: on an error classified as a network one (`net.Error`, `io.EOF`, `io.ErrUnexpectedEOF`, `sftp` connection lost, FTP 421/426), the worker closes `s`/`d` and calls `openTransferEndpoint` again (with the `Attempt("connect")` that already exists). The `Error` dialog stays the same; during reconnection the progress path shows `Reconnecting to <label>`. The partial `.farshmac-part-*` is deleted (the 2 s cleanup session already exists).
- **Edge cases:** SFTP with the password in memory — reconnect without asking again (the password is stored in `Config`); an unknown host key on reconnect — an error, not a dialog; FTP: a separate data session; cancellation during reconnect.
- **Tests:** `TestRemoteCopyRetryReconnects` (integration with a local SFTP server: close the server connection after the first file, Retry → the second file is copied); `TestRemoteDeleteRetryReconnects`.
- **Priority:** P1 (Retry does not work in the main scenario of its use).
- **Estimate:** M.

#### P2-04. `Skip` on a preparatory step (`connect`, `open destination`, `read destination`) finishes the operation as "Done — skipped"
- **Function and variant:** F5/F6 with a remote panel (or locally on a destination `Lstat` error): an error on a step before the transfer starts, the answer `Skip`/`Skip all`.
- **Current behavior:** `transfers.go:transferDialog`: `o.Attempt("open destination" …)` → `ErrSkipped` → `return err` → `workProgress` turns `ErrSkipped` into success → status `Done — 1 item(s) skipped`, the panels are re-read, nothing is copied. The same for `read destination`, `remotefs/transfer.go:openTransferEndpoint` (`connect`) in the walker and in the workers (via `fail(ErrSkipped)` → `first = ErrSkipped`).
- **Required behavior:** for steps without which the operation is impossible, do not offer `Skip`/`Skip all`: the `Error` dialog with the buttons `Retry`, `Cancel` (add a `Fatal bool` flag to `fsops.Failure`; `operationQuestions` builds the button set from it). If a worker could not connect but the walker is connected — offer `Retry`/`Cancel`; on `Cancel` → `operation cancelled`, on success of the other workers — continue with fewer workers (or simply Cancel, which is simpler).
- **Edge cases:** `Skip all` chosen earlier on a file error (`skipErrors == true`) — still show the dialog on a fatal step; cancellation during `connect` (already `ctx.Err()`).
- **Tests:** `TestFatalStepOffersOnlyRetryAndCancel` (ui, replace `targetEndpoint` with an error); `TestConnectSkipDoesNotReportDone`.
- **Priority:** P2 (a false "Done").
- **Estimate:** S.

#### P2-05. Progress of remote delete / remote move: eternal "Working / calculating… 0 items"
- **Function and variant:** F8/Shift+F8 on a remote panel; F6 on the same server.
- **Current behavior:** `remote.go:remoteDelete` and `remoteMoveDialog` do not call `o.Streaming()`/`o.Add`; `NewOperation` starts with `Scanning=true`; `remotefs.removeTreeStep`/`moveEntry` call only `o.Complete`. `drawProgress` shows `Working / calculating… ▰▱▱▱  0 items`, the path line is empty (no `o.Begin`), the statistics are `0 B / 0 B` until the end.
- **Required behavior:** `o.Streaming()` at the start; in `removeTreeStep`/`moveEntry` — `o.Add(p, size)` + `o.Begin(p, size)` before the action, `o.Complete` after, so that the window shows the current path and the `N items` counter (like local deletion). For `RemoveTree`, after `ReadDir` add the children to `Add` immediately (a total per directory).
- **Edge cases:** `Skip` of a child — `SkipItem` already calls `CompleteTree`; depth > 256.
- **Tests:** `TestRemoteDeleteProgressCountsItems` (snapshot `TotalFiles == N`, `Path != ""`, `Streaming == true`); `TestRemoteMoveProgressShowsPath`.
- **Priority:** P2.
- **Estimate:** S.

#### P2-06. A case-only rename (`File` → `file`) on a case-insensitive volume is rejected
- **Function and variant:** F6/Shift+F6 locally, the same name in a different case in the same folder (APFS is case-insensitive by default). Also F5 `File` → `file` into the same folder.
- **Current behavior:** `fsops/transfer.go:transfer`: `os.Lstat(dst)` finds the same inode → `os.SameFile(si, di)` → `source and destination are the same file` → the `Move / rename failed` dialog. For a directory `Folder` → `folder` the same. There is no test.
- **Required behavior:** in the move branch: if `SameFile(si, di)` and `filepath.Dir(src) == filepath.Dir(dst)` and `strings.EqualFold(Base(src), Base(dst))` and the names differ — perform `os.Rename(src, dst)` directly (APFS changes the case of the entry atomically), `o.CompleteTree(src)`; when `src == dst` (literally) — the previous error. For copy — keep the error, but with the text `cannot copy a file onto itself`. For remote move (`remotefs/move.go:moveEntry`), when `Lstat(dst)` exists and the names differ only in case — try `Rename` and show the server error only if it fails (the server may be case-sensitive — then it is an ordinary collision).
- **Edge cases:** case-sensitive APFS — `Lstat(dst)` will not find it, an ordinary `Move`; a hard link with the same inode under another name in the same directory (`a` and `A` — two hardlinks) — `EqualFold` is true, but `Rename` would simply replace `A` with the link `a`... need to compare `Nlink > 1` and in that case show a collision; `..`.
- **Tests:** `TestMoveCaseOnlyRename` (skip if the volume is case-sensitive — detect by creating `a`/`A`); `TestMoveCaseOnlyRenameDirectory`; `TestCopyOntoItselfMessage`.
- **Priority:** P2.
- **Estimate:** S.

#### P2-07. Remote copy with default settings asks `Cannot preserve metadata` for every file
- **Function and variant:** F5 local↔remote, remote↔remote with `Preserve permissions`/`Preserve attributes` enabled by default.
- **Current behavior:** `remotefs/transfer.go:portableMetadata` **unconditionally** adds `remote transfer cannot preserve owner/group and ACL` and `… xattrs, flags and creation time` even when `Chmod`/`Chtimes` on SFTP succeeded → `MetadataFailure` → a dialog on the first file; without `For all` — on every one. For FTP, `Chmod` always returns `FTP does not provide portable permission preservation`. With `workers > 1` the first N workers serialize on the very same question. The spec records this (discrepancy 8), but it is a UX defect: the default settings make every remote copy "questionable".
- **Required behavior:** separate a "real error" (a `Chmod`/`Chtimes` failure) from a "known protocol limitation". Show the limitation **once per operation** with the `Cannot preserve metadata` dialog with the text `Remote transfers keep only mode and modification time.\nOwner/group, ACL, xattrs, flags and creation time are not transferred.\nContinue for all files?` and the buttons `Continue`, `Cancel` (the answer is remembered for the operation, like `For all`). Real `Chmod`/`Chtimes` errors — the previous per-file dialog (Retry/Skip/…). For FTP with Permissions enabled — once `FTP cannot set permissions` in the same way. Optionally: a `Settings.RemoteMetadataNotice` setting "do not show again".
- **Edge cases:** symlinks (`cannot preserve symbolic-link metadata`) — include in the same single notice; `For all` from the previous dialog must cover it as well; `ApplyMetadata` of directories at the end — do not ask again.
- **Tests:** `TestRemoteCopyAsksMetadataOncePerOperation` (3 files onto SFTP → exactly one dialog); `TestRemoteChmodFailureStillAsksPerFile`.
- **Priority:** P2.
- **Estimate:** M.

#### P2-08. SFTP without `posix-rename@openssh.com`: `Overwrite` always fails with an unclear error
- **Function and variant:** F5 onto an SFTP server (not OpenSSH: some NAS devices, Go/Java servers), a collision → `Overwrite`; remote F6 → `Replace`.
- **Current behavior:** `remotefs/sftp.go:Rename` without the extension uses `SSH_FXP_RENAME`, which by protocol refuses when the destination exists → `copyFile` returns `sftp: "Failure" (SSH_FX_FAILURE)` → the `Error` dialog `Cannot copy … Failure`; `.farshmac-part-*` is deleted. The user does not understand the cause. The policy (`FILE_OPERATIONS.md`: "If the server cannot rename or replace an entry, it reports an error") is honored, but the text is useless.
- **Required behavior:** in `copyFile`/`moveEntry`, on `Replace` and a `Rename` error on SFTP without posix-rename, wrap it: `server cannot replace an existing file atomically (no posix-rename); delete <dst> first or choose Rename`. No automatic `Remove(dst)+Rename` (we keep the absence of non-atomic replacement). Hide `Overwrite` in the collision dialog for such a server? — no, keep it, but with a clear error.
- **Edge cases:** FTP `RNTO` onto an existing one — server behavior; the same wrapper by code 550.
- **Tests:** `TestSFTPReplaceWithoutPosixRenameExplains` (a test server without the extension).
- **Priority:** P2.
- **Estimate:** S.

#### P2-09. Archive commands do not check for a remote opposite panel
- **Function and variant:** Shift+F3 → `Extract to folder…` with a remote opposite panel; Shift+F1/Shift+F2 with a remote opposite panel.
- **Current behavior:** `transfers.go:archiveCommands` checks only `remoteUnsupported()` (the active panel); `archiveExtractPath` takes `a.Panels[1-a.Active].filesystemPath()` → for remote this is `RemoteLocalPath` (the local path before connecting) → the archive **is silently extracted into a local folder** that the user does not see in the panel. `archiveExtract` and `archiveCreate`, when `Panels[1-Active].Remote != nil`, do `return` **without a status** — the key simply does not work.
- **Required behavior:** at all three points, with a remote opposite panel — the status `Archives are extracted and created on local panels. Use F5 to transfer the result afterwards`, the operation does not start. For `Extract to folder…` the default value is the path of the active (local) panel if the opposite one is remote.
- **Edge cases:** the active panel is an archive, the opposite one is remote (Shift+F2 inside an archive).
- **Tests:** `TestArchiveCommandsRefuseRemoteOppositePanel` (three commands, checking the status and the absence of files in `RemoteLocalPath`).
- **Priority:** P2 (data appears in an unexpected place).
- **Estimate:** S.

#### P2-10. User menu and "Open current file with macOS default app" on an archive/remote panel work with the virtual path
- **Function and variant:** F2 → a command with `{}` on an archive panel; `Commands → File associations… → Open current file with macOS default app` on a remote or archive panel.
- **Current behavior:** `commands.go:runUserCommand` checks only `remoteUnsupported()`; on an archive panel `p.Current()` = `<archive>/<member>` — a non-existent path is substituted into the command. `commands.go:fileAssociations` (i == 1) does not check the mode at all: on a remote panel `/usr/bin/open <remote path>` opens the **local** file with the same path if it exists (for example `/etc/hosts`), otherwise `Opening failed`. Shift+Enter handles these cases (`openSystemSelected`, test `TestSystemOpenReportsFailureAndRejectsVirtualPaths`), but these two paths do not.
- **Required behavior:** `runUserCommand`: with `p.Archive != nil` and the cursor inside the archive → the status `Use F5 to copy this archive item out before running commands on it`. `fileAssociations` item 2 → call `openSystemSelected()` (which already contains all the checks) instead of a direct `exec`. Also remove the duplicate `workProgress("Opening")` with `Run()` that does not report the error.
- **Edge cases:** the cursor on `..` at the root of the archive (`Relative` → `.`, not inside) — it is acceptable to open the archive file itself.
- **Tests:** `TestUserMenuRefusesArchiveMember`; `TestAssociationsMenuOpenUsesSystemOpenChecks` (remote → status, archive → status).
- **Priority:** P2.
- **Estimate:** S.

#### P2-11. Shift+F8 with `Confirm deletion` disabled deletes without confirmation; on remote the confirmation is always shown
- **Function and variant:** Shift+F8/Shift+Delete locally, `Settings.ConfirmDelete == false`.
- **Current behavior:** `delete.go:deleteSelected`: `if a.Settings.ConfirmDelete { confirm } else { remove() }` — also for `permanent == true`. Remote deletion (`remote.go:remoteDelete`) always confirms, "Always make this distinction explicit". Inconsistent: irreversible local deletion is less protected than remote.
- **Required behavior:** `Confirm deletion` controls only F8 (Trash). Shift+F8 always confirms (`Delete permanently` / `Permanently delete N item(s)?` / `Delete`, `Cancel`). Reflect this in the Confirmations settings with the label `Overwrite files / Move to Trash (permanent deletion always asks)`.
- **Edge cases:** invocation from the menu `Files → Delete permanently…`; no migration of `Settings.ConfirmDelete` is required.
- **Tests:** extend `TestDeleteShortcuts`: `ConfirmDelete=false`, Shift+F8 → the dialog is present; F8 → no dialog.
- **Priority:** P2.
- **Estimate:** S.

#### P2-12. The delete confirmation dialog lists all names
- **Function and variant:** F8/Shift+F8 with hundreds/thousands of marked items.
- **Current behavior:** `delete.go:deleteSelected`: `message = "Move " + strings.Join(names, ", ") + " to Trash?"` — all basenames separated by commas; the dialog stretches over pages (PageUp/PageDown scrolling exists, but the text is unreadable). Remote: `Delete N selected item(s) on <label>?` — correct.
- **Required behavior:** as in Far: one item — `Move <name> to Trash?`; several — `Move 3 items to Trash?`/`Permanently delete 1,204 items?`; with directories — without traversal (as now). Optionally show the first 3 names and `… and N more`.
- **Edge cases:** a name with line breaks (impossible on macOS, but `\r` is possible) — escape it.
- **Tests:** `TestDeleteConfirmationSummarisesManyItems`.
- **Priority:** P3.
- **Estimate:** S.

#### P2-13. `~` in the destination for a remote panel expands to the local home directory
- **Function and variant:** F5 onto a remote panel or remote F6, entering `~/dir`.
- **Current behavior:** `app.go:expand` substitutes `os.UserHomeDir()` → `/Users/<local>/dir` is sent to the server as an absolute path. `remoteMkdir` is protected by `ValidName`; `remoteMoveDialog` and `transferDialog` are not.
- **Required behavior:** with a remote destination, treat `~` as `Config.Directory` (the starting directory of the connection) or reject with the status `~ is not available on remote destinations; enter an absolute server path`. The second is preferable (simple and honest).
- **Edge cases:** `~user/` — reject as well.
- **Tests:** `TestRemoteDestinationRejectsTilde`.
- **Priority:** P3.
- **Estimate:** S.

#### P2-14. A destination with a trailing `/` that does not exist creates a file instead of a directory
- **Function and variant:** F5/F6 of a single file with the input `backup/` (the directory does not exist).
- **Current behavior:** `expand` → `filepath.Clean` removes the `/`; `transferIntoDirectory` takes the `/` suffix into account only when `isDir`; a single file is copied **as a file named `backup`**; several files → `destination must be an existing directory`. Far Manager creates the directory on `/` and places the items inside.
- **Required behavior:** if the input ends with `/` and the path does not exist — create the directory (`MkdirAll` locally, `Mkdir` via `Attempt("create folder")` on remote) and copy inside. For several items — the same instead of the error.
- **Edge cases:** the parent does not exist on remote — `Mkdir` without `-p` → a Retry/Cancel error; `/` on an existing file — a collision? → the error `destination is not a directory`.
- **Tests:** `TestTrailingSlashCreatesContainer` (one and several items, local and SFTP).
- **Priority:** P3.
- **Estimate:** S.

#### P2-15. An archive open error is visible only in the status line
- **Function and variant:** Enter/Ctrl+PgDn on a damaged, encrypted or too large archive.
- **Current behavior:** `archive_panel.go:openArchiveAt` → `backgroundTask` without `reportError` → `pollTask` calls only `a.error(err)`: the text `archive operation failed (damaged, encrypted or unsupported archive): exit status 1: tar: …` is truncated to a single status line; the bsdtar stderr (up to 32 KiB) is not visible. For `Shift+F3 List/Test` (`backgroundProgress`) the same.
- **Required behavior:** `openArchiveAt`/`archiveCommands`: `a.task.reportError = true` → a modal `Opening archive failed`/`Reading archive failed` `[OK]` with the full text (scrolling is already supported). For encrypted ones: if stderr contains `Passphrase`/`encrypted` — the first line is `Encrypted archives are not supported`.
- **Tests:** `TestOpeningDamagedArchiveShowsDialog`; `TestEncryptedArchiveMessage`.
- **Priority:** P3.
- **Estimate:** S.

#### P2-16. Shift+F1 onto an existing name: Retry/Skip dialog, `Skip` reports `Done`
- **Function and variant:** Shift+F1, the archive name already exists (`archive already exists: <dst>`) or an unsupported extension.
- **Current behavior:** `transfers.go:archiveCreate` wraps the whole `CreateArchive` in `Attempt("create archive")` → the `Error` dialog `Cannot create archive\n<paths separated by \n>\nTo: <dst>` with Retry/Skip/Skip all/Cancel; `Skip` → `ErrSkipped` → `Done — 1 item(s) skipped`, the archive is not created. `Retry` without changing the name is pointless. The list of source paths in `Source` can take up the whole screen.
- **Required behavior:** perform the destination checks (`checkArchiveDestination`, format) in the UI **before** starting the operation: an existing file → the `Warning` dialog `The archive already exists\n<dst>` with the buttons `Rename`, `Cancel` (Rename → a repeated prompt with the candidate `<stem> (1).zip`); an unsupported extension → status. Errors inside the packing (read permissions on a file) — Retry/Skip per file (wrap the file read in `Attempt("read")` in `writeArchiveSources`/`CreateZIP`; Skip excludes the file from the archive with the summary `Done — N item(s) skipped`).
- **Edge cases:** the destination exists as a directory; a race — the file appeared after the check: `os.Link` returns EEXIST → an ordinary `Error`.
- **Tests:** `TestCreateArchiveExistingNameOffersRename`; `TestCreateArchiveSkipsUnreadableFile`.
- **Priority:** P3.
- **Estimate:** S–M.

#### P2-17. Encrypted ZIP: the index opens, a member fails with `zip: checksum error`
- **Function and variant:** Enter on an encrypted `.zip` (the central directory is read by Go), then F3/F5 on a member.
- **Current behavior:** `ReadArchiveIndex` does not look at the encryption flag (`zip.File.Flags & 1`) → the panel opens; `withArchiveSelection` for `Deflate` uses the Go path → unpacking garbage → `zip: checksum error` (or `flate: corrupt input`) without an "encrypted" hint. The test `TestEncryptedArchiveFailsWithoutPrompt` covers only `ArchiveContents` (bsdtar).
- **Required behavior:** in `ReadArchiveIndex` for zip: if any member has the encryption bit set — the error `encrypted archives are not supported` (and see P2-15 for displaying it). In `withArchiveSelection` — the same before opening a member (protection against a race/partial encryption: show encrypted members, but give a clear error on extraction).
- **Tests:** `TestEncryptedZIPIndexRefusedWithMessage`; `TestEncryptedZIPMemberExtractionMessage`.
- **Priority:** P3.
- **Estimate:** S.

#### P2-18. Message when copying a file onto itself: `destination is inside source`
- **Function and variant:** F5 of a file into the same folder with the same name (panels on the same folder, the suggested path).
- **Current behavior:** `remotefs/transfer.go:Copy`: for the same endpoint `a == b` → `destination is inside source` → the `Copy failed` dialog. The local `fsops.transfer` for the same case says `source and destination are the same file`; Far Manager: "cannot copy file onto itself".
- **Required behavior:** for a file (`!info.IsDir()`) → `cannot copy a file onto itself: <path>`; for a directory — the previous text. Show it before starting (in `transferDialog`) as a status, without the `Copy failed` dialog, as Far does.
- **Tests:** `TestCopyOntoItselfMessages` (file/dir, local/remote).
- **Priority:** P3.
- **Estimate:** S.
## Part 3. Internal viewer and editor

#### P3-01. Pre-checking permissions when opening for editing (TODO, items 1–3)
- **Function and variant:** F4 / Alt+F4 / Cmd+Shift+F4 / Shift+F4 (existing file) / Alt+F11 (`Edit …`) / F6 (viewer → editor) for a local file; F4 for a remote file. Variants: file without read permission; file readable, but neither the file nor the directory is writable; file writable, directory not; directory writable, file not (0444, owned by someone else); read-only volume; `uchg`/`uappnd`; remote file without write permission (by `mode`/owner, as far as known).
- **Current behavior:**
  - `document.go: openDocument` only does `EvalSymlinks` → `os.Open` → `Stat` → `IsRegular`. Without read permission: `open <path>: permission denied` in the truncated status (in Async via `openDocumentAsync` → `worker.Status` → `pollTask` → `a.error`, `reportError` is not set, no dialog).
  - Write permissions are checked neither for the file nor for the directory. The editor opens, the user edits, and only `fsops.Save` (`os.CreateTemp(filepath.Dir(path))`) reports `open <dir>/.farshmac-save-…: permission denied` / `read-only file system`, truncated.
  - There is always a single save strategy: a temporary file in the same directory + `rename` (`fsops/files.go: Save`). So "a writable file in a non-writable directory" **cannot** be saved, while "a 0444 file in a writable directory" **can** (silently).
  - `remote_document.go: openRemoteDocument(edit=true)`: no checks of `info.Mode()`; the error will come from the server on save.
  - F6 from the viewer (`documentKey`, `key == "F6"`) checks only encoding/binary status.
- **Required behavior:**
  1. Introduce `fsops.SaveAccess(path) (strategy, err)` (or an equivalent in `ui`): `strategy ∈ {replace, inplace, none}`. `replace`: the directory is writable (`access(W_OK)` on `Dir(path)`, taking a read-only volume into account via `statfs`/`MNT_RDONLY`) and the file has no `UF_IMMUTABLE`/`SF_IMMUTABLE`/`UF_APPEND`; `inplace`: the directory is not writable, but the file is writable (`access(W_OK)`) and has no flags; `none`: neither. The document remembers the strategy (`d.SaveStrategy`), and `saveDocument` uses it (see P3-03).
  2. No read permission (local): the document does not open; dialog `Cannot open file` with the text `<path>\n<reason>` (for example `permission denied`), button `OK`. For F4 add the hint `Check the file permissions or Full Disk Access.`
  3. Readable, but `strategy == none`: confirm `Read-only file`, text `<basename> cannot be saved here: <reason>\nOpen it read-only?` (reason: `folder is not writable`, `file is locked (uchg)`, `read-only volume`), buttons `View`, `Edit anyway`, `Cancel`. `View` → open in the viewer (like F3); `Edit anyway` → editor with `ReadOnly = false`, but the status `Saving may fail: <reason>`; `Cancel` → nothing. The dialog is shown from the apply part of `openDocumentAsync` (the user's decision is on the engine queue; the check itself is in the worker).
  4. F6 in the viewer: the same check; on `none` → the same confirm, but instead of `View` a `Read-only` button (switches to the editor with `ReadOnly = true`, indicator ` RO`).
  5. Remote file: if `info.Mode()` explicitly has no `w` for anyone (`mode&0222 == 0`) → confirm `Remote file is read-only` / `<remotePath> has mode <perm>. Saving will probably fail.` / `View`, `Edit anyway`, `Cancel`. Do not guess the owner/group; this is only a warning.
  6. Archive documents: no changes (already `ReadOnly` + `ArchiveSource`).
  7. The check is a heuristic: its result does not forbid saving; errors of the real save are handled per P3-02 (TODO, item 4).
- **Edge cases:** symbolic link: check the permissions of the resolved path (`resolved`) and its directory; file on a network volume (SMB): `access()` may lie, on a discrepancy rely on P3-02; Shift+F4 for a new file: check only the directory (`strategy == none` → the same confirm with `Edit anyway`/`Cancel`, without `View`); Alt+F11 `Edit` entries: the same path (`openDocument(path, true)`); F3 (view): no write checks; pages > 64 MiB: the editor is unavailable anyway.
- **Tests:** `TestEditOpenRefusesUnreadableFileWithDialog` (chmod 0000, Async: expects `Dialog.Title == "Cannot open file"`, the text contains the path); `TestEditOpenWarnsWhenFolderNotWritable` (file 0644 in a 0555 directory: confirm `Read-only file`, `View` opens the viewer, `Edit anyway` the editor); `TestEditOpenDetectsInplaceStrategy` (file 0644, directory 0555 → `strategy == inplace`, no dialog); `TestImmutableFileOpensReadOnly` (`chflags uchg` → `none`); `TestRemoteReadOnlyModeWarns` (SFTP loopback, mode 0444 → confirm). For directories with chmod the tests must be skipped under root (`os.Geteuid() == 0`).
- **Priority:** P1 (prevents the "nowhere to save the edits" situation; there is no actual loss of edits thanks to P3-02).
- **Estimate:** M.

#### P3-02. Open/save errors: readable message, buffer preservation, retry and Save as (TODO, items 4–5)
- **Function and variant:** F2 / Cmd+S / Shift+F10 / `Save` in `Save changes` (closing, `+`/`-`, Cmd+Q `Save all`) / Shift+F2 Save as; locally and remotely. Errors: `permission denied` (directory or file), `read-only file system`, `no space left on device`, `operation not permitted` (`uchg`, sticky), `no such file or directory` (the Save as directory was deleted), `link …: file exists` (Shift+F4: the file was created in the meantime), `file changed on disk; …` / `remote file changed; …` (see P3-04), server errors on `Dial`/`Create`/`Rename`, encoding `text cannot be represented in …`, `editor is read-only`.
- **Current behavior:**
  - The buffer and Dirty **are preserved** in all branches: sync — `saveDocument` returns the error before `markSaved`; Async — the clone is discarded, `*d = *copy` only in apply (`document_tasks.go: saveDocumentThen`; `task.go: pollTask`). `Saved` is printed only in apply. I.e. the requirement "never report success and never lose edits" is already met, except for one edge: `saveDocument` after a successful `rename` does `d.Path = path; d.Info, err = os.Stat(path)`; if `Stat` fails, `Path` is already replaced, `Info == nil`, `markSaved` is not called → the next F2 goes to `fsops.Create` → `os.Link` → `file exists`.
  - Error message: only `a.Status`, truncated to 20–32 characters (`drawDoc`/`tail`). `pollTask` shows the `<label> failed` dialog only with `task.reportError`; for `Saving file` / `Saving remote file` / `Opening file` it is not set. The path in the message is lost first (truncated from the left).
  - Retry: F2 again; Save as: Shift+F2 works, but is not offered to the user.
  - `Save all` on Cmd+Q: the first error interrupts the chain and cancels the quit, visible only in the status; which document was not saved is unknown (only the current screen is visible).
  - Shift+F10 / `Save` in the close dialog: on error the document stays (correct), but the reason is in the truncated status.
- **Required behavior:**
  1. Any save error → confirm `Save failed` with the text `<displayPath>\n<reason>` (two lines; `reason` is `err.Error()` without the temporary file prefix: replace `.farshmac-save-*`/`.farshmac-new-*` with `<folder>`; for `*os.PathError`/`*os.LinkError` print `Op` + a human-readable reason), buttons `Retry`, `Save as…`, `Cancel`. `Retry` repeats the same operation (same `path`, `overwrite`, `done`); `Save as…` opens `saveAs()` with the same `done` (after a successful Save as the close/quit continues); `Cancel`: the document stays open and Dirty, status `Not saved`.
  2. For `Save all` (Cmd+Q, window close): the same dialog, plus before it the failing document is made current (`selectScreen`) so that the user sees what was not saved. `Cancel` cancels the quit entirely.
  3. Open error (F3/F4/Alt+F11/Shift+F4/remote download/archive): confirm `Cannot open file` / `<path>\n<reason>` / `OK` instead of the truncated status. Expected "soft" refusals (`Editor limit is 64 MiB…`, `Binary data…`, `Encoding is not lossless…`, `Choose a regular file`) remain statuses; they are not failures.
  4. Remote save: the same dialog; `Retry` reconnects (a new `Dial`). On an error at the `Preparing upload` stage (encoding/ReadOnly): a status, as now, without a dialog.
  5. The `Stat` after `rename` edge: do not change `d.Path` until a successful `Stat`; on a `Stat` error consider the save successful (the data is on disk), `markSaved`, and take `Info` via a repeated `Lstat` in `Retry`; or, simpler: `os.Stat` → error → `d.Info = nil` and the status `Saved (cannot stat file)`; the next save via `fsops.Create` would fail with `file exists`, so choose the first option.
  6. Status after the dialog: `Not saved` (Cancel) / `Saved` (success after Retry/Save as).
- **Edge cases:** `Retry` while `Busy` (another task): do not show the dialog while `Busy`; Esc in the dialog = `Cancel`; remote document + `Save as…` saves locally and clears `Remote` (current behavior, keep); repeated `Retry` on ENOSPC: no limits; an error in `Shift+F10` → after `Cancel` the document is not closed; `Save all` with an error in a hidden document: switching the screen to it must reset `TermVisible`.
- **Tests:** `TestSaveFailureKeepsBufferAndOffersRetry` (directory 0555 → `Dialog.Title == "Save failed"`, `d.Dirty`, the text contains the path; chmod 0755 → `Retry` → `Saved`); `TestSaveFailureOffersSaveAs` (button 1 → prompt `Save as` → save to another path → `done` called); `TestSaveAllStopsOnFailureAndFocusesDocument` (two Dirty documents, the second one's directory is 0555 → `a.Doc` = the second, `Quit == false`); `TestAsyncSaveErrorShowsDialogNotTruncatedStatus`; `TestRemoteSaveFailureDialog` (SFTP loopback with a 0555 target directory); `TestSaveStatFailureDoesNotBreakNextSave`.
- **Priority:** P1 (owner's TODO; currently silence/an unreadable message, but without loss of edits).
- **Estimate:** M.

#### P3-03. "In-place" save strategy when the directory is not writable
- **Function and variant:** F2 / Save as for a file that is itself writable but lives in a directory without write permission (typically: `/etc/*`-like, shared directories, another user's directory with permissions explicitly granted on the file, directories with the sticky bit and a different owner).
- **Current behavior:** `fsops/files.go: Save` always does `os.CreateTemp(filepath.Dir(path), ".farshmac-save-*")` → `permission denied` → saving is impossible at all. The user can only Save as to another location.
- **Required behavior:**
  1. `fsops.Save` receives the strategy (from P3-01, or computes it itself on `EACCES` from `CreateTemp`): if the temporary file cannot be created but `access(path, W_OK)` passes, open the file `O_WRONLY|O_TRUNC` (**without** `O_CREAT`), write, `fsync`, close. Before that, the same `SameFile/size/mtime` check.
  2. An in-place write is not atomic: before the first such save of a document, confirm `Save in place` / `<folder> is not writable, so <basename> will be rewritten in place (not atomically). Continue?` / `Save`, `Cancel`. The answer is remembered per document (`d.InplaceConfirmed`).
  3. After success the status `Saved (in place)`. `Info` is updated via `Stat` (the inode is the same).
  4. If `O_TRUNC` opened but the write failed (ENOSPC), the file is truncated; report via P3-02 with the text `file may be truncated; retry or save elsewhere` and keep the buffer.
  5. hardlink/xattr/ACL, on the contrary, are preserved by an in-place write; note this in the help.
- **Edge cases:** `uchg` → `O_WRONLY` gives `EPERM` → P3-02; read-only volume → `EROFS` → P3-02; file larger than the data: `O_TRUNC` is mandatory; Save as into someone else's directory to a new file: the strategy is not applicable (`CREATE` is needed), error via P3-02.
- **Tests:** `TestSaveInPlaceWhenFolderNotWritable` (file 0644 in a 0555 directory → confirm → `Save` → contents updated, same inode, `Saved (in place)`); `TestSaveInPlaceCancelKeepsDirty`; `TestSaveInPlaceDetectsExternalChange` (mtime changed → "changed on disk" error, the file is not truncated); `fsops: TestSaveFallsBackToInplace`.
- **Priority:** P1 (follows directly from the TODO: "permissions needed by the actual save strategy (in-place write or temporary-file replacement)").
- **Estimate:** M.

#### P3-04. File changed/deleted on disk (or on the server) by the time of saving: a choice instead of a dead end
- **Function and variant:** F2 / Shift+F10 / `Save` on close / Cmd+Q when: the file was changed by another program; the file was deleted; the same file is open twice (F12) and the first screen has already saved; the remote file was changed on the server; the remote save was cancelled/interrupted after `Rename` (see P3-09).
- **Current behavior:**
  - `fsops.Save`: `!SameFile || size != || mtime !=` → `file changed on disk; reopen it before saving`; deleted → `lstat <path>: no such file or directory`. Remotely: `sameRemoteInfo` → `remote file changed; reopen before saving`.
  - Error → truncated status; **there is no way out**: "reopen" means closing (losing the edits, `Discard`) and opening again, or Shift+F2 to another path. There are no `Overwrite`/`Reload` buttons. There is no "re-read file" command in the viewer/editor either (neither auto-watching nor a key).
  - The same file twice: `showDocument` does not look for duplicates, the second screen's `Info` becomes stale → the same error after saving in the first.
- **Required behavior:**
  1. Locally: on `file changed on disk` → confirm `File changed on disk` / `<basename> was modified by another program (size/mtime).\nOverwrite it with your text?` / `Overwrite`, `Save as…`, `Cancel`. `Overwrite` → `fsops.Save(path, data, <current Lstat>)` (forced, preserving the current file's permissions); `Save as…` → `saveAs()`; `Cancel` → Dirty remains, status `Not saved`.
  2. File deleted: confirm `File was deleted` / `<basename> no longer exists on disk. Create it again?` / `Create`, `Save as…`, `Cancel`. `Create` → `fsops.Create(path, data, 0644)` (or the previous permissions `d.Info.Mode().Perm()`), then `markSaved`.
  3. Remotely: `remote file changed; …` → the same dialog with `Overwrite` (a repeated `saveRemoteDocument` with `force = true`: skip `sameRemoteInfo`, `ExpectedDestination = current`), `Save as…` (locally), `Cancel`.
  4. For a non-Dirty document (viewer or editor without edits), add a re-read command: `Shift+F3`? no; propose `Cmd+R` (Ctrl+R) `Reload file` in both modes: if Dirty → confirm `Reload` / `Discard changes and reload <basename>?` / `Reload`, `Cancel`; re-reads via `openDocument` into the same `Document` (keeping `ScreenID`, `Offset`, bookmarks where possible); for a remote one, a repeated download. Add to the F1 help and the menu.
  5. The same file twice: on `showDocument` for a local document with the same `Path` already open, do not block, but on a successful save update `Info`/`SavedRaw`/`SavedText` in **all** documents of the window with the same `Path` if they are not Dirty (Dirty duplicates will get the dialog of item 1 on their own save). Minimal variant: only the dialog of item 1.
- **Edge cases:** `Overwrite` when the file has become a directory/link (`!IsRegular`) → P3-02 error with the text `<path> is not a regular file`; `Overwrite` on a file whose permissions changed: take the current ones; `Reload` in a paged document: just close/open `File`; `Reload` of a remote Dirty document: a repeated download after confirmation; `Create` after the directory was deleted → P3-02.
- **Tests:** `TestSaveOverChangedFileOffersOverwrite` (touch/external write → dialog → `Overwrite` → the file is overwritten, `Dirty == false`); `TestSaveDeletedFileOffersCreate`; `TestRemoteSaveChangedOffersOverwrite` (SFTP loopback: change the file on the "server" between opening and saving); `TestReloadDiscardsChangesAfterConfirm`; `TestSameFileTwiceSecondSaveGetsDialog`.
- **Priority:** P1 (the only path now is Discard or Save as; edits are formally not lost, but the user will easily lose them by following the "reopen" hint).
- **Estimate:** M.

#### P3-05. Save as over a symbolic link or a directory: wrong message
- **Function and variant:** Shift+F2 / Cmd+Shift+S, the entered path is an existing symbolic link (to a file, or broken) or an existing directory.
- **Current behavior:** `document.go: saveAs` → `os.Lstat(path)` succeeds → confirm `Overwrite` / `<basename> already exists. Replace it?` → `saveDocumentThen(d, path, true)` → `saveDocument`: `os.Lstat(path)` → `fsops.Save(path, data, info)` → `!current.Mode().IsRegular()` → **`file changed on disk; reopen it before saving`**. The message is false; for a directory too. For a link, `rename` would replace the link itself rather than the target (if it got that far).
- **Required behavior:**
  1. In `saveAs` after `Lstat`: directory → status `<path> is a directory` without confirm; symbolic link → `EvalSymlinks`; if the target is a regular file: confirm `Overwrite` with the text `<basename> is a link to <target>. Replace the target file?` / `Overwrite`, `Cancel`, on confirmation save to the resolved path (`d.Path = target`); broken link → status `<path> is a broken symbolic link`.
  2. If the resolved path equals `d.Path`: a regular save (as "same path").
  3. Other non-regular files (FIFO, device) → status `<path> is not a regular file`.
- **Edge cases:** link to a directory → as a directory; relative link path; Save as of a remote document into a link: the same rules.
- **Tests:** `TestSaveAsOverDirectoryReportsDirectory`; `TestSaveAsOverSymlinkWritesTarget` (after saving the link is intact, the target is updated, `d.Path == target`); `TestSaveAsOverBrokenSymlinkRefuses`.
- **Priority:** P2.
- **Estimate:** S.

#### P3-06. The viewer does not open a UTF-16 file of odd length
- **Function and variant:** F3 (and any path into `openDocument` with `edit=false`) for a file with an `FF FE`/`FE FF` prefix and an odd number of bytes (truncated UTF-16).
- **Current behavior:** `openDocument`: `Codepage = Detect(raw)` → `UTF-16LE` → `Decode` → `incomplete UTF-16 code unit` → `a.error(err); return`: the document does not open **even for viewing** (hex is unavailable). For UTF-8/single-byte encodings `Decode` does not fail, so the error is only for UTF-16.
- **Required behavior:** in the viewer, on a `Decode` error: a `Codepage = "UTF-8"` attempt is not needed; open as binary: `Buffer = New("")`? no; `Buffer = New(string(raw))` is unacceptable (invalid UTF-8 in the text). The right way: decode as `ISO-8859-1` (all 256 bytes are representable), `Mode = "hex"`, status `incomplete UTF-16 code unit; shown as hex`. Shift+F8 will allow re-selecting. In the editor: refuse as now, but with the text `incomplete UTF-16 code unit; open with F3`.
- **Edge cases:** a file of 1 byte `FF`? Does not match the prefix; not affected. A file of only `FF FE` (2 bytes): empty text, fine.
- **Tests:** `TestViewerOpensTruncatedUTF16AsHex` (3 bytes `FF FE 41` → `a.Doc != nil`, `Mode == "hex"`); `TestEditorRefusesTruncatedUTF16`.
- **Priority:** P2.
- **Estimate:** S.

#### P3-07. `+`/`-` in the viewer with a remote active panel + TempDir leak
- **Function and variant:** `+`/`-` when the active panel is remote (the document was opened with F3 from FTP/SFTP, or the panel was switched via F12 → Panels); replacing a remote document with the adjacent one.
- **Current behavior:** `document_keys.go: adjacentFile` does not check `p.Remote`: Async → `openDocumentAsync(filepath.Join(p.Path, name))`: **the remote path is treated as local** → `open /srv/www/x.txt: no such file or directory`, or, if such a local path happens to exist, the **local** file silently opens. In sync mode `a.open(false)` → the same. On a successful replacement of the old document `old.File` is closed, but `old.TempDir` (the remote document's cache) **is not removed**: a leak of temporary directories until exit (`App.Close` cleans only the documents remaining in the lists).
- **Required behavior:** if `p.Remote != nil`, move the panel cursor and call `openRemoteDocument(false)` replacing the current document (in apply: remove `old.TempDir`, close `old.File`); on a download error restore the cursor. In any case, remove `old.TempDir` when replacing a document. Status after opening: as with F3 (`<label><remotePath>`).
- **Edge cases:** archive panel (the document is local, the panel is an archive after F12) → status `Return to the archive panel to choose another file` (currently only `a.Doc.ArchiveSource` is checked); Dirty remote document → confirm `Save changes` → `Save` → `saveRemoteDocument` → then the replacement.
- **Tests:** `TestAdjacentFileOnRemotePanelDownloads` (SFTP loopback, two files, `+` opens the second, `Remote != nil`); `TestAdjacentFileRemovesReplacedTempDir`; `TestAdjacentFileOnArchivePanelRefuses`.
- **Priority:** P2.
- **Estimate:** S–M.

#### P3-08. A remote symbolic link to a file does not open
- **Function and variant:** F3/F4/Enter on an SFTP/FTP item that is a symbolic link to a regular file.
- **Current behavior:** `remote_document.go: openRemoteDocument`: `f.Lstat(remotePath)` → `ModeSymlink` → `!IsRegular()` → `viewer requires a regular remote file`. The user sees a link to a file (for example `current -> release-1.2.txt`) and cannot open it; the message does not explain the reason.
- **Required behavior:** after `Lstat`, on `ModeSymlink` do `f.Stat(remotePath)` (if the `FS` provides Stat; SFTP does); if the target is a regular file, open by the original path, `RemoteInfo` = the `Stat` result (for `sameRemoteInfo` on save also use `Stat`). If the target is a directory/absent: status `<name> is a link to a folder` / `broken link`. If the backend cannot `Stat` (FTP), keep the refusal, but with the text `<name> is a symbolic link; copy it with F5 first`.
- **Edge cases:** saving through a link: `Rename(tmp, remotePath)` will replace the **link**, not the target (just like `rename` locally). For remote saving through a link, either save to the resolved path (`ReadLink`), or open such files read-only with the `Edit anyway` warning (P3-01 item 5).
- **Tests:** `TestRemoteSymlinkToFileOpensInViewer` (SFTP loopback with a created link); `TestRemoteSymlinkToFolderReports`.
- **Priority:** P2.
- **Estimate:** S.

#### P3-09. A remote save interrupted/cancelled after `Rename` leaves the document in a dead end
- **Function and variant:** Esc (cancel) or loss of connection in `saveRemoteDocument` between `remotefs.Copy` (including the server-side `Rename(tmp, dest)`) and the final `f.Lstat` / apply.
- **Current behavior:** `remotefs.Copy` has completed, the file on the server is already replaced; then `o.Check()` in `pollTask` (`applyCompleted` for a remote save is **not** set) or an `Lstat` error → apply is not executed, `copy` is discarded: `d.Dirty == true`, `RemoteInfo` is old. The next F2 → `sameRemoteInfo` is false → `remote file changed; reopen before saving` forever; the only way out is Save as locally or `Discard`. For a local save this edge is closed by the `applyCompleted` flag (`document_tasks.go: saveDocumentThen`).
- **Required behavior:** after a successful `remotefs.Copy` set an equivalent of `a.task.applyCompleted = true` (a flag in the closure: `uploaded = true`); if `Lstat` after the upload failed/was cancelled, still apply `*d = *copy` with `RemoteInfo = nil` and the status `Remote file saved; attributes unknown`; with `RemoteInfo == nil` the next `sameRemoteInfo` check is skipped once (or a repeated `Lstat` is performed before saving with the P3-04 item 3 dialog). Cancellation **before** `Rename`: as now (the temporary `.farshmac-part-*` is removed by the backend).
- **Edge cases:** cancellation during `Preparing upload`: nothing was sent, error `operation cancelled`; a disconnect during `Rename` (unknown whether it applied) → `RemoteInfo = nil` and the P3-04 dialog on the next save.
- **Tests:** `TestRemoteSaveCancelledAfterUploadKeepsDocumentSaveable` (cancellation via a hook after `Copy`; then `a.save()` passes); `TestRemoteSaveLstatFailureStillMarksSaved`.
- **Priority:** P2.
- **Estimate:** S.

#### P3-10. Saving loses xattr (Finder tags/comments), ACL
- **Function and variant:** F2 of a local file with Finder tags, a Spotlight comment, other xattrs, ACL.
- **Current behavior:** `fsops/files.go: Save`: a new inode + `Chmod(perm)` + `rename`; xattr/ACL/`chflags` are not copied (`fsops` can copy metadata for F5 — `clonefile`/xattr/Finder flags — but `Save` does not use it). The panel help honestly says "does not preserve ACLs/xattrs/hardlinks", but the editor user loses tags silently. A side benefit: `com.apple.quarantine` also disappears.
- **Required behavior:** before `rename`, copy to the temporary file the xattrs (except `com.apple.quarantine`? — at the owner's discretion; there is no Far equivalent; I propose copying everything, including quarantine, so that saving does not change the security status), ACL (`acl_get_file`/`acl_set_file` via the existing `fsops` helpers), `chflags` without `UF_IMMUTABLE`. A metadata copy error is not fatal: status `Saved; some attributes were not preserved`. Update the help text.
- **Edge cases:** a volume without xattr support (ENOTSUP) → ignore; `uchg` still blocks `rename` → P3-02; an in-place write (P3-03) preserves the metadata by itself.
- **Tests:** `fsops: TestSavePreservesXattrsAndFlags` (set a `user.test` xattr and `UF_HIDDEN`, save, verify); `TestSaveReportsPartialMetadata`.
- **Priority:** P2.
- **Estimate:** S–M.

#### P3-11. `Hex bytes` in the editor searches stale bytes and breaks the mode
- **Function and variant:** F7/Cmd+F with `Hex bytes` enabled in the **editor** (the toggle is always available).
- **Current behavior:** `document.go: searchDocument` does not check `d.Edit` for the hex branch; `findDocument` searches in `d.Raw`, the bytes of the last load/save rather than the current text (after edits the result is wrong), and sets `d.Mode = "hex"` when `Edit == true`. Rendering stays textual (`!d.Edit && d.Mode != "text"` is false), but `pollSyntax` sees `Mode != "text"` → highlighting silently goes out for the rest of the document's life; after F6 the document ends up in hex. Back to `text` in the editor: Mode is never reset (F2/F4 only in `viewerKey`).
- **Required behavior:** in the editor, `Hex bytes` → status `Hex search is available in the viewer (F6)` and do not execute; or (more complete) before a hex search encode the current text (`Encode(Endings(Text,EOL),Codepage,BOM)`) into a temporary `Raw`, search, translate the byte offset into a text offset (hard with CRLF/UTF-16): not justified. Also `findDocument` must set `Mode = "hex"` only when `!d.Edit`.
- **Edge cases:** Shift+F7/Space after a hex search in the viewer and then F6 into the editor → `HexQuery` is non-empty → Shift+F7 in the editor → the same problem; on `Edit` reset `HexQuery` and hint with a status.
- **Tests:** `TestHexSearchRefusedInEditorKeepsTextMode` (`Mode == "text"` after the search, status); `TestNextMatchAfterHexInEditorDoesNotSwitchMode`.
- **Priority:** P3.
- **Estimate:** S.

#### P3-12. Silence on editing in Read-only: step-by-step replace and paste from the clipboard
- **Function and variant:** Cmd+F7/Cmd+H without `Replace all` and Cmd+V/Shift+Insert/menu Paste when `ReadOnly` (Cmd+L, F9 `Read only`, archive document; F6 from an archive is blocked, but Cmd+L is available to everyone).
- **Current behavior:** `confirmReplacement` → `d.Replace(...)` → `Buffer.Replace` on `ReadOnly` silently returns; the `Replace this match?` dialog keeps iterating over matches, changing nothing and reporting nothing. `Replace all` in the same state gives `editor is read-only` (via `ReplaceAll`). `app.go: Handle("paste")` → `a.Doc.Insert(m.Text)` → silently nothing; ordinary key input gives `Read-only mode (⌘L to unlock)`.
- **Required behavior:** in `searchDocument(replace=true)` with `d.ReadOnly`: status `Read-only mode (⌘L to unlock)` and do not open the form; in `Handle("paste")` for `Doc.Edit && Doc.ReadOnly`: the same status.
- **Edge cases:** paste into the viewer: ignore as now; archive document + Cmd+L removes RO → edits are possible, but F6/saving… saving via `saveDocument` for `ArchiveSource` will go to the virtual `Path` (`fsops.Save` → `Lstat` → `no such file`): covered by P3-02; better, in `save()` for `ArchiveSource` immediately the status `Copy the file out with F5 before editing`.
- **Tests:** `TestReplaceInReadOnlyReportsStatus`; `TestPasteInReadOnlyReportsStatus`; `TestArchiveDocumentSaveRefused`.
- **Priority:** P3.
- **Estimate:** S.

#### P3-13. Paged viewing silently shows emptiness on a read error
- **Function and variant:** viewing a file > 64 MiB when the volume is unmounted / the file is truncated / the network disk dropped.
- **Current behavior:** `document.go: dataAt`: `read, _ := d.File.ReadAt(b, at)`: the error is ignored, empty lines are drawn; End/PgDn compute by the old `Size`. There is no search for such files, so there are no other failure points.
- **Required behavior:** on `err != nil && err != io.EOF`: status `read error: <reason>` (once per document, flag `d.ReadError`), the title adds ` !`; on a `Stat` error at the next redraw, offer to close (`Cannot read file` / `OK` → `finishCloseDocument`).
- **Edge cases:** the file became shorter: `ReadAt` gives `io.EOF` with partial data: recompute `Size` via `File.Stat()` on `EOF`.
- **Tests:** `TestPagedViewerReportsReadError` (substitute `File` with a truncated one after opening / close the descriptor in the test).
- **Priority:** P3.
- **Estimate:** S.

#### P3-14. Bookmarks Cmd+Shift+0..9 are not saved (confirmed)
- **Function and variant:** `Cmd+Shift+<digit>` (and `Ctrl+Shift+<digit>`) in a document, any keyboard layout.
- **Current behavior:** `document.go: documentKey`: `if command { if n, err := strconv.Atoi(key); … if m.Shift { Bookmarks[n] = … } }` parses `m.Key`. The renderer sends `Key: e.key` without normalization (`app.js:30: keyMessage(e){return {Type:'key',Key:e.key,Code:e.code||'',…}}`), and WebKit with Shift held returns the shifted character (`!@#$%^&*()` on US, `!"№;%:?*()` on Russian). That this is the real behavior is confirmed by the application's own code: `app.go:648` for Cmd+Shift+[ ] checks `m.Key == "{" || m.Key == "}"`, and `folder_shortcuts.go:15` for Ctrl+Shift+1..9 deliberately takes `m.Code == "DigitN"`. The test `TestWrapGotoBookmarksAndRendering` sends `Key:"3", Shift:true` and does not cover the path. Additionally: Cmd+Shift+3/4/5 is intercepted by macOS (screenshots), so these three bookmarks are unreachable via Cmd in principle; Ctrl+Shift+3/4/5 are reachable.
- **Required behavior:** in `documentKey` compute the digit as in `folderShortcutKey`: `if len(m.Code)==6 && HasPrefix(m.Code,"Digit") { key = m.Code[5:] }`; `Numpad0..9` is also acceptable (`HasPrefix(m.Code,"Numpad")` + `len(m.Key)==1`). Save the bookmark on `m.Shift`, restore without. In the F1 help and parity note: "Cmd+Shift+3/4/5 are taken by macOS screenshots; use Ctrl+Shift+3/4/5".
- **Edge cases:** `Key` like `"+1"`/`"-0"`: `Atoi` accepts them (`+`/`-` with Cmd do not arrive as such strings; after switching to `Code` it does not matter); Alt+Cmd+digit: do not touch (`m.Alt` → skip).
- **Tests:** `TestBookmarkSavedWithShiftedKeySymbol` (`Message{Key:"#", Code:"Digit3", Meta:true, Shift:true}` → `Bookmarks[3]` exists, status `Bookmark 3 saved`; then `Key:"3", Code:"Digit3", Meta:true` restores); renderer test `internal/native/tests/input.cjs`: the event `{key:'#', code:'Digit3', metaKey:true, shiftKey:true}` → the message contains `Code:'Digit3'`.
- **Priority:** P2 (the function is announced in the help and does not work).
- **Estimate:** S.

#### P3-15. F8 in the viewer is silent for Dirty and paged documents (discrepancy 10)
- **Function and variant:** F8 (CP866/Windows-1251) in the viewer when `Dirty` (after F6 from the editor with edits) or `File != nil` (> 64 MiB).
- **Current behavior:** `documentKey` (`key == "F8"`): the branch `else if d.File == nil && !d.Dirty { … }`; otherwise nothing, no status. Shift+F8 in the same cases gives `Save edited text before reinterpreting bytes` or changes only `Codepage` for a paged document.
- **Required behavior:** Dirty → status `Save edited text before reinterpreting bytes`; paged → change only `Codepage` (like Shift+F8, affects the dump) with the status `Code page: CP866` / `Code page: Windows-1251`.
- **Tests:** `TestViewerF8ReportsDirtyAndPaged`.
- **Priority:** P3.
- **Estimate:** S.

## Part 4. Terminal, command line, windows, native shell, renderer

#### P4-01. Lost integration key: `ShellReady` hangs, stale `panel-command`
- **Function and variant:** Running a command / `ChangeDir` / `RequestCommands`; zsh with plugins that recreate the keymap after `.zshrc` is loaded (zsh-vi-mode, a user `bindkey -N … && bindkey -A … main` in precmd), or a shell that accepted the key but did not run the widget/`bind -x` (for example readline in `vi-command` with a non-standard `set keymap`).
- **Current behavior:** `Session.request` creates `panel-command` with `O_EXCL`, writes the key into the PTY and returns `nil`; `App.execute` sets `ShellReady=false`, `TermVisible=true` (`app.go:execute`). If the shell did not call `_farshmac_panel_command`, the Ready marker never arrives: the footer stays `⌘O  Panels    ·    ⌘.  Interrupt` forever, the panel Enter gives `Shell is busy; ⌘O to return to it`, ⌘⇧H shows `Waiting for shell prompt…`. The `panel-command` file remains, and any subsequent `Run`/`ChangeDir`/`RequestCommands` returns the raw Go error `open /var/folders/…/panel-command: file exists` (`session.go:request`). The user pressing Enter in the terminal does not help (precmd fires, Ready arrives, but the file is still there → the next Run gives EEXIST again).
- **Required behavior:**
  1. In `Session.request` on `EEXIST`, remove the stale file and retry the creation once (the file is consumed only by the shell, and only if the shell is "at a prompt"; the presence of the file at the moment of a new request always means a loss).
  2. A watchdog in `App.Poll`: if after `Run`/`ChangeDir`/`RequestCommands` neither `Running` nor `Ready` arrives within 5 s (for `Run`: not even `Running`), consider the key lost: remove `panel-command`, `ShellReady=true` (the shell is actually at a prompt), status `Shell integration did not respond; command not sent — check shell key bindings`, the command is returned to `a.Command` (for `execute`), the command history overlay receives `commands: []` with the error caption. The timer is not started after `Running` (the program may run for as long as it likes).
  3. In the zsh rc, additionally re-bind the keys in a precmd hook (`bindkey` is idempotent): protection against plugins that recreate the keymap at the first prompt.
- **Edge cases:** the timer must be reset on `Exit` (restart); the watchdog also works when `TermVisible=false`; do not break `TestPanelDirectorySyncWaitsForShell` (there the request is deliberately deferred and the key is not sent; the timer starts only after the key is actually sent).
- **Tests:** Go `TestLostIntegrationKeyRecovers` (terminal): after `Run`, manually remove the binding (`bindkey -r` via `Write`) and make sure that the next `Run` after the file is removed does not give EEXIST; `TestStaleRequestFileIsReplaced` (`request` with an existing file); ui `TestShellIntegrationWatchdogRestoresReady` (substitute `Term` with a session that does not respond, check the status and the return of the command after 5 s with an accelerated timer).
- **Priority:** P1
- **Estimate:** M

#### P4-02. Infinite shell restart loop without backoff
- **Function and variant:** Restarting the shell after it exits; `.zshrc`/`.bash_profile` with `exit`, `exec` of a non-existent program, a broken `ENV`; a shell that crashes immediately after start.
- **Current behavior:** `scheduleShellRestart` always sets `shellRestartAt = now+1s`; `restartShellIfDue` starts a new shell; it exits again → a 1 Hz loop without limit (`shell_restart.go`). Every second `farshmac-shell-*` is created/removed, `[Restarting shell]` is appended to the terminal, the status flickers `Shell exited; restarting…` ↔ `Shell restarting…`. The reason is not explained to the user, and the loop cannot be turned off (except by closing the window).
- **Required behavior:**
  1. Consider a "fast crash" an Exit that arrives less than 5 s after `startTerminalAt` without a single `Ready` (`shellStarted=false`). Delay: 1 s, 2 s, 4 s, 8 s (exponential); after 5 fast crashes in a row the restarting stops: `shellRestartAt` = zero, status `Shell keeps exiting during startup (5 attempts). Check your shell startup files; press Enter in the terminal or ⌘O to retry`, the terminal footer `⌘O  Panels    ·    Shell stopped: <reason>    ·    Enter: retry`.
  2. In the "stopped" state, `input` with `\r`/`\n`, `execute` (panel Enter) and `showTerminal` trigger one restart attempt (resetting the counter). `execute` with `Term == nil` shows `Shell unavailable; retrying…` and initiates a restart instead of a silent refusal.
  3. A successful `Ready` resets the counter.
  4. On stopping, print one line to the terminal: `[Shell exited <N> times during startup; press Enter to retry]`.
- **Edge cases:** an `exit` typed by the user in a running shell (there was a `Ready`): still 1 s; `a.Quit`/`closed`: no restart; changing `Settings.Shell` in Terminal settings resets the counter (new configuration).
- **Tests:** Go ui `TestShellRestartBackoffStopsAfterRepeatedStartupFailures` (ZDOTDIR with `.zshrc` = `exit 1`; check the delays via `shellRestartAt`, the stop after 5 attempts, the status, the restart on Enter); extend `TestShellRestartDeletedDirectoryAndShutdown`.
- **Priority:** P1
- **Estimate:** M

#### P4-03. The first shell start error is not retried; a restart with an inaccessible cwd loops
- **Function and variant:** Starting the terminal at launch/new window/session restore; a restart when cwd exists but has no `x` permission (chdir is forbidden), the temp directory is unavailable (`os.MkdirTemp` error), PTYs are exhausted.
- **Current behavior:** (a) `native.Run`, `WindowGroup.NewWindow`, `EnableSession` on a `StartTerminal` error write `Shell: <err>` and leave `Term=nil` forever (`shellRestartAt` is not set); afterwards `execute` → `Shell unavailable` with no way to recover (except ⌘N). (b) `restartShellIfDue` checks cwd only via `os.Stat(...).IsDir()`; a directory without `x` passes the check, `exec.Command` with `cmd.Dir` fails (`chdir: permission denied`) → `Cannot restart shell: …; retrying…` every 3 s with the same cwd, endlessly (`shell_restart.go:restartShellIfDue`).
- **Required behavior:**
  1. On a `StartTerminal` failure at launch/in a new window, schedule a restart through the common mechanism (`shellRestartAt = now+3s`, `shellRestartCwd` = the panel path), status `Shell: <err>; retrying…`; apply the backoff from P4-02.
  2. In `restartShellIfDue`, on a start error with a specific cwd, the second attempt goes from `$HOME` (then `/`), status `Cannot start shell in <cwd>: <err>; using <home>`; if that also fails, the P4-02 backoff and a stop with diagnostics.
  3. The cwd check: `os.Open(cwd)` + `Stat`/`ReadDir(1)` is not enough for `x`; use `syscall.Access(cwd, X_OK)` or simply catch the start error.
- **Edge cases:** `-left`/`-right` to an inaccessible directory (the panel still starts via `accessibleFolder` only in a session; in `newApp` `Load` returns an error and the application terminates: outside this part); cwd on a volume unmounted during the restart: already handled by climbing to the parent.
- **Tests:** Go ui `TestShellRestartFallsBackToHomeWhenCwdNotExecutable` (directory `0600`), `TestInitialShellStartFailureIsRetried` (substitute `Settings.Shell` with an invalid value by setting it directly → `unsupported shell`, then fix it and wait for `Ready`).
- **Priority:** P1
- **Estimate:** S

#### P4-04. Returning from the terminal throws the panel out of the archive
- **Function and variant:** Shell → panel synchronization; the active panel is inside an archive (`Archive != nil`, `Remote == nil`).
- **Current behavior:** `showTerminal` sets `pendingShellCwd = filesystemPath()` = the directory containing the archive (`archive_panel.go:filesystemPath`). On ⌘O back, `syncCwd` (or `finishPanelReturn`) calls `Panels[Active].Load(ShellCwd)`; `Panel.Load` sees that `abs` is not inside the archive (`Archive.Relative` → `inside=false`), loads a regular directory and resets `p.Archive = nil` (`app.go:Panel.Load`). Result: just by peeking into the terminal and coming back, the user loses the position inside the archive, even without a `cd` in the shell. (For a remote panel the `Remote == nil` guard exists; for an archive there is none.)
- **Required behavior:** a panel in an archive is synchronized like a remote one: `returnFromTerminal`/`syncCwd`/`finishPanelReturn` skip a panel with `Archive != nil` if `ShellCwd` (after `EvalSymlinks`) equals the archive's `filesystemPath()` (the shell did not leave): the panel is not touched; if the shell changed the directory to another one, the panel leaves the archive and loads `ShellCwd` (this is an explicit user action). `showTerminal` for an archive panel still sends the archive's directory.
- **Edge cases:** archive inside an archive (nested): `filesystemPath()` returns the outer archive's directory; `ShellCwd` empty (the shell is still starting): do not touch; compare symlinks via `EvalSymlinks`.
- **Tests:** Go ui `TestTerminalRoundTripKeepsArchivePanel` (enter a `.zip`, ⌘O, wait for Ready, ⌘O → `Archive != nil`, the path inside the archive is preserved); `TestShellCdLeavesArchivePanel` (after `cd /` in the shell the panel loads `/`).
- **Priority:** P2
- **Estimate:** S

#### P4-05. Autocompletion in a remote/archive panel reads the virtual path locally
- **Function and variant:** ⌘Space autocompletion; the active panel is SFTP/FTP or an archive.
- **Current behavior:** `completeCommand` takes `base := a.Panels[a.Active].Path` (the virtual path) and reads `fsops.ReadDirectory(expand(dir, base))` locally (`completion.go`). For a remote panel this leads to an error like `open /home/user/dir: no such file or directory` in the status (or an accidental match with a local directory of the same name); for an archive: `not a directory`/an error on a path like `/x/y.zip/dir`. Meanwhile the command is executed in `filesystemPath()` (the local directory), so completion by `Path` is semantically wrong even when it succeeds.
- **Required behavior:** `base := filesystemPath()` (as in `execute`), i.e. complete relative to the directory where the command will be executed; in the status line for a remote panel the `local:` prefix already shows this to the user. For an archive: the same (the archive's directory). Show a read error as `Completion: cannot read <dir>: <err>`.
- **Edge cases:** a token with `~` and absolute paths do not depend on base; `RemoteLocalPath` empty: fallback `$HOME`.
- **Tests:** Go ui `TestCompletionUsesFilesystemPathForRemoteAndArchivePanels` (a panel with `Remote != nil`, `RemoteLocalPath = tempdir`, file `abc` → ⌘Space on `a` inserts `'abc'`).
- **Priority:** P2
- **Estimate:** S

#### P4-06. ⌘Q is silently aborted by a busy window in the background
- **Function and variant:** Quit with ⌘Q with several windows; one of the windows (not the active one) is `Busy` (a copy is in progress).
- **Current behavior:** `advanceQuit` calls `a.RequestQuit()` of the busy window → `Wait for the file operation to finish` is written to THAT window's status, `a.Quit=false`, `Form == nil && Dialog == nil` → `quitting=false`, `waiting=0` (`windows.go:advanceQuit`). `Update.attentionWindow` stays 0: the window is not brought forward. In the window where ⌘Q was pressed nothing happens: no dialog, no status. The user thinks ⌘Q does not work.
- **Required behavior:** when the quit is interrupted because of a `Busy` window: (1) `attentionWindow` = the id of the busy window (bring it forward, as with a dialog); (2) in all other windows the status `Quit cancelled: window <N> is running a file operation`; (3) in the busy window the current text. Likewise for a window where `RequestQuit` is rejected for another reason.
- **Edge cases:** the busy window is minimized (⌘M): `makeKeyAndOrderFront` restores it; quit from the Dock (`applicationShouldTerminate`): the same behavior.
- **Tests:** Go ui `TestQuitAbortedByBusyWindowRaisesAttention` (two Apps in a group, the second `Busy=true`; after `BeginQuit` check `Poll(1).AttentionWindow == 2` and the status of window 1).
- **Priority:** P2
- **Estimate:** S

#### P4-07. Inability to write session.json overwrites the status line every 500 ms
- **Function and variant:** Session saving; `~/Library/Application Support/FarshMac` is read-only, the disk is full, the directory is a file.
- **Current behavior:** `WindowGroup.Poll` → `SaveSession(false)` → error → `a.error(e)` (`windows.go:Poll`). `lastSessionBytes` is not updated, so every 500 ms the attempt is repeated and `a.Status` is replaced with the error text, `dirty=true` → a full panel redraw twice a second. Any other message (`Exit 0 · ⌘O …`, `Setup saved`, operation errors) lives no longer than 0.5 s.
- **Required behavior:** show the session write error once per unique text (remember `lastSessionError`), in the format `Cannot save window session: <err>`; subsequent attempts no more often than every 30 s; on a successful write clear the remembered error. Do not touch `dirty` if the status has not changed.
- **Edge cases:** a forced save on window close/quit: the error is always reported (as now); `sessionPath == ""`: no changes.
- **Tests:** Go ui `TestSessionSaveFailureReportedOnce` (sessionPath in a directory without permissions; a series of `Poll` calls with substituted time → the status is set once, then overwritten by another status and does not come back).
- **Priority:** P2
- **Estimate:** S

#### P4-08. A new window (⌘N) starts with an empty command history; AutoSave on window close overwrites setup.json
- **Function and variant:** Panel command history, Alt+F8, Alt+F12 (folder history) in a window created with ⌘N; closing windows with `AutoSave`.
- **Current behavior:** `WindowGroup.NewWindow` with a parent copies `Settings` (JSON) and `panelOptions`, but not `a.history`, `a.folderHistory`, `a.viewHistory`, `dialogValues`; `LoadSetup()` is called only without a parent (`windows.go:NewWindow`). In the new window Ctrl+E/⌘↑ do nothing, Alt+F8 → `Command history is empty`. On window close, `App.Close` with `AutoSave` calls this window's `saveSetup()` (`app.go:Close`) → `setup.json` receives its (empty) history; closing the ⌘N window later than the main window loses the user's entire command history. Also, two windows with AutoSave write the file in turn: the last closed one wins.
- **Required behavior:** (1) on `NewWindow` with a parent, copy `history`, `historyAt=len`, `folderHistory`, `viewHistory`, `dialogValues`; (2) in `saveSetup`, merge the command/folder histories: read the current `setup.json`, `appendUnique` over all entries of both lists preserving the order (new ones at the end), limit `HistoryLimit`; (3) when a command is executed in one window, optionally broadcast the entry to the other windows via `WindowGroup` (shared slice); minimum: items (1)+(2).
- **Edge cases:** `setup.json` missing/broken: write own history; `HistoryLimit` differs between windows: take the value of the saving window.
- **Tests:** Go ui `TestNewWindowInheritsCommandHistory`, `TestSaveSetupMergesCommandHistoryAcrossWindows` (two windows, different commands, close both, `LoadSetup` → both commands).
- **Priority:** P2
- **Estimate:** M

#### P4-09. Shortcuts are compared by `e.key`: non-Latin layouts (needs verification on a device)
- **Function and variant:** ⌘O/Ctrl+O, ⌘., ⌘C/⌘V/⌘A in the terminal; ⌘O, ⌘U, ⌘L, ⌘R, ⌘E/X/Y, Ctrl+F, Ctrl+; in the panels, with a Russian/other non-Latin layout active.
- **Current behavior:** renderer: `toggle(e)` → `e.key.toLowerCase()==='o'`, the rest use `e.key` (`app.js:attachCustomKeyEventHandler`, `keydown` capture); only ⌘⇧H and ⌘⇧. have an `e.code`/keyCode fallback (`app.js`, `window_darwin.m:performKeyEquivalent`). Go `key()` compares `m.Key` (`strings.EqualFold(m.Key,"o")`, `far_keys.go` by `k`), `m.Code` is used only for Alt accelerators of forms/dialogs and Numpad. If WebKit with Cmd+key in a Cyrillic layout returns `e.key = 'щ'` (rather than `o`), ⌘O in the terminal is not intercepted (`return !e.metaKey` → goes to the native layer, where ⌘O is not bound → nothing), and in the panels Go receives `Key:"щ"` and does not recognize it. Native menu items (⌘N/W/Q/C/V/A/M) always work (AppKit matches key equivalents by the Latin layout). The tests (`input.cjs`, `panel_keys.m`) cover only Latin.
- **Required behavior:** for all ⌘/Ctrl combinations with letters compare `e.code` (`KeyO`, `KeyC`, …) with a fallback to `e.key`; in Go, with `m.Meta||m.Ctrl` and `len(Code)==4 && HasPrefix(Code,"Key")` substitute the Latin letter from `Code` (as is already done for Alt in forms). Document in README.
- **Edge cases:** Dvorak/Colemak layouts: `e.code` is physical, `e.key` is logical; for them `e.key` (Latin) is needed, hence the order: if `e.key` is a Latin letter, use it; otherwise `e.code`. Caps Lock: `toLowerCase`.
- **Tests:** `input.cjs`: the event `{metaKey:true,key:'щ',code:'KeyO'}` → `key` with `Key:'o'`; Go `TestMetaShortcutsMapCodeForNonLatinKey`.
- **Priority:** P2
- **Estimate:** S

#### P4-10. Shell command history overlay: swallows ⌘O, hangs in `Loading…` after a shell restart or a request error
- **Function and variant:** The ⌘⇧H overlay; (a) pressing ⌘O while the overlay is open; (b) the shell exited/restarted while the overlay is waiting for a snapshot; (c) `RequestCommands` returned an error (EEXIST from P4-01).
- **Current behavior:** (a) `commandHistory.key(e)` when `visible` returns `true` for any key → the document listener exits, focus is in `<select>`/`<textarea>`, xterm does not see the event → ⌘O does not work until Esc is pressed (`commands.js:key`, `app.js keydown`). (b) `scheduleShellRestart` zeroes `shellCommands`; `requested=true` in the renderer remains, a repeated request is not sent → the caption `Loading command history…` forever (`commands.js:request/update`). (c) `a.error(err)` to the status, `ShellReady` does not change, `commands` does not arrive → the same stuck caption (`app.go:Handle` case `commandHistory`).
- **Required behavior:** (a) in `CommandHistory.key` let `toggle(e)` (⌘O/Ctrl+O) through: close the overlay and pass `key` to Go; (b) on the terminal `Update.mode` and a `shellReady` transition false→true after `requested` without received `commands` (or on `state.history`/a new `Term`), repeat the request; simpler: Go always answers `commandHistory`: on an error/unavailable shell it sends `Commands: &[]string{}` with the field `Update.commandsError` (`Cannot read shell history: <err>`), the renderer shows the text in the caption and keeps Esc; on `Exit` Go sends the same empty reply with the text `Shell exited; history unavailable`.
- **Edge cases:** the overlay is open while switching modes: already closes; `commands` arrived after closing: ignored (`if(!this.visible)return` is kept).
- **Tests:** `commands.cjs`: ⌘O with the overlay open → `sent.at(-1).Type==='key'`, the overlay is hidden; `farshmacUpdate({commands:[],commandsError:'…'})` → the caption; Go `TestCommandHistoryRequestFailureReportsToOverlay`.
- **Priority:** P3
- **Estimate:** S

#### P4-11. ⌘W/⌘Q with an open form replace it with a confirmation dialog; unsaved form edits are lost
- **Function and variant:** Closing the window / quitting with an open form (F9 → Options → … settings, Find file, Transfer dialog) and `Term != nil && !ShellReady` or unsaved documents.
- **Current behavior:** `RequestQuit` → `a.confirm(...)` → `a.Form = &Form{…}` without checking the existing `a.Form` (`form.go:confirm`). The open settings form with the entered values is destroyed; its `Cancel` is not called. With `a.Dialog != nil` (the F7 prompt) the dialog stays behind the form and is shown after the answer: acceptable. If no command is running and there are no documents, `Quit=true` immediately with the form open (the form simply closes with the window: fine).
- **Required behavior:** if `a.Form != nil` (not the quit confirm), first close the form via its `Cancel` (like Esc), then show the confirmation; or reject ⌘W/⌘Q with the status `Close the open dialog first` (the Far approach). The first is preferable: predictable for ⌘Q from the Dock.
- **Edge cases:** the `choose` form (a list) is also a `Form`; a nested `confirm` from the form itself (for example `Error … Retry/Cancel`) runs before RequestQuit, since `advanceQuit` waits for `Form == nil`.
- **Tests:** Go ui `TestQuitWithOpenFormCancelsFormFirst` (open `Terminal settings`, `Term` busy, `RequestQuit` → `Form.Title == "Quit"`, and the settings form's `Cancel` was called).
- **Priority:** P3
- **Estimate:** S

#### P4-12. The minimum window size is not consistent with the 60×15 lower bound at a large font
- **Function and variant:** Resizing the terminal/panels; `FontSize` 20–24 with the window shrunk to the minimum 700×400 pt.
- **Current behavior:** `contentMinSize = 700×400` (`window_darwin.m:open`); at fontSize 24 a cell is ≈ 14.4×27.6 px → `floor(692/14.4)=48` columns, `floor(392/27.6)=14` rows → `dimensions()` clamps to 60×15 (`app.js`), Go too (`app.go:Handle resize`). xterm renders 60 columns into an area that fits 48: the right part of the panels/terminal is cut off by `overflow`, the key bar and the command line are not fully visible; `#history-lines` at the same font goes past the edge.
- **Required behavior:** after measuring the cells, the renderer reports to the native layer the minimum content size `(60*cellW+8) × (15*cellH+8)` (a new message `{Type:'minSize',W,H}` → `window.contentMinSize`); on a font change, recompute; if the window is already smaller, `setContentSize` to the minimum. Or (cheaper) lower the lower bounds to 40×10 on both ends (`Draw` must withstand 40 columns: check `panelColumns`/`key bar`).
- **Edge cases:** split view/Stage Manager impose a size: AppKit respects `contentMinSize`; fullscreen on small screens at fontSize 24: the 15-row limit is reachable only on screens < 430 px high.
- **Tests:** `resize.cjs`: with `cellW=14.4` and `innerWidth=700` a `minSize` is sent with `W = 60*14.4+8`; manual check on macOS.
- **Priority:** P3
- **Estimate:** S

#### P4-13. A whitespace-only command is executed and gets into the history; a too-long command gives a misleading message
- **Function and variant:** Running a command (Enter): `a.Command` = `"   "`; a command/paste > 1 MiB.
- **Current behavior:** `key()` checks only `a.Command != ""` → `execute()` → `Run(path, "   ")`; zsh/bash accept the empty line (a new prompt), `appendUnique` stores `"   "` in `a.history` and `setup.json` (`app.go:key`, `execute`). For a long command `Session.request` returns `invalid panel command or directory`, the same message as for a NUL in cwd (`session.go:request`); the user does not understand the reason.
- **Required behavior:** `strings.TrimSpace(a.Command) == ""` → behave like an empty command (`enterFile()`), clear the line. In `request` distinguish the reasons: `command exceeds 1 MiB`, `directory path exceeds 1 MiB`, `directory must be absolute`, `command contains NUL`.
- **Edge cases:** a command with leading spaces and content is executed as is (zsh `HIST_IGNORE_SPACE` is shell behavior); tabs are whitespace too.
- **Tests:** Go ui `TestWhitespaceCommandIsNotExecuted`; terminal `TestRequestErrorMessages`.
- **Priority:** P3
- **Estimate:** S

#### P4-14. Two application instances: a race for session.json and setup.json
- **Function and variant:** Session/settings when two copies of FarshMac run simultaneously (for example from the `.app` and `go run`, or a double launch).
- **Current behavior:** There is no locking. Both instances read the same session at start (duplicating the windows), then each writes its own every 500 ms (temp+rename is atomic, but the last one wins); when one instance exits, the second's session is overwritten with the first's frames (`session.go:SaveSession`, `windows.go:Poll`). The same for `setup.json` with `AutoSave`.
- **Required behavior:** an advisory lock (`flock` on `<UserConfigDir>/FarshMac/session.lock`): an instance without the lock does not restore the session (starts with one window) and does not write `session.json`; status `Another FarshMac instance owns the window session; this instance will not save it`. For `setup.json`: the merge from P4-08.
- **Edge cases:** the lock file on a network/read-only directory: ignore the lock error and work as now; the owner crashes: `flock` is released by the OS.
- **Tests:** Go ui `TestSecondInstanceDoesNotOverwriteSession` (two `WindowGroup`s on the same path with a lock).
- **Priority:** P3
- **Estimate:** M

## Part 5. F9 menu, dialogs, settings, network connections, help

#### P5-01. Crash on ⌘C/⌘X on a form button
- **Function and variant:** any form with fields (`General settings`, `FTP / SFTP connection`, `Create link` …); focus on `OK`/`Primary` or `Cancel`; the user presses `⌘C` or `⌘X` (native Edit menu → `copy`/`cut` message).
- **Current behavior:** `document.go:copyDocument` executes `f.Fields[f.Focus].Toggle` when `f.Focus == len(f.Fields)` or `len(f.Fields)+1` → `index out of range` → panic; there is no `recover()` in `internal/ui` → the process terminates, unsaved documents are lost.
- **Required behavior:** with focus outside the fields, `copyDocument` does nothing (`Clipboard` is not changed). For lists (`Choice`): copy the text of the current item (useful for `Folder tree`, `Find`, histories). For confirm forms: copy `Message`.
- **Edge cases:** the `Remember` checkbox in a confirm (Focus == len(Choice)); a form with 0 fields; `cut` on a toggle field (already ignored).
- **Tests:** `TestFormCopyOnButtonDoesNotPanic` (a form with fields, Focus = len(Fields) and +1, `Handle({"type":"copy"})` and `cut`: no panic, `Clipboard == nil`); `TestChooseCopyCopiesCurrentItem`.
- **Priority:** P0.
- **Estimate:** S.

#### P5-02. ⌘C/⌘X take the password out of a Secret field into the system clipboard
- **Function and variant:** `FTP / SFTP connection`, the fields `Password (memory only)` and `Key passphrase (memory only)`; `⌘C`/`⌘X` with focus in the field.
- **Current behavior:** `copyDocument` copies the whole `field.Value` into `a.Clipboard` → `Update.Clipboard` → NSPasteboard; `Secret` is not checked. The password ends up in clipboard managers/Universal Clipboard, which contradicts the "memory only" promise (the test `TestRemoteUnknownHostConfirmationAndMasking` checks only the rendering).
- **Required behavior:** for `Secret` fields `⌘C` is a no-op, `⌘X` clears the field without writing to the clipboard; status `Secret fields are not copied`. Pasting (`⌘V`) into Secret fields remains allowed.
- **Edge cases:** `⌘A` + `⌘C`; `cut` while `Selected`.
- **Tests:** `TestSecretFieldNeverReachesClipboard` (Focus on fields 2 and 4, `copy`/`cut` → `Clipboard == nil`, after `cut` the value is empty).
- **Priority:** P1.
- **Estimate:** S.

#### P5-03. A host key change (MITM) is shown as a truncated raw error
- **Function and variant:** `connectRemote` to a host whose key in known_hosts does not match the received one (or is marked `@revoked`).
- **Current behavior:** `HostKeyCallback` correctly **does not accept** the key (returns `knownhosts.KeyError` with a non-empty `Want` / `RevokedError`), but `connectRemote` passes it to `a.error` → a status line like `ssh: handshake failed: knownhosts: key mismatch`, truncated to the width, without the fingerprint, without the file/line, without an explanation of the danger. There is no UI-level test (only `TestHostKeysAndSecrets` at the `remotefs` level).
- **Required behavior:** `connectRemote` recognizes `*knownhosts.KeyError` with `len(Want) > 0` and `*knownhosts.RevokedError` and shows the confirm `SSH host key changed` with the text:
  `WARNING: the host key of <host> does not match <file>:<line>.\nReceived: <SHA256:…> (<type>)\nExpected: <SHA256:…>\nThis may be a man-in-the-middle attack. The connection was refused.` Buttons: only `OK` (no "Trust anyway"). For revoked: the title `SSH host key revoked`. Status after closing: `Connection refused: host key mismatch`.
- **Edge cases:** several `Want` entries (list them all); hashed hosts (the fingerprint is taken from `Want[i].Key`); the error came via the `Trust once` path (`FixedHostKey` → `ssh: host key mismatch`): the same dialog.
- **Tests:** `TestRemoteHostKeyMismatchShowsWarningDialog` (`testnet.SFTP` + known_hosts with a different key → `Form.Title == "SSH host key changed"`, there is no trust button in `Choice`, `Panels[0].Remote == nil`); `TestRemoteRevokedHostKeyRefused`.
- **Priority:** P1 (security: the user does not understand what happened and will try to "fix" the connection).
- **Estimate:** M.

#### P5-04. A corrupted setup.json is silently replaced with defaults and then overwritten
- **Function and variant:** `LoadSetup` with invalid JSON, with type mismatches (`"FontSize": "14"`, `"Shortcuts"` longer than 10), without read permission; then `Save setup`, `AutoSave`, or any `saveSetup()` from the user menu / associations / folder shortcuts.
- **Current behavior:** `readConfigFile` with an error (other than `IsNotExist`) → `return` without a message; `json.Unmarshal != nil` → `return` (the partially parsed data is discarded, although `UnmarshalTypeError` allows continuing). The user sees factory settings without an explanation; the very first `saveSetup()` (for example saving the folder shortcut `Ctrl+Shift+1`) overwrites the file → loss of `UserMenu`, `Associations`, `Shortcuts*`, `MaskGroups`, histories.
- **Required behavior:**
  1. A read error (permissions) → status `Cannot read setup.json: <err>; using defaults, Save setup is disabled` and the flag `a.setupReadOnly`; `saveSetup` with the flag shows the confirm `Save setup` / `setup.json could not be read at startup. Overwrite it with current settings?` buttons `Overwrite`/`Cancel`.
  2. A JSON error → the file is renamed to `setup.json.broken-<YYYYMMDD-HHMMSS>` (not deleted), status `setup.json is damaged; saved as setup.json.broken-…, defaults loaded`; for `*json.UnmarshalTypeError` continue with the partially parsed structure and report `setup.json: <field> has wrong type; default used`.
  3. The same rules for `session.json`, outside this section.
- **Edge cases:** an empty file (0 bytes): treat as missing, without renaming; the fallback file from `Far Go/` is corrupted: must not be renamed (the old directory is not touched) → only a status; two windows start simultaneously (renaming under a `profilesMu`-like mutex `setupMu`).
- **Tests:** `TestCorruptSetupIsPreservedAndReported` (write garbage, `LoadSetup`, check `.broken-*` and the status, then `saveSetup` creates a new file); `TestSetupTypeErrorKeepsPartialSettings` (`"FontSize":"x"` → the other fields are loaded); `TestUnreadableSetupBlocksSilentOverwrite` (chmod 000, `saveSetup` → confirm, no write).
- **Priority:** P1 (loss of settings).
- **Estimate:** M.

#### P5-05. Connection errors are visible only in the truncated status line
- **Function and variant:** `connectRemote`: timeout (15 s), authentication refused, `no usable SSH identity…` with a long hint, a known-hosts error, `cannot read SSH private key …`, FTP `530 Login incorrect`, an FTPS TLS error. Also a profile pointing to an unreachable host.
- **Current behavior:** `backgroundTask` without `reportError` → `pollTask` puts `err.Error()` into `a.Status`; `Draw` truncates to the width; the next shell tick (`Exit N…`) or any action erases it. The form is closed, all entered fields (except the profile) are lost: the user types the URL/user again.
- **Required behavior:** the task `Connecting to <label>` gets `reportError = true`, but instead of the standard `<label> failed`/`OK`, a confirm `Connection failed` with the full error text (word wrap, PageUp/PageDown) and the buttons `Edit`/`Retry`/`Cancel`: `Edit` opens `FTP / SFTP connection` with the same values (the password and passphrase are kept in the form's memory for the duration of the retry), `Retry` repeats `connectRemote` with the same `Config`. Cancellation by the user (`Esc` in the progress): no dialog, status `Connection cancelled`.
- **Edge cases:** `UnknownHost`: a separate dialog (as now); an error after `Trust once` (the second `connectRemote`): `Edit` must keep `HostKey`; a connection from a folder shortcut (`goFolderShortcut`): `Edit` opens the form with the shortcut's profile.
- **Tests:** `TestConnectionFailureDialogOffersEditAndRetry` (an unreachable port on 127.0.0.1 → `Form.Title == "Connection failed"`, `Edit` → the form with the previous URL/user, the password field non-empty in memory but masked); `TestConnectionCancelHasNoDialog`.
- **Priority:** P2.
- **Estimate:** M.

#### P5-06. No keyboard-interactive password authentication
- **Function and variant:** SFTP to a server with `PasswordAuthentication no` + `KbdInteractiveAuthentication yes` (typical for macOS Remote Login, FreeBSD, many hardened configs and 2FA).
- **Current behavior:** `dialSFTP` adds only `ssh.PublicKeys` and `ssh.Password` → `ssh: unable to authenticate, attempted methods [none password], no supported methods remain`, although the password is correct.
- **Required behavior:** with a non-empty `Password`, add `ssh.KeyboardInteractive` that answers with the password to every prompt whose `echo == false` (once; on the second prompt an empty answer, to avoid looping); with `echo == true` (OTP/verification code) return the error `server asks for an interactive code; not supported` (in the P5-05 dialog).
- **Edge cases:** the server requires both a public key and a password (partial success): the method order `PublicKeys, Password, KeyboardInteractive`; an empty `Password` → the method is not added.
- **Tests:** `TestSFTPKeyboardInteractivePassword` in `remotefs` (a test ssh server with `KeyboardInteractiveCallback`, without `PasswordCallback` → the connection succeeds); `TestKeyboardInteractiveRefusesEchoPrompt`.
- **Priority:** P2.
- **Estimate:** S.

#### P5-07. An encrypted key / wrong passphrase does not return the user to the passphrase field
- **Function and variant:** `SSH private key` points to a key with a passphrase, the `Key passphrase` field is empty; or the passphrase is wrong (`x509: decryption password incorrect` / `ssh: this private key is passphrase protected`); or the default key is encrypted and the hint `id_ed25519 requires a passphrase or ssh-agent` is appended to the handshake error.
- **Current behavior:** `defaultPrivateKeys` returns an error → `connectRemote` → status `cannot read SSH private key /path: ssh: this private key is passphrase protected`; the form is closed; the passphrase has to be entered by reopening `Network connections` and the profile (the password too, again).
- **Required behavior:** `connectRemote` recognizes `*ssh.PassphraseMissingError` and the wrong-passphrase error (wrapped in `cannot read SSH private key`) and reopens `FTP / SFTP connection` with the previous values, focus on `Key passphrase (memory only)`, status `Key <name> is passphrase protected` / `Wrong key passphrase`. For an explicit `KeyFile` with a non-existent path: focus on the key field, status `cannot read SSH private key <path>: no such file`.
- **Edge cases:** the key is in ssh-agent and encrypted on disk: already works (the hint does not interfere); a wrong passphrase for default keys → the error is only in the hint, the connection may succeed through the agent: in that case do not show the dialog.
- **Tests:** `TestEncryptedKeyReopensDialogOnPassphraseField` (a key encrypted via `ssh.MarshalPrivateKeyWithPassphrase`; after `connectRemote` `Form.Title == "FTP / SFTP connection"`, `Form.Focus == 4`); `TestWrongPassphraseReported`.
- **Priority:** P2.
- **Estimate:** S.

#### P5-08. "Trust once" cannot be saved: the unknown-host question repeats on every new connection
- **Function and variant:** `Unknown SSH host key` → `Trust once`; then: selecting the same profile from `Network connections`, a folder shortcut in another window, an application restart.
- **Current behavior:** `c.HostKey` lives only in `p.Remote`; `saveConnectionProfile` and `saveFolderShortcut` zero `HostKey`; `windows.go` does not copy it (`json:"-"`). A user without the `ssh` CLI habit will never get an entry in known_hosts and will always press `Trust once`, which trains blind confirmation and devalues the MITM protection.
- **Required behavior:** in the `Unknown SSH host key` dialog, three buttons: `Trust once` (as now), `Trust and save` (appends the line `knownhosts.Line([]string{knownhosts.Normalize(addr)}, key)` to `~/.ssh/known_hosts`, creating `~/.ssh` 0700 and the file 0600 if missing; a write error → status `known_hosts not updated: <err>`, the key is still trusted for the session), `Cancel`. The dialog text is extended with the key type: `<host> <type> <SHA256:…>`.
- **Edge cases:** `~/.ssh/known_hosts` is read-only; a non-standard port (`Normalize` gives `[host]:port`); the host already exists with a different key type (this is not a mismatch for knownhosts: write the second type).
- **Tests:** `TestTrustAndSaveAppendsKnownHosts` (HOME in TempDir; after the button the file contains a line with the fingerprint, a repeated `connectRemote` without `HostKey` does not ask); `TestTrustAndSaveReportsWriteError`.
- **Priority:** P2.
- **Estimate:** S.

#### P5-09. An AutoSave error on window close is invisible
- **Function and variant:** `Save setup on exit` is enabled; on `App.Close` (window close / ⌘Q) the write fails (no permission on the directory, disk full, the directory replaced by a file).
- **Current behavior:** `Close` calls `saveSetup()`; the error lands in `a.Status` of the window that is already closing and never reaches an `Update`. The user is sure the settings were saved.
- **Required behavior:** `saveSetup` returns an `error`; `WindowGroup`/`BeginQuit`, on an autosave error of the last window, shows a native `NSAlert` `Settings were not saved` / `<err>\nQuit anyway?` with the buttons `Quit`/`Cancel` (via a new flag `Update.Alert{Title, Text, Buttons}`); when closing one of several windows: the status in the remaining focused window `Setup not saved: <err>`. Additionally write the error to stderr/os_log.
- **Edge cases:** `AutoSave` disabled: nothing; `Cancel` on an error during ⌘Q: cancel the quit (the documents are already saved: do not repeat the "Save changes" dialog).
- **Tests:** `TestAutoSaveFailureSurfacesOnQuit` (the configuration directory replaced by a file → `Update.Alert` is present, `Quit == false` until the answer); `TestAutoSaveFailureShownInRemainingWindow`.
- **Priority:** P2.
- **Estimate:** M.

#### P5-10. Several windows/instances: "last writer wins"
- **Function and variant:** window A changes `General settings` → window B (its own copy of `Settings`, copied on ⌘N) does `Save setup` or closes with `AutoSave` → A's changes are lost; likewise two application instances; `Edit user menu`/`File associations`/folder shortcuts in B immediately overwrite setup.json entirely.
- **Current behavior:** each `App` holds an independent copy of `Settings` (`windows.go:NewWindow`: a JSON round-trip), `saveSetup` writes its copy entirely, without reading the file. Only the connection profiles have merging (`saveConnectionProfile` re-reads the file under `profilesMu`).
- **Required behavior:** within one process, after a successful `Submit` of any settings form, `Colors`, `Code pages`, `Edit user menu`, `File associations`, folder shortcuts, the window group broadcasts the changes to all `g.Apps` (copying `Settings` + `configurePanel` + `refresh`), so that any window saves the current state; the status in the other windows does not change. Between instances: do not merge on save (complex and non-obvious); document: "the last save wins", plus `saveSetup` compares the file's mtime with the time of the last load and on a discrepancy shows the confirm `Save setup` / `setup.json was changed by another FarshMac instance. Overwrite?` (`Overwrite`/`Cancel`).
- **Edge cases:** per-window settings (`panelOptions`, `Paths`) stay per window: saved from the window that invoked Save; `FontSize` after the broadcast changes the font in all windows (expected).
- **Tests:** `TestSettingsPropagateAcrossWindows` (A changes `ShowKeys` → B.Settings.ShowKeys changed, `saveSetup` from B writes it); `TestSaveSetupDetectsExternalChange`.
- **Priority:** P2.
- **Estimate:** M.

#### P5-11. Pasting into form fields does not filter control characters and line breaks
- **Function and variant:** `⌘V` of multi-line text / text with `\t`, `\r`, `\x1b` into any `Form` field (`Create link` → `Link name`, `File association` → `Masks`, `FTP / SFTP connection` → `Server URL`, `Mask group` → `Name`, `User command` → `Command`).
- **Current behavior:** `app.go:Handle` for `a.Form != nil` calls `formPaste` **before** the cleanup (`strings.Map` is applied only for Dialog/the command line). `modalInput` shows control characters as spaces, i.e. the value looks normal but contains `\n`: `Link name` creates a file with a line break in the name; `Server URL` gives `invalid control character in address` without an obvious reason; the association's `Masks` is saved with `\n` in setup.json.
- **Required behavior:** in `formPaste` apply the same normalization as for Dialog (control characters → space, then `\r\n`/`\n` → space, collapse trailing spaces); for `Secret` fields remove only `\r\n` (a password with a tab is allowed); for a multi-line paste into the user menu's `Command` field, replace line breaks with `; `? No: with a space (single-line fields). Status `Pasted as a single line` when line breaks were present.
- **Edge cases:** pasting while `Selected` (replaces); pasting an empty string; pasting into a toggle (ignored, already exists).
- **Tests:** `TestFormPasteStripsControlCharacters` (a multi-line paste into `Create link` → the value without `\n`, status); `TestSecretPasteKeepsTabs`.
- **Priority:** P2.
- **Estimate:** S.

#### P5-12. Choice lists without PageUp/PageDown and type-ahead search
- **Function and variant:** `choose` lists with dozens/hundreds of items: `Folder tree`, `Find file…` results, `Command history` (up to 10000), `Folder history`, `Network connections`, `Volumes and home`, `File associations`.
- **Current behavior:** `formKey` handles `PageUp/PageDown` only with `f.Buttons`; for lists only `ArrowUp/Down/Tab/Home/End` and `Alt+letter` (puts focus on the first item with that letter; pressing again does not move to the next). With 10000 items movement is only possible line by line.
- **Required behavior:** `PageUp/PageDown` by `available` rows (from `formGeometry`); `Alt+letter` again: the next item with the same mnemonic; `Ctrl+Enter`/printable characters: an incremental filter (like the panels' fast find): the title is extended with ` [<query>]`, `Backspace` deletes a character, `Esc` resets the filter rather than closing the list if the filter is non-empty.
- **Edge cases:** the mouse wheel in a list (currently `scrollMouse` with an open form?) must move `Scroll` without changing the focus; `MenuWrap` on PageDown on the last page: clamp, not wrap.
- **Tests:** `TestChoosePageKeysAndTypeAhead` (a list of 100 lines: PageDown → Focus = available, Home/End, typing `ab` → filter, Esc → reset); `TestChooseWheelScrolls`.
- **Priority:** P2.
- **Estimate:** M.

#### P5-13. Connection profiles: no deletion, one profile per Label, Directory is not synchronized
- **Function and variant:** (a) a stale profile to an unreachable host stays in `Network connections` forever; (b) two profiles to the same server with different folders are impossible (`saveConnectionProfile` replaces by `Label()`); (c) `Save setup` updates `Directory` only in memory (`Settings.RemoteConnections`), `connections.json` takes priority at start, so "saving Directory on exit" effectively does not work: the profile keeps the folder specified at connection time.
- **Current behavior:** `remote.go:networkMenu`, `connection_profiles.go:saveConnectionProfile`, `settings.go:saveSetup`.
- **Required behavior:**
  - In the `Network connections` list, on a profile item `F8`/`Delete`/`Shift+F8` → confirm `Delete connection profile` / `Remove <label><dir> from connections.json?` (`Delete`/`Cancel`); deletion under `profilesMu` with a re-read of the file.
  - `Save setup` and `disconnectRemote` write the current `Directory` of the connected panels to `connections.json` via `saveConnectionProfile` (an error goes to the status `; profile not saved`).
  - For (b): the profile key = `Label()+Directory` (allow several folders per server); "same Label, different Directory" → a new item rather than a replacement. Update `TestConnectionProfilesPersistWithoutSaveSetup` (the test currently requires a replacement when Directory changes).
- **Edge cases:** `choose` currently does not pass the `F8`/`Delete` keys to the callback: a `Form.OnKey func(Message) bool` or a separate item `Delete profile…` is needed; connections.json broken during deletion → an error in the status, touch nothing.
- **Tests:** `TestDeleteConnectionProfile`; `TestSaveSetupWritesRemoteDirectoryToConnections`; `TestProfilesWithDifferentDirectoriesCoexist`.
- **Priority:** P2.
- **Estimate:** M.

#### P5-14. Menu items unavailable for a remote/archive panel are not marked
- **Function and variant:** the active panel is SFTP/FTP or an archive; the `Files` menu shows `Open with macOS`, `New file…`, `Link…`, `Create archive…`, `Extract archive…`, `Archive commands…`, `File attributes…`, `Apply command…`, `Describe files…`, `Edit` (archive); `Commands` → `Find file…`, `Find folder…`, `Folder tree`, `User menu`; `Left/Right` → `Info panel`, `Tree panel`, `Quick view`.
- **Current behavior:** `menuSections` knows nothing about the mode; the item looks ordinary, after `Enter` the status `This command requires a local panel…` / `Archive is read-only…` / `Choose a file view for remote panels`.
- **Required behavior:** `menuItem.Disabled func() bool`; disabled items are drawn `Gray` on `Cyan` (in themes via the palette), the cursor can land on them, `Enter`/click → close the menu and show the existing status (the behavior does not change, only the indication). The test `TestCleanMenuOrderAndSeparators` is not affected.
- **Edge cases:** quick view/tree in Left/Right for a remote panel; `Edit` in an archive (but `View` is available).
- **Tests:** `TestMenuMarksUnavailableItemsForRemoteAndArchive` (grid: the text color of the `Link…` item with `Panels[0].Remote != nil`).
- **Priority:** P3.
- **Estimate:** S.

#### P5-15. A validation error in a settings form closes the form and loses the other changes
- **Function and variant:** `General settings` (`Font size must be 10–24`), `Terminal settings` (`Choose bash, sh or zsh`, `History entries must be 1–10000`), `Editor settings` (`Tab width must be 1–16`), `Mask group` (`Invalid group name`…), `File association` (mask error), `File attributes`, the prompts `Tree settings`/`File descriptions`/`Folder description files`/`Sort groups`/`Save folder shortcut`.
- **Current behavior:** `formKey` zeroes `a.Form` before `Submit`; `Submit` on an error sets the status and returns: the toggled toggle fields are not applied, the form is gone, the user fills everything in again.
- **Required behavior:** a common helper `a.reopenForm(fields, focus, status)`: on a validation error the form opens again with the entered values, focus on the erroneous field, `Selected = true` for it, the status with the error text. For prompts: `a.prompt` again with the entered value.
- **Edge cases:** cancel on the second attempt: exit without applying; `RememberDialogValues` must not remember an invalid value (currently `Confirm` saves before calling `fn`).
- **Tests:** `TestInvalidFontSizeReopensFormWithValues` (`Font size = 99`, `Key bar` unchecked → after Enter `Form != nil`, Focus 0, Fields[1].Checked == false, status); `TestPromptValidationReopens`.
- **Priority:** P3.
- **Estimate:** S.

#### P5-16. Edit user menu / File associations: editing moves the entry to the end, an empty Label deletes silently
- **Function and variant:** editing an existing command/association; `Label` empty with a non-empty `Command`; two associations with the same mask.
- **Current behavior:** `commands.go:editUserMenu`/`fileAssociations` delete element `i` and `append` to the end → the order (and therefore the order of the F2 menu and the association priority) changes on every edit; `c.Name == ""` with a filled-in command → the entry is deleted without a message ("empty command deletes" was expected, not label).
- **Required behavior:** replace in place (`list[i-1] = c`); deletion only on an empty `Command` (user menu) / all three commands empty (associations); an empty `Label` with a non-empty command → status `Enter a label` and reopening the form (P5-15); empty association masks → `Enter file masks`.
- **Edge cases:** `Add command…` with empty fields → nothing is added, no status needed.
- **Tests:** `TestEditUserMenuKeepsOrder`; `TestUserMenuRequiresLabel`; `TestEditAssociationKeepsPriority`.
- **Priority:** P3.
- **Estimate:** S.

#### P5-17. Groups of file masks: a name with ` = ` breaks list parsing; validation of description file names
- **Function and variant:** `Mask group` with `Name` = `a = b` (allowed: only `<>,;|` are forbidden); then selecting the item `a = b = *.x` in the list → `SplitN(" = ", 2)` gives the name `a`, masks `b = *.x`; editing deletes the non-existent key `a`, creates a new one, and the old `a = b` stays forever. Also `File descriptions` accepts `.`/`..`/names with spaces at the edges.
- **Current behavior:** `settings.go:settingsMenu` (`Groups of file masks`, `File descriptions`).
- **Required behavior:** the list stores `keys := sortedKeys(...)` and in the callback takes `name = keys[i-1]` without parsing the caption; `Name` additionally forbids `=`, spaces at the edges (`TrimSpace`), and must be non-empty; `File descriptions`/`Folder description files` reject `.`, `..`, names with `/`, trim spaces: status `Enter a file name without a path`.
- **Tests:** `TestMaskGroupNameWithEqualsIsEditable`; `TestDescriptionFileRejectsDots`.
- **Priority:** P3.
- **Estimate:** S.

#### P5-18. Sort modes… on a remote panel allows sort modes forbidden by the hotkeys
- **Function and variant:** `Left/Right → Sort modes…` or `Ctrl+F12` on an SFTP/FTP panel, selecting `Created`/`Accessed`/`Description`/`Owner`.
- **Current behavior:** `panel_options.go:sortMenu` sets `p.Sort` without a check; `far_keys.go` for `Ctrl+F8…F11` on a remote panel writes `This sort mode requires local file metadata`. Through the menu the mode "applies", but `compareStat` for remote entries has no data → the order is effectively by name, and the user thinks the sort works.
- **Required behavior:** the same check in `sortMenu`: status `This sort mode requires local file metadata`, `Sort` is not changed. In the `Sort mode` list mark the current mode and the flag states (`✓ Name`, `[x] Reverse order`): currently the state is not shown.
- **Tests:** `TestSortMenuRefusesMetadataSortsOnRemote`; extend `TestFarSortRangeAndCurrentCopy`.
- **Priority:** P3.
- **Estimate:** S.

#### P5-19. A corrupted connections.json is not recovered
- **Function and variant:** `connections.json` with invalid JSON (manual editing; an interrupted write before rename is impossible, but the file may be damaged by a third-party tool).
- **Current behavior:** `loadConnectionProfiles` silently ignores it; `saveConnectionProfile` returns the error `invalid character …` → on **every** connection the status `Connected: …; profile not saved: invalid character 'x' …`; the user does not know the file path and cannot fix it from the UI.
- **Required behavior:** on a parse error, rename to `connections.json.broken-<ts>`, status `connections.json is damaged; saved as connections.json.broken-…`; the next write creates a new file from `Settings.RemoteConnections` (the copy from setup.json).
- **Tests:** `TestBrokenConnectionsFileIsRenamedAndRecreated`.
- **Priority:** P3.
- **Estimate:** S.

#### P5-20. IDN hosts and non-UTF-8 FTP file names
- **Function and variant:** (a) `sftp://сервер.рф/`: `url.Parse` accepts a unicode host, `net.Dial` does not perform IDNA → `no such host`; (b) an FTP server returns names in CP1251/Latin-1: `ValidName` does not check `utf8.ValidString`, the name gets into the panel as garbage, copying to APFS gives the error `invalid argument` for the file name.
- **Current behavior:** `remotefs/fs.go:Parse`, `ValidName`; `remote.go:loadRemote`.
- **Required behavior:** (a) `Parse` applies `idna.Lookup.ToASCII(host)` (`golang.org/x/net/idna`), `Label()` shows the unicode form; (b) invalid UTF-8 → show the name via `strings.ToValidUTF8(name, "�")` with a note in the status `Some names are not valid UTF-8; copying them is refused`; `remotefs.Copy` rejects such sources with `filename is not valid UTF-8: <hex>`; optionally an FTP encoding choice in the connection form (out of scope).
- **Tests:** `TestParseConvertsIDNHost`; `TestRemoteInvalidUTF8NamesAreDisplayedButNotCopied`.
- **Priority:** P3.
- **Estimate:** M.

#### P5-21. FTPS with a self-signed certificate and plain FTP without a warning
- **Function and variant:** `ftps://` to a server with a self-signed certificate; `ftp://` (the password in clear text).
- **Current behavior:** FTPS: `tls.Config` without `InsecureSkipVerify`, system roots → `x509: certificate signed by unknown authority` in the status (correct, test `TestExplicitFTPSProtectsDataConnection`), but without a `Trust once` equivalent by certificate fingerprint. FTP: the password goes in clear text without any warning, including when selecting a profile.
- **Required behavior:** FTPS: `VerifyPeerCertificate` → on a chain error return `*remotefs.UnknownCertificate{Host, Fingerprint, Raw}`; `connectRemote` shows the confirm `Unknown FTPS certificate` / `<host> SHA256:<fp>\n<Subject>, valid until <date>. Trust for this connection?` (`Trust once`/`Cancel`); trust is a pin by the raw fingerprint in `Config.CertFingerprint` (`json:"-"`), not `InsecureSkipVerify`. FTP: the first time for a profile, confirm `Plain FTP` / `Passwords and files are sent unencrypted. Continue?` (`Continue`/`Cancel`) with `Remember choice`.
- **Tests:** `TestFTPSSelfSignedAsksForTrust` (based on `ftps_test.go`); `TestPlainFTPWarnsOnce`.
- **Priority:** P3.
- **Estimate:** M.

#### P5-22. Status line: overwriting and truncation of messages
- **Function and variant:** `Setup saved`/`Connected: …`/a connection error appears at the moment the shell finishes starting or a command → `Poll` sets `Exit N · ⌘O panels / terminal` even in panel mode and erases the message; long errors (`no usable SSH identity…; id_rsa: …; id_ecdsa: …`) are truncated without a truncation indicator; there is no way to read a past message.
- **Current behavior:** `app.go:Poll` (`e.Ready`), `draw.go:Draw` (`g.Text(... g.W)`), `task.go:pollTask`.
- **Required behavior:** the shell `Ready` event updates the status only if `TermVisible` or the current status is empty / is the previous shell status (store `a.shellStatus`); on truncation add `…` in the last column; `Ctrl+Shift+S`? no: add the item `Commands → Last message` / a click on the status line opening the confirm `Message` with the full text; `a.error` keeps the last 20 messages (`a.messages`) for this window.
- **Tests:** `TestShellReadyDoesNotOverwritePanelStatus`; `TestLongStatusIsTruncatedWithEllipsis`; `TestStatusClickShowsFullMessage`.
- **Priority:** P3.
- **Estimate:** S.

#### P5-23. Dialog (prompt): no cursor, Delete, or insertion in the middle
- **Function and variant:** all single-line prompts (`Make folder`, `Change directory`, `Select group`, `Remote move / rename`, `Tree settings` …): `ArrowLeft/Right/Home/End` only clear the selection, the cursor is always at the end; `Delete` is not handled; insertion only at the end; editing a long path (`Remote move / rename` with a prefilled value) requires erasing the tail.
- **Current behavior:** `app.go:key` (the `a.Dialog != nil` branch), `draw.go:drawDialog` (`cursor = len(Input)`).
- **Required behavior:** move `Dialog` onto the `Form` engine with a single field (`a.prompt` → `a.form` with `Primary="OK"`), which gives a cursor, `Delete`, insertion at a position, `⌘A`, mnemonics; keep `RememberDialogValues`/`SelectDialogInput`, update the visual fixtures (`TestDialogVisualFixtures`). Or, minimally: add `Cursor` to `Dialog` and handling of `ArrowLeft/Right/Home/End/Delete`, insertion at the cursor.
- **Tests:** `TestPromptCursorEditing` (Home → typing → insertion at the beginning; Delete).
- **Priority:** P3.
- **Estimate:** M (moving to Form) / S (minimal variant).

# Decisions on discrepancies between the code and the documentation

The lists of discrepancies are given at the end of each part of `docs/FUNCTIONAL_SPEC.md`. Here, for each item, it is proposed what to change: the documentation (README, the F1 help, the parity documents) or the code.

## Part 1. File panels and navigation

| Discrepancy | Decision (docs/code) | Rationale |
|---|---|---|
| 1. F1 `Cmd+R Refresh panels`, the code re-reads the active one | **docs** | The behavior matches Far (Ctrl+R is the active panel); both panels are refreshed by `refresh()` after the terminal/settings. Fix the help line to `Cmd+R  Re-read active panel` and mention `Left/Right → Re-read` for a specific panel; README in sync. |
| 2. `Cmd+Shift+.` "display only" | **docs** | The setting is deliberately shared with `Panel settings` and is saved in setup.json (`TestHiddenSettingsAndHotkeyIndependent` pins the link). Remove "display only", add `Ctrl+H` as a synonym. |
| 3. Ctrl vs ⌘ in the help | **docs** | `cmd := m.Ctrl \|\| m.Meta` is a deliberate decision for Mac keyboards. In the help and README introduce a `Ctrl/⌘` legend and explicitly list the "Ctrl only" exceptions (Ctrl+Q, Ctrl+L, Ctrl+H, Ctrl+E/X, Ctrl+1..9, Ctrl+Shift+1..9). |
| 4. Undocumented keys | **docs** | The functions work and are covered by tests (`TestFarSortRangeAndCurrentCopy`, `TestMouseMarkRangeDragAndHeaderHitTesting`, …). Extend F1/README/BEHAVIOR_PARITY; describe the Shift variants of Ctrl+[ / ] only with Ctrl. |
| 5. Jumping to a bookmark resets `states` | **code** (+docs) | Resetting `p.states = nil` in `goFolderShortcut` also loses the remembered **marks** of all visited folders; the rule "on entering the parent, the cursor is on the folder just left" is already implemented in `beginLoad` and does not require a reset. Remove the reset; extend the test `TestControlFolderShortcuts` with a check that positions are preserved. If the reset is kept, describe it in F1. |
| 6. Alt+Num* unmarks folders | **docs** | The behavior matches Far (`Alt+Num*` inverts files, unmarks folders). Add a line to the help. |
| 7. `Sort mode → Directories first` toggles `MixDirectories`, no indicators | **code** | The item's semantics are the opposite of its name, and the four toggles have no `✓` (the Left/Right menu has an indicator: inconsistent). Rename to `Mix directories and files`, add `✓ ` for `Reverse order`, `Case sensitive`, `Mix…`, `Selected first` and for the current mode; test `TestSortMenuShowsState`. |
| 8. `isArchiveName` includes `.gz/.bz2/.xz` without `.tar` | **code** | Ctrl+PgDn on `file.gz` currently "enters the archive" but actually opens the file with the system application: a side effect not described anywhere. Replace `isArchiveName` with `fsops.ArchiveFormat(name) != ""` (a single source of truth; `.war/.cpio/.tbz` also become available). |
| 9. BEHAVIOR_PARITY: paste in fast find "up to the first non-matching" | **docs** | Always accepting the first character is a deliberate decision, pinned by `TestFastFindIncludesFirstLetterWithoutMatch` (the search box appears, showing that the input was accepted). Refine the wording in BEHAVIOR_PARITY. |
| 10. F4 on a folder opens `File attributes` | **docs** | A useful function (in Far, F4 on a folder is also unavailable for editing, but attributes are changed via Ctrl+A). Describe in README/F1: `F4 on a folder  File attributes (permissions, modified)`. |
| 11. Legacy modes in F1, `Medium` not mentioned | **docs** | `configurePanel` forcibly replaces them with Full; the drawing code is kept for old sessions. Remove the `File links view` line from the help, add `Medium` to the Left/Right description. (If the modes are to be brought back, that is a separate feature, not a discrepancy.) |
| 12. Tab activates a hidden panel | **code** | See P1-01: keys on an invisible panel are a real danger (F8 on an invisible file). Tab must skip the hidden panel; with both hidden, show the active one. |
| 13. Quick view for remote via Ctrl+Q but not via the menu; a `stat` error for archive members | **code** | Two paths to the same function behave differently for no reason: allow `setView(index, "Quick view")` for remote (the `toggleOtherView` logic already allows it; drawing does not require a local path). For an archive source, in `updatePreview` check `source.Archive != nil && Relative(path) inside` and show `Archive member: press F3 to extract and view` instead of `stat …: no such file or directory` (symmetric to the line for remote). Tree/Info for remote remain forbidden (they need a local path). |

## Part 2. File operations, archives, running programs

| Discrepancy | Decision (docs/code) | Rationale |
|---|---|---|
| 1. Help: "Cross-volume move is refused…"; the comment in `move_darwin.go` | **docs** (Help in `app.go:help`, the comment `fsops/move_darwin.go:Move`) | The code implements a safe cross-volume move (`moveAcrossVolumes`, tests `TestCrossVolumeMovePreservesMetadataAndAllContents`, `TestMoveMetadataFailureRetainsSourceUntilConsent`); `FILE_OPERATIONS.md` already describes it correctly. After P2-02 the Help text: "Cross-volume moves copy each file with metadata, ask before any loss, then delete the source". |
| 2. Help: "not ACLs/xattrs" | **docs** | The code preserves ACL/xattr/flags/crtime when the checkboxes are enabled (`metadata_darwin.go:preserve_metadata`, test `TestCopyMetadataOptionsIndependent`); the F5 dialog says so explicitly. |
| 3. Help: "F5 / F6 … (no overwriting)" | **docs** → "(never overwrites silently)" | Overwriting exists through an explicit collision dialog; the wording "no silent overwriting" reflects the staging + `RENAME_EXCL` policy. |
| 4. Help does not list Alt+F7/F8 | **docs** (Help + README) | The commands exist in the code and the menu (`findFiles`, `historyMenu("Commands")`). |
| 5. Ctrl+PgDn uses `isArchiveName` ≠ `ArchiveFormat` | **code** (`far_keys.go`: replace `isArchiveName` with `fsops.ArchiveFormat(name) != ""`) | Two format lists are a source of desynchronization; `.gz/.bz2/.xz` without `.tar` are not opened by `enterFile` anyway, so narrowing the list breaks nothing; `.war/.cpio/.tbz/.txz` start working. Test `TestCtrlPageDownMatchesArchiveFormat`. |
| 6. Shift+F2 does not check that the file is an archive | **docs** (+ improve the error text) | bsdtar/libarchive reads formats outside the `ArchiveFormat` list (`.iso`, `.xar`, `.deb`, `.cab`, single `.gz`); a check by extension would take that ability away. Document: "Extract archive tries to unpack any file via libarchive; a non-archive gives a decoder error". Optionally code: if `ArchiveFormat == ""`, append `(<name> has no known archive extension)` to the error. |
| 7. "Esc cancels a pending decision" | **docs** | In the code, Esc while `Busy` = `cancelOperation()` of the whole operation (`app.go:key`); the result for the user is the same as the `Cancel` button, but refine the wording in `FILE_OPERATIONS.md`: "Esc cancels the whole operation, like the Cancel button". No code change is needed: all questions have a `Cancel` button with the same meaning. |
| 8. The `Cannot preserve metadata` question for every file with remote | **code** (P2-07) + docs | Unconditionally added "errors" are not errors but a known limitation; a dialog for every file with the default settings is a UX defect. Until fixed, document in `FILE_OPERATIONS.md` ("server differences"). |
| 9. README: Alt+F7/F8/F11/F12, Shift+F1/F2/F3 not mentioned; "Enter to run" refers to the terminal history | **docs** | The code matches the spec; README is incomplete. Clarify that `Alt+F8` only inserts the command. |
| 10. ARCHIVE_SUPPORT.md: "Restart restores the containing directory and selects the archive" | **docs** (the spec: drop the discrepancy) | `session.go:98`: with `p.Archive != nil`, `Cursor = filepath.Base(p.Archive.Path)` is saved and the path is the archive's directory; the documentation is correct, and `openArchiveAt` should not be called at start. |
| 11. `ZIPContents`/`ExtractZIP` exist, but the UI does not use them | **code** (remove the dead code from `fsops/archive.go` or mark `//nolint` + docs) | The UI deliberately goes through bsdtar for list/test/extract (the single validator `extractArchive` on `os.Root`); a second ZIP extraction path without the same protections is a divergence risk. Bring the table in ARCHIVE_SUPPORT.md in line with reality: ZIP: Go for the index/selective extraction/creation, bsdtar for list/test/full extract. |
| 12. Help: "F7 New directory"; `Cmd+8` not documented | **docs** | The dialog title in the code is `Make folder`; `Cmd+1…9` = F1…F9: add an explicit line to Help. |
| 13. "Rename asks for a destination path"; the candidate format is not described | **docs** (`FILE_OPERATIONS.md`) | Code: the `Rename` form, the `New name` field, the candidate `<stem> (copy)<ext>`, relative input is relative to the destination directory; a remembered Rename appends ` (copy)` again. Describe precisely. |

## Part 3. Internal viewer and editor

| No. | Discrepancy | Decision | Rationale |
|---|---|---|---|
| 1 | Alt+F3/F4, Cmd+Shift+F3/F4 not described | **docs** (README "Keyboard essentials", F1 `help`, VIEWER_EDITOR_PARITY) + minor **code**: add subitems with the keys to the Files menu | The behavior is meaningful and matches Far (Alt+F3/F4 are the alternate viewer/editor). |
| 2 | The `File view/edit history` menu item has no key | **code** (`menu.go`: specify `Alt+F11`) | The key works; the empty caption is just an omission; a one-line fix |
| 3 | Space = next match in the viewer | **docs** (F1 `documentHelp`, parity) | Convenient and safe behavior, matches the habits of Far plugins; no point removing it |
| 4 | Cmd+D/U/L, Shift+Enter, Ctrl+Tab, double/triple click, dragging the bar, Shift+F10 in the viewer | **docs** | Everything is implemented and covered by tests (`TestClipboardAndMouseSelection`, `TestMouseWheelAndDocumentSelection`); only needs describing |
| 5 | The help describes only Cmd, the code accepts Ctrl too | **docs**: one line in F1 "Ctrl works as Cmd in the viewer/editor" | The behavior is intentional (`command := m.Meta \|\| m.Ctrl`) and important for external keyboards; moreover, Ctrl+Shift+3/4/5 is the only way around the system screenshots (P3-14) |
| 6 | Insert ignores modifiers; Shift+Delete is deletion | **code** (P3, S): Ctrl+Insert → `copyDocument(false)`, Shift+Delete → `copyDocument(true)`, Insert without modifiers is the toggle | That is how it is in Far (Ctrl+Ins copy, Shift+Del cut) and already in the FarshMac panels (Ctrl+Insert copies names): an internal inconsistency of the application. Shift+Insert is already paste on the renderer side |
| 7 | F9: the help mentions only encodings/EOL/BOM/RO | **docs** (F1, parity): list all 8 fields and mark the 4 global ones | The form is useful; it is important to warn that `Syntax`/`Auto indent`/`Tab width`/`Expand tabs` change the application settings, not the document |
| 8 | Alt+Left/Right limits the dump width by the hex ceiling `(Cols-14)/4` | **code** (S): in `binaryWidth` for `dump` use `min(BytesPerLine, Cols-14)` | The `(Cols-14)/4` ceiling is designed for a hex line of 3 columns per byte; for dump it is a side effect, not an intent |
| 9 | Parity "Shift+F2 selects word/character wrapping" | **docs** (parity → "toggles word/character wrap and enables wrap") | The behavior matches the F1 help; parity is worded imprecisely |
| 10 | F8 is silent in the viewer for Dirty/paged; from any encoding the first press gives CP866 | **code** (P3-15) for the silence; **docs** for "first press → CP866" | The silence is a UX defect; the CP866→1251 order is as in Far (F8 is "DOS/Win"), describe it |
| 11 | `+`/`-` always open the adjacent file in the viewer, bypassing the external settings, "even from the editor" | **docs** (and fix the specification itself: from the editor `+`/`-` is ordinary input, `adjacentFile` is called only from `viewerKey`; a Dirty viewer is possible after F6) | In Far `+`/`-` are also only in the viewer and always internal; the spec wording is imprecise. The remote panel is a separate defect, P3-07 |
| 12 | Alt+F11 opens directly via `openDocument`; parity claims the history is not persistent | **docs** (parity: the history is persistent, the positions are not; the history is only for local files, see A) | `viewHistory` is really saved in `savedSetup.Viewed`; opening directly, bypassing external programs, is an acceptable simplification; describe it |
| 13 | Remote editing/saving is absent from parity/README | **docs** | The function is implemented and tested (`TestRemotePanelConnectNavigateCopyEditAndDisconnect`); it needs describing, including the limitations (no Save as to the server, `remote file changed`) |
| 14 | Viewing from an archive is described in one phrase | **docs** (ARCHIVE_SUPPORT + parity: ReadOnly, the F6/`+`/`-` prohibition, messages) | The behavior is correct |
| 15 | Parity does not mention highlighting in the viewer and the language status | **docs** | A fact of the code |
| 16 | The light theme leaves `0x55ff55`/`0xaaaaaa`/`0xff88ff`/`0xff8888` bright on a light background | **code** (S): add substitutions for Light (`0x55ff55→0x1d6f1d`, `0xaaaaaa→0x6a6a6a`, `0xff88ff→0x8a2a8a`, `0xff8888→0xa02020`) and describe the theme in parity | This is readability, not taste: light green on `0xf1f3f5` is practically invisible |
| 17 | New files are created 0644, not by umask | **docs** (panel help/parity) + with P3-10 clarify what is preserved and what is not | A fixed 0644 is predictable and safe; no need to change |
| 18 | README "Keyboard essentials" without viewer/editor keys | **docs** | Add a short block (F2/F6/F7/F8/F9/F12, Shift+F2, Cmd+Z, Cmd+0..9) |
| 19 | Bookmarks `Atoi(m.Key)` vs `m.Code` | **code** (P3-14) — **confirmed** | `app.js` sends `e.key` as is; the application's own code (`app.go:648`, `folder_shortcuts.go:15`) already works around shifted characters via `m.Code`; the test does not cover real input. Plus document the capture of Cmd+Shift+3/4/5 by the system |

## Part 4. Terminal, command line, windows, native shell, renderer

| Discrepancy | Decision (docs/code) | Rationale |
| --- | --- | --- |
| 1. F1 help: "persistent zsh terminal", "Your .zshrc is loaded" with bash as the default | docs (the `help` text in `app.go`) | The bash default is pinned deliberately (`TestDefaultShellIsBash`, fast start without heavy zsh configs). Rewrite: "persistent bash/sh/zsh terminal (bash by default; choose in Options → Terminal settings). Your shell startup files are loaded…". |
| 2. Ctrl+E/X only with Ctrl; undocumented Ctrl+↑/↓ | docs | ⌘X is taken by the native Cut, ⌘E is free, but ⌘E/⌘X symmetry is impossible; add to the help `Ctrl+↑/↓, ⌘↑/↓ Command history`. |
| 3. ⌘F/⌘;, Ctrl+Alt+Insert, Ctrl+[ / Ctrl+], Ctrl+Shift+Enter are absent from the help | docs | The behavior is useful and tested (`TestRemoteCommandsDoNotModifyLocalPaths`); extend the help and README "Keyboard essentials". |
| 4. "F10 closes only viewer/editor"; in the panels F10 does nothing | docs | The "quit only with ⌘Q" decision is recorded in the code as a comment (`function(10)`); refine the wording: "F10 closes the viewer/editor; in panels it does nothing — quit with ⌘Q". Making F10 = Quit (as in Far) is undesirable: an accidental exit from all windows. |
| 5. TERMINAL_PARITY.md: "1024-byte chunks" vs 4094 | docs | The chunk size is chosen because of the scrollback limit (`min(4094, scrollback-2)`, test `terminal.cjs`); fix the number in the document. |
| 6. TERMINAL_PARITY.md: ⌘⇧H = output archive | docs | The document already notes this in the preamble; replace the navigation table with the current one (the commands overlay; the output archive is the Window menu / wheel). A code change (a shortcut for the archive): see item 9. |
| 7. Terminal settings "new windows", but the restart uses the new settings | docs (field captions/status) | The behavior "applies to new windows and to a shell restart in this window" is convenient (can be applied with `exit`) and cheap; change the caption to `(new windows / after shell restart)` and the status to `Shell applies to new windows and after the shell exits; Save setup to keep settings`. |
| 8. ⌘W while a command is running: "Close the application?" | code | The text is misleading (one window is closed). Pass the context into `RequestQuit(closeWindow bool)` → `confirmQuitShell` shows `Close window` / `A command is running. Close this window?`; for ⌘Q the old text. Estimate S. |
| 9. No ⌘= / ⌘− / ⌘0 (font) and ⌘⇧? (output history) | code for ⌘=/−/0 (P3, S); docs for ⌘⇧? | Font zoom by keys is the macOS terminal standard, the infrastructure exists (`Update.fontSize` → renderer → `resize`): add to `key()` (both modes) handling of `Meta` + `=`/`+`/`-`/`0` with the range 10–24 and the status `Font size <N>`; in the renderer intercept these ⌘ keys in `attachCustomKeyEventHandler` and send `key`. Do not add a shortcut for the output archive for now (⌘⇧? conflicts with "Help" in macOS); update the task/doc: "only the Window menu / wheel". |
| 10. README: ⌘⇧H without "terminal only" | docs | Add "(terminal mode)" to the README line; the menu is already deactivated in the panels. |
| 12. Shift+Insert is implemented only in the renderer | docs | Correct for the application (the native pasteboard is available only through the renderer); the headless tests use the `paste` message. Note in the spec as an architectural peculiarity. |
| 13. TERMINAL_PARITY.md: "at most the window height" vs `term.rows-2` | docs | The current value is dictated by the overlay toolbar; fix the wording. |

## Part 5. F9 menu, dialogs, settings, network connections, help

| # | Discrepancy | Decision | Rationale |
| --- | --- | --- | --- |
| 1 | `Save setup` has no shortcut in the menu, `Shift+F9` is implemented | code | Add `⇧F9` to `menuSections` (the shortcut column is 9 characters: it fits). The shortcut is already in the help; the menu must show it, as in Far. |
| 2 | `Link…`, `Create/Extract archive…`, `Archive commands…`, `File view/edit history`, `Folder history`, `Sort modes…` without a shortcut in the menu; `Find folder…` has no key | code + docs | In the menu add `⌥F6`, `⇧F1`/`⇧F2`/`⇧F3`, `⌥F11`/`⌥F12`, `⌃F12`; in the F1 help add `Alt+F6 Link`. Leave `Find folder…` without a key (Far has none either): record in docs. |
| 3 | Help: `Ctrl+L / Ctrl+T` are Info/Tree, `Cmd+L` is Change directory; Commands has no `Info/Tree on/off` | docs | The code is consistent (`toggleOtherView` only for `Ctrl`); separate Far-style menu items are not needed: the views are in `Left/Right`. Describe explicitly in the specification. |
| 4, 16 | Mixed shortcut notation in the menu (`Ctrl+Q`, `AltF1`, `ShiftF4`, `⇧F8`, `⌃⌘F`) | code | Unify on macOS symbols (`⌃Q`, `⌥F1`, `⇧F4`); it is one string table in `menu.go` and `TestCleanMenuOrderAndSeparators`. Cosmetic, but cheap, and removes the discrepancy with the help. |
| 5 | MENU_PARITY: "ten slots" vs 1–9 in the code | docs | Slot 0 is not used (`Shortcuts[10]`, the dialog `Slot 1–9`, `Ctrl+1…9`); there is no point introducing a 10th slot (`Ctrl+0`): fix the document. |
| 6 | Unreachable forms `Dialog settings`, `AutoComplete settings`, `System settings`, `Interface settings`, `Menu settings`, `Confirmations`, `Command line settings` | code (remove) | All their fields are duplicated in `General settings`/`Terminal settings`; two ways of editing the same fields are a source of discrepancies (different error texts: `Enter a number from 1 to 10000` vs `History entries must be 1–10000`). Remove the branches and make `default: Settings: <kind>` a panic in tests/a status in production. |
| 7 | MENU_PARITY does not mention `connections.json` and the `Far Go/` fallback | docs | The behavior is implemented and covered by tests (`TestRenamedConfigFallsBackWithoutOverwriting`, `TestConnectionProfilesPersistWithoutSaveSetup`); describe the location of the three files in the document. |
| 8 | F1 help: "persistent zsh terminal", ".zshrc is loaded" with the default `bash` | code (the `help` text) | Replace with "persistent shell (bash by default; sh/zsh in Terminal settings)", "your shell's rc files are loaded". The help is part of the code (`app.go:help`). |
| 9 | "Cross-volume move is refused" (help) vs "copy before removing" (MENU_PARITY) | docs (after verification by the file operations section) | The behavior is defined by the `transfer` code; that section must establish the truth, after which both the help and MENU_PARITY are brought in line. Here, only note the conflict. |
| 10 | Help: "F5 / F6 … (no overwriting)" while `Confirm overwrite` and the `Overwrite/All/Skip` buttons exist | code (the `help` text) | An outdated line; replace with "Copy / move or rename (asks before overwriting)". |
| 11 | Help: "Copies preserve permissions, timestamps and links, not ACLs/xattrs" vs the options `Preserve permissions (… ACL)`, `Preserve attributes (xattrs …)` | code (the `help` text) | The copy dialog is the source of truth; fix the help line: "Copy options: permissions/ACL, xattrs/flags/timestamps (F5 dialog)". |
| 12 | The help mentions `File links view`; the view was removed and is normalized to `Full` | code (the `help` text) | Remove the line; `panelViews` still contains `File links` and other legacy names only for normalization: keep. |
| 13 | README: `Ctrl+Shift+1…9 / Ctrl+1…9`; the `Folder shortcuts…` menu item has no shortcut | code | Show `⌃1…9` in the shortcut column (7 characters). |
| 14 | `Cmd+1…9` = `F1…F9`; `⌘9` opens and closes the menu | docs | The behavior is correct and expected; a description is enough (already present). |
| 15 | `Full screen ⌃⌘F` duplicates the native item, absent from the help | docs | The duplicate is harmless (a Far-like menu should be self-sufficient); add a line to the F1 help: that is code, but trivial; I classify it as docs + one `help` line. |
| 17 | The test expects 3 `Network connections` items with one profile | docs | Matches the code; when implementing P5-13 (profile deletion), update the test and the specification together. |
| 18 | `RestoreFolders` is a dead `Settings` field | code (remove) | It is read and edited nowhere; `json.Unmarshal` silently ignores the extra key in old setup.json files, so removal is safe; `TestSettingsRoundtrip` is not affected. |
| 19 | `Edit sort groups…` (Commands) and Advanced → `Sort groups` lead to the same prompt | docs | Two entries into one dialog is normal (Far: Commands → "Edit sort groups", Options → "Sorting"); record in the specification as deliberate duplication. |
