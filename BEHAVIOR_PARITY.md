# FarshMac behavior: version 0.7 audit

Reference: [official Far Manager help](https://github.com/FarGroup/FarManager/blob/master/far/FarEng.hlf.m4),
sections FastFind, FuncCmd, ViewerSettings, EditorSettings, FileAssoc, ScrSwitch.
This historical audit records implemented scenarios and validation limits. See
[CHANGELOG.md](CHANGELOG.md) for subsequent fixes and additions.

## Windows and screens

- ⌘N / File → New Window / Commands → New window creates an independent Cocoa
  window, initially using the source window's directories and a copy of its
  command, folder, and view/edit histories. Documents, selections, command line,
  PTY/shell, and operations are independent afterwards.
- ⌘W and the red close button check that window's documents and shell before
  closing it; a running command asks **Close window** ("A command is running.
  Close this window?"). ⌘W/⌘Q with a form open cancel the form first. Closing the last window leaves the macOS menu available; ⌘N opens another.
- ⌘Q checks every window. None closes until all confirmations finish. Cancel
  retains unsaved documents, including those previously marked Discard. Other
  windows cannot be edited during this check. A window that refuses to quit
  because it is busy is brought to the front.
- Window → Next/Previous Window, ⌘` / ⌘Shift+`, and the native window list navigate windows.
- F12 lists **Panels** and the current window's viewer/editor screens. Panels hides
  the document without closing it. Ctrl+Tab / Ctrl+Shift+Tab or ⌘Shift+] / ⌘Shift+[
  switches internal screens; ⌘Tab remains assigned to macOS.
- F10 closes only the current document; in panels it does nothing (quit with ⌘Q).
  Settings are shared live by all windows. Save setup writes them to setup.json and
  merges histories with the copy on disk; if another FarshMac instance changed
  setup.json, it asks "Overwrite?". A damaged setup.json or session.json is kept as
  `.broken-<time>` and defaults are used.
- ⌘+ / ⌘− change the font size (10–24) in every window (⌘0 resets it in the terminal only; in panels and documents ⌘0 is bookmark 0). ⌘= zooms only in
  terminal mode; in panels it remains "Same folder in other panel". ⌘/Ctrl letter
  shortcuts use the physical key on non-Latin layouts.

## Enter, viewer, and editor

With an empty command line, Enter:

1. Enters an ordinary directory, or launches an `.app` bundle through macOS.
2. Runs a matching FarshMac Run association.
3. Runs an executable file, including a symlink to one, using its shell-quoted
   absolute path in the persistent PTY, in the active panel's directory.
4. Opens other files through the default macOS association (`open`).

Terminal execution hides panels; ⌘O switches back and forth. An existing command
is not interrupted to launch another.

F3/F4 choose the viewer/editor using preferences and View/Edit associations.
Alt+F3 / Alt+F4 selects the alternative: external if internal is default, and vice
versa. ⌘Shift+F3 / ⌘Shift+F4 forces the internal implementation; Ctrl variants
also work. F3 on a folder calculates size; F4 opens attributes.

Options → Viewer settings / Editor settings provides External by default and a
Command field. An empty command uses macOS `open` / `open -t`. A command such as
`less -- {}` or `vim -- {}` runs in the embedded shell. `{}` is a shell-quoted path:
do not add quotes around it. Without the placeholder, the path is appended.
Commands → File associations has separate Run, Viewer, and Editor commands.
macOS chooses system applications; FarshMac does not change global associations.

## Fast find

Alt/Option+characters opens Fast find and selects by prefix. Continue typing
without Alt. Supported input includes `*` and `?` (wildcards), `[` and `\`
(literal characters), Cyrillic, Backspace, and paste. The first character is always
accepted so the search box appears even without a match; later pasted characters
stop at the first unmatched one. Matching is NFC-normalized, so decomposed
filenames match typed text. ⌘Enter / ⌘Shift+Enter (also Ctrl) and ↑/↓
cycle matches. Esc retains the found position; Enter opens/runs the selected item.
Search does not replace the command line.

Cocoa takes Option input from the current keyboard layout without Option's
character transformation, avoiding accent dead keys such as Alt+e. This interception
is disabled in editor and terminal. Transliteration and Far's full mask language
are absent. Later Option-input fixes are documented in the changelog.

## Panels

- Tab skips a hidden panel; with both panels hidden it shows the active one. On
  Quick view, Info panel, Tree panel or a hidden panel, file keys (Enter, F3–F8,
  Insert, …) report "Switch to a file panel first (Tab)". A single click on Quick
  view activates it.
- Sort modes (Ctrl+F12) mark the current mode and the toggles with ✓; the toggle
  is named **Mix directories and files**. Remote panels refuse metadata sorts
  (created, accessed, owner, description).
- ⌘C equals Ctrl+Insert: copies the command line or selected names; on `..` it
  copies the panel path. Ctrl+Alt+Insert copies real paths (links resolved).
- ⌘0…9 bookmark jumps keep remembered cursor positions and marks of visited
  folders and put the cursor on the file that was under it when the bookmark was saved.
  ⌘⌥0…9 saves. Unlike Far's folder shortcuts (Ctrl+digit / Ctrl+Shift+digit), bookmarks belong
  to one window and are not saved; the same ten slots and keys also hold viewer/editor
  positions (see VIEWER_EDITOR_PARITY.md). ⌘1…9 are no longer aliases of F1…F9.
- Ctrl+PgDn enters exactly the archive formats in [ARCHIVE_SUPPORT.md](ARCHIVE_SUPPORT.md);
  a bare `.gz`/`.bz2`/`.xz` is not entered.
- Folder history entries remember their source (local path,
  `sftp://user@host:port/path`, `[Archive] path`); remote entries reconnect.
- Remote symbolic links to folders are resolved in listings (up to 500 per folder;
  the rest on Enter).
- Quick view works on remote panels from the menu too; an archive member shows
  "Archive member: press F3 to extract and view".
- EPERM from macOS privacy protection reads "Access denied by macOS privacy
  protection" with a Full Disk Access hint.
- In the Tree panel, Enter on an `.app` re-roots the tree; Shift+Enter opens it.
- An invalid panel filter is ignored with a footer note. Deleting a mask group used
  elsewhere asks first.
- Other keys: Shift+arrows/PgUp/PgDn/Home/End mark a range, Alt+Num* inverts files
  and unmarks folders, Shift+F11 toggles sort groups, ⌘⇧S toggles human-readable
  sizes, Ctrl+[ / Ctrl+] insert the left/right panel path (with Shift: active /
  inactive), Ctrl+↑/↓ and ⌘↑/↓ walk command history.

## F7

Successful creation selects the new folder without entering it. For nested paths,
the final folder's parent is shown. An explicitly created hidden folder is shown
even when hidden files are disabled. Failure preserves the selection.

## Operation progress

Copy/Move/Delete and ZIP creation/extraction/testing display total/current-file
progress, bytes/items, speed, elapsed time, and ETA. Prescanning does not traverse
symlink directories; with **Symbolic links: Copy contents** the totals grow as
followed folders are read, and a same-volume folder move into another folder walks
it once to fix links (cancellable). Bytes represent logical work: atomic
rename and Skip count immediately. Concurrent source changes can invalidate
initial totals.

Esc requests cancellation directly. Reads check between blocks; deletion checks
between entries. Incomplete temporary copies are removed; completed work is not
rolled back. A short syscall, sync, or publication is not interrupted midway.
Search, tree, and size scans display a path and indeterminate indicator while the
total is unknown. Short external/helper actions also use indeterminate progress.

## Validation

- `go test -race ./...`, `go vet ./...`, JavaScript resize/backpressure regression.
- Real PTY execution with spaces, quotes, and semicolons in filenames; correct cwd
  and output; independent PTYs, including closing one while another runs.
- Independent window settings/state, individual close, cancelling global quit
  after another window's confirmation, internal screens, and panel restoration.
- Viewer/editor dispatch, associations, alternative and forced internal modes.
- Fast find, Unicode, masks, paste, cycling, and F7 with nested/hidden folders.
- Byte/item totals, cancellation, source preservation, no publication of cancelled
  copies, and progress layout at minimum/maximum window sizes.

Universal arm64/x86_64 builds target macOS 12+. At this audit, Cocoa automation
returned `Invalid app`; multiple native window layout, real Option/dead-key input,
and specific GUI viewer/editor launches lacked end-to-end confirmation. This is
not a claim of complete Far compatibility.

## Session persistence (0.6)

Saved state includes every open window's Cocoa-point frame, both panel directories,
view/sort modes, active panel, cursor/top row, and focused window. Atomic writes
are limited to twice per second on changes and forced before quit. Session saving
is independent of Save setup/automatic preference saving. Cmd+Q saves the window
set before closing it. The last individually closed window becomes the startup
window; other individually closed windows are not restored.

Restored frames are constrained to available screens, including disconnected
monitor cases. Missing/inaccessible directories fall back to the nearest accessible
parent. Corrupt sessions do not prevent startup. Explicit CLI directories bypass
restoration. Fullscreen state, PTY processes, documents, and unsaved buffers are
not persisted.

Model tests cover multiple windows, focus, cursor positions, geometry, saving
without AutoSave, individual/last-window close, global quit, invalid coordinates
and JSON, missing directories, and explicit CLI paths. Native restoration after
relaunch was not verified with macOS automation in this audit.

Only one instance owns the window session (`session.lock`); a second instance
reports "Another FarshMac instance owns the window session…" and does not save it.
If saving settings fails at quit, the app asks "Settings were not saved … Quit
anyway?". "Save setup on exit" saves once, at quit.

## Tree fix (0.6.1)

A cancellable request replaces synchronous recursive Panel.Load. The worker owns
the result and sends an immutable set to the engine through a buffered channel.
Another Load cancels and detaches the old request. Sort settings maps are copied;
panel swaps do not change the worker's destination. Branch labels are prepared in
the background; drawing reads visible rows only. Session persistence accounts for
pending cursors; F7 selects the created directory after loading.

Traversal uses ReadDir(256), skips ordinary files without Info/stat, and does not
read below TreeDepth. Limits/access errors appear in status; Alt+F10 also marks
the title `(partial)`.

A reproduced width discrepancy counted 😀 as one column in bundled xterm.js and
two in Go. ANSI drawing explicitly positions the next cell after Unicode text
and clears wide-character continuation cells, preventing shifted borders and
stale frames while scrolling emoji filenames.

Tests cover 6,000 files, 1,800 directories, traversal limits, cancellation between
batches, inaccessible/hidden directories, cyclic links, blocked workers during
resize/Tab/swap/navigation, session/F7, and different window sizes. Actual bundled
xterm parses ANSI frames for cell-by-cell assertions on emoji, CJK, combining
characters, and redraw over previous frames. Native visual verification remains
an automation limitation.

## Unified cancellation (0.7)

Every background task has an Operation with cancellation context. Esc runs before
document/operation-dialog handling; it neither closes the editor nor modifies
text during save. Cancellation/window close releases Overwrite waits. Helpers
use CommandContext.

Native startup uses NewAsync: ordinary panel reads, document open/save, search/
replace, and transformations run outside the engine queue. Synchronous New remains
available for direct model use. Background results apply only on the engine queue
and only if not cancelled. Closing a window releases descriptors from unfinished
results. A successfully published save reconciles editor state even after late
cancellation, but subsequent close/quit is suppressed.

Search, size, copy, delete, and ZIP traversal use ReadDir(256) with cancellation
checks between entries, avoiding whole-directory reads/sorts. Cleanup, the current
syscall, or a library step such as regex search may finish after Esc; stale results
are not applied. Esc in the terminal remains PTY input.

Regressions cover cancellation with an open editor and no dialog, input blocking
until cancellation finishes, real sleep termination, conflict cancellation/window
close, panel/editor read cancellation, preserving the old directory after failure,
F7/completion after background loading, cancelled replacement, normal/late-cancelled
save, Save all during quit, cancelled traversal, and no publication or temporary
files after cancelled Save/Create.

## Folder-size retention (0.19.1)

In upstream [filelist.cpp](https://github.com/FarGroup/FarManager/blob/master/far/filelist.cpp),
`CountDirSize` stores calculated values in panel items and marks them precise.
`MoveSelection` transfers them during list refresh and marks them outdated.
This is panel state, not a time-based or persistent directory-size database.
Ordinary successful directory changes do not provide a cross-directory size cache.

FarshMac retains the outdated-value behavior and additionally keeps bounded
previous-directory snapshots in memory: up to 128 directories, 16,384 results,
and 2 MiB estimated storage per panel, evicting the least recently used snapshots.
The current panel's map is separate. Keys isolate local paths, remote server/user,
archive index, and .DS_Store measurement policy. Nothing is persisted on disk.
Removed entries are pruned when a directory listing is installed. `~` means a
cached value may be outdated, not a lower bound; `>=` means a scan was incomplete.
Deep external changes are not watched. Cmd+Option+F3 (Ctrl+Alt+F3 alias) recalculates
all displayed folders, preserving marks. F3 recalculates selected/current folders.
Both fresh and cached values participate in size sorting.
