<!-- Historical implementation notes; use the root README for current setup. -->

# Development history (through 0.18.0)

The first working version was a standalone Go file manager with a Far Manager-style
interface in a native macOS window, without requiring Terminal.app. This is a
rewrite, **not a complete Far Manager port or a compatible replacement for its
plugin platform**. Pixel-perfect parity has not been verified. Names and paths in
this document use the current FarshMac branding; feature descriptions retain their
historical versions. Later fixes are in [CHANGELOG.md](../CHANGELOG.md).

## Synchronization, folder sizes, and Info panel (0.18.0)

- After shell `cd`, Cmd+O opens the active panel in the new directory, even when
  switching before the next prompt. A late shell response does not override later
  manual panel navigation.
- Following [Far Manager's implementation](https://github.com/FarGroup/FarManager/blob/master/far/filelist.cpp),
  F3 on a folder calculates marked folders, or the current folder when no folders
  are marked, and stores their sizes in the panel. Files → Calculate all folder
  sizes processes every displayed folder without changing selection. Local,
  archive, FTP, and SFTP folders are supported. Calculated sizes participate in
  size sorting (Cmd+F6 / Ctrl+F6). F3 on `..` calculates the current directory.
- Esc stops calculation and retains completed results. `>=` indicates incomplete
  sizes after access errors. Traversal does not follow symlinks. Hidden contents
  count; the .DS_Store exclusion preference still applies. Re-read / Cmd+R clears
  calculated sizes.
- Info panel / Ctrl+L displays computer/user, filesystem, capacity/free space,
  volume label/UUID, physical/compressed memory, swap, power/battery, directory
  description, and information about the opposite archive/network panel. macOS
  metrics replace Windows-only fields in [Far help](https://github.com/FarGroup/FarManager/blob/master/far/FarEng.hlf.m4).
  Memory/power refresh in the background every two seconds. Wheel/PgUp/PgDn scroll;
  Ctrl+F12 configures sections; F3/F4 views/edits the description.
- Terminal output uses larger bounded batches and overlaps archive writes with
  rendering. Queues and scrollback remain bounded. A 30,000-colored-line test
  improved from 953 to 358 ms and from 605 to 189 xterm.write calls. This measures
  the output handler, not arbitrary command performance.

## Terminal (0.17.2)

- Cmd+O opens the shell in the active panel's local directory. A running command
  delays directory synchronization until completion. Pending input is retained;
  the internal cd does not enter history.
- A simple fast prompt is the default. Profiles, PATH, and aliases load, but custom
  prompt hooks are disabled, avoiding delays such as kube-ps1 in PROMPT_COMMAND.
  Options → Terminal settings → Use custom shell prompt / hooks restores them for
  new windows. Save setup persists the choice.
- Switching window mode does not wait for another output batch. Shell startup has
  a visible status indicator.

## SFTP fixes (0.17.1)

SSH negotiation prefers host-key types already stored in known_hosts, including
hashed entries. Verification remains strict: mismatches/revocation are never
bypassed. User known_hosts/known_hosts2 and system ssh_known_hosts/ssh_known_hosts2
are read.

An empty SSH private key field tries `~/.ssh/id_rsa`, `id_ecdsa`, `id_ed25519`, and
ssh-agent. Encrypted keys accept a dialog passphrase or an agent-loaded key.
Explicit key paths take priority. Private keys/passphrases are not stored in
profiles. Full ~/.ssh/config parsing, Host-alias IdentityFile handling, and
hardware keys outside the agent are not supported.

## Additional Far shortcuts (0.17)

- Ctrl+Q toggles the inactive panel's Quick view, restoring its former mode and
  visibility. Ctrl+L selects Info; Ctrl/Cmd+T Tree; Ctrl/Cmd+P toggles the inactive
  panel; Ctrl/Cmd+B toggles the function-key bar.
- Ctrl+O complements Cmd+O for panels/terminal. Ctrl+R rereads; Ctrl+H toggles
  hidden files. Cmd+H retains its macOS role.
- Ctrl/Cmd+F3…F11 sorts by name, extension, modification time, size, unsorted order,
  creation time, access time, description, and owner. Repeating reverses direction.
  F8…F11 require local metadata. Ctrl/Cmd+F12 opens sort modes; Shift+F11 toggles
  sort groups; Shift+F12 puts selected entries first.
- Shift+Up/Down, PageUp/PageDown, Home/End selects ranges. Shift+F5/F6 copies/moves
  only the cursor item, retaining other marks.
- Shift+F1/F2/F3 creates/extracts/opens archive commands. Shift+F9 saves setup.
  Alt+F6 opens links; Alt+F9 zooms/restores the native window; Alt+F11/F12 opens
  view/edit and folder histories.
- Ctrl/Cmd+PgUp opens the parent; modified PgDown opens a folder/archive without
  executing a file. Num5 views. Ctrl/Cmd+Left/Right changes panel widths with an
  empty command line; Ctrl+Num5 restores equal widths.
- Ctrl+E/X selects previous/next commands; Ctrl/Cmd+Y clears input. Ctrl+Enter/J
  inserts the current filename; Ctrl+Shift+Enter uses the other panel. Ctrl/Cmd+F
  inserts a full path; Ctrl/Cmd+; uses the other panel's path. Ctrl+[ / ] inserts
  left/right paths; Ctrl+Shift+[ / ] inserts active/inactive paths.
- Ctrl+Insert copies the command line, or selected names if empty. Ctrl+Shift+Insert
  copies names; Alt+Shift+Insert full paths; Ctrl+Alt+Insert resolved symlink paths
  (URLs for network entries). Shift+Insert pastes text.
- Ctrl/Cmd+Shift+S toggles bytes/human-readable sizes.

Existing assignments remain: Ctrl+1…9 folder slots, Cmd+1…9 function keys, Cmd+L
goto path, Cmd+Q quit, Cmd+Shift+[ / ] internal screens, Shift+F8 permanent delete.
Panel shortcuts do not intercept editor/dialog/terminal input, except Ctrl/Cmd+O.
Windows-only features (8.3 names, UNC, NTFS plugins, console modes), macros, and
unavailable plugin commands are not simulated. Far's full command-line editor
(cursor, block selection, word operations) is not ported. Panel height remains
fixed; Ctrl+Up/Down retains history navigation.

## Selection by mask

Num + / Num − selects/deselects by mask; Num * inverts selection. Cmd/Ctrl+Num +/−
selects by the current extension; Alt+Num +/− by basename. Shift+Num +/− selects/
deselects all files. Cmd/Ctrl+Num * also inverts folders; Alt+Num * inverts files
and clears folder selection. Cmd/Ctrl+M restores the previous selection.

Without a keypad, ordinary +, −, * work when the command line is empty; otherwise
they enter text. Panel settings → Select folders with file masks defaults off.
Masks support `*`, `?`, `[a-z]`, comma/semicolon lists, `|` exclusions, and groups
such as `<source>` and `<arc>`; example: `*.go;*.md|*_test.go`. Full Far Perl-regex
mask compatibility is not implemented.

## Graphical Quick view

Left / Right → Quick view previews the opposite panel's selection: a thumbnail
(JPEG, PNG, GIF, TIFF, BMP, HEIC/HEIF, WebP where ImageIO supports it) or the first
PDF page. It displays name, file size, date, image dimensions or PDF page count.
Animations use their first frame. Text files/folders retain text previews.

Decoding runs in a separate process with a four-second timeout and Esc cancellation.
Thumbnails are capped at 768 pixels on the longer side; source images never enter
WebKit. Rapid selection changes cancel obsolete work. Images hide under menus/
dialogs and in editor/terminal mode. Network/archive files must first be copied
locally for graphical preview.

## Panel folder shortcuts

Ctrl+Shift+1…9 stores the current directory; Ctrl+1…9 opens the slot in the active
panel. Slots persist automatically and appear in Commands → Folder shortcuts.
Local, archive, and FTP/SFTP folders work. Disconnected servers open a prefilled
connection dialog; passwords are not saved. Editor/viewer/terminal key behavior
is unchanged.

## FTP, SFTP, and parallel copying (0.17.1)

- **F9 → Commands → Network connections…** connects FTP, explicit FTPS, or SFTP
  (SSH): `ftp://host/path`, `ftps://host/path`, `sftp://user@host:22/path`; `ssh://`
  is accepted. SSH is a remote panel, without installing system SSHFS.
- Authentication supports passwords, encrypted/unencrypted private keys, and
  ssh-agent. FTP without a username uses anonymous. Passwords/passphrases are not
  saved. Profiles and initial directories save automatically after connection;
  reconnect by selecting a profile after relaunch.
- Unknown SSH host keys require fingerprint confirmation and are trusted only for
  that connection. Known-key mismatches are rejected. FTPS validates certificates.
- Enter opens remote directories. F3 downloads a temporary viewer copy; F4 edits
  a temporary copy and uploads on save. Changed source size, mtime, or permissions
  prevent saving. Save As writes locally.
- **F5 → Parallel transfers (1–8)** processes up to eight files, default four.
  Local copies, uploads, downloads, and server-to-server transfers through the app
  are supported. Workers have independent sessions and 256 KiB buffers; the queue
  is bounded. A single file is not split across workers. Esc cancels and closes
  network connections.
- Completed temporary files publish by rename, with another target check. Completed
  copies survive cancellation. An unreachable server may retain `.farshmac-part-*`
  files from unfinished transfers.
- F7 creates/selects a directory. F6 renames/moves within one server without replacing
  an existing target. Between servers or local/remote panels, use F5 then delete.
  Remote deletion is permanent and always confirmed.
- Metadata preservation supports SFTP mode and mtime where the server allows it.
  Incomplete owner/group, ACL, xattrs, BSD flags, or creation-time preservation
  prompts **Cannot preserve metadata → For all / Cancel**. FTP has no portable
  permissions preservation. SFTP preserves symlinks; FTP does not.
- The terminal remains local. SSH config/ProxyJump, system mounts, remote shell,
  automatic reconnection, and partial-transfer resumption are absent. Archive
  operations, recursive search, system open/viewer/editor, and local attributes
  require a local copy first.

## Copy permissions and attributes (0.14.1)

**F5 Copy** has two independent options, both enabled initially:

- **Preserve permissions (mode, owner, group, ACL)**. When off, new files/folders
  use ordinary permissions subject to umask and destination inheritance.
- **Preserve attributes (xattrs, flags, timestamps)** preserves macOS extended
  attributes, flags, and creation/modification/access times.

Options apply recursively and to new/replaced entries during merges. Existing
destination directories keep their metadata. Confirming Copy remembers the choice;
Save setup / Save setup on exit persists it. Archive copies can preserve only
metadata restored by the extractor.

Preservation failure offers **Copy anyway / For all / Cancel**. Cancelling an
overwrite retains source and previous destination. Moves always try to preserve
all metadata. APFS cloning is used when both groups are enabled; disabling either
uses 256 KiB buffered copying so clonefile cannot silently transfer excluded metadata.

## Hidden files and cross-volume moves (0.14)

- **Options → Panel settings → Hide and ignore .DS_Store** independently filters
  panels, search, copy, packing, and extraction, including nested/archive contents.
  Initially off; when enabled it applies even if other hidden files are visible.
  External shell commands are unchanged.
- **Show hidden files** controls only display of dotfiles and macOS `hidden` items.
  **Cmd+Shift+.** toggles both panels, including archives. Hidden contents still
  copy regardless of visibility. Save setup / Save setup on exit persists settings.
- **Moves preserve all contents**, including .DS_Store. Same-filesystem moves use
  rename without recursive prescanning. Existing-directory merges rename individual
  entries; destination directory metadata remains unchanged.
- Cross-filesystem moves copy data, mode, owner/group, ACL, xattrs, times, and flags,
  preserving symlinks and internal hardlinks. System [copyfile](https://github.com/apple-oss-distributions/copyfile/blob/main/copyfile.3)
  transfers metadata; permissions/owner/flags/times are checked afterward. Failures
  offer **Move anyway / For all / Cancel**, scoped to the operation. Cancel retains
  the source, removed only after successful copying or explicit acceptance of
  metadata changes. Streaming copies publish through rename without requiring
  destination hardlink support.
- Permanent directory deletion includes .DS_Store; Trash moves the entire folder
  so it can be restored.

Tests cover filters/shortcuts, ordinary/archive panels, ZIP/tar.gz/tar.bz2/tar.xz/7z,
rename inode preservation, metadata/links, confirmations, For all, and Esc. Tests
invoke the EXDEV path directly; physical external disks/network volumes were not used.

## Menus, file sizes, shell, and History (0.13)

- **Options** groups General, Panel, Terminal, Viewer, Editor, Advanced, with separate
  Full Disk Access, Save setup, and Keyboard reference. General combines dialog,
  interface, confirmation, and menu preferences; Terminal includes completion.
  Advanced holds colors, tree, descriptions, masks, sorting, and encodings.
- **Left / Right** lists Brief, Medium, Full, Wide, Detailed; Info, Tree, Quick view;
  Sort modes, Panel on/off, Re-read, Change volume. Keyboard navigation skips
  separators. Main ordering follows [Far help](https://github.com/FarGroup/FarManager/blob/master/far/FarEng.hlf.m4#L1280).
  Descriptions, Long descriptions, File owners, File links, Alternative full, and
  extra Parent/Home/Change directory entries were removed. Old saved modes become
  Full. Cmd+L still opens a path; Change volume offers home; `..` goes up.
- **Panel settings → Human-readable sizes** selects K/M/G/T versus exact bytes
  (default). It affects size columns, totals, and Info without changing sorting.
- **Terminal settings → Shell** selects system `/bin/bash` (default), `/bin/sh`,
  or `/bin/zsh` with a real PTY. Changes apply to new windows without stopping
  existing sessions; **Options → Save setup** persists them.
- **Cmd+Shift+H** originally toggled output history. Capture-phase handling prevented
  duplicate xterm/document actions. Window → Terminal History provided native access.
  Closing cancelled pending page requests. **Since 0.18.2 this shortcut opens command
  history; the output archive is separate.**

## Trash deletion (0.12.3)

**F8, Delete, Cmd+8** move selected files/folders to Trash. **Shift+F8, Shift+Delete**
delete permanently. Confirmation identifies the mode and follows Options →
Confirmations. Files has separate Move to Trash / Delete permanently commands.
Editor/dialog/terminal keys are unchanged; archive panels remain read-only.

System [NSFileManager.trashItem](https://developer.apple.com/documentation/foundation/filemanager/trashitem(at:resultingitemurl:))
chooses the volume's Trash and resolves name conflicts. Failure never falls back
to permanent deletion. Whole directories move without scanning; progress counts
selected entries. Esc stops between items, not inside an already running system
Trash operation. Completed moves remain in Trash.

## Shift+Enter (0.12.2)

**Shift+Enter** opens the cursor item using `/usr/bin/open`: directory in Finder,
file in its default application, `.app` launched as in Finder. It also works after
Fast find, retains command text, and bypasses internal associations/viewer/editor
settings. F9 → Files → Open with macOS exposes the same action. Archive entries
must first be copied out with F5; the archive file itself can open externally.
Editor/dialog/terminal Enter behavior is unchanged.

## Protected macOS directories (0.12.1)

The first `.app` launch offers **Full Disk Access** setup before loading panels or
restoring directories. Open Settings opens system preferences, selects the running
app in Finder, and ends startup. Enable access and reopen. Continue starts without
setup; the introduction does not repeat on every launch/update.

Setup remains available through **FarshMac → Full Disk Access…** and **F9 → Options
→ Full Disk Access…**. In System Settings → Privacy & Security → Full Disk Access,
add this copy using `+` or drag it from Finder, enable it, then restart via Cmd+Q.
The application cannot grant itself permission; it neither reads TCC.db nor probes
private data to infer access.

**Repeated prompts after rebuilds:** default local builds are ad-hoc signed; binary
changes can change their identity and require permission again. Stable permissions
need the same signing certificate and bundle ID across updates:

```sh
FARSHMAC_SIGN_IDENTITY='Developer ID Application: Your Name (TEAMID)' ./scripts/build.sh --universal
```

The certificate/private key must be available in Keychain. A requested signing
failure stops the build without replacing the existing app. Without a certificate,
an explicit warning accompanies the ad-hoc build. Switching certificates can require
one-time permission renewal. Signing grants no permissions and does not replace
notarization. Full Disk Access does not bypass Unix permissions, SIP, or other macOS
restrictions. [Apple discussion](https://developer.apple.com/forums/thread/678819).

## Dialog appearance (0.12)

Dialogs use gray padding around a double border, centered titles, teal inputs with
labels above, checkboxes before text, and a separated button row. Search, settings,
paths, confirmations, and progress share the style. Search checkboxes use two
columns; available search features were not expanded.

Tab/Shift+Tab moves among fields/buttons; Enter activates, Esc cancels. Yellow
mnemonics work with Option, accounting for macOS keys that produce special
characters. Short windows scroll to the focused field. Click hit testing uses
the same layout as drawing.

## Panel column fixes (0.11.1)

File links/File owners separators no longer step sideways with differing digit
counts: widths are shared across the panel, including blank rows and other views.
Link counts/UIDs align right; unavailable values show `-`. **File links (hard links)**
clarified that the count represents hardlinks, not symlink listings. Old saved
settings remained usable at this version; these modes were later removed in 0.13.

## Archive panels and panel controls (0.11)

- **Enter on an archive** opens it as a directory with subfolders, `..`, marks,
  sorting, and position restoration. **F5** copies selections out; **F3** opens a
  temporary read-only viewer. Progress and Esc cancellation apply.
- Ordinary ZIP reads are selective; listing does not decompress contents. Streaming
  formats scan in the background. A bounded metadata index is retained, and only
  selected entries are extracted to disk.
- **Cmd+F1/F2** toggles left/right panels; **Option+F1/F2** selects a volume.
  **Cmd+Shift+\** or **Cmd+=** opens the current directory in the other panel,
  including archive directories, and selects the same item. Ctrl aliases work.
- Original Far uses Ctrl+F1/F2 for visibility, Alt+F1/F2 for drives, and includes
  a [Ctrl+Shift+Backslash macro](https://github.com/FarGroup/FarManager/blob/master/extra/Addons/Macros/Panel.CtrlShiftBackSlash.lua)
  for matching directories. FarshMac implements that action directly, without Lua.

Archives are read/extract only: no modifications, passwords, or direct nested
archive entry. See [archive details](../ARCHIVE_SUPPORT.md).

## Chroma, indentation, and copying (0.10)

- Chroma 2.20.0 highlights editor/text viewer using filename then the first 8 KiB
  for detection. Colors suit the blue background; selection/cursor draw above them.
  Multiline strings/comments parse as a whole.
- Parsing starts after 200 ms idle in a separate process, at most two across all
  windows. Editing, switching, or closing cancels old work. Tasks have two seconds;
  failures/limits leave text accessible without colors.
- **Highlighting is disabled above 1 MiB**, with a status indication. The active
  document cache holds at most 100,000 color spans, not token strings. Helpers have
  a soft 64 MiB Go-memory limit and exit after each task; this is not a whole-app
  hard RSS limit.
- **Document F9** offers Chroma, autoindent, tab width 1–16, and spaces for tabs,
  also available in Options → Editor settings and Save setup. Enter copies only
  indentation before the cursor/selection start. Tab/Shift+Tab shifts selected
  lines without including the following line when selection ends at its start.
  Autoindent copies spaces/tabs; it does not format language-specific braces.
- Edits update the line index without rescanning all text. Combined undo/redo is
  capped at 32 MiB / 10,000 actions, evicting oldest actions. An over-budget edit
  clears history and cannot be undone. Text editing remains limited to 64 MiB;
  larger files use paged hex/dump. The text model is still entirely in memory.
- Ordinary copies use atomic APFS cloning where available, otherwise streaming
  with a shared 256 KiB buffer and cancellation checks. Per-file fsync was removed
  from copying, but retained for editor saves. OS buffering does not guarantee
  survival of sudden power loss. Progress includes logical APFS-cloned bytes.

Local Apple M3 Pro/APFS measurement, 2,000 × 4 KiB files, excluding UI/prescan:
old Copy **11.23 s**, new **0.26 s**, `/bin/cp -Rp` **0.86 s** on repeat (1.25 s
initially). These are single warm-cache runs, not Finder/network/cross-volume
guarantees. Reproduce with
`go test ./internal/fsops -run '^$' -bench BenchmarkSmallFiles -benchtime=1x`.

## Archives (0.9)

Create/list/test/extract supports **ZIP, TAR, tar.gz/tgz, tar.bz2/tbz/tbz2,
tar.xz/txz, 7z**. JAR/WAR use ZIP; system libarchive reads/extracts RAR and CPIO.
F9 → Files → Archive commands offers List contents / Test archive / Extract to
folder. Files also offers Create archive / Extract archive. Filename extension
selects creation format, default ZIP. Existing archives are not replaced.
Extraction creates a new directory, e.g. `backup.tar.gz` → `backup`.

Background operations display current file/volume; Esc cancels and removes temporary
results. Streaming reads have unknown totals and display activity, processed bytes,
and current-file progress. Publication occurs only on success. Internal links work;
unsafe paths, devices, and duplicate file entries are rejected. Passwords, multipart
archives, and editing existing archives are absent. Browsing arrived in 0.11.
See [formats, tests, and limits](../ARCHIVE_SUPPORT.md).

## Full screen (0.8.2)

**⌃⌘F (Control+Command+F)** toggles native fullscreen through Window → Enter/Exit
Full Screen, available in panels, editor, and terminal.

## Command dispatch and colored ls (0.8.1)

A private ZLE widget changes directory and submits the original command. The
`builtin cd … && { … }` wrapper no longer appears onscreen or in new zsh history.
Unfinished input is replaced entirely, including vi mode. Existing history is
not edited.

New sessions default to `CLICOLOR=1` for macOS ls when unset and NO_COLOR is not
enabled, complementing TERM/COLORTERM. Startup files can override it. CLICOLOR_FORCE
is not used, so redirected output stays plain. GNU ls needs `--color=auto`; user
aliases/functions remain untouched.

## Terminal, disk archive, and mouse (0.8)

xterm.js 5.5 supports TERM=xterm-256color, COLORTERM=truecolor, 16/256/RGB colors,
alternate screen, cursor queries, bracketed paste, mouse protocols, and Unicode 11
width tables. The live buffer holds 4096 scrollback lines. Output, attributes,
and offsets are stored in temporary files, up to 1 GiB per window; oldest segments
expire. Originally **⌘⇧H** opened paged output history; since 0.18.2 it opens command
history instead. Esc returns live output, and scrolling above live-buffer start
also opens the archive. Window close removes temporary history; relaunch does not
restore it.

**⌘C/⌘V/⌘A** copies/pastes/selects the available buffer (current archive page).
Option+drag selects even during application mouse tracking. **Ctrl+C/⌘.** interrupts.
Panels support left-click positioning, double-click opening, Shift-click range
selection, Cmd-click marking, right/Control-click marking, right-drag marking/
unmarking, and wheel scrolling under the pointer. Editor supports drag selection,
double-click words, triple-click lines, and scrollbar interaction.
See [terminal architecture and limits](../TERMINAL_PARITY.md).

## Esc cancellation (0.7)

Esc cancels manager operations directly without Stop/Continue: copy, move, delete,
ZIP, search, sizes, attributes/descriptions, tree, ordinary panel reads, Quick view,
completion, process listing, and system-launch waits. Document open/save, search,
Replace all, and encoding conversion run in the background. Esc leaves the editor
open; cancelled replacement never changes text. Overwrite conflicts also cancel
with one Esc.

Cancellation is checked between entries, blocks, and stages. An active syscall or
library stage may take time to return while UI events continue. Temporary cleanup
finishes before the operation ends; completed changes are not rolled back. A save
already atomically published remains saved after late Esc. In the terminal, Esc
is input to programs such as vim; Ctrl+C/⌘. interrupts commands.

## Tree fixes (0.6.1)

Tree panel reads in background batches of 256 without stat on ordinary files.
Esc cancels. Navigation, mode changes, and window close invalidate old results.
Alt+F10 uses the same traversal. Branches use `├─`, `└─`, `│`; filename control
characters are escaped only for display. Go/xterm Unicode-width differences,
including emoji, no longer shift columns.

Tree settings controls depth. Each scan is limited to 50,000 directories and
1,000,000 examined entries. Limits, cancellation, and access errors mark partial
results; enter a subdirectory to continue. Symlinks are excluded. A pending read
syscall, e.g. on a network disk, finishes before cancellation, without blocking
the UI queue.

## Session and icon (0.6)

Window position/size, both directories, active panel, cursor, and top row save
automatically, independently of Save setup. Disconnected monitors cause clamping
to an available screen; missing folders fall back to an accessible parent.
Explicit `--left` / `--right` starts with those paths.

State uses `~/Library/Application Support/FarshMac/session.json`. Cmd+Q saves all
open windows. Individually closed windows are excluded, except the last closed
window retained for startup. Fullscreen/minimized state, shell processes, and
unsaved documents are not restored; windows use normal mode with saved frames.
The official Far Manager icon was converted to `.icns`; see [source/license](../resources/upstream/README.md).

## Windows and file actions (0.5)

Added ⌘N/⌘W windows, global ⌘Q, F12 with Panels, executable/.app Enter launch,
system/custom viewer/editor, Alt-character search, F7 selection, and file/ZIP
progress/cancellation. See [behavior audit](../BEHAVIOR_PARITY.md).

## Resize and menus (0.4)

Resize handling and bounded frame queues prevent Go from blocking Cocoa's main
thread. Confirmations use buttons instead of typing DELETE/QUIT. F9 added working
panel/file/search/history/ZIP/settings commands. F2 opens the user menu; ⌘R refreshes.
See [menu audit](../MENU_PARITY.md).

## Running

Double-click `build/FarshMac.app`. Local builds are ad-hoc signed, without Developer
ID/notarization for public distribution. Source builds require Go 1.25+ and Xcode
Command Line Tools:

```sh
./scripts/build.sh
open 'build/FarshMac.app'
./scripts/build.sh --universal
'build/FarshMac.app/Contents/MacOS/farshmac' --left ~/Downloads --right ~/Documents
```

Target: macOS 12+. Runtime testing used Apple Silicon; Intel needs its own runtime
check.

## Command line and terminal

Type directly in panels and press Enter to hide them and run in the same window's
real PTY, using the active directory. Output remains visible afterward; **⌘O**
switches panels/terminal, even during execution, without closing the PTY or stopping
the process. Persistent bash (default), sh, or zsh retains variables, aliases,
functions, history, and jobs, loading startup files through a temporary wrapper
removed on close. User startup files are not edited. Shell cd synchronizes the
active panel on return. Submitting while busy returns to the existing terminal.

ANSI/256/RGB, alternate screen, 4096-line live scrollback, disk archive, PTY resize,
interactive vim/less, selection, and paste are supported. **⌘. / Ctrl+C** interrupts;
**⌘C/⌘V** copies/pastes. Panel command history uses **⌘↑/⌘↓**. **⌘⇧H** now opens
command history; its original output-archive role changed in 0.18.2.

## File management

Directory navigation remembers cursor, top row, and marks per panel. Returning via
`..` restores state without reintroducing deleted entries. Visited-directory history
and marks live for the current run; current paths/cursor/top row persist. Version
0.18.5 additionally ensures that parent navigation selects the directory just left.

F9 opens the active panel menu. Arrows navigate items/sections, Tab chooses left/
right menus, Enter executes, Esc closes; top-bar/item clicks work. See [menu matrix](../MENU_PARITY.md).
⌘Enter appends the filename without running it, escaping spaces/quotes/shell syntax
and rejecting control characters.

| Shortcut | Action |
| --- | --- |
| Tab | Switch active panel |
| ↑ ↓, PgUp/PgDn, Home/End | Navigate |
| Enter | Enter directory / run executable or association / execute command |
| Backspace | Delete command character, never leave directory |
| Space / Insert | Mark item |
| F1 / ⌘1 | Help |
| F2 / ⌘2 | User menu |
| ⌘R | Refresh panels |
| ⌘Space | Complete command-line filename |
| F3 / ⌘3 | Text/hex/dump viewer |
| F4 / ⌘4 | Editor |
| Shift+F4 | Create/open file in editor |
| F5 / ⌘5 | Copy |
| F6 / ⌘6 | Move/rename |
| F7 / ⌘7 | Create directory |
| F8 / Delete / ⌘8 | Trash |
| Shift+F8 / Shift+Delete | Permanent delete |
| F9 / ⌘9 | Main menu |
| ⌘L | Go to directory |
| ⌘Enter | Insert current filename |
| ⌘U | Swap panels |
| ⌘O | Panels/terminal |
| ⌘N / ⌘W | New/close window |
| ⌘Q | Quit all windows |
| ⌃⌘F | Fullscreen/normal |
| Alt+characters | Panel Fast find |
| Alt+F3 / Alt+F4 | Alternative viewer/editor |
| F10 | No panel action; close document in viewer/editor |

Viewer/editor use a Go model and character grid, without an HTML textarea. F6
switches modes, F7 searches, ⌘F7 replaces, Shift+F8 selects encoding, F12 lists
documents, F2/⌘S saves, Shift+F2 opens Save As, and document F1 shows reference help.
See [viewer/editor matrix](../VIEWER_EDITOR_PARITY.md). Esc/F10 closes only the
document, confirming unsaved changes. Panels also handle clicks on function keys.

Recursive copying preserves symlinks as links. Conflicts offer Overwrite / All /
Skip / Skip all / Cancel. Moves to unused paths use `renamex_np(RENAME_EXCL)`;
cross-volume moves copy then remove the source. Batch operations stop at the first
error and retain completed items. Early versions deleted permanently; since 0.12.3
default deletion uses Trash, with Shift for permanent deletion.

## Architecture

- `internal/ui`: Go panel state, keys, dialogs, file workflows, and grid rendering.
- `internal/fsops`: copying, moving, and saving.
- `internal/textfile`: UTF-8 text model, selection, undo/redo, search, encoding/EOL.
- `internal/terminal`: PTY, bash/sh/zsh, I/O, resize, shell-state markers. Its parser
  passes terminal escape sequences through.
- `internal/native`: Cocoa/cgo bridge; local WKWebView/xterm.js renders panels and
  emulates the terminal, connecting buffer/mouse handling to Go models/disk archive.
- `cmd/farshmac`: startup; `scripts/build.sh`: app packaging.

xterm.js is bundled: no CDN, Electron, localhost server, or UI network requests.
Shell commands have the user's normal access, including network when requested by
the command. Output reaches xterm as bytes, never HTML. WebKit navigation is
restricted to the embedded page.

## Validation

```sh
go test -race ./...
go vet ./...
node internal/native/tests/resize.cjs
node internal/native/tests/input.cjs
node internal/native/tests/terminal.cjs
```

Tests use temporary directories and cover overwrite refusal, links, recursive copy,
move, external editor conflicts, macOS shortcuts, edit transactions, search/replace,
rectangles, encodings, CRLF, and panel/terminal switching. Integration runs real
bash/sh/zsh PTYs for cwd, variables, resize, interruption, and saving in interactive
`vim -Nu NONE`. The app was built/launched through Finder, but desktop automation
returned `Invalid app` on window attachment; native visual/focus checks were
unfinished at the time of this historical report.

## Historical limitations and remaining work

- Windows DLL plugins, Lua macros, and some Far commands/settings are absent.
  Early notes also listed archive panels, tree search, and network VFS as missing;
  later releases above added those capabilities.
- Text decoding is limited to 64 MiB in RAM. Larger files use paged hex/dump, without
  streaming text/search. Eight encodings and Go/RE2 differ from full Far support.
- Early copying preserved mode/mtime/links but not ACLs, xattrs/resource forks,
  ownership, or hardlink topology. Copy/move metadata support was expanded in 0.14
  and 0.14.1. Atomic editor saving still does not preserve ACL/xattrs/hardlink identity.
- External-edit protection checks inode/size/mtime, not transactional writer locking.
- Background reads and cancellation wait for current syscalls/library stages;
  completed operations are not rolled back.
- Ordinary wide characters are handled; complex emoji/ZWJ, tabbed rectangular
  blocks, and IME require further validation.
- Custom zsh startup hooks that replace/clear precmd/preexec can break readiness
  detection and directory synchronization.
- Early versions required application relaunch after shell exit. **0.18.7 adds
  automatic parent-shell restart.**
- Windows/directories persist; temporary output archives are deleted on window close.

## Sources

Behavior/palette references: [Far Manager](https://github.com/FarGroup/FarManager)
and [Far palette](https://github.com/FarGroup/FarManager/blob/master/far/palette.cpp).
Far C++ sources were not translated line-for-line or compiled into this application.
This is not an official Far Group build. An existing C++ macOS port is
[far2l](https://github.com/elfmz/far2l). Bundled dependency licenses are in
[THIRD_PARTY_NOTICES.txt](../THIRD_PARTY_NOTICES.txt). See [FEATURE_AUDIT.md](../FEATURE_AUDIT.md)
for the detailed historical comparison and porting priorities.
