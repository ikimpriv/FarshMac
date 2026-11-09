> Historical audit of versions 0.2/0.3. Later menu, confirmation, archive, search,
> settings and limitation notes are in [MENU_PARITY.md](MENU_PARITY.md).
> Many gaps below were addressed in later releases; consult [CHANGELOG.md](CHANGELOG.md).

# FarshMac 0.3 compared with Far Manager

Date: November 7, 2026. The implementation was compared with the official
[Far Manager help](https://github.com/FarGroup/FarManager/blob/master/far/FarEng.hlf.m4),
particularly PanelCmd, CmdLineCmd, FuncCmd, Menus, Viewer, Editor and Contents.
This is a major-feature audit, not validation of every shortcut or third-party plugin.
A command available in zsh does not count as an implementation of Far's corresponding UI.

## Fixed in 0.2

- Each panel remembers the selected name, scroll position and marked entries per directory. Returning through `..` restores them while rereading entries; Backspace navigation was removed in 0.3. Restoration uses names when files are added and clamps the cursor if an entry disappears.
- F9 / Cmd+9 opens the active panel's main menu. Arrows, Tab, Enter, Esc and the mouse operate implemented menu actions.
- Cmd+Enter appends the current filename, shell-quoted for zsh, without executing it. Cmd+L remains Change directory.

## Missing or partial features at the audited versions

| Area | Implementation and remaining work |
| --- | --- |
| F2 / user menu | Missing in 0.3: F2 was Refresh. Needed user commands, directory-local menus and filename substitutions. |
| Panel command line | Partial: append-only input, Backspace, session history and paste. No cursor movement, middle editing, selection, word deletion, completion or history search. Full zsh editing remained available in terminal mode. |
| Path insertion | Current filename only; missing dedicated full-path, passive-panel name, panel-path and marked-file-list commands. |
| Enter / associations | Enter opened the internal viewer. Missing configurable file-type actions, executable launch and external viewer/editor selection. |
| Fast find | Missing prefix search and cycling through matches. |
| Find files/folders | Missing tree/content/mask/result/attribute search dialogs. |
| Sorting / filters | Fixed directories-first, case-insensitive name sort. Needed size, date, extension, reverse order, groups and filters. |
| Group selection | Space/Insert and marked-item operations existed. Missing masks, inversion, extension selection and restore-selection command. |
| Panel types / views | One file-panel layout. Missing tree, info, quick view, brief/detailed modes, custom columns, single-panel hiding, directory sizes and folder comparison. |
| Menus / settings | Working but reduced F9 menu; Options only showed keyboard help. Missing settings editing, themes, highlighting rules, key mapping and saved configuration. |
| Copy / move | Recursive copy and non-overwriting rename. Missing conflict dialogs, merge/overwrite/append, progress, cancellation, queue and cross-volume moves. ACLs, xattrs, resource forks, ownership and hard-link topology were not preserved. |
| Delete / attributes / links | Permanent deletion with confirmation; copied links retained. Missing Trash, link creation UI, permission/date editing, descriptions and Apply command. |
| Archives | Updated in 0.11: create/list/test/extract ZIP, TAR, tar.gz, tar.bz2, tar.xz and 7z; read RAR/CPIO. Enter opens a read-only archive panel, F5 extracts selected members, F3 views files. No archive updates, nested panels, passwords or multi-volume support. See [formats](ARCHIVE_SUPPORT.md). |
| Viewer | Expanded in 0.3: Text/Hex/Dump, search, code pages, wrapping, goto, bookmarks, F6 and paged binary access. Remaining gaps included large texts, fuzzy search, exact Far semantics and persistent state; see [viewer/editor notes](VIEWER_EDITOR_PARITY.md). |
| Editor | Expanded in 0.10: isolated Chroma highlighting capped at 1 MiB, configurable indentation, bounded undo and incremental line indexing. Cursor/selection, rectangular blocks, undo/redo, search/replace, encodings/EOL, Save As, new files and F12 were present. Advanced Far parity remained incomplete. |
| History / state | Directories and commands remembered within a run. Missing persistent directory/view/edit histories, folder bookmarks, window positions and restart restoration. |
| Plugins / macros | No plugin API, F11, Lua or keyboard macro recording. F12 for internal documents arrived in 0.3. Windows DLL plugins cannot run directly on macOS. |
| Networking | No VFS panels. Running SSH/SFTP/FTP utilities in the shell was not a replacement for network panels. |
| Mouse / Unicode / help | Selection and double-click existed. Missing panel-to-panel file drag-and-drop, reliable widths for every Unicode grapheme and searchable contextual help. |

## Known implementation limitations at the time

- Returning through Cmd+O synchronized cwd unconditionally, potentially replacing a manually selected panel directory with an older shell cwd. Synchronization needed to track actual changes.
- Directory loading was synchronous; large or slow directories could delay the UI. External changes required a manual refresh.
- New search forms had cursors and field navigation, but older one-line dialogs remained append-only. Full field selection was incomplete.
- Exiting the shell required restarting the application. This historical limitation was removed in 0.18.7.
- Model and PTY tests passed. Native automation was limited by `Invalid app`; exact visual parity and all WebKit focus scenarios were unverified.

## Proposed implementation order at the time

1. Command/dialog editing, correct cwd synchronization, fast find, sorting, mask selection and a full F2 menu.
2. Tree/content search, associations, persistent histories/settings and the separate viewer/editor parity matrix.
3. Reliable file operations with conflicts, progress, cancellation, Trash, macOS metadata and cross-volume moves.
4. Additional panels, archives, network VFS, portable plugin APIs and macros; adapt Windows-specific functions to macOS volumes and APIs.

The comparison uses the official BSD-licensed help distributed with Far's sources.
Implementation evidence is in `internal/ui`, `internal/fsops`, `internal/terminal`
and `internal/native`. In 0.3, F10 stopped quitting the application and Backspace
stopped navigating to the parent directory.
