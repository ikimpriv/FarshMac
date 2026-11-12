> Historical menu audit through 0.13. Later features are recorded in the
> [README](README.md) and [changelog](CHANGELOG.md).
> Since 0.7, Esc cancels long operations directly without another confirmation;
> see [behavior notes](BEHAVIOR_PARITY.md). The same document covers windows,
> Enter, external viewers/editors, fast find, F7 and progress introduced in 0.5.

# FarshMac 0.13 menus

Menu shortcuts use macOS symbols: ⌘ Command, ⌥ Option/Alt, ⇧ Shift, ⌃ Control
(for example ⌥F7, ⇧F4, ⌃1…9). Items that do not apply to a remote or archive panel
are shown grayed. Lists accept PageUp/PageDown, the mouse wheel, repeated Alt+letter
to cycle items with the same hotkey, and type-to-filter (Esc clears the filter).
Prompts have a movable caret, Delete, and insertion at the caret; pasted text in a
form field becomes one line. Secret fields (passwords) are never copied to the
clipboard: ⌘C is ignored with "Secret fields are not copied", ⌘X clears the field.

Compared with the official [Far Manager help](https://github.com/FarGroup/FarManager/blob/master/far/FarEng.hlf.m4),
sections LeftRightMenu, FilesMenu, CmdMenu, OptMenu and ConfirmDlg. This adapts a
working command set to macOS; it does not reproduce every original setting,
archive plugin or extension.

## Left / Right

- Brief (three columns), Medium (two), Full, Wide and Detailed. Detailed uses the full window width. A separator precedes Info / Tree / Quick view, followed by another separator and panel commands. Descriptions, Long descriptions, File owners, File links and Alternative full were removed; saved settings for those modes normalize to Full.
- Info panel: file counts, volume space, host and directory description.
- Tree panel (0.6.1): background tree with branch lines, scan counter and Esc cancellation. Limits: configured depth, 50,000 directories and 1,000,000 entries per scan. Partial trees are marked. Symlink directories are excluded. Enter opens the selected directory; Alt+F10 uses the same traversal.
- Quick view: first 16 KiB of a file or up to 100 directory entries from the opposite panel. Reads run in the background, never during drawing.
- Sort modes (⌃F12): name, extension, size, modification time, reverse order, case sensitivity, **Mix directories and files**, selected first and mask-based sort groups. ✓ marks the current mode and every enabled toggle. Remote panels refuse sorts that need local metadata.
- Quick view is also available from the menu on remote panels; Info and Tree need a local folder. Ctrl+L / Ctrl+T toggle Info / Tree in the other panel; there are no separate Commands items for them because the views are in Left/Right.
- Panel on/off, Re-read and Change volume. Cmd+L changes directory; `..` goes up; Home is available in the volumes list.
- Show long names is unnecessary because the macOS model does not use DOS 8.3 names.

## Files

- View / folder size, Edit, New file, Copy, Rename or move, Link, Make folder, Move to Trash (F8), and Delete permanently (Shift+F8 / Shift+Delete).
- Existing directories merge during copy/move. File conflicts offer **Overwrite / All / Skip / Skip all / Cancel**. Replacements are staged first; destination symlinks are not followed, except that a symlink to a folder given as the destination acts as that folder. Cross-volume moves copy each file with metadata, ask before any loss, then delete that source file.
- Symbolic (relative by default) and hard links, created inside an existing folder given as the name; nested directory creation and size calculation without following directory symlinks. Local copy/move offers Smart / As is / Copy contents for symbolic links and keeps hard-link groups (see [file operations](docs/FILE_OPERATIONS.md)).
- ZIP/JAR/WAR, TAR, tar.gz/tgz, tar.bz2/tbz/tbz2, tar.xz/txz and 7z: create, extract to a new directory, list and test. RAR/CPIO: read/extract. Internal links are supported when confined to the destination. Early versions exposed archive actions on Enter; read-only archive panels were added in 0.11. Archive updates, full-extraction merging, passwords and multi-volume archives remain unsupported. See [archive details](ARCHIVE_SUPPORT.md).
- File attributes: POSIX mode 000–777 and local modification time; symlinks are not modified. ACLs, BSD flags, owner/group and independent access/creation times were absent from this dialog.
- Apply command replaces `{}` with each selected file's shell-quoted path and runs in the embedded terminal.
- Describe files supports basic `descript.ion`, including quoted names. Copy/Move/Delete do not automatically move/delete descriptions.
- Select group, Deselect group, Invert selection and Restore selection. Masks support `*`, `?`, `[abc]`, comma/semicolon-separated alternatives, `|` exclusions and `<group>` expansion.
- Wipe is deliberately absent: overwriting a file cannot guarantee erasing every copy on APFS/SSD.

## Commands

- Find file / Find folder: masks, subdirectories, containing text and case sensitivity. Text is streamed; results are capped at 10,000, with unreadable paths counted. Archive search, regex and all Far encodings were not implemented in this search dialog.
- Command history (⌥F8) inserts without immediate execution. File view/edit history (⌥F11) and Folder history (⌥F12) list recent entries; folder history remembers whether an entry is local, remote or inside an archive. Far's folder shortcuts are **Bookmarks…**: ten window-scoped slots 0–9 shared with the viewer/editor (⌘⌥0…9 saves the folder and the file under the cursor, ⌘0…9 jumps; no Ctrl or Shift variants, since macOS takes Ctrl+digit for switching desktops and ⌘⇧3/4/5 for screenshots). They are not saved and are not shared between windows.
- Find folder has no shortcut, as in Far.
- Folder tree, Swap panels, Panels on/off / terminal and Compare folders. Comparison is nonrecursive and uses names, sizes and times, not content hashes.
- **F2** opens the user menu. Edit user menu and File associations configure commands with `{}` substitution, saved in `setup.json`. Enter applies associations; opening through macOS is also available. FarMenu.ini, hierarchical user menus and Far's complete substitution language are unsupported.
- Edit sort groups (the same prompt as Options → Advanced → Sort groups), File panel filter and Screens list.
- **Last messages…** shows the last 20 status messages, newest first; clicking the status line does the same. A status line too long for the window ends with `…`.
- Task list snapshots processes through `ps`; use the terminal for process management. Mounted volumes replaces Windows drive/hotplug lists. Eject was not added.
- Full screen (⌃⌘F) replaces legacy fixed video modes; it duplicates the native View menu item so the Far-style menu stays self-contained.
- Plugin commands is absent until a compatible plugin API exists.

## Options

- General: font size (10–24, also ⌘+ / ⌘−), key bar, wrapping navigation, dialogs, confirmations and Save setup on exit. **Confirm Move to Trash** covers F8 only; permanent deletion always asks. The former Dialog, AutoComplete, System, Interface, Menu, Confirmations and Command line settings forms were removed: their fields are in General and Terminal settings.
- Panel: hidden-file visibility (also ⌘⇧. / Ctrl+H, saved with the setup), independent Hide and ignore .DS_Store, highlighting and human-readable/exact byte sizes. Hiding affects display only. The .DS_Store filter applies to copy/archive creation; moving or deleting a selected directory processes it completely.
- Terminal: bash/sh/zsh (bash by default), history length and completion. The shell applies to new windows and after the current shell exits.
- Viewer and Editor: corresponding document options.
- Advanced: colors, Info panel, tree depth, descriptions, masks, sorting and encodings.
- Full Disk Access, Save setup and Keyboard reference.

Settings are stored in `~/Library/Application Support/FarshMac/`:

- `setup.json`: settings, panel modes/paths and histories, written by Save setup
  (⇧F9). Save setup merges histories with the copy on disk and asks "Overwrite?"
  if another FarshMac instance changed the file. Automatic saving ("Save setup on
  exit") is disabled by default and saves once, at quit; if saving fails then, the
  app asks "Settings were not saved … Quit anyway?".
- `session.json`: windows and directories, saved automatically since 0.6,
  independently of Save setup. Only the instance holding `session.lock` saves it; a
  second instance says "Another FarshMac instance owns the window session…".
- `connections.json`: network connection profiles, saved immediately when a
  profile is added, edited or deleted, without Save setup. Passwords and
  passphrases are not stored. A profile's folder is updated on Save setup and on
  disconnect.

A damaged file is renamed to `<name>.broken-<time>` and defaults are used. If a
file is missing, FarshMac reads the one from the pre-rename `Far Go/` folder
without modifying it; writes always go to `FarshMac/`. Settings changed in one
window apply live to all windows.

## Network connections

Commands → Network connections lists saved profiles. Several profiles per server
are allowed; a profile is identified by server plus folder. F8/Delete deletes the
selected profile after confirmation. Internationalized (IDN) host names work.

- SFTP host keys are checked against `~/.ssh/known_hosts`. An unknown key offers
  **Trust once**, **Trust and save** (appends to `~/.ssh/known_hosts`) and Cancel.
  A changed or revoked key shows "SSH host key changed" / "revoked" with the
  fingerprints and the known_hosts file:line, with OK only.
- Password login also answers keyboard-interactive password prompts.
- FTPS with an untrusted certificate can be trusted once by fingerprint.
- Plain FTP warns that credentials are sent unencrypted; **Remember choice** keeps
  the answer for that profile.
- Other connection failures show "Connection failed" with Edit / Retry / Cancel.

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
