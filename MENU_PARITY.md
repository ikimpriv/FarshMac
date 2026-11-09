> Historical menu audit through 0.13. Later features are recorded in the
> [README](README.md) and [changelog](CHANGELOG.md).
> Since 0.7, Esc cancels long operations directly without another confirmation;
> see [behavior notes](BEHAVIOR_PARITY.md). The same document covers windows,
> Enter, external viewers/editors, fast find, F7 and progress introduced in 0.5.

# FarshMac 0.13 menus

Compared with the official [Far Manager help](https://github.com/FarGroup/FarManager/blob/master/far/FarEng.hlf.m4),
sections LeftRightMenu, FilesMenu, CmdMenu, OptMenu and ConfirmDlg. This adapts a
working command set to macOS; it does not reproduce every original setting,
archive plugin or extension.

## Left / Right

- Brief (three columns), Medium (two), Full, Wide and Detailed. Detailed uses the full window width. A separator precedes Info / Tree / Quick view, followed by another separator and panel commands. Descriptions, Long descriptions, File owners, File links and Alternative full were removed; saved settings for those modes normalize to Full.
- Info panel: file counts, volume space, host and directory description.
- Tree panel (0.6.1): background tree with branch lines, scan counter and Esc cancellation. Limits: configured depth, 50,000 directories and 1,000,000 entries per scan. Partial trees are marked. Symlink directories are excluded. Enter opens the selected directory; Alt+F10 uses the same traversal.
- Quick view: first 16 KiB of a file or up to 100 directory entries from the opposite panel. Reads run in the background, never during drawing.
- Sort modes: name, extension, size, modification time, reverse order, case sensitivity, directories first, selected first and mask-based sort groups.
- Panel on/off, Re-read and Change volume. Cmd+L changes directory; `..` goes up; Home is available in the volumes list.
- Show long names is unnecessary because the macOS model does not use DOS 8.3 names.

## Files

- View / folder size, Edit, New file, Copy, Rename or move, Link, Make folder, Move to Trash (F8), and Delete permanently (Shift+F8 / Shift+Delete).
- Existing directories merge during copy/move. File conflicts offer **Overwrite / All / Skip / Skip all / Cancel**. Replacements are staged first; destination symlinks are not followed. Cross-volume moves copy before removing the source.
- Symbolic/hard links, nested directory creation and size calculation without following directory symlinks.
- ZIP/JAR/WAR, TAR, tar.gz/tgz, tar.bz2/tbz/tbz2, tar.xz/txz and 7z: create, extract to a new directory, list and test. RAR/CPIO: read/extract. Internal links are supported when confined to the destination. Early versions exposed archive actions on Enter; read-only archive panels were added in 0.11. Archive updates, full-extraction merging, passwords and multi-volume archives remain unsupported. See [archive details](ARCHIVE_SUPPORT.md).
- File attributes: POSIX mode 000–777 and local modification time; symlinks are not modified. ACLs, BSD flags, owner/group and independent access/creation times were absent from this dialog.
- Apply command replaces `{}` with each selected file's shell-quoted path and runs in the embedded terminal.
- Describe files supports basic `descript.ion`, including quoted names. Copy/Move/Delete do not automatically move/delete descriptions.
- Select group, Deselect group, Invert selection and Restore selection. Masks support `*`, `?`, `[abc]`, comma/semicolon-separated alternatives, `|` exclusions and `<group>` expansion.
- Wipe is deliberately absent: overwriting a file cannot guarantee erasing every copy on APFS/SSD.

## Commands

- Find file / Find folder: masks, subdirectories, containing text and case sensitivity. Text is streamed; results are capped at 10,000, with unreadable paths counted. Archive search, regex and all Far encodings were not implemented in this search dialog.
- Command history inserts without immediate execution; View/edit history, Folder history and Folder shortcuts provide ten slots.
- Folder tree, Swap panels, Panels on/off / terminal and Compare folders. Comparison is nonrecursive and uses names, sizes and times, not content hashes.
- **F2** opens the user menu. Edit user menu and File associations configure commands with `{}` substitution, saved in `setup.json`. Enter applies associations; opening through macOS is also available. FarMenu.ini, hierarchical user menus and Far's complete substitution language are unsupported.
- Edit sort groups, File panel filter and Screens list.
- Task list snapshots processes through `ps`; use the terminal for process management. Mounted volumes replaces Windows drive/hotplug lists. Eject was not added.
- Full screen replaces legacy fixed video modes.
- Plugin commands is absent until a compatible plugin API exists.

## Options

- General: font, key bar, wrapping navigation, dialogs, confirmations and Save on exit.
- Panel: hidden-file visibility (Cmd+Shift+.), independent Hide and ignore .DS_Store, highlighting and human-readable/exact byte sizes. Hiding affects display only. The .DS_Store filter applies to copy/archive creation; moving or deleting a selected directory processes it completely.
- Terminal: bash/sh/zsh (bash by default), history length and completion.
- Viewer and Editor: corresponding document options.
- Advanced: colors, Info panel, tree depth, descriptions, masks, sorting and encodings.
- Full Disk Access, Save setup and Keyboard reference.

Settings are stored in `~/Library/Application Support/FarshMac/setup.json`.
Save setup persists settings, panel modes/paths and current histories; automatic
setup saving is disabled by default. Since 0.6, windows and directories are saved
automatically in `session.json`, independently of Save setup.

The interface is English-only; there is no placeholder Languages selector.
Plugin configuration, every original dialog setting, arbitrary column editing and
color groups are not implemented. Completion handles the final unquoted path prefix,
not zsh's complete syntax or completion engine. Full shell behavior is available via Cmd+O.

## Confirmations

Typing `DELETE` or `QUIT` was removed. Deletion, shell closure, saving and overwrite
use ordinary buttons: Tab/arrows select, Enter confirms, Esc cancels, and mouse input
works. Buttons wrap in narrow windows. Early releases used permanent deletion only;
current local deletion defaults to Trash, with permanent deletion explicitly labeled.

## Resize

- Go handlers and frame construction run on a separate serial queue. The Cocoa main thread does not wait for filesystem I/O or grid construction.
- The timer runs during live resize through NSRunLoopCommonModes.
- Size changes are coalesced over 60 ms; unchanged dimensions do not trigger a new frame. Cell measurement does not depend on xterm's intermediate resize dimensions.
- A frame is sent only after both xterm instances acknowledge the previous frame. PTY output per poll is bounded; hidden panels are not redrawn for every terminal input.
- Long viewer lines are not scanned past the visible right edge. Panels beneath a document are not drawn. File metadata is cached during panel loading.
- Builds are published as a separate bundle without overwriting the executable of a running instance.

Validation included Go race tests, vet, JavaScript syntax, 1,000 resize events,
stable geometry, xterm backpressure, dimensions from 60×15 to 300×120, long lines,
menu scrolling, buttons and all panel modes. File-operation coverage included
overwrite/skip/cancel, directory merging, symlink protection, ZIP round trips and
traversal, search and settings.

Native automation could attach to the old 0.3 window but stopped finding the 0.4
bundle (`Invalid app` / `Running application not found`), although Finder opened it.
Thus the resize changes were supported by code/regression tests; extended native
live-resize testing and separate Intel execution remained unverified for this audit.
