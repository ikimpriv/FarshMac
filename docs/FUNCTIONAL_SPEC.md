# FarshMac 0.20.0 Functional Specification

This document describes everything the application does today, based on the source code (not on the README or the parity documents). For every feature it lists how it is invoked, when it is available, the step-by-step behavior, the dialogs, the exact message strings and the source files. Interface strings are given verbatim in English, as in the code. Discrepancies between the code and the existing documentation are collected at the end of each part and consolidated in the "Discrepancy summary" section.

The document was compiled on 2026-11-11 against the `main` branch (commit `b91313a`). When behavior changes, update the corresponding part.

> **Update (branch `backlog-fixes`, 2026-11-11).** All 85 items of `docs/IMPLEMENTATION_BACKLOG.md` have been implemented, so the behavior of a number of features has changed relative to what is described below. The discrepancy lists at the end of each part and the "Discrepancy summary" section are closed: the decisions from the backlog's "Discrepancy decisions" section have been applied to the code, the F1 help, README and the parity documents. Below, only the most visible entries have been updated (Tab, Ctrl+PgDn, the sort menu, bookmarks, fast find); the remaining parts describe the state as of commit `b91313a` — where they differ, the code and the backlog remain the source of truth.

## Contents

1. [Part 1. File panels and navigation](#part-1)
2. [Part 2. File operations, archives, launching programs](#part-2)
3. [Part 3. Internal viewer and editor](#part-3)
4. [Part 4. Terminal, command line, windows, native shell, renderer](#part-4)
5. [Part 5. F9 menu, dialogs, settings, network connections, help](#part-5)
6. [Discrepancy summary](#discrepancy-summary)
7. [Appendices: additions based on the gap analysis](#appendices)



<a id="part-1"></a>
# Part 1. File panels and navigation

The source of truth is the code in `internal/ui` (the version shown in the menu bar: `FarshMac 0.20.0`, `internal/ui/draw.go`). All interface strings are given verbatim in English. Everywhere below, "cmd" means the Ctrl **or** Cmd (⌘) modifier unless explicitly stated otherwise (`cmd := m.Ctrl || m.Meta` in `far_keys.go`).

General code conventions that matter for the whole section:

- The screen is a character grid `Cols × Rows` (limits `60..300` × `15..120`, `app.go: Handle("resize")`). Mouse coordinates are grid cells.
- A panel is the `Panel` struct (`app.go`): `Path`, `Entries []Entry{Name, Info, Dir}`, `Cursor`, `Offset` (index of the first visible entry), `Marked map[string]bool`, `Err` (text that replaces the panel footer), `panelOptions{View, Sort, Filter, Reverse, CaseSensitive, MixDirectories, SelectedFirst, Hidden, DisableSortGroups}`, `Remote`, `Archive`, `History`, `states` (remembered cursor/top line/marks for every visited path).
- All panel keys are handled only when `PanelKeys == true`: no open document, form, dialog or menu, and the application is not `Busy` (`Poll()`).
- Key handling order in `App.key()` (important for conflicts): Busy → Cmd+N / Cmd+Q → Ctrl+Tab / Cmd+Shift+[ ] → Cmd/Ctrl+O → terminal visible (nothing) → form → dialog → document → Info panel keys → fast find (printable without Ctrl/Cmd, not Numpad) → selection keys → folder bookmarks → Delete/Shift+F8 → Shift+Enter → Cmd+Shift+. → fast find (the rest) → Cmd/Ctrl+Shift+F3/F4 → `farPanelKey` → Cmd/Ctrl+F1/F2/=, Cmd+Shift+\ → Alt+F1/F2/F3/F4/F7/F8/F10 → Shift+F10 → Shift+F4 → F12 → menu → Cmd+digit / Cmd+letter → F-keys → basic navigation.

---

## 1. Screen layout

#### Menu bar (y = 0)
- **Invocation:** always visible in panel mode.
- **Behavior:** Cyan background, Black text. Sections: `Left` (x=2), `Files` (x=11), `Commands` (x=21), `Options` (x=34), `Right` (x=46). At width ≥ 70, `FarshMac 0.20.0` is printed on the right (x = W − len − 2). A left click on a section name opens the corresponding drop-down menu (`click()`: `a.Menu = &MenuState{Column: i}`).
- **Code:** `draw.go: Draw`, `menu.go: menuSections`, `app.go: click`.

#### Panels (y = 1 … Rows−5)
- **Behavior:** both panels are drawn by `drawPanel(g, i, x, 1, w, Rows-4)`. Geometry comes from `panelRect(index)`:
  - By default split = `Cols/2`; when `panelSplit != 0` — `Cols*panelSplit/100` (left `[0, split)`, right `[split, Cols)`).
  - If the active panel is in `Detailed` or `Long descriptions` mode and is not hidden, and the inactive panel does **not** show `Quick view`/`Info panel`/`Tree panel` (or is hidden), the active panel takes the full width (`0, Cols`), the other gets width 0 and is not drawn.
  - A hidden panel (`Hidden`) or `w < 1` — the area is filled Gray on Black without a frame.
- Frame: double (`╔═╗ ║ ╚═╝`) LightCyan on Blue.
- **Panel title (y = 1):** the string ` <label> `, centered, truncated on the left with `…` to `w-8` characters (`tail`). `label`:
  - local: `p.Path`;
  - remote: `Remote.Label() + Path` (for example `sftp://user@host` + path);
  - archive: `"[Archive] " + label`;
  - Info panel: `Information`.
  Active panel: Black on Cyan; inactive — LightCyan on Blue.
- **Column header line (y = 2):** Yellow on Blue, depends on the mode (see §2).
- **File lines:** y = 3 … (1 + h − 4), i.e. `rows = Rows − 9` visible lines per column.
- **Separator (y = Rows − 6):** `╟───╢`.
- **Panel footer (y = Rows − 5):** the panel status line (x+2, width w−4):
  - basic: `"%s in %d items"` where `%s` = `panelSizeLabel(TotalBytes)` → `"<n> bytes"` or human-readable (`1.5M`), `ItemCount` — entries excluding `..`;
  - if there are marks: `"%d selected · " + basic`;
  - if a filter is set: `"[" + Filter + "] " + …`;
  - if `p.Err != ""` — `Err` is printed instead of everything (load errors, the read progress `Reading folder… Esc stops`, tree status, etc.).
- **Code:** `panel_draw.go: panelRect, drawPanel, loadInfo`.

#### Status line (y = Rows − 3)
- Gray on Black. Prints `a.Status`; if empty — the hint `Tab Switch panel   Space Select   ⌘O Terminal   ⌘⌥0…9 Bookmark`.
- Reset by Esc (when the command line is empty and there are no background reads). After a command finishes in the terminal: `Exit %d · ⌘O panels / terminal`.
- **Code:** `draw.go: Draw`, `app.go: Poll`.

#### Command line (y = Rows − 2)
- `promptPath + ">" + Command`; for a remote panel `promptPath = "local:" + filesystemPath()` (the terminal's local folder). The line is truncated on the left (`tail`) to `Cols−1`; the cursor `▌` is placed after the text.
- Input: any printable character without Ctrl/Alt is appended (`default` in `key()`); Space on an empty line marks the file, otherwise inserts a space; Backspace deletes a character (never leaves the directory); Esc clears the line and the status; Enter on a non-empty line — `execute()` (run in the terminal), on an empty line — `enterFile()`. Paste (⌘V) when no modal windows are open appends the text without line breaks (control characters → space).
- Command history: ⌘↑/⌘↓, Ctrl+↑/Ctrl+↓, Ctrl+E/Ctrl+X — previous/next (`a.history`, limit `Settings.HistoryLimit`, default 200); Ctrl/⌘+Y — clear the line; Alt+F8 — the `Command history` menu (`Command history is empty` when empty).
- **Code:** `draw.go: Draw`, `app.go: key, execute`, `far_keys.go`, `commands.go: historyMenu`.

#### Key bar (y = Rows − 1)
- **Invocation:** visible when `Settings.ShowKeys` (default true); toggled by Ctrl/⌘+B, `Options → General settings → Key bar`, `Interface settings`.
- **Behavior:** 10 cells of width `Cols/10`; number (Gray on Black) + label (Black on Cyan): `Help`, `UserMenu`, `View`, `Edit`, `Copy`, `RenMov`, `MkFold`, `Trash`, `Menu`, `` (the 10th is empty). For a remote active panel the 8th label is `Delete`. Labels do not change while modifiers are held.
- A left click on the key bar invokes `function(min(10, X*10/Cols+1))`; if the key bar is hidden the click is ignored.
- **Code:** `draw.go: Draw`, `app.go: click`.
#### Color themes
- `Options → Advanced → Colors`: `Classic Far`, `Dark`, `Light`. Dark/Light are a post-processing pass over the palette of the whole grid (Blue→`0x151b24`, LightCyan→`0xc7d7e9`, Cyan→`0x486987`; Light: Blue→`0xf1f3f5`, LightCyan→`0x182b49`, White→Black, Yellow→`0x885000`, Cyan→`0x80b8dd`).
- **Code:** `draw.go: Draw`, `settings.go: settingsMenu("Colors")`.

#### Window title
- `Update.Title = Panels[Active].Path` (when a document is open — the document's file name).
- **Code:** `app.go: Poll`.

---

## 2. Panel modes (View)

List of modes (`panel_options.go: panelViews`): `Brief`, `Medium`, `Full`, `Wide`, `Detailed`, `Descriptions`, `Long descriptions`, `File owners`, `File links`, `Alternative full`, `Info panel`, `Tree panel`, `Quick view`. An empty `View` is treated as `Full`.

#### Switching the mode via the Left/Right menu
- **Invocation:** F9 → `Left` / `Right` → items `Brief`, `Medium`, `Full`, `Wide`, `Detailed`, (separator), `Info panel`, `Tree panel`, `Quick view`. The current mode is marked with `✓ ` (when `View == ""`, `Full` is marked). F9 opens the menu on the `Left` column for an active left panel and on `Right` for the right one.
- **Where available:** any panel; restrictions are in `setView`.
- **Behavior (`setView(index, view)`):**
  1. Remote panel and view ∈ {`Tree panel`, `Info panel`, `Quick view`} → status `Choose a file view for remote panels`, no changes.
  2. Archive panel and `Tree panel` → `Tree view is unavailable inside archives`.
  3. Otherwise `View = view`, `Hidden = false`, reload `Load(Path)`, reset of the preview key.
- The modes `Descriptions`, `Long descriptions`, `File owners`, `File links`, `Alternative full` are not available from the menu (legacy; when loading the setup/`configurePanel` they are forcibly replaced with `Full`), but the drawing code for them is kept.
- **Code:** `menu.go: menuSections`, `panel_options.go: setView`, `settings.go: configurePanel`.

#### Columns and content per mode
- Number of columns (`panelColumns`): `Brief` = 3, `Medium` = 2, everything else = 1. Column width `cw = (w−2)/columns`; a vertical `│` between columns.
- Metadata (`metadata`) is drawn only with 1 column, `View != "Tree panel"` and width ≥ 12: name │ extra.
- Column header (Yellow): `Name` (multi-column and Tree? — no, see below), `Name / Size / Modified` (single-column by default: Full, Wide, Alternative full), `Name / Description` (Descriptions, Long descriptions), `Name / Size / UID` (File owners), `Name / Size / Hard links` (File links), `Name / Size / Modified / Accessed / Created / Mode` (Detailed), `Folder tree` (Tree panel).
- Width of the extra column (`panelExtraWidth`, independent of the row values): Full/Alternative full 24 (size 9 + space + date 14), Wide 9, File links 15, File owners 20, Detailed 72, Descriptions/Long descriptions `w/2`; everything is capped at `w/2`. `sizeWidth` = max(9, length of `panelSize(TotalBytes)`, lengths of the calculated folder sizes including the `~`/`>=` prefixes).
- The extra field for a row:
  - by default (Full): `"%*s %s"` size + date `02.01.06 15:04` (`ModTime`);
  - `Wide`: size only;
  - `Descriptions`: the description from `descript.ion`;
  - `Long descriptions`: size + description;
  - `File owners`: size + UID (10 positions); `File links`: size + `Nlink` (5 positions); if the size does not fit — `humanSize`;
  - `Detailed`: size, date, `Mode()` (for example `-rw-r--r--`), then ` A:<atime sec> C:<birthtime sec>`.
- Size: `<DIR>` for folders (or the calculated size, see §8), `<LINK>` for symbolic links, otherwise bytes or human-readable (`Settings.HumanReadableSizes`, format `%.1f` + `K/M/G/T`).
- Name color: folders White; with `Settings.Highlight` symbolic links Cyan, executables (`mode&0111`) `0x55ff55`, archives by the `<arc>` mask group `0xff55ff`; marked — Yellow; the cursor row in the active panel — Black on Cyan (a marked entry under the cursor — Yellow on Cyan). The mark marker in the first cell of the row: `√`.
- In `Tree panel` the branch label `treeLabels[name]` (`├─`, `└─`, `│ `) is printed instead of the name; `..` — as is.
- **Code:** `panel_draw.go: drawPanel, panelExtraWidth, panelColumns, panelSize, humanSize`.

#### Quick view
- **Invocation:** Ctrl+Q (Ctrl only, not ⌘) — toggles the **inactive** panel; `Commands → Quick view on/off`; via the Left/Right menu (`setView`) — for any panel.
- **Where available:** Ctrl+Q and the Left/Right menu → `Quick view` also work for a remote panel (after the backlog, `setView` allows Quick view for remote; Info/Tree still require a local folder). For an archive member, `Archive member: press F3 to extract and view` is shown. Ctrl+L (Ctrl only; ⌘L — Change directory) / Ctrl+T (also ⌘T) toggle Info/Tree; there are no separate Commands items for them — the views are available in Left/Right.
- **Behavior (`toggleOtherView("Quick view")`):**
  - if the inactive panel is already in this mode — the remembered pair `{View, Hidden}` from `viewReturns[view]` is restored (nested toggles are restored as a chain: Brief → Ctrl+L → Ctrl+Q → Ctrl+Q returns `Info panel` → Ctrl+L returns `Brief`);
  - otherwise the current pair is remembered, `View = "Quick view"`, `Hidden = false` are set;
  - background loads/tree of the inactive panel are cancelled, the preview is reset (`previewKey`, `previewImage`, cancellation of `previewOperation`).
- The preview source is the file under the cursor of the **other** panel (`updatePreview` in `Poll`, as long as no document/terminal is open and not Busy). Key = path + size + mtime; a key change during loading cancels it.
- The preview text (`previewText`) is printed line by line from y+2, at most `h−3` lines, without scrolling (the wheel over Quick view is ignored):
  - remote source panel: `Remote file: use F3 to download and view`;
  - while loading: `Loading…`;
  - folder: `"<path>\nDirectory (preview, up to 100 entries)\n"` + up to 100 names (hidden ones are skipped when `!ShowHidden`), then `…`;
  - image/PDF (`preview.Supported`: `.jpg .jpeg .png .gif .tif .tiff .bmp .heic .heif .webp .pdf`): `"<name>\n<size> bytes · <2006-01-02 15:04>"` + `"\n<FORMAT> · <W> × <H> pixels"` or `"\nPDF · page 1 of <N>"`; on error `"\nPreview unavailable (unsupported, damaged or limit exceeded)"`;
  - regular file: the first 16 KiB as text;
  - `os.Stat` error — the error text (in particular, the preview does not work for archive members: the virtual path does not exist on disk);
  - after Esc: `Preview cancelled`.
- Image: decoded in a separate helper process (`farshmac --preview-helper <path>`, 4 s timeout, output ≤ 5 MiB, PNG ≤ 3 MiB, thumbnail ≤ 768 px, source ≤ 100000×100000 and ≤ 500 Mpixels; for PDF the 1st page is rendered taking rotation into account). The result is a PNG data URI passed to the renderer as `Update.Preview{X: x+2, Y: 7, W: w−4, H: Rows−13}`; it is not shown if `w < 8` or `Rows < 14`, if the source is a remote/archive panel, if a document/menu/form/dialog is open or Busy, or while the preview is `pending`.
- **Messages:** `This panel view requires a local folder` (toggleOtherView for remote with view ≠ Quick view).
- **Code:** `far_keys.go: toggleOtherView`, `commands.go: updatePreview`, `image_preview.go`, `preview/preview.go`, `preview/preview_darwin.go`, `panel_draw.go`.

#### Info panel
- **Invocation:** Ctrl+L (Ctrl only) — the inactive panel; Left/Right menu → `Info panel`.
- **Where available:** local panels; for remote `setView` → `Choose a file view for remote panels`, `toggleOtherView` → `This panel view requires a local folder`.
- **Behavior:** title `Information`. The source is the opposite panel (if it is also an Info panel — the panel itself): `path = source.filesystemPath()`; `plugin` = `Remote.Label()+Path` for a remote panel or `"Archive: " + Archive.Path` for an archive panel. Reading is done in one background goroutine per panel (`readInfoPanel`), the result is applied in `Poll`; the key includes the path, plugin and all Info settings; the same key is not re-read sooner than 2 s later. Before the first result — `Reading information…`.
- Lines (in output order; section headers are drawn as `─── Title ───`, values are Yellow, right-aligned):
  - `InfoHost` (`Computer and user`): `Computer name`, `User name`;
  - `InfoVolume` (`Drive information`): section `Disk <mount> (<fstype>)`; `Total bytes`, `Free bytes` (statfs: `Blocks*Bsize`, `Bavail*Bsize`), `Volume label`, `Volume UUID` (CFURL volume keys), `Device` (`Mntfromname`);
  - `InfoMemory`: section `Memory`; `Memory used` (`%.0f%%` = (Total − min(Total, Available))/Total), `Installed memory` (`hw.memsize`), `Free physical`, `Available (estimate)` (free+inactive pages), `Compressed`, `Swap total`, `Swap free` (`vm.swapusage`);
  - plugin ≠ "": section `Filesystem panel` + the plugin line;
  - `InfoPower`: section `Power status`; `Power source` (the IOPS providing type or `Unknown`), `Charge status` (`Charging` / `Critical` <10% / `Low` <33% / `Normal` / `High` >66% / `Unknown` / `No battery`), `Battery` `<n>%` (if ≥ 0), `Time remaining` `<h>h <m>m` (if > 0);
  - `InfoDescription` and plugin == "": section `Folder description`; the names from `Settings.FolderDescriptions` are tried (default `file_id.diz,README.md,README,readme.txt`; names containing a path are skipped); the first existing regular file: a line with the file name, then up to 4096 bytes of content line by line.
  Numbers without human-readable formatting use a space as the thousands separator.
- Scrolling (`InfoOffset`, panel active): ↓/↑ by 1, PgDn/PgUp by `Rows−7`, Home/End, mouse wheel (whichever panel is under the pointer, ±12 per event). `▲`/`▼` indicators on the right frame.
- Ctrl/⌘+F12 in an active Info panel — the `Info panel settings` form with the toggles `Computer and user`, `Drive information`, `Memory information`, `Folder description`, `Power status` (resets the `infoKeys` cache).
- F3 / F4 in an active Info panel — open the description file (the first candidate, even if it does not exist yet? — no: `infoDescriptionPaths` is used, which points to the found file or to the first candidate) via `viewPlan` (view/edit).
- Mouse: a click on the Info panel makes it active; if the line belongs to the `Folder description` section and there is a description path: left button — view, right/Control-click — edit.
- Selection keys, fast find via Numpad and the like have no effect in the Info panel (`selectionKey` returns false; `infoPanelKey` intercepts arrows/PgUp/PgDn/Home/End/F3/F4 before everything else).
- **Code:** `info_panel.go`, `sysinfo/info_darwin.go`, `settings.go: settingsMenu("Info panel settings")`.

#### Tree panel
- **Invocation:** Ctrl/⌘+T — the inactive panel; Left/Right menu → `Tree panel`.
- **Where available:** only local panels outside archives: `Tree view is unavailable inside archives`, `This panel view requires a local folder` / `Choose a file view for remote panels`.
- **Behavior:** `Load(path)` in this mode starts a background scan (`loadTree → startTree → scanTree`) rooted at `path`:
  - depth `Settings.TreeDepth` (1..12, default 4; `Options → Advanced → Tree settings`, hint `Maximum levels for folder tree (1–12)`, error `Enter a number from 1 to 12`);
  - limits: 50000 nodes (`treeNodeLimit`), 1 000 000 visited entries (`treeEntryLimit`); reading in batches of `ReadDir(256)`; symbolic links are not followed; regular files are not stat-ed; hidden files and `.DS_Store` are filtered by the panel settings (taking `ForcedVisiblePath` into account); subfolders are sorted by the panel's current sort settings;
  - during the scan the footer shows `Reading tree… Esc stops`, then `Reading tree… %d entries · Esc stops`;
  - on completion: entries are relative paths (`a`, `a/b`), the first line is `..` (if the root is not `/`), `ItemCount` = number of folders, `TotalBytes = 0`; footer (the `Err` field, stays permanently): `"%d folders · depth %d"`, when the limit is hit `Partial tree · limit reached; open a subfolder`, when cancelled `Stopped · partial tree`, on a root error — the error text; when entries were skipped ` · %d unreadable`.
  - Labels: `├─name` / `└─name` with the prefixes `│ ` / `  `; control characters in the name are escaped (`\n`, `\r`, `\t`, `\uXXXX`).
- Enter on a node — `enterFile` → `Load(Join(Path, rel))` → the tree is rebuilt with the new root; Enter on `..` — the root goes one level up. Ctrl+PgUp/PgDn likewise.
- Marks via Insert/Space/Shift+arrows work on node names; Num+/−/* and Ctrl+M do not (`selectionKey` is disabled for Tree).
- Esc during the scan: `Stopping tree scan…`.
- Alt+F10 (`Commands → Folder tree`) — the same scan in a background task `Reading folder tree` with the modal list `Folder tree` (or `Folder tree (partial)`), the first line is the root; selecting loads the folder into the active panel. Not available for remote (`This command requires a local panel. Use F5 to copy remote files first.`).
- **Code:** `tree.go`, `commands.go: treeMenu`, `far_keys.go: toggleOtherView`.

#### Detailed / Long descriptions — full-screen panel
- When the active panel is in `Detailed`/`Long descriptions`, the second panel is not drawn (width 0), except when it shows Quick view/Info/Tree (then the usual split). A mouse click in a zero-width area hits no panel.
- **Code:** `panel_draw.go: panelRect`.

#### Human-readable sizes
- **Invocation:** ⌘/Ctrl+Shift+S; `Options → Panel settings → Human-readable sizes (off = bytes)`.
- **Behavior:** `Settings.HumanReadableSizes` is toggled, `configurePanel` for both panels without re-reading; the sizes in the rows and the footer are recalculated immediately (`drawPanel` reads `Settings.HumanReadableSizes`), only the non-displayed `InfoText` becomes stale.
- **Code:** `far_keys.go`, `panel_draw.go`.

---

## 3. Directory loading and state

#### Panel.Load (the general mechanism)
- **Behavior:**
  1. The path is made absolute. Remote → `loadRemote` (or asynchronously `loadPanel`); Archive and a path inside the archive → `loadArchiveDirectory`; `View == "Tree panel"` → `loadTree`; otherwise a local read (`fsops.ReadDirectory`), in the application always asynchronous (`Async == true`, `NewAsync`), with the footer `Reading folder… Esc stops` and the operation `Reading folder`.
  2. Filtering while reading: `.DS_Store` when `IgnoreDSStore`; hidden entries (name starting with `.` or the `UF_HIDDEN` flag 0x8000) when `HideHidden`, except `ForcedVisiblePath` (a folder created with F7); for non-folders — the panel's `Filter` mask (`matchesMask`). Symbolic links to folders count as folders (`os.Stat` of the target).
  3. `beginLoad`: saves the state of the previous path in `states[prev]` (name under the cursor, top line, marks), restores the state of the new path; **when going to the parent the cursor is always placed on the folder just left** (even if the parent had a saved state); the path is added to the panel's `History` (unique, limit 200); `..` is added as the first entry (if `Dir(abs) != abs`).
  4. `descript.ion` descriptions are read (`Settings.DescriptionFile`), sorting (`sortEntries`), `installEntries` (restoring the cursor/top line by names, marks only for existing names; the cursor is clamped, `Offset ≤ Cursor`), `loadInfo` (`TotalBytes` — the sum of file sizes, `ItemCount`, `InfoText`).
  5. Asynchronous load error: the panel rolls back to the previous state (`previous`), `Err` = the error text (visible in the panel footer). A failed synchronous `Load` — `a.error(err)` → the status line.
- Esc during an active read: `Stopping folder scan…` (cancels the operation); in this case Esc does not clear the command line.
- During an asynchronous load, cursor position requests (F7, matchOtherPanel) are recorded in a pending state (`pendingPosition`).
- **Code:** `app.go: Load, beginLoad, installEntries`, `panel_load.go`, `hidden_darwin.go`, `remote.go: loadRemote`, `archive_panel.go: loadArchiveDirectory`.

#### Panel refresh (Re-read)
- **Invocation:** ⌘R or Ctrl+R (active panel); menu `Left/Right → Re-read ⌘R` (a specific panel).
- **Behavior (`rereadPanel`):** resets the Info panel keys; for an archive — re-opens the archive index (`openArchive`), otherwise `Load(Path)`. Cursor/marks are preserved by names.
- A full `refresh()` of both panels happens after returning from the terminal (⌘O), changing panel settings, F7, creating a link, etc.
- **Code:** `archive_panel.go: rereadPanel`, `app.go: refresh`.

---

## 4. Keyboard navigation

#### Tab — switch the active panel
- **Invocation:** Tab.
- **Behavior:** `Active = 1 − Active`; a hidden panel is skipped; when both are hidden the active panel is shown (P1-01). On Quick view / Info / Tree / a hidden panel, file keys (Enter, F3–F8, Insert, etc.) have no effect, status `Switch to a file panel first (Tab)`.
- **Code:** `app.go: key`.

#### Arrows ↑/↓
- Cursor ±1, limited to `[0, len−1]`. With Ctrl — command history (see §1). In the Info panel — scrolling. With fast find open — next/previous match.
- **Code:** `app.go: key`.

#### ←/→
- Only in multi-column modes (`Brief`, `Medium`): cursor ± `Rows−9` (one column), clamped. In single-column modes — nothing (the command line is not edited with arrows).
- With Ctrl/⌘ on an empty command line — panel widths (§12).
- **Code:** `app.go: key`.

#### PgUp / PgDn
- Cursor ± `(Rows−9) × columns` (a page), clamped to the bounds. Shift+PgUp/PgDn — range selection (§5). Ctrl/⌘+PgUp/PgDn — parent/enter (below).
- **Code:** `app.go: key`.

#### Home / End
- Cursor to 0 / `len−1`. Shift — selection; in the Info panel — scrolling.

#### Enter — open
- **Invocation:** Enter on an empty command line; left double-click; Enter in fast find.
- **Behavior (`enterFile`):**
  - remote panel: folder → `Load`, file → `openRemoteDocument(false)`;
  - archive: `enterArchiveMember` (§10);
  - local: `os.Stat` (error → status); folder (including `.app`; launching — Shift+Enter) → `Load`; an archive per `fsops.ArchiveFormat` (`.tar.gz .tgz .tar.bz2 .tbz2 .tbz .tar.xz .txz .tar .zip .jar .war .7z .rar .cpio`) → `openArchive`; an association with a `Command` by mask → run in the terminal; a regular executable file (`mode&0111`) → executed in the terminal as a command (`terminal.Quote(path)`); otherwise → `/usr/bin/open <path>` (task `Opening <name>`).
- On `..` — `Current()` = the parent → `Load(parent)`.
- **Code:** `launch.go: enterFile, externalPlan, launch`.

#### Ctrl/⌘+PgUp — to the parent directory
- **Behavior:** `Load(filepath.Dir(Path))`. At the archive root it leaves the archive into the folder containing the archive (the path ends up outside the archive → local load, `Archive = nil`). For remote — the parent on the server. The cursor lands on the folder just left.
- **Code:** `far_keys.go`.

#### Ctrl/⌘+PgDn — enter a folder/archive
- **Behavior:** if a folder is under the cursor or `fsops.ArchiveFormat(name) != ""` — `enterFile()`. Enters exactly the supported formats (including `.war .cpio .tbz .txz`); single `.gz/.bz2/.xz` without `.tar` are not considered archives.
- **Code:** `far_keys.go`, `launch.go`.

#### Backspace
- Deletes the last character of the command line; on an empty line does nothing (does not go to the parent). In fast find — deletes a query character.

#### Cmd+L — Change directory
- **Dialog:** title `Change directory`, text `Enter a directory`, field = the current path, buttons `OK`/`Cancel`.
- **Behavior:** `expand(s, Path)`: `~`/`~/…` → the home directory; a relative path — relative to the current one; `Load`; error → status.
- **Code:** `app.go: gotoPrompt, expand`.

#### Alt+F1 / Alt+F2 — volume selection
- **Invocation:** Alt+F1 (left), Alt+F2 (right); menu `Left/Right → Change volume… AltF1/AltF2`; `Commands → Mounted volumes` (active).
- **Dialog:** the list `Volumes and home`: `/`, the home directory, then all `/Volumes/*` entries.
- **Behavior:** selecting makes the panel active, resets `Remote = nil` (the archive is not reset explicitly, but a `Load` of a local path outside the archive clears `Archive`), loads the path.
- **Code:** `panel_options.go: volumes`.

#### ⌘= / ⌘Shift+\ (Ctrl variants) — the same folder in the other panel
- **Invocation:** Ctrl/⌘+`=`; Ctrl/⌘+Shift+`\`/`|`; `Commands → Same folder in other panel ⌘=`.
- **Behavior (`matchOtherPanel`):** the source's `Remote`, `RemoteLocalPath`, `Archive` are copied to the target panel; if it was in Tree/Info/Quick view — it switches to `Full`; `Load(src.Path)`; on error the previous Remote/Archive are restored, status = the error; the cursor is placed on the same name as in the source (or on the pending position for an asynchronous load); `Hidden = false`.
- **Code:** `archive_panel.go: matchOtherPanel`.

#### ⌘U / Ctrl+U — swap panels
- Swaps the `Panels[0]` and `Panels[1]` structs (including modes, paths, marks); the active index does not change (focus moves to the other folder). Resets the preview.
- **Code:** `app.go: key`, `menu.go`.

#### F7 — new folder (the navigation part)
- **Dialog:** `Make folder`, `Folder name`, empty; for remote — `remoteMkdir`. In an archive — forbidden (`Archive is read-only. Use F5 to copy files out.`).
- **Behavior:** `os.MkdirAll(expand(s, base), 0755)`; error → confirmation `Error` / `Cannot create folder\n<path>\n\n<err>` with the buttons `Retry`, `Cancel`; success → `refresh()` + `focusCreatedDirectory`: `ForcedVisiblePath = path` (a created hidden folder is shown even with hidden files off), the parent of the created folder is loaded, the cursor is placed on it (or pending), the panel becomes active; if not found — `Folder created; hidden by panel settings`.
- **Code:** `app.go: function(7)`, `launch.go: focusCreatedDirectory`.

#### Folder history
- **Invocation:** Alt+F12; `Commands → Folder history`.
- **Behavior:** merges `a.folderHistory` and the `History` of both panels (unique, limit `HistoryLimit`), shows them in reverse order (most recent on top) in the `Folder history` list; selecting → `Load` into the active panel. The history is saved to setup.json (`Folders`) on `Save setup`.
- **Code:** `commands.go: historyMenu("Folders")`, `settings.go: saveSetup`.

#### View/edit history
- **Invocation:** Alt+F11; `Commands → File view/edit history`. The list `View/edit history` (`View <path>` / `Edit <path>`), empty → `View/edit history is empty`.

#### Find file / Find folder (navigating to the result)
- **Invocation:** Alt+F7 / `Commands → Find file…`, `Find folder…`. Local panels only.
- **Dialog:** `Find file` / `Find folder`: `Name masks` (`*`), `Containing text`, `Search subfolders` (on), `Case sensitive text`; button `Find`.
- **Behavior:** the search starts from the current folder (limit 10000 matches, task `Searching <root>`); the result is the list `"<title> — %d found, %d unreadable"` (+ ` (limit)`), relative paths; selecting → `revealPath`: folder → load it, file → load the parent and place the cursor (or `File is hidden by the current panel filter`). No matches: `No matches (%d unreadable paths)`.
- **Code:** `commands.go: findFiles, revealPath`.

#### Screens (F12, Ctrl+Tab)
- F12 — the `Screens` list: `Panels` + open documents (`View `/`Edit `, `* ` for modified ones). Ctrl+Tab / Ctrl+Shift+Tab, ⌘Shift+] / ⌘Shift+[ — cyclic switching between the panels and documents.
- **Code:** `document.go: screenMenu, cycleScreen`, `app.go: key`.

---

## 5. File selection

General: `..` is never marked. `changeSelection` saves `PreviousMarked` and `RestoreMarked` before a change. Folders are included in bulk operations only when `Settings.SelectFolders` (`Panel settings → Select folders with file masks`), unless stated otherwise. Marks are stored by name and restored when returning to the folder (`states`).

#### Insert / Space — mark the current entry
- **Invocation:** Insert; Space on an empty command line.
- **Behavior (`mark`):** inverts the mark of the current entry (except `..`), cursor down by 1 (limited to the last element). Works in any file mode, including Tree panel.
- **Code:** `app.go: mark`.

#### Shift+↑/↓/PgUp/PgDn/Home/End — range selection
- **Behavior:** the target position is computed as for normal navigation (page step `(Rows−9)×columns`); the mark value `on = !Marked[current]`; all entries from the old position up to the target, **excluding the target**, receive `on`; `RestoreMarked` = the previous marks; the cursor goes to the target. Also works in Tree panel.
- **Code:** `far_keys.go: farPanelKey`.

#### Num + / Num − — Select/Deselect group by mask
- **Invocation:** NumpadAdd / NumpadSubtract (always), or the `+`/`-` keys of the main keyboard on an empty command line; `Files → Select group… Num +`, `Deselect group… Num −`.
- **Where available:** not in Quick view/Info/Tree.
- **Dialog:** `Select group` / `Deselect group`, text `Masks: *.go;*.md | *_test.go (exclude)`, default value `*`, `OK`/`Cancel`.
- **Behavior:** `validateMasks` (error `unknown mask group` or a `filepath.Match` error); masks separated by `,`/`;`, `*.*` ≡ `*`, case-insensitive, the part after `|` is the exclusions, `<name>` groups are expanded from `Settings.MaskGroups`. Matching files are marked/unmarked, the others are unchanged.
- **Code:** `selection_keys.go`, `panel_options.go: selectGroup, matchesMask, validateMasks`.

#### Num * — invert
- **Invocation:** NumpadMultiply or `*` on an empty line; `Files → Invert selection Num *`.
- **Behavior:** inverts files; folders are included when `SelectFolders`, or with Ctrl/⌘ or Alt; with Alt+Num* folders are **unmarked** (`return false`), files are inverted.
- **Code:** `selection_keys.go`.

#### Ctrl/⌘+Num +/− — by extension; Alt+Num +/− — by base name
- **Behavior:** the current entry is taken (if `..` — nothing); `strings.ToLower(ext)` (cmd) or `lower(name without ext)` (Alt) is compared; matching entries are marked (+) / unmarked (−); folders when `SelectFolders`.

#### Shift+Num +/− — all files
- Only with Numpad codes: mark/unmark all (folders when `SelectFolders`).

#### Ctrl/⌘+M — Restore selection
- Swaps `Marked` and `RestoreMarked` (pressing again swaps back). Also `Files → Restore selection ⌘M`.
- **Code:** `panel_options.go: selectGroup("Restore")`.
#### Compare folders
- `Commands → Compare folders`: in both panels (marks are saved to `RestoreMarked`) files that are missing on the opposite side, differ in size, or are newer are marked. Status: `Marked files missing, different in size, or newer than the opposite panel (non-recursive)`.
- **Code:** `commands.go: compareFolders`.

#### Copying names to the clipboard (Ctrl+Insert and others)
- **Invocation:** Ctrl/⌘+Insert; Alt+Shift+Insert; Ctrl+Alt+Shift+Insert. Also ⌘C on an empty command line (`copyPanelCommand`).
- **Behavior:** if the command line is non-empty and there is no Shift/Alt — the command line is copied. Otherwise: the names of the selected entries (`Selected()`: marked ones, or the current one if it is not `..`) separated by `\n`; with Alt — full paths; with Alt+Ctrl — paths with symlinks resolved (`EvalSymlinks`, error → status) or `Remote.Label()+path` for remote. If there is nothing — the panel path.
- **Code:** `far_keys.go`, `mouse.go: copyPanelCommand`.

---

## 6. Sorting

#### Ctrl/⌘+F3…F11 — sort mode
- F3 `Name`, F4 `Extension`, F5 `Modified`, F6 `Size`, F7 `Unsorted`, F8 `Created`, F9 `Accessed`, F10 `Description`, F11 `Owner`. Pressing the same mode again toggles `Reverse`; a new mode resets `Reverse = false`. Then `Load(Path)`.
- For remote, the modes `Created`, `Accessed`, `Owner`, `Description` → `This sort mode requires local file metadata`.
- Note: ⌘+Shift+F3/F4 are intercepted earlier (forced internal viewer/editor), ⌘+Alt+F3 — folder size calculation.
- **Code:** `far_keys.go`.

#### Ctrl/⌘+F12 — sort menu
- **Dialog:** the list `Sort mode`: `Name`, `Extension`, `Size`, `Modified`, `Reverse order`, `Case sensitive`, `Directories first`, `Selected first`, `Unsorted`, `Created`, `Accessed`, `Description`, `Owner`. Items 4–7 are toggles; the item `Directories first` has been renamed to `Mix directories and files` (toggles `MixDirectories`). The current mode and the enabled toggles are marked with `✓ `. For a remote panel, modes requiring local metadata are rejected (`This sort mode requires local file metadata`). Also `Left/Right → Sort modes…`.
- In an active Info panel Ctrl+F12 opens `Info panel settings`.
- **Code:** `panel_options.go: sortMenu`.

#### Shift+F12 — Selected first; Shift+F11 — Sort groups on/off
- Shift+F12 toggles `SelectedFirst`, Shift+F11 — `DisableSortGroups`; both re-read the panel.

#### Comparison rules (`sortEntries`)
1. `..` is always first.
2. Folders before files if `!MixDirectories`.
3. `SelectedFirst`: marked entries come earlier.
4. Sort groups (`Settings.SortGroups`, masks separated by `;` in priority order; `Options → Advanced → Sort groups`, hint `Masks in priority order, separated by semicolons`; `Commands → Edit sort groups…`): when `!DisableSortGroups` and the mode ≠ `Unsorted`, the index of the first matching mask (non-matching — 10000).
5. `Unsorted` — directory read order (stable sort).
6. The mode key: `Created`/`Accessed` (Birthtime/Atime sec+nsec), `Owner` (UID), `Description` (the description string), `Extension` (lower ext), `Size` (`entrySize`: for folders — the calculated size or 0), `Modified` (ModTime).
7. Ties → name (case-insensitive if `!CaseSensitive`).
8. `Reverse` inverts the result of steps 6–7 (does not affect `..`, folders/files, selected first, groups).
- **Code:** `panel_options.go: sortEntries`, `sort_stat.go: compareStat`, `folder_sizes.go: entrySize`.

---

## 7. Filters, masks, mask groups, hidden files

#### File panel filter
- **Invocation:** `Commands → File panel filter…` (active panel).
- **Dialog:** `File panel filter`, text `Include masks | exclude masks; empty shows all`, value — the current filter.
- **Behavior:** `validateMasks`; `Filter` applies only to files (folders are always visible) on every load — local, remote, archive; the panel footer shows `[<filter>]`. The filter is saved in the setup (`panelOptions`).
- **Code:** `panel_options.go: filterPanel, matchesMask`.

#### Groups of file masks
- **Invocation:** `Options → Advanced → Groups of file masks`.
- **Dialog:** the list `Add group…` + `name = masks`; the form `Mask group`: `Name`, `Masks (empty deletes)`. Errors: `Invalid group name` (empty or contains `<>,;|`), `Use literal masks in groups` (nested `<>`), mask errors.
- Defaults: `arc` = `*.zip,*.tar,*.gz,*.bz2,*.xz,*.tgz,*.tbz,*.tbz2,*.txz,*.7z,*.rar,*.jar,*.war,*.cpio`, `source` = `*.go,*.c,*.h,*.js,*.ts,*.py`, `exec` = `*.sh,*.command`. Used in the filter, selection, sort groups, associations and highlighting (`<arc>`).
- **Code:** `settings.go`, `panel_options.go: expandMasks`.

#### Hidden files
- **Invocation:** ⌘Shift+. (intercepted natively, keyCode 47), Ctrl+H; `Options → Panel settings → Show hidden files (Cmd+Shift+.)`.
- **Behavior:** toggles `Settings.ShowHidden` (default true), resets `ForcedVisiblePath`, `configurePanel` + `refresh()` of both panels; the cursor stays on the same name. Hidden entries are names starting with `.` and files with the `UF_HIDDEN` flag (for local; in archives and remote — by the dot only). The setting is saved in the setup (it is not a "display only" state).
- **Code:** `app.go: key`, `far_keys.go`, `hidden_darwin.go`, `settings.go`.

#### .DS_Store
- `Options → Panel settings → Hide and ignore .DS_Store` (`Settings.IgnoreDSStore`, default false): `.DS_Store` is hidden in the panels, the tree, folder previews, and is also excluded from size calculation (`sizeKey` includes the flag, so the size cache is separate) and from operations (`o.IgnoreDSStore`).
- **Code:** `app.go: Load`, `tree.go`, `folder_size_cache.go`.

#### Panel settings (full list)
- The form `Panel settings`: `Show hidden files (Cmd+Shift+.)`, `Highlight files`, `Human-readable sizes (off = bytes)`, `Hide and ignore .DS_Store`, `Select folders with file masks`. Applying → `refresh()`.

---

## 8. Folder size calculation

#### F3 on a folder — sizes of the marked/current folders
- **Invocation:** F3 / ⌘3 / Numpad5 when a folder is under the cursor; `Files → View / folder size F3`.
- **Where available:** local, remote (1 worker) and archive panels; not when `Busy`.
- **Behavior (`calculateFolderSizes(false)`):** the list = the marked folders; if none — the folder under the cursor; on `..` without marks the **current** folder is calculated and the result is shown in the `..` row instead of `<DIR>`. A background task with the progress `Calculating folder sizes` (`ScanProgress` every 64 entries), up to 4 workers (`min(4, n)`), for remote — 1. Traversal: locally `fsops.WalkDir`, archive — by the index, remote — recursively (depth ≤ 128: `remote directory depth exceeds 128`; ≤ 100000 entries per folder: `remote folder exceeds 100000 entries`; an unsafe name: `server returned an unsafe filename`). For each folder `Bytes`, `Files`, `Dirs`, `Unreadable` (traversal/Info errors) are counted.
- The result is applied only if the panel is still on the same path/source: the results are merged into `folderSizes`, the panel is re-sorted (cursor/top are preserved by names), status `Calculated %d folders`, with unreadable entries `; %d unreadable entries (>= means partial size)`, when stopped ` · <error>`. Cancellation (Esc) keeps the folders already completed (`applyCompleted = true`).
- Display: in the size column a number replaces `<DIR>`; `>=` before a partial one (has `Unreadable`), `~` before a stale one (cache). Calculated sizes take part in `Size` sorting. Marks are not changed.
- **Code:** `folder_sizes.go`, `launch.go: openConfigured`.

#### ⌘Option+F3 (Ctrl+Alt+F3) — all folders
- `calculateFolderSizes(true)`: all folders of the panel (except `..`), regardless of marks. `Files → Calculate all folder sizes ⌘⌥F3`.

#### Size cache
- On every `beginLoad` (`loadFolderSizes`): key `{Path, Source, IgnoreDSStore}` (`Source` = `local`, `Remote.Label()` or `archive:<generation>:<path>` — a re-opened archive gets a new generation and the cache is not reused). When the key changes, the current map is saved to the snapshot history (if ≤ 16384 entries and ≤ 2 MiB estimated), and for the new key the saved map is restored; all restored values are marked `Stale` (`~`). History limits: 128 directories, 16384 entries in total, 2 MiB in total; the oldest are evicted. Re-loading the same path (Re-read) also marks the values `~`. After the list is installed, `pruneFolderSizes` removes sizes for folders that have disappeared. Nothing is saved to disk.
- **Code:** `folder_size_cache.go`.

---

## 9. Window bookmarks

Ten slots 0–9 per window instance: a folder bookmark (panel) or a document bookmark (viewer/editor). They are not saved and are not visible to other windows; on ⌘N the new window starts with empty slots. The fields `Settings.Shortcuts/ShortcutRemotes/ShortcutArchives` remain only for reading old `setup.json` files and are not used.

#### ⌘⌥0…9 — save the folder
- **Invocation:** ⌘⌥ + digit (the same keys as in the viewer/editor), not Numpad; the digit is taken from the `DigitN` code, the layout and Option do not interfere. Shift and Ctrl variants have no effect (macOS takes ⌘⇧3/4/5 for screenshots and Ctrl+digit for desktops). ⌘1…9 no longer duplicate F1…F9. `Commands → Bookmarks… → Save current folder…` → the dialog `Save bookmark`, `Slot 0–9`, value `1`, error `Choose 0–9`.
- **Behavior:** `Bookmarks[n] = {Kind: folder, Path, Cursor: name of the file under the cursor (not `..`)}`; for remote — a copy of the configuration without password/passphrase/host key with `Directory = Path`; for an archive — `Archive = Archive.Path`. Status `Bookmark %d saved: <path/file>`.
- **Code:** `bookmarks.go: bookmarkKey, panelBookmarkKey, saveFolderBookmark`.

#### ⌘0…9 — go to
- **Behavior:** empty slot → `Bookmark %d is empty`. A document bookmark → see the section "Cmd+0..9 / Cmd+Option+0..9 — bookmarks" (opens the viewer/editor). A folder bookmark: from a document the panels are shown first (the document screens stay open). Remote: if some panel is already connected to the same server — the active panel is reconnected to the saved directory, otherwise the connection dialog with pre-filled fields. Archive → `openArchiveAt(archive, path, cursor)`. Local: an `os.Stat` check (error → status), not a folder → `Bookmark is not a directory`; then loads are cancelled, `Remote = nil`, `Archive = nil`, `loadSource(path)`. After loading the cursor is placed on the remembered file (`placeCursor`; for an asynchronous load via `pendingPosition`); if the file is missing — the cursor as usual. The remembered cursor positions and marks (`states`) are preserved.
- The `Bookmarks` menu: `Save current folder…`, then `0  <label>` … `9  <label>`: folder — `<path>/<file>` (for remote with the `Label()` prefix), document — `Edit|View <path>:<line>`, help — `Help`.
- **Code:** `bookmarks.go: goBookmark, goFolderBookmark, bookmarksMenu`.

---

## 10. Fast find (quick prefix search)

#### Alt+character — start the search
- **Invocation:** Option+printable character (Cocoa translates the character by the current layout without the Option transformation, `panel_keys_darwin.h`; Numpad `*`, `+`, `-` keep their codes). Also pasting text (⌘V) while the search window is open.
- **Behavior (`fastFindText`):** characters are added one by one; for each prefix the first match of `filepath.Match(lower(query)+"*", lower(name))` is searched from the current cursor forward, wrapping around (except `..`); if the prefix is not found and the query is already non-empty — input stops at the first non-matching character (the first character is always accepted). Control characters end the input. The query may contain `*` and `?` (wildcards); `[` and `\` are treated literally; comparison uses NFC normalization.
- Window: Black on Gray frame, 3 lines, width `min(40, W−4)`, at the bottom edge (`y = H−7`), on the left for the left panel and on the right for the right one; title ` Find `, the field Black on Cyan with the tail of the query and `▌`.
- Keys while the search is open: printable without Ctrl/⌘ — add; Backspace — delete a character and repeat the search; ↓/↑ or ⌘Enter/Ctrl+Enter / ⌘Shift+Enter — next/previous match, wrapping around; Enter — close and `enterFile()`; Esc — close (the position is kept); any other key closes the search and is handled normally. A mouse click, Delete, Shift+Enter, ⌘Shift+., Num+/−/*, bookmarks also close the search.
- Priority: printable Alt characters and an already open search are handled before the selection keys, except for explicit Numpad codes.
- **Code:** `fastfind.go`, `app.go: key, Handle("paste")`, `internal/native/panel_keys_darwin.h`.

---

## 11. Archives in the panel

#### Enter on an archive — open the archive as a folder
- **Behavior (`openArchive`):** the background task `Opening archive` reads the index (`fsops.ReadArchiveIndex`); then `Remote = nil`, `Archive = catalog`, the Tree/Info/Quick view mode is replaced with `Full`, `Load(target)` (if the target path is not an archive directory — the root). If the panel already shows this same archive, the current subdirectory is kept (used by Re-read).
- Inside: title `[Archive] <path>`; entries are the members of the directory (`Children[rel]`), honoring `.DS_Store`/hidden (by the dot)/the filter; `TotalBytes` — the sum of member sizes, `ItemCount`; `InfoText` = `Archive (read-only)\nF5 extracts selected files/folders\n.. exits the archive`; errors `outside archive`, `not an archive directory`.
- Enter in an archive (`enterArchiveMember`): folder → `Load`; a nested archive → `Copy the nested archive out with F5 to open it`; file → `viewArchiveMember` (extraction to the temporary directory `farshmac-archive-view-*`, task `Opening archive member`, the document is read-only; symlink → `Use F5 to copy the archive link`).
- `..` at the archive root or Ctrl+PgUp → a path outside the archive → a normal local load (the cursor on the archive file).
- F4/F6/F7/F8 → `Archive is read-only. Use F5 to copy files out.`; Shift+Enter on a member → `Use F5 to copy this archive item out before opening it with macOS`.
- `filesystemPath()` for an archive — the folder containing the archive (used by the terminal and the command line).
- Bookmarks and matchOtherPanel carry `Archive` over.
- **Code:** `archive_panel.go`, `launch.go: enterFile`, `fsops/formats.go`.

---

## 12. Inserting names/paths into the command line
#### ⌘Enter / Ctrl+J — the current file's name
- **Behavior (`insertFilename`):** the name of the entry under the cursor (including `..`); if it contains control characters → `Cannot insert a filename containing control characters`; a name with a leading `-` gets the prefix `./`; if there are characters outside `[A-Za-z0-9_./-]` — `terminal.Quote`; a space is added before the insertion if the line does not end with a space/tab; a space after the name. The command is not executed. Also `Commands → Insert filename ⌘Enter`. With fast find open, ⌘Enter — next match.
- **Code:** `app.go: insertFilename`.

#### Ctrl/⌘+F — the full path of the current file; Ctrl/⌘+; — the current file of the other panel
- `appendPanelValue(p.Current())` / of the other panel: always `terminal.Quote(value)` + a space; an empty value is ignored; control characters → `Cannot insert a path containing control characters`.

#### Ctrl/⌘+Shift+Enter — the name of the current file of the other panel
- `appendPanelValue(other.Entries[other.Cursor].Name)` (name only, quoted when needed).

#### Ctrl/⌘+[ and Ctrl/⌘+] — the path of the left / right panel; with Shift — active / inactive
- Codes `BracketLeft`/`BracketRight` or the characters `[ ] { }`. Note: ⌘Shift+[ / ] are intercepted earlier as screen switching (`cycleScreen`), so the Shift variants are really only available with Ctrl.
- **Code:** `far_keys.go: farPanelKey, appendPanelValue`.

#### ⌘Space — autocompletion
- `Settings.CompleteFiles` (default true, `Terminal settings`/`AutoComplete settings`). Completes the last word of the command line relative to the active panel's path; quotes/`$`/`\` in the word → `Completion: use an unquoted path prefix`; a single match is inserted (quoted when needed, folders with `/`), several — the list `Complete filename`; none → `No matching paths`. Case per `CompleteIgnoreCase`.
- **Code:** `completion.go`.

#### Shift+Insert — paste from the clipboard
- Handled by the renderer (`pasteRequest`) → `paste` → command line/dialog/search. An architectural peculiarity: the system pasteboard is available only to the renderer, so there is no Shift+Insert handling in Go; headless tests use the `paste` message.

---

## 13. Mouse

#### Hit testing (`panelAt`, `panelItem`)
- The panel is determined by `panelRect`: `1 ≤ y < Rows−4`, `px < x < px+w−1`, the panel is not hidden. A file row: `3 ≤ y < Rows−6`; column `min(columns−1, (x−px−1)/((w−2)/columns))`; index `Offset + column×(Rows−9) + (y−3)`; clicks on the title, frame, footer, outside the list are ignored (the cursor does not change). Quick view and Info panel have no items (Info is handled separately).
- **Code:** `mouse.go`.

#### Left click
- Activates the panel, places the cursor on the item, remembers the anchor (`mouseAnchors[panel]`, reset when the path changes), starts a drag session. Marks are not changed.
- **Shift+click:** marks the range from the anchor to the item (adds to the existing ones).
- **⌘+click or right button / Control+click (the renderer sends `Button: 2`):** inverts the item's mark and places the cursor; anchor = the item.
- **Left double-click (without Shift/⌘):** ends the drag and sends Enter (`enterFile`).
- A click while a menu/dialog/form/document is open or Busy with button ≠ 0 is ignored; when `Busy`, a left click on the progress `Cancel` button cancels. Any click closes fast find.
- **Code:** `mouse.go: panelMouse`, `app.go: click`.

#### Drag (with the button held)
- Left: marks are reset to the state at the moment of the press (`base`) and the range anchor…current is marked; the cursor follows. Right/Control: the range from the last item to the current one receives the value of the first click (mark or unmark). Going past the top/bottom of the list scrolls `Offset` by 1 per event; x is limited to the panel. The drag stops when the panel path changes or on `mouseUp`.
- **Code:** `mouse.go: dragMouse`.

#### Wheel / scroll
- `wheel` (`Delta`) and `scroll` (`Y×3`), limited to ±12 per event. Over a file panel: `Offset` is shifted within `[0, len−capacity]`, the cursor is clamped into the visible area. Over the Info panel — `InfoOffset`. Over Quick view — nothing. The panel is determined by the pointer position (not the active one). With a dialog/Busy — ignored; in a menu/form — moves the selection.
- **Code:** `mouse.go: scrollMouse`.

#### Click on the menu bar / key bar / command line
- y=0 left: opens the menu section under the cursor (with a menu open, a click outside it closes the menu; inside — executes the item).
- y=Rows−1 left: the function key by position (only when the key bar is visible).
- A click on the status/command line (y = Rows−3, Rows−2) does nothing.

---

## 14. Panel visibility, width, zoom

#### ⌘F1 / ⌘F2 (Ctrl+F1/F2) — hide/show the left/right panel; Ctrl/⌘+P — the inactive one
- **Behavior (`togglePanel`):** `Hidden` is inverted; if the active panel is being hidden and the other is visible, the other becomes active. Menu `Left/Right → Panel on/off ⌘F1/⌘F2`. The hidden area is a gray fill; Tab can return focus to the hidden panel. Toggle view (`toggleOtherView`, `setView`, `matchOtherPanel`) clears `Hidden`.
- **Code:** `archive_panel.go: togglePanel`, `far_keys.go`.

#### Ctrl/⌘+← / → — panel widths; Ctrl/⌘+Num5 — reset
- Only on an empty command line and without Shift. `panelSplit` (0 = 50/50) changes by ±2 within `20..80` percent of the left panel width. Num5 with cmd — `panelSplit = 0`. Not saved in the setup.
- **Code:** `far_keys.go`, `panel_draw.go: panelRect`.

#### Ctrl/⌘+O — panels ↔ terminal
- Hides the panels and shows the built-in terminal (closes the menu and fast find); on showing, syncs the shell cwd with the active local panel (when the shell is at a prompt). Returning: with a ready shell the active panel is re-read in the shell cwd (`syncCwd`), otherwise deferred until the prompt; then `refresh()` of both panels. Terminal footer: `⌘O  Panels    ·    Shell ready    ·    <cwd>` / `⌘O  Panels    ·    ⌘.  Interrupt` / `⌘O  Panels    ·    Starting <shell> (loading shell configuration)…    ·    ⌘. Interrupt`.
- **Code:** `app.go: key, showTerminal, returnFromTerminal, syncCwd`.

#### Alt+F9 — window zoom
- `Update.Zoom = true` → native `[window zoom:]` (maximize/restore the macOS window). Font scale: `Options → General settings → Font size (10–24)` (`Font size must be 10–24`); there are no separate zoom keys in the panels.
- **Code:** `far_keys.go`, `window_darwin.m`.

#### ⌃⌘F — Full screen
- `Commands → Full screen`: `Update.Fullscreen = true` (native toggle).

---

## 15. Other panel-related features

#### Ctrl/⌘+B — key bar on/off
- Toggles `Settings.ShowKeys`.

#### Shift+Enter — open in macOS
- `openSystemSelected`: remote → `Copy remote files to a local panel before opening them with macOS`; an archive member → the message above; otherwise `/usr/bin/open <path>` (folders — in Finder) in the task `Opening <name>`.
- **Code:** `launch.go`.

#### File descriptions (descript.ion)
- `Files → Describe files…`: the dialog `Describe files`, `Description (empty removes it)`, value — the current description of the first selected entry; the `Settings.DescriptionFile` file (`Options → Advanced → File descriptions`, `Description file name`, error `Enter a file name without a path`) is written atomically; names with spaces are quoted; error `this filename cannot be stored in descript.ion`. Reading: lines `name desc` or `"name with spaces" desc`, line limit 1 MiB. Not for remote/archive.
- **Code:** `panel_options.go: readDescriptions, describeFiles`.

#### Save setup (Shift+F9, Options → Save setup)
- Saves `Settings`, the `panelOptions` of both panels (mode, sort, filter, flags, Hidden), the paths (`filesystemPath`), the command/folder/view history to `~/Library/Application Support/FarshMac/setup.json`; status `Setup saved`. With `AutoSave` — on close. Folders are restored via session.json (separately).
- **Code:** `settings.go`.

#### Alt+F6 — Create link
- The form `Create link`: `Link name` (the other panel's path + name), `Symbolic link` (on); `os.Symlink`/`os.Link`; `refresh()`. Not for remote/archive.

#### F1 — help
- Opens the document `FarshMac — Help` with the `help` text (see the discrepancies below).

---

## Discrepancies between the F1 help / README / parity documents and the code

> **Closed on the `backlog-fixes` branch:** the decisions for each item are in the "Discrepancy decisions" section of `docs/IMPLEMENTATION_BACKLOG.md`.

1. **F1: `Cmd+R Refresh panels`** — the code re-reads only the active panel (`rereadPanel(a.Active)`); both panels are re-read only on `refresh()` (return from the terminal, settings change). The menu `Left/Right → Re-read ⌘R` re-reads the specific panel regardless of which is active.
2. **F1: `Cmd+Shift+.  Show / hide hidden files (display only)`** — in fact the persistent setting `Settings.ShowHidden` is toggled (the same as in `Panel settings`), and it is saved to setup.json; "display only" is wrong. Ctrl+H does the same but is not mentioned in the help/README.
3. **F1: `Ctrl+Q`, `Ctrl+L / Ctrl+T`, `Ctrl+P / Ctrl+B`, `Ctrl+F3..F11`, `Ctrl+F12`, `Ctrl+F / Ctrl+;`, `Ctrl+PgUp/PgDn`, `Ctrl+Left/Right`, `Ctrl+Insert`, `Ctrl+E/X/Y`** — in the code most of these combinations also accept ⌘ (`cmd = Ctrl || Meta`): Ctrl+T, Ctrl+P, Ctrl+B, Ctrl+F3..F12, Ctrl+F, Ctrl+;, Ctrl+PgUp/PgDn, Ctrl+←/→, Ctrl+Y, Ctrl+Insert also work with ⌘. Exceptions — Ctrl only: Ctrl+Q, Ctrl+L (⌘L = Change directory), Ctrl+H, Ctrl+E/X, Ctrl+1..9 / Ctrl+Shift+1..9. The help does not reflect this.
4. **Not documented in F1/README but present in the code:** Ctrl+J (insert file name), Ctrl/⌘+Shift+Enter (the other panel's file name), Ctrl+[ / Ctrl+] (the left/right panel path) and Ctrl+Shift+[ / ] (active/inactive), ⌘/Ctrl+Shift+S (human-readable sizes), Shift+F11 (sort groups on/off), Alt+F6 (create link), Alt+F9 (window zoom), Alt+F7 (find file), Alt+F8 (command history), Alt+F10 (folder tree), Numpad5 = F3, Ctrl+Alt+Shift+Insert (paths with resolved symlinks), Ctrl+U (swap), Ctrl+R, Ctrl+↑/↓ (command history), Shift+F10 (last menu), Compare folders, Shift+click/⌘+click/right button/mouse drag.
5. **F1: `Ctrl+Shift+1..9 Save folder shortcut (panels)` / `Ctrl+1..9`** — correct, but going to a local bookmark additionally **resets all remembered cursor positions of the panel** (`p.states = nil`), which is described nowhere.
6. **F1: `Num * Invert files; Cmd/Ctrl+Num * includes folders`** — it is not mentioned that Alt+Num* inverts files and at the same time unmarks folders.
7. **Menu `Sort mode → Directories first`** — the item toggles `MixDirectories` (i.e. when selected, folders *stop* coming first if the flag was off); the toggles `Reverse order`, `Case sensitive`, `Directories first`, `Selected first` have no state indicators.
8. **F1: `Ctrl+PgUp/PgDown Parent / enter folder or archive`** — `isArchiveName` for Ctrl+PgDn includes `.gz`, `.bz2`, `.xz` without `.tar`, which `fsops.ArchiveFormat` does not support: Ctrl+PgDn on such a file calls `enterFile`, which opens the file with the system application rather than as an archive.
9. **BEHAVIOR_PARITY: "Esc retains the found position"** and **"Search does not replace the command line"** — match the code. But Paste in fast find is described as "until the first unmatched character" — the code always accepts the first character, even without a match (`TestFastFindIncludesFirstLetterWithoutMatch`).
10. **README: `F3 / F4 View / edit; F3 on a folder calculates selected folder sizes`** — correct; additionally F4 on a folder opens `File attributes` (not documented in README/F1).
11. **Legacy modes** `Descriptions`, `Long descriptions`, `File owners`, `File links`, `Alternative full` are mentioned in F1 (`File links view   Names, sizes and hard-link counts`) but are not available from the menu or the settings: `configurePanel` forcibly switches them to `Full` at startup/refresh. The `Medium` mode (2 columns) is in the menu but not mentioned in the help.
12. **F1: `Tab Switch active panel`** — Tab also switches focus to a hidden (⌘F1/⌘F2) panel; in that state the cursor and title are not visible.
13. **F1 help and the menu `Commands → Quick view on/off Ctrl+Q`** — Quick view via Ctrl+Q is allowed for a remote inactive panel (shows `Remote file: use F3 to download and view`), whereas via the Left/Right menu it is forbidden for remote (`Choose a file view for remote panels`); for archive members Quick view shows a `stat` error because the virtual path does not exist (described nowhere).


<a id="part-2"></a>
# Part 2. File operations, archives, launching programs

The description is based on the current code (`internal/ui`, `internal/fsops`, `internal/remotefs`), not on the documentation. Interface strings are given verbatim (English, as in the code). Discrepancies between the documentation and the code are collected at the end.
## Common Operation Infrastructure

### Background Tasks and "Busy"

#### Background task (`backgroundTask` / `workProgress` / `backgroundProgress`)
- **Invocation:** internally, by any long-running operation (copying, deletion, archives, search, opening an archive, process list, etc.).
- **Where available:** only if `a.Busy == false` and the window is not closed; otherwise the call is silently ignored (one operation per window).
- **Behavior:**
  1. An `fsops.Operation(label)` is created with `IgnoreDSStore = Settings.IgnoreDSStore`; `a.Busy = true`, `a.Operation = o`, `a.Status = label`.
  2. The work runs in a goroutine; the result (`apply`, `cleanup`, `err`) is received in `pollTask` (from `Poll`, on the engine queue).
  3. While `Busy`: `PanelKeys=false`; keys — only `Escape`/`Alt+C` (cancel) and the keys of an open question dialog; clicks — only on the progress `Cancel` button or on the question; `Cmd+Q` responds with the status `Wait for the file operation to finish`.
  4. On completion: `a.LastProgress` = a snapshot of the progress, `Busy=false`, `Status=""`; on error → `a.error(err)` (status line = error text) and, if `task.reportError` (always for `workProgress`), a modal confirmation `"<Label> failed"` with the error text and an `OK` button — except for cancellation (`operation cancelled`/`context canceled`), for which there is no dialog, only the status `operation cancelled`.
  5. `workProgress` additionally: attaches `operationQuestions(o)` (Retry/Skip and read-only questions), treats `ErrSkipped` as success; on success the status is `Done` or `Done — N item(s) skipped`; `cleanup` always does `a.refresh()` (re-reads both panels).
  6. `backgroundProgress` — the same task without the question handler and without the final error dialog (the error appears only in the status).
- **Code:** `internal/ui/task.go` (`backgroundTask`, `pollTask`), `internal/ui/progress.go` (`workProgress`), `internal/ui/commands.go` (`backgroundProgress`).

#### Progress window
- **Invocation:** drawn automatically while `a.Operation != nil`.
- **Behavior (drawing, `drawProgress`):** a modal 88×15 window (smaller on a small screen) with the title = the operation label. Contents:
  - line 1: the current path (`Progress.Path`, trimmed from the left with `tail`).
  - While `Scanning` or `Streaming` (total unknown): `Working / calculating… ▰▱▱▱  N items` with frame animation `▰▱▱▱ ▱▰▱▱ ▱▱▰▱ ▱▱▱▰` (150 ms/frame), where N = `TotalFiles`. In `Streaming` mode there is an additional line `File  ████░░░░` (progress of the current file).
  - Otherwise: `Total %5.1f%%  <Files> / <TotalFiles> items`, below it the overall bar (`█`/`░`, by bytes; if `TotalBytes==0` — by files), then `File  <bar of the current file>`.
  - If `Workers > 1`, the file line is replaced with `Parallel transfers: <Active> active / <Workers> workers`.
  - Statistics: `<bytes> / <total>  ·  <rate>/s  ·  <elapsed>s` (+ `  ETA Ns` if the rate > 0 and the total is known); in `Streaming`: `<bytes> processed · <rate>/s · <elapsed>s`.
  - Button `{ Cancel }` (mnemonic `c`).
- **Cancellation:** `Esc`, `Alt+C` or a click on `Cancel` → `cancelOperation()`: `Operation.Cancel()`, the open question dialog is closed (`OperationQuestion`, or one with the title `Overwrite`/`Cannot preserve metadata`), status `Cancelling…`. The worker sees the cancellation via `o.Check()`/the context (including window closing via `Close()`).
- **Code:** `internal/ui/progress.go`, `internal/fsops/progress.go` (`Progress`, `Operation`, `Streaming`, `Scan`, `Ready`).

#### Questions during an operation (common mechanism)
- **Behavior:** the worker pushes a closure onto `a.jobs` which opens `confirm(...)` with `Form.OperationQuestion = true`, and blocks on the answer channel; parallel workers are serialized by the `o.Questions` mutex. Cancelling the operation (Esc, window close) unblocks the question — the last option (`Cancel`) is returned. Esc in a question dialog = `cancelOperation()` of the whole operation (since `Busy`), not just closing the dialog. `PageUp/PageDown` scroll long messages; Alt+letter — button mnemonics.
- **Code:** `internal/ui/operation_questions.go` (`operationChoice`), `internal/ui/app.go` (`key` while `Busy`).

#### Retry/Skip error dialog (`o.Failure`)
- **Dialogs:** title `Error`; text: `Cannot <operation>\n<source>` + (if present) `\nTo: <destination>` + `\n\n<OS error text>`; buttons `Retry`, `Skip`, `Skip all`, `Cancel`.
- **Behavior:** `Retry` repeats only the step that failed (`o.Attempt`), not the whole batch; `Skip` marks the item as skipped (`Skipped++`, `CompleteTree`); `Skip all` — all subsequent errors in this operation are skipped without questions; `Cancel`/Esc — `ErrCancelled`. `context.Canceled` errors are turned into `ErrCancelled`. Operation steps wrapped in `Attempt`: `copy`, `move`, `read folder`, `read attributes`, `create folder`, `delete`, `delete source`, `delete source folder`, `move to Trash`, `open destination`, `read destination`, `connect`, `create archive`, `extract archive`, `create folder` (remote).
- **Code:** `internal/fsops/recovery.go` (`Attempt`, `SkipItem`, `IgnoreSkipped`), `internal/ui/operation_questions.go`.

#### "read-only" question on deletion (`o.BeforeDelete`)
- **When:** before deleting/moving to Trash a regular file without write bits (`mode & 0222 == 0`); not asked for directories and other types.
- **Dialog:** `Warning`: `The file is read only\n<path>\nDelete it?`; buttons `Delete`, `All`, `Skip`, `Skip all`, `Cancel`. `All`/`Skip all` are remembered until the end of the operation.
- **Code:** `operation_questions.go` (`operationQuestions`), applied in `fsops.removeTreeStep`, `ui/delete.go` (Trash), `remotefs.removeTreeStep`.

## Copying and Moving

#### F5 — Copy
- **Invocation:** `F5`, `Cmd+5`, menu `Files → Copy…` (`F5`); `Shift+F5` — only the current item (see below).
- **Where available:** the active panel is local, remote or archive; **unavailable** if the opposite panel is an archive (status `Choose a regular destination panel; writing into archives is not supported`). If the selection is empty (empty panel or the cursor on `..` without marks) — nothing happens.
- **Item selection:** `Panel.Selected()` — all marked items (except `..`), otherwise the current item (not `..`). Before the operation, `RestoreMarked` is saved (for `Cmd+M`).
- **Dialog (form, title `Copy`):**
  - `Copy N item(s) to:` — default value: the path of the opposite panel; if a single item is selected — `<path of the other panel>/<item name>`. The text is selected (`Form.Selected = Settings.SelectDialogInput`, on by default): typing a character replaces the value, `Cmd+A` selects all, pasting replaces.
  - `[x] Preserve permissions (mode, owner, group, ACL)` — defaults to `Settings.CopyPermissions` (default true).
  - `[x] Preserve attributes (xattrs, flags, timestamps)` — `Settings.CopyAttributes` (default true).
  - `Parallel transfers (1–8):` — `Settings.CopyWorkers` (default 4, normalized to 1..8).
  - Buttons `{ Copy }` `[ Cancel ]` (Primary = `Copy`). Checkboxes are toggled with Space/←/→/mouse/Alt+mnemonic.
  - Validation: a non-number or a value outside 1..8 → status `parallel transfers must be between 1 and 8`, the form closes, the operation does not start. The checkbox values and workers are saved in `Settings` (persisted with setup).
- **Destination interpretation:**
  - `expand(s, base)`: `~`/`~/…` → the home directory; a relative path — relative to `base` (the active panel path; for an archive — the directory of the archive file; when a remote panel is involved — the destination panel path); `filepath.Clean`.
  - The destination is checked with `Lstat` via `targetEndpoint` (locally or on the server). If more than one item is selected and the destination is not an existing directory → error `destination must be an existing directory`.
  - `transferIntoDirectory(input, dst, suggested, count, isDir)`: items are placed **inside** the directory `dst` (as `dst/<basename>`) if `dst` is an existing directory **and** (there is more than one item, **or** the entered path differs from the suggested one, **or** the input ends with `/`). The suggested single path, even if it is an existing directory, means that object itself (merging the directory `folder` into the existing `folder`, not `folder/folder`).
- **Execution (`remotefs.Copy`, also used for local copies):**
  1. `o.CopyMetadata = &{Permissions, Attributes}`; `metadataQuestions(o, false)` and `collisionQuestions(o, Settings.ConfirmOverwrite)` are attached.
  2. From an archive: first `catalog.ExtractSelection(names, <tmp>/content, o)` into a temporary directory `farshmac-archive-copy-*` (removed via `defer`), then a normal copy from temp.
  3. `o.Parallel(workers)`, `o.Streaming()` (the total is not known in advance — the "Working / calculating…" indicator and counter). Workers are separate goroutines with their own sessions (for FTP/SFTP — separate connections); the queue is `workers*2`.
  4. Walk: `Ignored` entries are skipped (`.DS_Store` with `IgnoreDSStore`), limits of `1 000 000` entries / depth `256` → `transfer tree limit exceeded`. Directories are created by the walker (locally with Permissions: `Mkdir 0700`, then final metadata; without Permissions — `Mkdir 0777` through umask); an existing destination directory is reused (merge), a non-directory → `destination is not a directory: <path>`. Files are put into the worker queue.
  5. Local→local, each file goes through `fsops.Transfer(src, dst, move=false, …)`; when a server is involved — `copyFile` (see "Remote panels").
  6. Pre-start checks: `destination is inside source` (same endpoint and the destination inside the source), `validateLocalPair`.
  7. After all files — directory metadata in reverse order (`ApplyMetadata`).
- **Local file (`fsops.Copy` → `copyEntry`):**
  - Symbolic links are never dereferenced: the link itself is copied (`Readlink`+`Symlink`).
  - Hard links: within a single operation they are only tracked when `moving` (moving between volumes), see F6.
  - Regular file: if both metadata checkboxes are on (or the options are not set) — `fclonefileat(... CLONE_NOOWNERCOPY)` (APFS clone, atomic, no replacement of an existing file). On `ENOTSUP/EXDEV/EINVAL/ENOSYS/EPERM` — streaming copy: a temporary file `.farshmac-copy-<rand>` in the destination directory (mode 0600, or 0666 when Permissions is off), a 256 KiB buffer with progress, then publication via `renamex_np(RENAME_EXCL)` — an existing file is never overwritten "blindly". If one of the checkboxes is off, clonefile is not used (so that metadata does not "leak").
  - Special files (sockets, devices…) → `unsupported special file: <path>`.
  - Metadata (`preserveMetadata`, `metadata_darwin.go`): with the options on, after the copy — `lchown` (owner/group), `copyfile(COPYFILE_NOFOLLOW | ACL | XATTR)`, `lchmod`, `setattrlist` (creation/modification/access time with nanoseconds, also for symlinks), `lchflags`. Then the `lstat` of the source and the copy are compared: differences → error `<src>: destination did not preserve <permissions|owner/group|file flags|modification time|creation time>` and the "Cannot preserve metadata" dialog (see below). On refusal the copy result is removed (`RemoveAll(dst)`), the source is untouched.
  - A directory under the implicit policy (`CopyMetadata == nil`, e.g. the append prefix) — `Chmod`+`Chtimes` as in the source.
- **Collisions:** see "Collision dialog".
- **Cancellation:** `Esc` → `Cancelling…`; the unfinished temporary file is removed; already completed files remain (they are "not rolled back"); directories created by the walker remain (when walking from `remotefs.Copy`); with `fsops.Copy` of a whole directory (the cross-volume move/append-prefix path) the unfinished new directory is removed with `RemoveAll`.
- **Messages:** status `Copy` → `Done` / `Done — N item(s) skipped`; error → status + dialog `Copy failed` `[OK]`; `operation cancelled`.
- **Special cases:** `.DS_Store` is skipped with `Settings.IgnoreDSStore` (`Hide and ignore .DS_Store` in Panel settings); F5 from an archive extracts (see "Archives"); remote panels — see below. The test `TestCopyOptionsDialogAndPersistence` confirms: with the checkboxes off → permissions/date are not preserved; the choice is remembered in setup.
- **Code:** `internal/ui/transfers.go` (`transferDialog`, `transferIntoDirectory`), `internal/remotefs/transfer.go` (`Copy`), `internal/fsops/files.go` (`Copy`, `copyEntry`, `copyRegularStream`, `createCopyTemp`), `internal/fsops/clone_darwin.go`, `internal/fsops/metadata_darwin.go`, `internal/fsops/parallel.go`, `internal/fsops/ignore.go`, `internal/fsops/walk.go`.

#### F6 — Move / rename
- **Invocation:** `F6`, `Cmd+6`, menu `Files → Rename or move…`; `Shift+F6` — only the current item.
- **Where available:** a local panel; in an archive → status `Archive is read-only. Use F5 to copy files out.`; if either panel is remote → `remoteMoveDialog` (see below); an archive as the opposite panel → `Choose a regular destination panel; writing into archives is not supported`.
- **Dialog:** a simple `prompt` (Dialog) with the title `Move / rename`, the text `N item(s) to`, an input field (value as for F5: the other panel's path or `<other panel>/<name>`), buttons `OK` / `Cancel`. There are no metadata checkboxes or workers (a move always preserves all metadata and runs sequentially, without parallel workers). If `Settings.RememberDialogValues` is on and the value is empty — the previous input is substituted.
- **Behavior (`fsops.Transfer(src, dst, move=true)` for each item, sequentially):**
  1. `o.IgnoreDSStore = false` — when moving, `.DS_Store` is carried along with the directory (`Ignored` takes `!o.moving` into account). `o.Streaming()` — the tree is not scanned in advance (a same-volume rename is instant).
  2. `moveMetadataQuestions` and `collisionQuestions` are attached. The same destination/`transferIntoDirectory` logic as for F5.
  3. Checks: `destination is inside source`; `source and destination are the same file`.
  4. Destination does not exist → `fsops.Move` = `renamex_np(RENAME_EXCL)` (atomic, no replacement). `EXDEV` → `moveAcrossVolumes`: `o.Scan` (counting the total), `Copy` with `o.moving=true` (always Permissions+Attributes), then `Remove(src)` via `Attempt("delete source")`. The source is removed only after a complete copy and explicit consent to the loss of metadata.
  5. Directory → existing directory: recursive merge item by item; after the walk, if the source became empty — `Attempt("delete source", os.Remove)`; with skips the source directory remains.
  6. File → existing file: the collision dialog. `Replace`: a check that the destination has not changed (`destination changed during move`), then `os.Rename` (same volume) or a staging copy in `.farshmac-transfer-*` + `Rename` over it + removal of the source. `Append`: `appendFile` then removal of the source.
  7. Hard links (`hardlinks.go`): when `moving`, the (dev, inode) of copied files with `Nlink ≥ 2` are remembered; encountering the same inode again → `os.Link` to the already copied file instead of copying; on failure — a metadata question with the error `cannot preserve hard-link relationship: …`. After publication through staging the paths are relinked (`relocateHardlinks`).
- **"Cannot preserve metadata" dialog (`metadataQuestions`):** title `Cannot preserve metadata`; text: `Move with changed metadata and remove the source?\n<src>\nTo: <dst>\n\n<err>` (for F5: `Copy with changed metadata?`); buttons `Move anyway` (F5: `Copy anyway`), `For all`, `Retry`, `Skip`, `Skip all`, `Cancel`. `For all`/`Skip all` are remembered; `Retry` → `ErrRetryMetadata` (repeats only the metadata); `Skip` → the item is skipped, the source remains; `Cancel`/Esc → cancellation of the whole operation.
- **Messages:** status `Move / rename` → `Done`/`Done — N item(s) skipped`; `Move / rename failed` `[OK]`; errors `destination changed during move`, `cannot replace a file with a directory or a directory with a file`, `choose a different destination name`, `destination must be an existing directory`.
- **Code:** `internal/ui/transfers.go`, `internal/ui/move_metadata.go`, `internal/fsops/transfer.go` (`transfer`, `moveAcrossVolumes`), `internal/fsops/move_darwin.go`, `internal/fsops/hardlinks.go`, `internal/fsops/append.go`.

#### Shift+F5 / Shift+F6 — copy/move only the current item
- **Invocation:** `Shift+F5`, `Shift+F6` (`farPanelKey`).
- **Behavior:** `p.Marked` is temporarily cleared, `transferDialog` is called, then the marks and `RestoreMarked` are restored — the dialog suggests `<other panel>/<current name>` and operates only on the current item. All other rules are as for F5/F6.
- **Code:** `internal/ui/far_keys.go` (`currentTransfer`).

#### Collision dialog (file already exists)
- **When:** the destination exists and at least one side is not a directory (directory→directory merges without a question). If `Settings.ConfirmOverwrite` is off (`Confirm overwrite` in General settings / `Overwrite files` in Confirmations) — automatically `Replace` (but the read-only question is still asked).
- **Dialog:** title `Warning`; text:
  ```
  The file already exists
  <destination>

  New: <N> bytes  2006-01-02 15:04:05
  Existing: <N> bytes  2006-01-02 15:04:05
  ```
  (for a directory, `Folder` instead of the size; when info is unavailable — `New: unavailable`). Buttons: `Overwrite`, `Skip`, `Rename`, `Append` (only if both sides are regular files), `Cancel`; checkbox `[ ] Remember choice` (Space/Enter on it, `Alt+M`, click). The remembered choice applies until the end of this operation (not saved in settings); `Cancel` is not remembered; a remembered `Append` for a pair where append is impossible triggers the question again.
- **Rename:** the candidate is `<stem> (copy)<ext>` in the same directory. Without "Remember" — a `Rename` form with the field `New name` (prefilled with the candidate, selected), `{ OK } [ Cancel ]`; empty/Esc → cancellation of the operation; relative input is expanded relative to the destination directory. With "Remember" — the candidate is applied without questions for every collision; if the candidate also exists, the transfer checks it again (a new collision → the next candidate `… (copy) (copy)`), never overwriting blindly. Empty name/the same name → `choose a different destination name`.
- **Append (`appendFile`):** only two regular files (`append requires two regular files`). The old destination is copied to staging `.farshmac-append-*` (with its metadata via a child operation), `chmod 0600`, the source is appended with progress, `chmod` to the destination's permissions, checks `destination changed during append`/`source size changed during append`, `Rename` over it; on move the source is then removed. Cancellation leaves the old file untouched.
- **Overwrite (Replace):** `cannot replace a file with a directory or a directory with a file`; the copy is placed in staging `.farshmac-transfer-*/item` next to it, a check that the destination has not changed (`destination changed during copy`), then `os.Rename` over it.
- **Read-only destination:** on `Overwrite`/`Append` onto a regular file without write bits — `Warning`: `The file is read only\n<dst>\nOverwrite it?`, buttons `Overwrite`, `All`, `Skip`, `Skip all`, `Cancel` (All/Skip all are remembered).
- **Code:** `internal/ui/operation_questions.go` (`collisionQuestions`, `fileComparison`), `internal/fsops/transfer.go`, `internal/fsops/append.go`, `internal/fsops/recovery.go` (`Resolve`, the constants `Replace/Skip/Cancel/Append/Rename`).

#### Errors during copying/moving
- See "Retry/Skip error dialog". Example (test `TestLocalTransferPermissionErrorStaysVisible`): a destination directory without write permission → an `Error` dialog with `permission denied`; Esc → the source is intact, the destination is not created.
- Additional texts: `transfer tree limit exceeded`, `unsupported special file: …`, `destination is not a directory: …`, `source size changed while copying …` (remote), `outside archive`.

## Creating Folders

#### F7 — Make folder (local)
- **Invocation:** `F7`, `Cmd+7`, menu `Files → Make folder…`.
- **Where available:** a local panel; in an archive → `Archive is read-only. Use F5 to copy files out.`; a remote panel → see remote.
- **Dialog:** `prompt` with the title `Make folder`, the text `Folder name`, an empty field (or the previous value with `Remember dialog values`), buttons `OK`/`Cancel`.
- **Behavior:** an empty/whitespace string — nothing. `expand(s, <panel path>)` (`~`, relative and absolute paths are supported), `os.MkdirAll(path, 0755)` — nested paths `a/b/c` are created in full; an existing directory is not an error. Then `refresh()` and `focusCreatedDirectory`: the panel loads the parent of the created directory and places the cursor on it (`ForcedVisiblePath` makes it visible despite filters); if it is still hidden — status `Folder created; hidden by panel settings`.
- **Error:** confirmation `Error`: `Cannot create folder\n<path>\n\n<err>`, buttons `Retry`, `Cancel` (Retry repeats MkdirAll).
- **Code:** `internal/ui/app.go` (`function` case 7), `internal/ui/launch.go` (`focusCreatedDirectory`).

#### F7 on a remote panel — Make remote folder
- **Dialog:** `Make remote folder` / `Folder name` / empty.
- **Behavior:** the name must be a single component (`remotefs.ValidName`), otherwise status `enter one folder name`; background task `Creating remote folder`: `Dial`, `Mkdir(<panel path>/<name>)` via `Attempt("create folder")` with an `Error` dialog Retry/Skip/Skip all/Cancel; Skip → status `Folder creation skipped`; error → `<err> (create folder <target>)` + dialog `Creating remote folder failed`; success → refresh and the cursor on the new folder.
- **Code:** `internal/ui/remote.go` (`remoteMkdir`).

## Deletion

#### F8 / Delete / Cmd+8 — Move to Trash
- **Invocation:** `F8`, `Delete` (without modifiers), `Cmd+8`, menu `Files → Move to Trash…`. Key bar: `8Trash` (on a remote panel `8Delete`).
- **Where available:** a local panel; archive → `Archive is read-only…`; remote → `remoteDelete`.
- **Dialog (if `Settings.ConfirmDelete`, default on; `Confirm deletion`):** title `Move to Trash`, text `Move <name1>, <name2>, … to Trash?` (basenames of all selected items joined with `, `), buttons `Move to Trash`, `Cancel`.
- **Behavior:** `workProgress("Moving to Trash")`, `o.Streaming()`; for each path: `o.Check()`, `o.Add(path,0)`, `Attempt("move to Trash")`: `Lstat`, the read-only question (`BeforeDelete`, only for regular files without write permission), then `fsops.Trash(path)`: UTF-8/NUL check (`invalid filename encoding`), `Lstat`, `o.Begin`, the ObjC call `farshmac_trash_item` → `[[NSFileManager defaultManager] trashItemAtURL:resultingItemURL:error:]` (the Trash of the corresponding volume; a directory is moved as a whole, without a walk; moving a single item cannot be interrupted — cancellation takes effect before the next one). Error → `trash <path>: <localizedDescription or "Unable to move item to Trash">` in an `Error` dialog (Retry/Skip/Skip all/Cancel). **A Trash failure never turns into permanent deletion**; after an error/cancellation the remaining items are not touched (test `TestTrashFailureNeverFallsBackAndCancelStopsBatch`).
- **Messages:** `Done`, `Done — N item(s) skipped`, `Moving to Trash failed` `[OK]`, `operation cancelled`.
- **Code:** `internal/ui/delete.go`, `internal/fsops/trash_darwin.go`, `internal/fsops/trash_darwin.m`.

#### Shift+F8 / Shift+Delete — Delete permanently
- **Invocation:** `Shift+F8`, `Shift+Delete`, menu `Files → Delete permanently…` (`⇧F8`).
- **Dialog:** `Delete permanently`: `Permanently delete <names>?`, buttons `Delete`, `Cancel` (with `ConfirmDelete`).
- **Behavior:** `workProgress("Deleting permanently")`, `Streaming`; `fsops.Remove(path, o)` → `removeTree`: a walk without following symlinks, `Attempt("delete")` on each item (Retry/Skip/Skip all/Cancel), the `BeforeDelete` read-only question for files, `.DS_Store` with `IgnoreDSStore` is not counted in the progress, but the directory is removed in full. Skipping a child item keeps the parent directory (`ErrSkipped` propagates up the tree) and continues with the siblings. On `ENOTEMPTY` of a directory (Finder managed to write a `.DS_Store`) — re-read and delete again, up to 3 attempts, but only if it is the same inode (does not follow a directory replaced by a symlink). Large directories (a test with 2200 entries) are removed in a streaming fashion in batches of 256.
- **Code:** `internal/ui/delete.go`, `internal/fsops/progress.go` (`Remove`, `removeTreeStep`), `internal/fsops/remove_large_test.go`.

#### Deletion on a remote panel
- **Invocation:** the same keys (`F8`, `Delete`, `Shift+F8`, `Shift+Delete`) — the behavior is the same everywhere, there is no Trash.
- **Dialog (always, regardless of `ConfirmDelete`):** `Delete remote files permanently`: `Delete N selected item(s) on <label>? No remote Trash is available.`, buttons `Delete`, `Cancel`.
- **Behavior:** `workProgress("Deleting remote files")`, `remotefs.RemoveTree`: recursive, depth ≤ 256 (`remote tree depth exceeds 256`), `Attempt("delete")`, the `BeforeDelete` read-only question, `unsafe server filename` for unsafe names from the server; skips keep the parent.
- **Code:** `internal/ui/remote.go` (`remoteDelete`), `internal/remotefs/transfer.go` (`RemoveTree`).

## Remote Panels (FTP/FTPS/SFTP) in File Operations

#### F5 involving a remote panel
- **Behavior:** the same `Copy` dialog (checkboxes and workers). Workers open separate connections. A file is copied to a temporary name `.farshmac-part-<rand>` in the destination directory, then `Rename` (on cancellation the temporary file is removed; for remote — by a separate short session with a 2 s timeout). Before publication it is checked that the destination has not changed (`destination changed during transfer` / `destination appeared during transfer`), `source size changed while copying …`. Symlinks are transferred as links (`Readlink`/`Symlink`); special files → `unsupported remote special file`. Remote directories > 100 000 entries → `remote directory exceeds 100000 entries`; names are checked with `ValidName` (`unsafe filename from server`).
- **Metadata (portable):** with `Preserve permissions` — `Chmod` + the error `remote transfer cannot preserve owner/group and ACL` is **always** added; with `Preserve attributes` — `Chtimes(mtime)` + **always** `remote transfer cannot preserve xattrs, flags and creation time`; for symlinks — `remote transfer cannot preserve symbolic-link metadata`. Therefore, with the checkboxes on, the `Cannot preserve metadata` question is asked for every file (`Copy anyway` / `For all` / …); `For all` suppresses further questions. To avoid the questions — turn off both checkboxes in the dialog.
- **Collisions:** the same dialog (Overwrite/Skip/Rename/Append/Cancel); Append is implemented by copying the old destination to temp and appending (`destination size changed during append`); after Append — `Chmod` to the destination's permissions (with a metadata question on error).
- **Code:** `internal/remotefs/transfer.go` (`Copy`, `copyFile`, `portableMetadata`).

#### F6 with a remote panel — Remote move / rename
- **Where available:** only if **both** panels are on the same server (`Endpoint.Same` by label). Otherwise status `Remote move is available within the same server. Between servers use F5 Copy, then Delete.`
- **Dialog:** `prompt` `Remote move / rename`, text `Destination on the same server`, value as for F6.
- **Behavior:** `workProgress("Renaming remote files")`; the destination is `Lstat`-ed; `destination must be an existing directory` with multiple items; `transferIntoDirectory` as locally; `remotefs.Move`: `destination is inside source`, `Rename` if the destination does not exist; directory→directory — a recursive merge (`delete source folder` after it is emptied); file→file — a collision: Skip/Rename/Append (copy+`delete source`)/Replace (check `destination changed during move`, then `Rename`); `cannot replace a file with a directory or a directory with a file`. A server that cannot rename across volumes returns an error (there is no degradation to copy+delete).
- **Code:** `internal/ui/remote.go` (`remoteMoveDialog`), `internal/remotefs/move.go`.

#### Commands unavailable on a remote panel
- `Shift+F1/F2/F3` (archives), `Alt+F7` (search), `Alt+F6` (link), `File attributes…`, `Apply command…`, `F2` user menu (launch), `Shift+F4` new file, `Alt+F10` tree → status `This command requires a local panel. Use F5 to copy remote files first.`
- `Shift+Enter` → `Copy remote files to a local panel before opening them with macOS`.
- Quick view of a remote file: `Remote file: use F3 to download and view`.
- **Code:** `internal/ui/remote.go` (`remoteUnsupported`), `launch.go`, `commands.go`.

## Archives

#### Supported formats (`formats.go`)
- `ArchiveFormat(name)` by suffix (case-insensitive): `.tar.gz`/`.tgz` → tar.gz; `.tar.bz2`/`.tbz2`/`.tbz` → tar.bz2; `.tar.xz`/`.txz` → tar.xz; `.tar`; `.zip`/`.jar`/`.war` → zip; `.7z`; `.rar`; `.cpio`. Anything else is not an archive.
- Reading: ZIP — Go `archive/zip` via the central directory (must be valid; `archive exceeds 100000 entries`); everything else (and ZIP for list/test/non-standard compression methods) — `/usr/bin/tar -c -P -f - --format=pax @<src>` (macOS bsdtar/libarchive) in a separate process group with `COPYFILE_DISABLE=1`, without `TAR_OPTIONS`; on Esc — `SIGKILL` to the whole group. gzip streams are additionally verified by Go (`gzip integrity: …`, `nonzero data after gzip stream`). RAR/CPIO — read only, as far as the system libarchive can.
- Creation: `.zip` — Go; `.tar`, `.tar.gz` (`-z`), `.tar.bz2` (`-j`), `.tar.xz` (`-J`), `.7z` (`--format=7zip`) — Go generates a PAX stream, bsdtar packs it; any other extension → `creation supports .zip, .tar, .tar.gz, .tar.bz2, .tar.xz and .7z`.
- Decoder error: `archive operation failed (damaged, encrypted or unsupported archive): <err>: <stderr up to 32 KiB>`.
- Limits: 100 000 entries (`archiveEntryLimit`), report up to 10 000 lines / 4 MiB.
- **Code:** `internal/fsops/formats.go`, `archive.go`, `archive_index.go`, `archive_zip_selection.go`.

#### Entering an archive (Enter / Ctrl+PgDn / double click)
- **Invocation:** `Enter` on a file for which `ArchiveFormat != ""` (even if the file is executable — the archive check precedes associations and the exec bit); `Ctrl/Cmd+PageDown` — if the name matches `isArchiveName` (the list `.zip .tar .gz .bz2 .xz .7z .rar .tgz .jar`); double click = Enter.
- **Behavior:** `backgroundTask("Opening archive")` → `fsops.ReadArchiveIndex` (metadata only: ZIP — the central directory; otherwise streaming reading via bsdtar with `Streaming` progress). Index limits: `archive index limit (100,000 entries / 16 MiB names)`, `archive path too long/deep` (> 4096 bytes or > 64 `/`), `duplicate/conflicting archive entry`, `archive parent is not a directory`, `unsupported archive entry` (only dir/reg/symlink/hardlink), `unsafe archive path: …` (absolute, `..`, `\`, `C:`), `not a regular archive`, `invalid archive root`. When ready, the panel switches to archive mode: `p.Archive = catalog`, `Remote=nil`, the Tree/Info/Quick view → `Full`; the root is loaded (or the previously opened subdirectory on Cmd+R). The Info panel for an archive: `Archive (read-only)\nF5 extracts selected files/folders\n.. exits the archive`.
- **Navigation:** Enter on a directory — enters it; `..` at the archive root loads the parent directory of the archive file (a normal load, `p.Archive = nil`, cursor on the archive). Inside, marks, sorting, filter (`Filter`/`MaskGroups`), hiding of `.`-files and `.DS_Store` all work. `Cmd+R` re-reads the archive directory (`openArchive`). If the archive file has changed (`SameFile`/size/mtime) — `archive changed on disk; leave and reopen it` on any operation.
- **Limitations:** `Ctrl+T` (Tree) on an archive panel → `Tree view is unavailable inside archives`.
- **Code:** `internal/ui/archive_panel.go` (`openArchive`, `openArchiveAt`, `loadArchiveDirectory`, `enterArchiveMember`), `internal/ui/launch.go` (`enterFile`), `internal/ui/far_keys.go` (`isArchiveName`), `internal/fsops/archive_index.go`.

#### Viewing an archive member (F3 / Enter)
- **Invocation:** `F3`, `Enter` on a regular file inside an archive (double click).
- **Behavior:** `Enter` on a nested archive → status `Copy the nested archive out with F5 to open it`; a symlink member → `Use F5 to copy the archive link`. Otherwise `backgroundTask("Opening archive member")`: `ExtractSelection([name], <tmp farshmac-archive-view-*>/content)`, the document opens in the internal viewer in read-only mode (`ReadOnly`, `ArchiveSource`, `Path` = the virtual path); the temporary directory is removed when the document/window is closed. `F4` in an archive → `Archive is read-only. Use F5 to copy files out.`; `Shift+F4`, `F6`, `F7`, `F8`, `Alt+F6`, `File attributes…`, `Apply command…`, `Shift+F1` — the same message. `F3` on an archive directory — size calculation (`calculateFolderSizes`).
- **Code:** `internal/ui/archive_panel.go` (`viewArchiveMember`, `archiveReadOnly`), `internal/ui/document.go` (`finishCloseDocument`).

#### F5 from an archive — extracting the selection
- **Behavior:** see F5: `catalog.Relative` of each path (`outside archive`), `ExtractSelection(names, tmp)` — the selected subtrees are extracted plus the targets of their hard links (`missing hard-link target`, `cyclic archive hard link`, `archive entry not found`, `no archive entries selected`); for ZIP (Store/Deflate) only the selected members are read with a CRC check, otherwise a full bsdtar pass. Then a normal copy from temp to the destination (directory merging, collisions, progress, Esc); temp is always removed. Writing into an archive is impossible (an archive opposite panel as the destination → `Choose a regular destination panel; writing into archives is not supported`).
- **Code:** `internal/ui/transfers.go`, `internal/fsops/archive_index.go` (`ExtractSelection`), `archive_zip_selection.go`.

#### Shift+F1 — Create archive
- **Invocation:** `Shift+F1`, menu `Files → Create archive…`.
- **Where available:** a local active panel and a local opposite panel; inside an archive → `Archive is read-only…`; an empty selection — nothing.
- **Dialog:** `prompt` `Create archive`, text `New filename: .zip / .tar.gz / .tar.bz2 / .tar.xz / .7z`, value `<other panel path>/<basename of the first selected item>.zip`.
- **Behavior:** `workProgress("Creating archive")` → `Attempt("create archive", <paths separated by \n>, dst)` → `fsops.CreateArchive(paths, expand(s, <active panel path>))`. Checks: `no files selected`, `archive already exists: <dst>` (never overwrites), `duplicate archive root: <name>` (identical basenames), `archive destination is inside the source directory`. First `o.Scan(paths)` (the total is known → percentages/ETA), writing to a temporary file `.farshmac-zip-*`/`.farshmac-archive-*` next to it, publication via `os.Link` (no replacement). Names in the archive are relative to the parent of each root. Symlinks are stored as links; special files → `unsupported special file: <path>`; > 100 000 entries → `archive exceeds 100000 entries`. ZIP: Deflate, directories with `/`. bsdtar compression may continue after all data has been sent — cancellation is available.
- **Code:** `internal/ui/transfers.go` (`archiveCreate`), `internal/fsops/formats.go` (`CreateArchive`, `checkArchiveDestination`, `writeArchiveSources`), `archive.go` (`CreateZIP`).

#### Shift+F2 — Extract archive
- **Invocation:** `Shift+F2`, menu `Files → Extract archive…`, the `Extract to folder…` item in Shift+F3.
- **Where available:** local panels (the active and the opposite are not remote). The source is the current item; inside an archive panel — the archive file itself. The format of the current file is **not checked** (a non-archive yields a decoder error).
- **Dialog:** `prompt` `Extract archive`, text `New destination folder`, value `<filesystemPath of the other panel>/<ArchiveStem(name)>` (`ArchiveStem` strips the known suffix; an empty stem → `archive`).
- **Behavior:** `workProgress("Extracting archive")` → `Attempt("extract archive")` → `fsops.ExtractArchive`: the destination must not exist (`destination already exists: <dst>`, no merging); staging `.farshmac-extract-*` next to it (0700) via `os.Root`; name validation (`unsafe archive path`, `invalid root archive entry`, `duplicate archive entry`, `unsupported archive entry type`, `unsafe archive link`, `hard link target is not a regular file`); files are created with `O_EXCL` with permissions `mode|0600`, directories `mode|0700`, file mtime is restored; links are created after files (hard links — only to already extracted regular files, symbolic — relative within the root, then a `Stat` check); `.DS_Store` is skipped with `IgnoreDSStore`; publication `Move(staging, dst)` (RENAME_EXCL). An error/cancellation removes the staging. `Streaming` progress (the total is unknown).
- **Code:** `internal/ui/transfers.go` (`archiveExtract`, `archiveExtractPath`), `internal/fsops/formats.go` (`extractArchive`, `safeArchiveName`, `ArchiveStem`).

#### Shift+F3 — Archive commands
- **Invocation:** `Shift+F3`, menu `Files → Archive commands…`.
- **Where available:** a local panel; the path = the current item (in an archive — the archive itself); empty — nothing.
- **Dialog:** a list `Archive: <basename>` with the items `List contents`, `Test archive`, `Extract to folder…` (buttons `Select`/`Cancel`).
- **Behavior:** `List`/`Test` → `backgroundProgress("Reading archive")` → `fsops.ArchiveContents(path, test)` via bsdtar (all members are read in full even for the listing); the result is shown as a report document titled `Archive: <basename>`: lines `%12d  <name>` (control characters → `�`, directories with `/`, links ` -> "target"`), on truncation `Listing truncated (10,000 rows / 4 MiB); all entries were read.`, for the test `Archive integrity: OK (all entries decoded; available format checksums verified)`. `Extract to folder…` → Shift+F2.
- **Code:** `internal/ui/transfers.go` (`archiveCommands`), `internal/ui/commands.go` (`report`), `internal/fsops/formats.go` (`ArchiveContents`). (`ZIPContents`/`ExtractZIP` in `archive.go` are not called from the UI.)

#### Archive limitations
- Read only: no writing/editing/deleting/moving of members, no writing into an archive as a destination.
- Nested archives are not opened directly (copy out with F5).
- Encrypted/multi-volume archives are not supported (decoder error; a hidden password prompt is impossible — there is no terminal).
- `Shift+Enter` on a member → `Use F5 to copy this archive item out before opening it with macOS`.
- Session restore after a restart does not return into the archive (see ARCHIVE_SUPPORT.md; in the code `openArchiveAt` is not called at startup).

## Launching and Opening Files

#### Enter on a panel item
- **Invocation:** `Enter` with an empty command line, double click, `Ctrl/Cmd+PageDown` (directories/archives only), selection in fast find.
- **Behavior (local panel, `enterFile`):**
  1. A directory, including `.app` → enter it (launching the application — Shift+Enter).
  2. A file with `ArchiveFormat != ""` → enter the archive.
  3. The first association (`Settings.Associations`) with a non-empty `Command` whose mask matches (`matchesMask`, with mask groups) → the command runs in the terminal: `{}` is replaced with the shell-quoted path, otherwise the path is appended at the end; the user's command line is not overwritten (`submitCommand(..., record=false)`), the panels are replaced by the terminal.
  4. A regular file with the execute bit (`0111`) → `'<path>'` (quoted) is executed in the terminal.
  5. Otherwise → `/usr/bin/open <path>` (macOS association) in the background `Opening <name>`; on error a status and the dialog `Opening <name> failed` with `<err>: <stdout/stderr>`.
- **Remote panel:** a directory → enter; a file → `openRemoteDocument(false)` (download `Downloading remote file` into a temporary directory and the internal viewer; `viewer requires a regular remote file`).
- **Archive:** see above.
- **Errors:** an `os.Stat` error → status.
- **Code:** `internal/ui/launch.go` (`enterFile`, `externalPlan`, `launch`, `submitCommand`), `internal/ui/app.go` (`execute`: `Shell unavailable`, `Shell is busy; ⌘O to return to it`).

#### Shift+Enter — Open with macOS
- **Invocation:** `Shift+Enter`, menu `Files → Open with macOS` (`⇧Enter`).
- **Behavior:** always `/usr/bin/open <absolute path>` as a single argument (no shell; associations, the exec bit, viewer settings and the command line are ignored; the command draft is preserved). A directory opens in Finder, an `.app` is launched, `..` opens the parent. Runs as `workProgress("Opening <name>")` with cancellation on Esc; error → status + dialog `Opening <name> failed` `[OK]` (test: `No application is registered`).
- **Limitations:** a remote panel → `Copy remote files to a local panel before opening them with macOS`; an archive member → `Use F5 to copy this archive item out before opening it with macOS`; an empty panel — nothing.
- **Code:** `internal/ui/launch.go` (`openSystemSelected`), `system_open_test.go`.

#### F3 on an image / PDF — graphical preview
- **Invocation:** F3 (and Alt+F3 / ⌘⇧F3, when the plan is the internal viewer) on a regular local file with an extension from `preview.Supported` (`.jpg .jpeg .png .gif .tif .tiff .bmp .heic .heif .webp .pdf`).
- **Behavior:** `App.Image = imageView{Path}`; decoding in the sandbox helper (`preview.RunSized`, up to 2048 px on the long side; photos are sent as JPEG, PNG/GIF/PDF — as PNG; a 12 MB limit in the helper, 20 MB for reading, 8 s timeout). While loading — a black screen with the caption `Loading <name>…` centered; then `Update.Preview{Full: true, X:0, Y:0, W: Cols, H: Rows}` and the renderer places the image over the whole grid (`#quick-preview.full`, `#panels.image` — black background, `object-fit: contain`). The window title is the file name, `PanelKeys = false`, `Document = true`. Keys: Esc, F3, F10, Enter close it; the rest are ignored; the mouse is ignored. Decoding error: the caption `<name>: cannot be shown (unsupported, damaged or too large)` and the same text in the status; file missing — `<name>: <reason>`. Quick view is not updated during this time. Remote files and archive members still go through download/extraction into the text viewer.
- **Code:** `image_view.go: openImage, closeImage, imageKey, drawImage`, `image_preview.go: imagePreview`, `launch.go: openConfigured`.

#### F3 / F4 — view / edit (choosing internal or external)
- **Invocation:** `F3`/`F4`, `Cmd+3`/`Cmd+4`, `Numpad5` (=F3), menu `Files → View / folder size`, `Files → Edit`; `Alt+F3`/`Alt+F4` — invert the internal/external choice; `Cmd/Ctrl+Shift+F3`/`F4` — force internal.
- **Behavior (`openConfigured`):**
  - A directory under the cursor: F3 → folder size calculation (`calculateFolderSizes(false)`); F4 → the `File attributes` dialog (see below).
  - A remote panel → download and an internal document (F4 — editing with a subsequent upload; details with the remote agent).
  - An archive → F3 views the member; F4 → the read-only message.
  - A file: `viewPlan`: the base is `Settings.ExternalViewerDefault`/`ExternalViewer` (Viewer settings: `External by default`, `Command (blank=system)`) or `ExternalEditorDefault`/`ExternalEditor` (Editor settings). The first matching association with a non-empty `Viewer`/`Editor` overrides the command and makes the launch external. `Alt` inverts the result. External launch: an empty command → `/usr/bin/open <path>` (viewer) or `/usr/bin/open -t <path>` (editor, TextEdit/the default text editor); otherwise the command runs in the terminal (`{}` or appending the quoted path). Internal → `openDocument(path, edit)`.
- **Code:** `internal/ui/launch.go` (`openConfigured`, `viewPlan`, `externalPlan`), `internal/ui/settings.go`.

#### File associations… (Commands menu)
- **Dialog:** the list `File associations`: `Add association…`, `Open current file with macOS default app`, then `<mask> → <command>` for each one. The second item runs `/usr/bin/open <current path>` (`workProgress("Opening")`).
- **Form** `File association — {} inserts filename`: fields `Masks`, `Run command`, `Viewer command`, `Editor command`; the masks are validated (`validateMasks`, error in the status); an empty mask or all three commands empty — the association is removed; saved with `saveSetup()`.
- **Code:** `internal/ui/commands.go` (`fileAssociations`).

#### F2 — User menu
- **Invocation:** `F2`, `Cmd+2`, menu `Commands → User menu`.
- **Behavior:** if `Settings.UserMenu` is empty — the menu editor opens immediately. Otherwise the list `User menu` with the command names; selection → `runUserCommand`: on a remote panel → `This command requires a local panel…`; `{}` is replaced with the shell-quoted path of the current item (if there is no `{}`, the path is **not** appended), the command is written into `a.Command` and `execute()` runs (it goes into the command history, the terminal opens).
- **Editing (`Commands → Edit user menu…`):** the list `Edit user menu`: `Add command…` + existing ones; the form `User command — {} inserts filename` with the fields `Label`, `Command (empty deletes)`; an empty name or command removes the entry; `saveSetup()`.
- **Code:** `internal/ui/commands.go` (`userMenu`, `editUserMenu`, `runUserCommand`).

#### Apply command… (Files menu)
- **Where available:** a local panel, not an archive, with a selection.
- **Dialog:** `prompt` `Apply command`, text `Use {} for a shell-quoted filename`, empty.
- **Behavior:** for each selected path a command is built (`{}` → the quoted path, otherwise ` <quoted path>` appended at the end); all commands are joined with `\n` into `a.Command` and executed as a single submission to the terminal.
- **Code:** `internal/ui/commands.go` (`applyCommand`).

## Search and Histories

#### Alt+F7 — Find file / Find folder
- **Invocation:** `Alt+F7`, menu `Commands → Find file…` (`AltF7`); `Commands → Find folder…` (no key).
- **Where available:** a local panel (remote → `This command requires a local panel…`); the search root is the active panel path (in an archive — the virtual path, searching it fails with an `Lstat` error).
- **Dialog (form `Find file` / `Find folder`):** `Name masks:` (`*`), `Containing text:` (empty), `[x] Search subfolders`, `[ ] Case sensitive text` (checkboxes in 2 columns), buttons `{ Find }` `[ Cancel ]`. The masks are validated (`validateMasks`, error in the status).
- **Behavior:** `backgroundProgress("Searching <root>")`, `fsops.WalkDir` (without following symlinks, batches of 256, cancellable): unreadable paths are counted as `skipped`; without `Search subfolders` only the root is walked; mask matching (for Find folder — directories only); text is searched in regular files in 64 KiB blocks with a tail (`len(needle)*4+4`), case-insensitively — via `ToLower`; a limit of 10 000 results (`fs.SkipAll`, the `(limit)` mark).
- **Result:** empty → status `No matches (N unreadable paths)`; otherwise the list `<title> — N found, M unreadable[ (limit)]` with relative paths. Enter/`Select` → `revealPath`: a directory — enter it; a file — load its directory and put the cursor on it (or status `File is hidden by the current panel filter`). There are no separate F3/F4 actions in the result list (it is an ordinary selection list).
- **Code:** `internal/ui/commands.go` (`findFiles`, `fileContains`, `revealPath`).

#### Alt+F8 — Command history
- **Invocation:** `Alt+F8`, `Commands → Command history` (`AltF8`).
- **Behavior:** the list `Command history` (newest on top) from `a.history` (commands executed from the panels' command line, limit `HistoryLimit`, default 200, saved in setup.json); selection places the command into the command line (does **not** execute it). Empty → status `Command history is empty`. Also `Cmd/Ctrl+↑/↓`, `Ctrl+E/X` scroll the history in the line.
- **Code:** `internal/ui/commands.go` (`historyMenu("Commands")`).

#### Alt+F11 — View/edit history
- **Invocation:** `Alt+F11`, `Commands → File view/edit history`.
- **Behavior:** the list `View/edit history` (newest on top), items `View <path>` / `Edit <path>`; selection → `openDocument(path, edit)` with the internal viewer/editor. Empty → `View/edit history is empty`.
- **Code:** `commands.go` (`historyMenu("Files")`).

#### Alt+F12 — Folder history
- **Invocation:** `Alt+F12`, `Commands → Folder history`.
- **Behavior:** merges `a.folderHistory` with the `History` of both panels (`appendUnique`, limit `HistoryLimit`), the list `Folder history` (newest on top); selection → `Load` in the active panel (error in the status). If empty — status `Folder history: empty`.
- **Code:** `commands.go` (`historyMenu("Folders")`, `appendUnique`).

## Other Commands from `commands.go`

#### Task list (menu Commands → Task list)
- **Behavior:** `backgroundProgress("Reading processes")` → `/bin/ps -axo pid,ppid,%cpu,%mem,comm`; the output is shown as the report document `Processes` (view only; processes cannot be terminated).
- **Code:** `commands.go` (`taskList`).

#### Compare folders (Commands → Compare folders)
- **Behavior:** no dialog; in both panels, files (not directories) are marked that are missing in the other panel, are a directory there, differ in size or are newer by mtime; the previous marks are saved in `RestoreMarked`. Status: `Marked files missing, different in size, or newer than the opposite panel (non-recursive)`.
- **Code:** `commands.go` (`compareFolders`).

#### File attributes… (Files → File attributes…, F4 on a directory)
- **Where available:** a local panel, not an archive, with a selection.
- **Dialog:** the form `File attributes`: `Permissions (octal):` (`%03o` of the first selected item), `Modified (local):` (`2006-01-02 15:04:05`).
- **Behavior:** validation: `Permissions must be 000–777` (status), a date parse error → status; `workProgress("Updating attributes")` for all selected items: `Chmod` + `Chtimes(atime=mtime=stamp)`; a symlink → error `attributes of symlinks are not changed: <path>` (the operation is aborted).
- **Code:** `commands.go` (`fileAttributes`).

#### Alt+F6 / Files → Link… — Create link
- **Dialog:** the form `Create link`: `Link name:` (`<other panel>/<current name>`), `[x] Symbolic link`.
- **Behavior:** `os.Symlink(src, target)` or `os.Link` (hard); error in the status; refresh. The path is expanded relative to the active panel.
- **Code:** `commands.go` (`createLink`), `far_keys.go`.

#### Alt+F10 — Folder tree
- **Behavior:** `backgroundProgress("Reading folder tree")`, tree scanning (`scanTree`, `TreeDepth`), the list `Folder tree` (`(partial)` on limit/skips); selection → navigation in the active panel. Remote — unavailable.
- **Code:** `commands.go` (`treeMenu`), `tree.go`.

#### Quick view (`updatePreview`)
- **Behavior:** for a panel in `Quick view` mode when the current item changes in the other panel: a directory — a listing of up to 100 entries (`Directory (preview, up to 100 entries)`); images/PDF — via the helper (`Preview unavailable (unsupported, damaged or limit exceeded)`, `PDF · page 1 of N`, `<fmt> · W × H pixels`); a regular file — the first 16 KiB of text; remote → `Remote file: use F3 to download and view`. Not performed while `Busy`/a document is open/the terminal is active.
- **Code:** `commands.go` (`updatePreview`).

## Discrepancies Between Documentation and Code

> **Closed on the `backlog-fixes` branch:** the decisions on each item are in the "Decisions on discrepancies" section of `docs/IMPLEMENTATION_BACKLOG.md`.

1. **Help (F1) in `app.go`:** "Cross-volume move is refused. Copy, verify, then delete explicitly." — in the code `moveAcrossVolumes` performs a copy with full metadata preservation and removal of the source (with the `Cannot preserve metadata` dialog on losses). `docs/FILE_OPERATIONS.md` describes this correctly. The comment in `move_darwin.go` ("Cross-volume moves are refused") is also outdated.
2. **Help:** "Copies preserve permissions, timestamps and links, not ACLs/xattrs" — with the checkboxes on, the code preserves ACL (`COPYFILE_ACL`), xattr (`COPYFILE_XATTR`), flags and creation time, and the F5 dialog explicitly offers "Preserve permissions (mode, owner, group, ACL)" / "Preserve attributes (xattrs, flags, timestamps)".
3. **Help:** "F5 / F6 Copy / move or rename (no overwriting)" — overwriting exists via the collision dialog (`Overwrite`/`Append`/`Rename`).
4. **Help:** "Shift+Enter Open selected item with macOS (folders in Finder)" and README — matches; but Help lists `Alt+F11/F12` and not `Alt+F7/F8` (search/command history) — they exist in the code and in the menu but are absent from the help.
5. **ARCHIVE_SUPPORT.md:** "Enter opens an archive as a file panel, even when the archive has its executable bit set" — correct; however `Ctrl+PgDn` uses a separate list `isArchiveName` (`.zip .tar .gz .bz2 .xz .7z .rar .tgz .jar`) that does not match `ArchiveFormat`: `.war`, `.cpio`, `.tbz`, `.tbz2`, `.txz` do not open via Ctrl+PgDn, while single `.gz/.bz2/.xz` pass the check but `enterFile` does not treat them as archives (it opens them via macOS/an association).
6. **ARCHIVE_SUPPORT.md:** "F9 → Files → Archive commands also provides …" — correct, but Shift+F2 (Extract archive) in the code does not check that the current item is an archive; a non-archive yields the decoder error `archive operation failed (damaged, encrypted or unsupported archive)`. This is not described in the documentation.
7. **docs/FILE_OPERATIONS.md:** "Esc cancels a pending decision" — in the code, Esc in a question dialog while `Busy` cancels the **whole operation** (`cancelOperation`), not just the current decision; the result is the same (`Cancel`), but the wording is imprecise.
8. **docs/FILE_OPERATIONS.md:** "Read-only confirmation uses POSIX mode bits" — correct; but the document does not mention that for remote copies with the metadata checkboxes on, the "Cannot preserve metadata" question is asked **for every file** (the errors `remote transfer cannot preserve owner/group and ACL` / `xattrs, flags and creation time` are added unconditionally) until `For all` is chosen.
9. **README:** "Cmd+Shift+H Shell command history: Enter to run, F4 to edit" refers to the terminal history; the panels' command history (`Alt+F8`) only inserts the command, it does not execute it. README does not mention Alt+F7/F8/F11/F12 and Shift+F1/F2/F3.
10. **ARCHIVE_SUPPORT.md:** "Restart restores the containing directory and selects the archive" — session restore was not examined in the code within the scope of this area; could not be confirmed (there are no `openArchiveAt` calls at startup — consistent).
11. **ARCHIVE_SUPPORT.md:** the table promises "List / test / extract" for ZIP via the common interface — in the code the functions `ZIPContents`/`ExtractZIP` (the Go implementation for ZIP) exist in `archive.go`, but the UI does not use them: list/test/full extraction always go through bsdtar (`ArchiveContents`/`extractArchive`), and Go-ZIP is used only for the index and selective extraction (F5/F3) and for creation.
12. **Help:** "F7 New directory" — the dialog title in the code is `Make folder`; "F8 / Delete Move selected items to Trash" — correct; `Cmd+8` is also Trash (not documented anywhere except the general "Cmd+1 … Cmd+9").
13. **docs/FILE_OPERATIONS.md:** "Rename asks for a destination path" — in the code the form is called `Rename` with the field `New name`, the candidate is `<stem> (copy)<ext>`; the document does not describe the candidate format.


<a id="part-3"></a>
# Part 3. Internal Viewer and Editor

The source of truth is the code in `internal/ui/document*.go`, `internal/ui/remote_document.go`, `internal/ui/launch.go`, `internal/textfile/*.go`, `internal/highlight/*.go` as of commit `b91313a`. Interface strings are given verbatim (English). All paths are relative to the repository root `/Users/ikim/src/remote-codex/farshmac`.

## 0. Data model and general rules

- A document is described by the `Document` struct (`internal/ui/document.go`). Key fields:
  - `*textfile.Buffer` — the text (`Text`, always with `\n` line endings after `Normalize`), `Cursor`/`Anchor` (UTF-8 byte offsets), `Rect` (rectangular selection), `Overwrite`, `ReadOnly`, the `UndoStack`/`RedoStack` stacks.
  - `Path` — local path (for remote documents — the path of the temporary cache file; for archives — the "virtual" path of the item inside the archive); `displayPath()` gives the displayed name (`Remote.Label()+RemotePath` for remote documents).
  - `Edit` (editor/viewer), `Dirty`, `Mode` (`"text"`, `"hex"`, `"dump"`), `Wrap`, `WordWrap`, `Codepage`, `EOL` (`"LF"`, `"CRLF"`, `"CR"`), `BOM`.
  - `Raw` — the original bytes (only for files ≤ 64 MiB), `SavedRaw` — the bytes of the last loaded/saved version, `File *os.File` — an open descriptor for paged reading of large files (they are not loaded into memory), `Size`, `ByteOffset` (position in hex/dump), `BytesPerLine`.
  - `Offset` (number of the first visible line), `Column` (horizontal scroll), `ViewStart` (byte offset of the first visible visual line — used in wrap mode).
  - `HideKeys`, `HideStatus`, `Scrollbar` (`true` by default), `Bookmarks map[int]docPosition`, `Positions []docPosition` (position history, ≤100), `Search textfile.Search`, `HexQuery []byte`, `MatchStart/MatchEnd` (highlight of the hex match), `Help` (help document), `ScreenID`, `Remote/RemotePath/RemoteInfo`, `ArchiveSource`, `TempDir`, `TabSize`, `ManualScroll`.
- `Dirty` is recomputed in `Document.modified()`: `Text != SavedText || Codepage != SavedCP || EOL != SavedEOL || BOM != SavedBOM`. That is, changing the codepage/EOL/BOM in the editor also makes the document "dirty".
- `markSaved()` records `SavedRaw/SavedText/SavedCP/SavedEOL/SavedBOM` and clears `Dirty`.
- The constant `maxTextBytes = 64 * 1024 * 1024` (64 MiB) is the limit for loading text into memory.
- In `App`, the open document is `a.Doc`; the other open ones are the stack `a.Documents []*Document`. While `a.Doc != nil`, all keys go to `documentKey` (except the global ones: Cmd+N, Cmd+Q, Ctrl+Tab/Cmd+Shift+[ ] — screen switching, Cmd+O — terminal; when a form/dialog is open — to them).
- The `a.Async` mode (the production mode of the application, `NewAsync`): opening, saving, searching, replace all, codepage change and F6 run as `backgroundTask` background tasks on a clone of the document (`cloneDocument`), and the result is applied on the engine queue (`*d = *copy`). While a task is running, the application is `Busy`, a progress with a `Cancel` button is shown, and Esc cancels. In tests (`New`) everything is synchronous.
- Status messages (`a.Status`) are shown in the document in the top right corner (see §3.1).

---

## 1. Opening documents

### 1.1 Opening from a panel

#### F3 / F4 — view / edit the current file
- **Invocation:** F3 / F4; Cmd+3 / Cmd+4; click on the key bar `3View` / `4Edit`; menu Files → `View / folder size` (F3), `Edit` (F4).
- **Where available:** panels (local, remote, archive).
- **Behavior (`App.openConfigured(edit, alternate=false, forceInternal=false)`, `internal/ui/launch.go`):**
  1. F3 on a folder → folder size calculation (`folderSize`), no document is opened. F4 on a (local) folder → the file attributes dialog (`fileAttributes`).
  2. Remote panel → `openRemoteDocument(edit)` (§1.4); external program settings are ignored.
  3. Archive panel → F3: `viewArchiveMember()` (§1.5); F4: `archiveReadOnly()` → status `Archive is read-only. Use F5 to copy files out.`
  4. Local panel: `os.Stat(path)`; on error → a status with the error text. Then `viewPlan` (§2) decides — internal document (`openDocument(path, edit)`) or external program.
- **Code:** `launch.go: openConfigured, viewPlan, launch`; `app.go: function(3/4)`, handling of `F3`/`F4` in `key`.

#### Alt+F3 / Alt+F4 — "alternate" view/edit
- **Invocation:** Alt+F3 / Alt+F4 in the panels.
- **Behavior:** `openConfigured(edit, alternate=true, false)` — the result of the "internal/external" choice is inverted: if, according to the settings/associations, an external program would have opened, the internal one opens, and vice versa (§2).
- **Code:** `app.go` (branch `if m.Alt { case "F3","F4": ... }`), `launch.go: viewPlan`.

#### Cmd+Shift+F3 / Cmd+Shift+F4 (also Ctrl+Shift) — force internal
- **Invocation:** F3/F4 with Shift and Meta or Ctrl.
- **Behavior:** `openConfigured(edit, false, forceInternal=true)` — always the internal viewer/editor, regardless of settings and associations.
- **Code:** `app.go` (`if (m.Key == "F3" || m.Key == "F4") && m.Shift && (m.Meta || m.Ctrl)`).

#### Shift+F4 — new file / open by name
- **Invocation:** Shift+F4 in the panels or in the editor; menu Files → `New file…` (`ShiftF4`).
- **Where available:** local panel only. Remote → status `This command requires a local panel. Use F5 to copy remote files first.`; archive → `Archive is read-only. Use F5 to copy files out.`
- **Dialog:** prompt, title `Edit file`, label `Existing file or new file name`, default value empty (if `RememberDialogValues` is enabled — the last value entered for this title).
- **Behavior:** an empty string — nothing. The path is expanded relative to the active panel's path (`expand`). If the file exists — `openDocument(path, true)`; if it does not exist — an empty in-memory document is created (`newDocument(path, "")`, `Edit=true`, `Codepage = Settings.DefaultCodepage`, EOL `LF`), and the file on disk is created only on save (via `fsops.Create`, permissions 0644). Any other `os.Stat` error → status.
- **Code:** `document.go: newFile`; `settings.go` (`Code pages` → `Default encoding for new files`).

#### Enter on a file (not the viewer)
- Enter in a panel does not open the internal viewer: archives open as folders, associations with a `Command` are run, executable files (`0111`) are run in the terminal, everything else — `/usr/bin/open` (the system application). Shift+Enter → always the macOS application (`openSystemSelected`); for a remote panel the status is `Copy remote files to a local panel before opening them with macOS`, for an archive item — `Use F5 to copy this archive item out before opening it with macOS`.
- **Code:** `launch.go: enterFile, openSystemSelected`.

#### Alt+F11 — view/edit history
- **Invocation:** Alt+F11 in the panels; menu Commands → `File view/edit history` (in the menu without a key shown).
- **Behavior:** the `View/edit history` list (choose form), items `View <path>` / `Edit <path>`, newest on top. Selecting → `openDocument(path, edit)` directly (internal document, bypassing external program settings and remote/archive panels). Empty history → status `View/edit history is empty`.
- **History:** `a.viewHistory []viewRecord{Path, Edit}` is appended on every open (`openDocument`/`openDocumentAsync`), trimmed to `Settings.HistoryLimit` (default 200, 1–10000), saved in the setup (`savedSetup.Viewed`).
- **Code:** `commands.go: historyMenu("Files")`, `far_keys.go` (Alt+F11), `document.go: openDocument`, `document_tasks.go: openDocumentAsync`, `settings.go`.

### 1.2 Loading a local file (`openDocument`)

#### Opening algorithm
- **Behavior (`App.openDocument(path, edit)`, `document.go`):**
  1. In Async mode → `openDocumentAsync` (background task `Opening file`; a worker copy of `App` with the same `Settings` performs a synchronous `openDocument`; if the document did not open, the `worker.Status` text becomes the task's error; `ScreenID` is allocated in the owning window).
  2. `filepath.EvalSymlinks(path)` — the document is opened by the resolved path. Error → status.
  3. `os.Open`, `Stat`; not a regular file → status `Choose a regular file`.
  4. `newDocument(resolved, "")` is created: `Codepage "UTF-8"`, `EOL "LF"`, `Mode "text"`, `Scrollbar true`. `d.Wrap = Settings.ViewerWrap` (the `Wrap lines` setting in `Viewer settings`). A record is written to `viewHistory`.
  5. **Size > 64 MiB:**
     - editor (`edit=true`) → status `Editor limit is 64 MiB. Large files open in paged hex/dump viewer.`, the document is not opened;
     - viewer → `d.File = f` (the descriptor stays open), `Mode = "hex"`, `Raw` empty; data is read page by page via `ReadAt` (`dataAt`). Text mode is unavailable for such files.
  6. **Size ≤ 64 MiB:** read entirely via `io.LimitReader(f, maxTextBytes+1)`, honoring the operation's progress/cancellation. If more than the limit was read (the file grew) → `File grew while opening; reopen it`.
  7. `Codepage = textfile.Detect(raw)` (§7.1); `Decode` — on error → status (`invalid UTF-8; choose another codepage` etc.) and the document is not opened.
  8. `binary := strings.ContainsRune(decoded, 0)` (there is a NUL).
  9. For the editor: a reversibility check — `Encode(decoded, cp, bom)` must match `raw` byte for byte, otherwise status `Encoding is not lossless; inspect in viewer and choose a codepage before editing`. For a binary file → `Binary data: open with F3 in hex/dump mode`.
  10. `BOM`, `EOL = DetectEOL(decoded)`, `Buffer = textfile.New(decoded)` (CRLF/CR are normalized to `\n`), a binary file in the viewer → `Mode = "hex"`. `markSaved()`, the line index is built, `showDocument(d)`.
- **Display (`showDocument`):** allocates a `ScreenID` (counter `a.nextScreenID`), the current `a.Doc` (if different) is pushed onto the `a.Documents` stack, closes the menu.
- **Special cases:** opening via a symlink → the real path; reopening the same file creates a separate screen (duplicates are not merged).
- **Code:** `document.go: openDocument, newDocument, showDocument`; `document_tasks.go: openDocumentAsync`.

### 1.3 External viewer and editor — see §2.

### 1.4 Opening from a remote panel (FTP/FTPS/SFTP)

#### Download to a temporary file
- **Invocation:** F3/F4 (and the Alt/Cmd+Shift variants — all lead here), Enter on a file in a remote panel (view).
- **Behavior (`remote_document.go: openRemoteDocument(edit)`):** background task `Downloading remote file`:
  1. `config.Dial`, `Lstat(remotePath)`; not a regular file → error `viewer requires a regular remote file`.
  2. `os.MkdirTemp("", "farshmac-remote-view-*")`, the file `<tmp>/<basename>` is created with `O_CREATE|O_EXCL`, permissions 0600.
  3. Copy with progress (256 KiB buffer); if not `info.Size()` bytes were copied → `remote file changed during download`.
  4. The worker `App` performs `openDocument(local, edit)` — all the checks of §1.2 (64 MiB limit, encoding, binary detection) apply; the status text of the worker `App` becomes the task's error.
  5. `doc.Remote = &config`, `RemotePath`, `RemoteInfo = info` (used when saving), `TempDir = dir`.
  6. Apply: `showDocument(doc)`, status `<label><remotePath>` (`config.Label()+remotePath`).
- **Cleanup:** on cancellation/error the temporary directory is deleted; when the document is closed, `finishCloseDocument` deletes `TempDir`.
- **Limitations:** editing is allowed (F4), saving — see §9.9; F3 with Alt does not switch to an external program.
- **Code:** `remote_document.go: openRemoteDocument`, `launch.go: openConfigured, enterFile`.

### 1.5 Opening from an archive — view only

#### Viewing an archive member
- **Invocation:** F3 in an archive panel; Enter on a file inside an archive (`enterArchiveMember`).
- **Behavior (`archive_panel.go: viewArchiveMember`):**
  - Folder/empty panel → nothing. A symbolic link inside the archive → status `Use F5 to copy the archive link`. Enter on a nested archive → `Copy the nested archive out with F5 to open it` (F3 on a nested archive views it as a file).
  - Background task `Opening archive member`: `MkdirTemp("", "farshmac-archive-view-*")`, `catalog.ExtractSelection([name], <tmp>/content)`, `openDocument(<tmp>/content/<name>, false)`.
  - `doc.Path = <virtual path of the item>`, `doc.ReadOnly = true`, `doc.ArchiveSource = true`, `doc.TempDir = temp`.
- **Limitations for archive documents:** F4 in the panel → `Archive is read-only. Use F5 to copy files out.`; F6 in the document → `Copy the file out with F5 before editing`; `+`/`-` → `Return to the archive panel to choose another file`.
- **Code:** `archive_panel.go: viewArchiveMember, enterArchiveMember, archiveReadOnly`; `document.go: documentKey` (first check), `document_keys.go: adjacentFile`.

---

## 2. External viewer/editor and associations

#### External program settings
- **Invocation:** F9 → Options → `Viewer settings…` / `Editor settings…`.
- **Dialogs:**
  - `Viewer settings`: `Wrap lines` (toggle, `Settings.ViewerWrap`), `External by default` (toggle, `ExternalViewerDefault`), `Command (blank=system)` (`ExternalViewer`).
  - `Editor settings`: `Auto indent` (toggle), `Syntax highlighting (Chroma)` (toggle), `Tab width (1–16)`, `Expand tabs to spaces` (toggle), `External by default` (`ExternalEditorDefault`), `Command (blank=system)` (`ExternalEditor`). Validation error → `Tab width must be 1–16`.
  - Options → `Code pages` → choose `Default encoding for new files` (the `textfile.Codepages` list).
- **Defaults:** `EditorAutoIndent true`, `EditorSyntax true`, `EditorTabSize 4`, `EditorExpandTabs false`, `ViewerWrap false`, external — off, commands empty, `DefaultCodepage "UTF-8"`.
- **Code:** `settings.go` (forms), `menu.go` (Options).

#### Internal/external choice (`viewPlan`)
- **Behavior (`launch.go: viewPlan(path, edit, alternate, forceInternal)`):**
  1. `forceInternal` → internal.
  2. Base pair: for viewing `ExternalViewerDefault`/`ExternalViewer`, for editing `ExternalEditorDefault`/`ExternalEditor`.
  3. Associations (`Settings.Associations`, Commands → `File associations…`): the first entry whose mask (`matchesMask`, with `MaskGroups` groups) matches the file name; if it has a non-empty `Viewer` (for F3) / `Editor` (for F4) — that command is used and the file is considered "associated" (`useExternal = true`).
  4. `useExternal = external || associated`; `alternate` inverts it.
  5. Internal → `LaunchPlan{Internal: true}`; otherwise `externalPlan(path, command, edit)`.
- **Launching the external program (`externalPlan`, `launch`):**
  - Empty command: viewing → `/usr/bin/open <path>`; editing → `/usr/bin/open -t <path>`. Executed as the background operation `Opening <basename>` (`workProgress`), waits for the `open` process to finish (it returns immediately after handing the file to the application), error → `<err>: <output>`.
  - Non-empty command: the path is quoted (`terminal.Quote`); if the command contains `{}` — it is substituted for every `{}`, otherwise appended with a space. The command is executed in the built-in terminal via `submitCommand(cmd, record=false)` → `execute()` (the panels are replaced by the terminal, the command is not added to the command-line history). There is no waiting for completion — it is an ordinary shell command.
- **Code:** `launch.go: viewPlan, externalPlan, launch, submitCommand`.

---

## 3. Document screen

### 3.1 Status line (top)
- **Where:** row 0, black text on `Cyan`; hidden by `HideStatus` (then the text starts at row 0).
- **Text (text mode and editor):** `" %s  %s  %s %s  Ln %d Col %d"` = `tail(displayPath, 32)` (long paths are truncated on the left with `…`), mode, codepage, EOL, line and column number (1-based; the column is in runes, a tab counts as one character).
  - Mode: viewer — `TEXT` / `HEX` / `DUMP`; editor — `EDIT INS` or `EDIT OVR`; ` RO` is appended when `ReadOnly` and ` *` when `Dirty`.
  - If highlighting is enabled and the document has a highlighting status — `  [<status>]` is appended, where status is the Chroma language name (for example `[Go]`), `syntax off: >1 MiB` or `syntax paused (limit/error)`.
- **Text (hex/dump in the viewer):** `" %s  %s  Offset %d / %d"` — path, mode, `ByteOffset`, `Size`.
- **`a.Status` messages:** drawn over the right edge of row 0 in yellow (`Yellow`) on `Blue`, truncated to `min(32, W/3)` characters on the left with `…`. Not shown when `HideStatus`.
- **Code:** `document_draw.go: drawDoc`, `grid.go: tail`.

### 3.2 Key bar (bottom line)
- **Where:** row `H-1`; hidden by `HideKeys`. 10 cells of width `W/10`, the number in gray on black, the label in black on `Cyan`.
- **Viewer labels:** `1Help 2Wrap 3Close 4Hex 5 6Edit 7Search 8Codepg 9Options 10Close`.
- **Editor labels:** `1Help 2Save 3 4Close 5 6View 7Search 8Codepg 9Options 10Close`.
- **Mouse:** a click on the key bar row → `documentKey(F<n>)`, `n = min(10, X*10/Cols+1)` (no modifiers).
- **Code:** `document_draw.go: drawDoc, documentClick`.

#### Cmd+B / Cmd+Shift+B — hide the key bar / status
- **Invocation:** Cmd+B (or Ctrl+B) toggles `HideKeys`; Cmd+Shift+B — `HideStatus`.
- **Where:** both modes. The setting is per document and is not saved.
- **Code:** `document.go: documentKey` (`case "b"`).

### 3.3 Scrollbar
- **Where:** the right column (`W-1`), only in text mode/editor, if `Scrollbar` and the number of lines > the height. Track `░` (`Cyan`), thumb `█` (`LightCyan`) at `top + Offset*(height-1)/(lines-1)`.
- **Toggle:** Cmd+S in the viewer (in the editor Cmd+S is save); F9 → `Scrollbar`. Enabled by default.
- **Dragging:** click/drag on column `W-1` → `Offset = (Y-top)*(lines-1)/(Rows-top-2)`, `ViewStart = At(Offset,0)`, `ManualScroll = true`; the `documentScrollbarDrag` flag keeps X on the bar until `mouseUp`.
- **Code:** `document_draw.go: drawDoc, documentClick`; `app.go: Handle("mouseUp")`; `mouse.go: dragMouse`.

### 3.4 Text rendering
- The background of the whole screen is `LightCyan` on `Blue`. The width of the text area is `W-1` (a column for the scrollbar).
- A tab expands to the next `tabWidth()` position (1–16, default 4; taken from `Settings.EditorTabSize` on every draw and for the viewer too). Control characters are drawn as `·` with width 1. Wide (CJK) characters take 2 columns; zero-width characters are glued to the previous cell.
- The editor cursor is a `Blue` cell on `Gray`. Selection is `Black` on `Cyan` (over the syntax highlighting colors).
- **Editor auto-scroll:** if not `ManualScroll`, `Offset` is pulled so that the cursor line is visible; `Column` is pulled so that the cursor is within `[Column, Column+W-1)` (`Column = col-W+2` when going out to the right). `ManualScroll` is set by the wheel/scrollbar and cleared on any key.
- In the editor, line wrapping (`Wrap`) is not applied — it is temporarily disabled during drawing.
- **Code:** `document_draw.go: drawDoc, visualRows, displayWidthTabs`.

### 3.5 Themes
- `Settings.ColorTheme`: empty/classic — the colors as is; `"Dark"` — `Blue→0x151b24`, `LightCyan→0xc7d7e9`, `Cyan→0x486987`; `"Light"` — `Blue→0xf1f3f5`, `LightCyan→0x182b49`, `White→0x000000`, `Yellow→0x885000`, `Cyan→0x80b8dd`. The substitution is performed over the whole grid after drawing (including the document and dialogs). Syntax highlighting colors that match the constants (`0xffffff` keyword/operator, `0xffff55` strings) are also substituted; the others (`0x55ff55`, `0xaaaaaa`, `0xff88ff`, `0xff8888`) remain.
- **Code:** `draw.go: Draw` (end), `grid.go` (constants).

---

## 4. View modes

#### F4 — Text / Hex; Shift+F4 — mode selection
- **Invocation:** F4 (viewer). Shift+F4 → choose `View mode` with the list `Text`, `Hex`, `Dump`.
- **Where:** viewer. (In the editor F4 closes the document, Shift+F4 — new file.)
- **Behavior:** F4: `text → hex`; from hex/dump → `text`, but for a paged file (`File != nil`) → `dump`. Shift+F4 for a paged file when choosing `Text` → status `Large files currently support paged hex/dump only`.
- **Code:** `document_keys.go: viewerKey`.

#### Hex mode (appearance)
- Line: `"%010X: "` (offset, gray, 12 columns) + bytes `"%02X "` (3 columns each starting at position 12) + an ASCII column starting at position `13+width*3` (bytes <32 and >126 → `·`).
- Line width `binaryWidth()`: if `BytesPerLine>0` — `min(BytesPerLine, (Cols-14)/4)`; otherwise `min(16, (Cols-14)/4)`, minimum 1.
- Highlight of the hex search match `[MatchStart, MatchEnd)` — `Black` on `Cyan`.
- Data: `dataAt(ByteOffset, width*height)` — from `Raw` or `File.ReadAt`.
- **Code:** `document_draw.go: drawBinary, binaryWidth`.

#### Dump mode (appearance)
- Line: offset + one character per byte. Each byte is decoded separately via `textfile.Decode([]byte{b}, Codepage)`: for single-byte encodings — the encoding's character; for UTF-8, bytes >126 (not valid on their own) → `·`; for UTF-16 (odd length → error) bytes >126 → `·`. Bytes <32 and 127 → `·`.
- Width: `Cols-14` (if `BytesPerLine` is not set; otherwise the hex limit `min(BytesPerLine,(Cols-14)/4)` applies — a side effect of Alt+Left/Right).
- **Code:** `document_draw.go: drawBinary`.

#### Alt+Left / Alt+Right — bytes per line
- **Invocation:** Alt+← / Alt+→ in hex/dump.
- **Behavior:** `BytesPerLine = max(1, width-1)` / `min(64, width+1)`, where `width` is the current `binaryWidth()`. The actual width is always limited by `(Cols-14)/4`.
- **Code:** `document_keys.go: viewerKey`.

#### F2 — line wrap; Shift+F2 — word wrap
- **Invocation:** F2 / Shift+F2 (viewer).
- **Behavior:**
  - If the document is in hex/dump: F2 (with any Shift) returns to `text` (for a paged file — to `dump` and status `Large files currently support paged hex/dump only`), wrapping is not changed.
  - F2: `Wrap = !Wrap`. Shift+F2: `WordWrap = !WordWrap` and `Wrap = true`.
  - After toggling, `ViewStart = At(Offset, 0)`.
  - The initial value of `Wrap` is taken from `Settings.ViewerWrap`; `WordWrap` is false by default (wrap by characters).
- **Wrap algorithm (`visualRows(width, count)`):** the line is cut when the total column width exceeds `width = Cols-1`; with `WordWrap` the cut is moved to the last whitespace character of the line segment (if there is one after the segment start). Tabs are counted per `tabWidth()`.
- **Code:** `document_keys.go: viewerKey`, `document_draw.go: visualRows, previousVisualStart`.

#### Large files (> 64 MiB)
- View only, hex/dump only, paged reading via `ReadAt`. Search is unavailable (`Search in paged files larger than 64 MiB is not implemented yet`). F6 → `Editor limit is 64 MiB`. F8 — silently ignored; Shift+F8 changes only `Codepage` (affects dump). Syntax highlighting is disabled. The descriptor is closed when the document is closed / when moving to an adjacent file.
- **Code:** `document.go: openDocument, dataAt, finishCloseDocument`.

---

## 5. Navigation in the viewer

#### Text mode without wrapping
- **Keys (`viewerKey`):** ↓/↑ — `Offset ±1`; PgDn/PgUp — `±(Rows-2)`; Home — `Offset=0, Column=0`; End — `Offset = max(0, lines-Rows+2)` (the last page); ←/→ — `Column ∓1`, with Cmd/Ctrl — by 20 columns; `Column` has no upper bound. `Offset` is clamped to `[0, lines-1]`, `ViewStart = At(Offset,0)`.
- **Code:** `document_keys.go: viewerKey`.

#### Text mode with wrapping
- ↓/↑ — by one visual line (`visualRows(...,2)[1].Start` / `previousVisualStart`); PgDn/PgUp — `Rows-2` visual lines; Home — `ViewStart=0`; End — the start of the last logical line. ←/→ have no effect. `Offset = Pos(ViewStart).Row`.
- **Code:** `document_keys.go: viewerKey`, `document_draw.go: previousVisualStart`.

#### Hex/dump
- ↑/↓ — `±width` bytes; PgUp/PgDn — `±width*(Rows-2)`; Home — 0; End — `max(0, Size-page)`; ←/→ — `∓1` byte (Alt — see §4). `ByteOffset` is clamped to `[0, Size-1]`.
- **Code:** `document_keys.go: viewerKey`.

#### Mouse wheel / trackpad
- **Messages:** `wheel` (Delta) and `scroll` (Delta = Y*3); `delta` is clamped to `[-12, 12]`.
- **Behavior (`scrollMouse`):** hex/dump — `ByteOffset += delta*width` (within `[0, Size-1]`); text with wrapping (viewer) — by visual lines; otherwise `Offset += delta` within `[0, lines-1]`. Sets `ManualScroll = true` (in the editor the cursor does not bring the screen back until the next key). Does not work while a dialog is open/busy; when a form/menu is open it scrolls them.
- **Code:** `mouse.go: scrollMouse`.

#### Click and drag in the text
- **Behavior (`documentClick(m, drag)`):** in hex/dump, clicks (except the key bar) are ignored. Otherwise the position is computed from row `Y-top` among `visualRows` and column `X+Column` (with wrapping — `X`), taking tabs and wide characters into account; `d.Move(target, selecting = drag || Shift, rect = Alt)` — i.e. a click places the cursor, Shift+click/drag select, Alt — rectangular. In the viewer the cursor/selection also exist (visible as a selection; used for Cmd+C and as the starting point of a search).
- **Double click:** selects a "word" — a contiguous run of characters of one class (letters/digits/`_`; whitespace; other), within the line.
- **Triple click:** selects the whole line together with `\n`.
- **Auto-scroll while dragging** past the top/bottom of the area — by 1 line per event (`dragMouse`).
- **Code:** `document_draw.go: documentClick`, `mouse.go: dragMouse`, `app.go: click`.

#### `+` / `-` — next / previous file of the panel
- **Invocation:** the `+` / `-` keys in the viewer (in the editor these are ordinary input).
- **Behavior (`adjacentFile`):** archive document → `Return to the archive panel to choose another file`. The nearest non-folder in the active panel forward/backward from the cursor is looked up. If the current document is `Dirty` → confirm `Save changes` / `Save changes before viewing the next file?` / buttons `Save`, `Discard`, `Cancel`. Then the panel cursor is moved and the file is opened **in the internal viewer** (`open(false)` → `openDocument`, bypassing external settings). `adjacentFile` is called only from `viewerKey`: in the editor `+`/`-` are ordinary input (a Dirty viewer is possible after F6); the old document is replaced (its `File` is closed). If opening failed — the old document and panel cursor remain. No file → `No next file` / `No previous file`.
- **Code:** `document_keys.go: adjacentFile`.

#### Alt+F8 / Cmd+G — Go to
- **Invocation:** Alt+F8, Cmd+G (Ctrl+G).
- **Dialog:** prompt `Go to`; label `Line:column (1-based), or percentage e.g. 50%` (text/editor) or `Byte offset (decimal / 0xHEX) or percentage` (hex/dump in the viewer); value empty.
- **Behavior:** the position is remembered (`remember()` → Cmd+Z in the viewer returns to it). `NN%`: 0–100, otherwise `Enter a percentage from 0 to 100`; in hex → `ByteOffset = (Size-1)*v/100`; in text → line `(lines-1)*v/100`, cursor at its start, `Offset`/`ViewStart` on it. Hex: a number (decimal or `0x…`), outside `[0, Size)` → `Offset outside file`. Text: `row[:col]`, 1-based; error/beyond the number of lines → `Invalid line / column` (a column > the line length is not an error, the cursor goes to the end of the line); the selection is cleared, `Offset = row-1`.
- **Code:** `document.go: gotoDocument`.

#### Cmd+0..9 / Cmd+Option+0..9 — bookmarks
- **Invocation:** Cmd (or Ctrl) + digit 0–9: restore; Cmd+Option+digit (or Ctrl+Shift+digit) — save. The digit is taken from `m.Code` (`DigitN`), so the keyboard layout and Option do not interfere. Cmd+Shift+digit does not set a bookmark: macOS takes Cmd+Shift+3/4/5 for screenshots.
- **Behavior:** saves `bookmark{Kind: document, Path, Edit, Help, ScreenID, Row, Col, View: docPosition}` into `App.Bookmarks[n]` — the same ten window slots as folder bookmarks (section 9). Status `Bookmark %d saved`. Jumping (from the panels or from any document screen): if the bookmark's screen (`ScreenID`) or another screen with the same file is open — focus goes to it, the current position goes into `Positions`, then `restoreBookmark`; otherwise the file is reopened in the same mode (viewer/editor) and the position is restored. A folder bookmark from a document → the panels (see section 9). ⌘0 resets the font size only in the terminal. If the text at the bookmark point has not changed — the exact view is restored; otherwise the cursor is placed at the remembered line and column, and if the file has shrunk below that line — at the end of the file. If the file does not exist or is not a regular file — status `Bookmark %d: <path>: <reason>`, nothing is opened. No bookmark — `Bookmark %d is empty`; a bookmark in closed help — `Bookmark %d: help is closed`. Bookmarks are in memory and are not saved.
- **Code:** `bookmarks.go: documentBookmarkKey, saveDocumentBookmark, goDocumentBookmark, restoreBookmark`.

#### Cmd+Z in the viewer — previous position
- **Behavior:** `Positions` (a stack of up to 100) is appended before a search (`findDocument`), Go to and jumping to a bookmark. Cmd+Z in the viewer pops the last one and restores it. In the editor Cmd+Z is undo (§9.5).
- **Code:** `document.go: remember, documentKey` (`case "z"`).

---

## 6. Search and replace

#### F7 / Cmd+F — search
- **Invocation:** F7; Cmd+F (Ctrl+F) — in both modes always a plain search. Cmd+F7 — replace in the editor (§below), a plain search in the viewer.
- **Where:** files ≤ 64 MiB. Paged → `Search in paged files larger than 64 MiB is not implemented yet`.
- **Dialog:** form `Search`, fields: `Search` (value — the last query `d.Search.Query`), toggles `Case sensitive`, `Whole words`, `Regex (Go/RE2)` (remember previous values), `Backwards` (always off), `Hex bytes` (on by default if the viewer is in hex mode). Esc — cancel, Enter — execute; Alt+letter — field/button mnemonics; Tab/arrows — navigation.
- **Text search behavior (`findDocument` / `Buffer.Find`):**
  - The regular expression is compiled as `(?m)` + (`(?i)` if not Case) + (QuoteMeta if not Regex). An empty query → error `enter search text`. A compilation error → its text in the status.
  - All matches are found with `FindAllStringIndex` over the whole text; `Whole words` discards matches whose neighbor is a letter/digit/`_`.
  - Start: in the viewer without a selection the cursor is placed at the start of the first visible line (`At(Offset,0)`); with a selection the start = the end of the selection (forward) or the start (backward); for an empty selection forward — the next character. Forward — the first match with `start >= start position`, otherwise **the first in the file (cyclically)**; backward — the last with `start < start position`, otherwise the last in the file.
  - The match found is selected (`Anchor=start`, `Cursor=end`). `Offset` = the line of the match, `Column = max(0, col-5)`, `ViewStart = Anchor`; in the viewer the mode switches to `text`. Status `Match found`; no matches → `Not found`. The search is cancellable (`Operation.Check`); in Async — background task `Searching document` on a clone of the document.
- **Hex search (`Hex bytes`):** the string is parsed as hex pairs (spaces are ignored); error/empty → `Enter pairs of hex digits, e.g. 48 65 6c 6c 6f`. The search runs in `d.Raw` (the original bytes): forward from `ByteOffset` (or from `MatchEnd` of the previous match), wrapping to the start; backward — `LastIndex` up to `MatchStart`, wrapping to the end. Found → `MatchStart/End`, `ByteOffset = hit`, `Mode = "hex"`. `Not found`. With `Replace` → `Hex replacement is not available in text editor`.
- **Code:** `document.go: searchDocument, findDocument`, `document_tasks.go: findDocumentThen`, `textfile/buffer.go: Search, Find, Matches`.

#### Shift+F7 / Alt+F7 / Space — next / previous match
- **Invocation:** Shift+F7 — forward; Alt+F7 — backward; Space in the viewer — forward.
- **Behavior:** `findDocument(backward)` with the saved `d.Search` or `HexQuery` (if the last search was hex — the hex search continues). Without a previous query → `enter search text`.
- **Code:** `document.go: documentKey`, `document_keys.go: viewerKey`.

#### Cmd+F7 / Cmd+H — search and replace
- **Invocation:** Cmd+F7 (Ctrl+F7) or Cmd+H (Ctrl+H) in the editor. In the viewer Cmd+H does nothing.
- **Dialog:** form `Search` with the same 6 fields plus `Replace with` (empty) and toggle `Replace all` (off).
- **Behavior:**
  - Without `Replace all`: the first match is found, then confirm `Replace` / `Replace this match?` / buttons `Replace`, `Skip`, `Cancel`. `Replace` replaces the selected match (for Regex — `re.ReplaceAllString(matchText, replacement)`, i.e. `$1`, `${name}` are supported), then the next one is found and the dialog repeats; `Skip` — next without replacing; `Cancel`/Esc — stop. Each replacement is a separate undo record.
  - `Replace all`: `Buffer.ReplaceAll` — all matches in a single `Replace(0, len)` (one undo record; for Regex — `ExpandString` with groups); status `Replaced %d matches` (including `Replaced 0 matches`); in Async — background task `Replacing matches`. When `ReadOnly` → error `editor is read-only`.
- **Code:** `document.go: searchDocument, confirmReplacement`, `document_tasks.go: replaceAllAsync`, `textfile/buffer.go: ReplaceAll`.

---

## 7. Codepages, BOM, line endings

### 7.1 Detection on open
- `textfile.Detect(raw)`: prefix `FF FE` → `UTF-16LE`; `FE FF` → `UTF-16BE`; valid UTF-8 → `UTF-8`; otherwise `Windows-1251` (the default heuristic for "not UTF-8").
- `Decode(raw, cp)`: strips the BOM (`EF BB BF` for UTF-8, 2 bytes for UTF-16; flag `BOM=true`); errors: `invalid UTF-8; choose another codepage`, `incomplete UTF-16 code unit`, `unsupported codepage %s`. For single-byte encodings a BOM is not recognized.
- `DetectEOL`: has `\r\n` → `CRLF`; otherwise has `\r` → `CR`; otherwise `LF`. Inside the buffer all line endings are normalized to `\n` (`Normalize`).
- Supported codepages (`textfile.Codepages`, in list order): `UTF-8`, `UTF-16LE`, `UTF-16BE`, `Windows-1251`, `CP866`, `Windows-1252`, `ISO-8859-1`, `KOI8-R`.
- **Code:** `textfile/encoding.go`.

### 7.2 Encoding on save
- `Encode(Endings(Text, EOL), Codepage, BOM)`: the BOM is written only for UTF-8/UTF-16; for single-byte encodings an unrepresentable character → error `text cannot be represented in %s: …` (the save is not performed, the file is not touched). If the text/codepage/EOL/BOM have not changed — exactly `SavedRaw` is written to disk (byte-for-byte save, mixed line endings are not normalized).
- **Code:** `textfile/encoding.go: Encode, Endings`, `document.go: saveDocument`.

#### F8 — toggle CP866 / Windows-1251
- **Invocation:** F8 (without Alt/Shift).
- **Behavior:** the target `cp = "CP866"`, and if the current one is already `CP866` — `Windows-1251` (from UTF-8 the first press gives CP866).
  - Editor: check `Encode(d.Text, cp, BOM)`; error → status; otherwise `Codepage = cp`, `modified()` (the document becomes Dirty — the output encoding will change).
  - Viewer: only if the file is not paged and not Dirty: the `Raw` bytes are decoded anew (`Decode`), `Buffer` is recreated, `Offset=0`, `markSaved()`. Error → status. Paged/dirty — silently nothing.
  - In Async — background task `Converting document`.
- **Code:** `document.go: documentKey` (`key == "F8"`).

#### Shift+F8 — choose codepage
- **Invocation:** Shift+F8.
- **Dialog:** choose `Code page` — a list of 8 codepages.
- **Behavior:** editor — as F8 (encodability check taking EOL into account, `modified()`). Viewer: Dirty → `Save edited text before reinterpreting bytes`; paged → only `Codepage` changes (affects dump); otherwise `Raw` is re-decoded, `BOM`, `EOL`, `Buffer` are updated, `Offset=0`, `markSaved()`. In Async — task `Converting code page`.
- **Code:** `document.go: chooseCodepage`.

#### F9 — document options (Options)
- **Invocation:** F9 in a document (in the panels F9 is the main menu).
- **Dialog:** form `Viewer / editor options`: `Line endings` (text, the current EOL), `Write BOM` (toggle), `Read only` (toggle), `Scrollbar` (toggle), `Syntax highlighting (Chroma)` (toggle, global setting), `Auto indent` (toggle, global), `Tab width (1–16)` (text, global), `Expand tabs to spaces` (toggle, global).
- **Behavior:** validation: `Tab width must be 1–16`; `Line endings must be LF, CRLF or CR` (case-insensitive). `EOL`/`BOM` are applied only in the editor (with `modified()`); `ReadOnly`, `Scrollbar` — per document; the remaining four — in `a.Settings` (apply to all documents; saved with the settings on `AutoSave`/`Save setup`).
- **Code:** `document.go: documentSettings`.

---

## 8. Switching viewer ↔ editor

#### F6
- **Invocation:** F6 (key bar `6Edit` / `6View`).
- **Behavior (`documentKey`):**
  1. Archive document → `Copy the file out with F5 before editing`. Help document → nothing.
  2. Paged → `Editor limit is 64 MiB`.
  3. Viewer → editor: text with NUL → `Binary data cannot be edited as text`; if not Dirty — a reversibility check (`Decode(Raw)` → `Encode` == `Raw`), otherwise `Encoding is not lossless; choose a codepage before editing`.
  4. Editor → viewer: the current text is encoded (`Encode(Endings(Text,EOL), Codepage, BOM)`) and becomes `Raw`/`Size` (so that hex shows the current bytes); if not Dirty — `SavedRaw` is taken. Encoding error → status, no switch.
  5. `Edit = !Edit`; when switching to the viewer `Offset` = the cursor line, `ViewStart = Cursor`. Text, cursor, Dirty and the undo history are preserved.
  6. In Async — background task `Converting document`.
- **Code:** `document.go: documentKey` (`key == "F6"`).

---

## 9. Editor

### 9.1 Text input
- Any single character (`utf8.RuneCountInString(m.Key) == 1`, without Cmd/Ctrl/Alt) is inserted via `Buffer.Insert`. With a selection — replaces it. In `Overwrite` mode (Insert) the insertion overwrites characters up to the end of the line (not across `\n`). With a rectangular selection — the insertion goes into all lines of the block at column `left`, replacing the column range (shorter lines are padded with spaces).
- `Insert` toggles `Overwrite` (`EDIT INS`/`EDIT OVR` in the status). There is no modifier check: Ctrl+Insert/Shift+Insert that reach `editorKey` also toggle (Shift+Insert is intercepted by the renderer as paste and does not reach the Go key handler).
- When `ReadOnly`, any edit → status `Read-only mode (⌘L to unlock)` (navigation, F2, Insert, Shift+F4 work).
- **Code:** `document_keys.go: editorKey`, `textfile/buffer.go: Insert, Replace`.

### 9.2 Navigation in the editor
- ←/→ — character (Cmd/Ctrl — word: `Word(at, dir)`, a word = letters/digits/`_`); ↑/↓ — line, keeping the column in runes (`At(row±1, col)`); PgUp/PgDn — `Rows-2` lines (Cmd — start/end of file); Home/End — start/end of line (Cmd — of file). Shift — extends the selection, Alt (with Shift) — rectangular (`Move(target, Shift, Alt)`).
- **Code:** `document_keys.go: editorKey`, `textfile/buffer.go: Move, Word, At`.

### 9.3 Selection
- Shift+arrows/PgUp/PgDn/Home/End — stream; Alt+Shift+… — rectangular (`Rect`; columns in runes, tab = 1). Cmd+A (and menu Edit → Select All, message `selectAll`) — everything (`Anchor=0, Cursor=len`, `Rect=false`). Cmd+U — clear the selection. Mouse — §5. `Selected()` of a rectangular block — the lines joined with `\n`.
- **Code:** `document.go: documentKey` (`a`, `u`), `app.go: Handle("selectAll")`, `textfile/buffer.go: Selected`.

### 9.4 Clipboard
- **Invocation:** Cmd+C / Cmd+X (Ctrl+C / Ctrl+X) → `copyDocument(cut)`; menu Edit → Copy/Cut (JS `farshmacCopy/farshmacCut` → messages `copy`/`cut`); Cmd+V, Shift+Insert, menu Edit → Paste → `pasteRequest` → message `paste` with the text of the system clipboard.
- **`copyDocument` behavior:** if a form is open — the value of the focused field is copied/cut (toggle — nothing); if a dialog — its `Input`; otherwise the document: `Selected()` is taken; if there is no selection — in the editor the whole current line is copied together with `\n` (on cut — deleted), in the viewer without a selection nothing happens. Cut in the viewer = copy. The text goes to the system via `Update.Clipboard`.
- **Paste:** in the editor `Insert(text)` + `modified()` (multi-line text — as is, line endings are normalized); in the viewer ignored; when a form is open — into the form field; in a dialog — into the input field.
- Shift+Delete in the editor is **not** cut, but ordinary deletion (see 9.6).
- **Code:** `document.go: copyDocument`, `app.go: Handle` (`copy`, `cut`, `paste`, `selectAll`), `internal/native/assets/app.js`, `window_darwin.m` (Edit menu).

### 9.5 Undo / Redo
- **Invocation:** Cmd+Z / Cmd+Shift+Z (Ctrl too) in the editor.
- **Behavior:** every `Replace` is a record (`At, Old, New, Before/After cursor, Anchor`). Undo restores the text, cursor and `Anchor` (selection); Redo — clears the selection. A new edit after undo clears redo. Limits: `MaxUndoBytes = 32 MiB` in total and 10 000 records (old ones are dropped); a single edit larger than 32 MiB clears the whole history and is itself not undoable. When `ReadOnly`, undo/redo do not work. After undo/redo `modified()` — if the text matches the saved one, Dirty is cleared.
- **Code:** `textfile/buffer.go: Replace, Undo, Redo`.

### 9.6 Deletion
- Backspace — the selection or the previous character; Delete — the selection or the next character. Cmd+Backspace / Cmd+Delete — word backward/forward. Cmd+Y — the whole current line together with `\n`. Cmd+K — from the cursor to the end of the line. Cmd+D — delete the selection (`Insert("")`).
- **Code:** `document_keys.go: editorKey`.

### 9.7 Enter, tab, indentation
- **Enter:** `Newline(auto)` — replaces the selection with `\n` + (with `Auto indent`) the leading spaces/tabs of the current line, but only those before the cursor/start of the selection. Shift+Enter — inserts the name (basename) of the current file of the active panel.
- **Tab:** with a selection (cursor ≠ anchor) — indents the block; `Expand tabs to spaces` → spaces up to the next tab stop (by the displayed width of the line before the cursor); otherwise `\t`. Shift+Tab — unindent.
- **Alt+I / Alt+U** — indent / unindent (`IndentWithOptions(reverse, width, spaces)`): the lines from the first to the last line of the selection (the last one is excluded if the selection ends at its column 0); indent — `\t` or `width` spaces; unindent — one leading `\t` or up to `width` spaces. The cursor and anchor are recomputed, rectangularity is preserved; one undo record.
- **Code:** `document_keys.go: editorKey`, `textfile/buffer.go: Newline, IndentWithOptions`.

### 9.8 Read only
- Cmd+L (Ctrl+L) toggles `ReadOnly` in both modes; F9 → `Read only`. Archive documents are `ReadOnly` initially. Indicator ` RO` in the status. Saving when `ReadOnly` → error `editor is read-only`.
- **Code:** `document.go: documentKey` (`l`), `saveDocument`.

### 9.9 Saving

#### F2 / Cmd+S — save
- **Invocation:** F2, Cmd+S (Ctrl+S), message `save`; key bar `2Save`. Editor only (in the viewer Cmd+S is the scrollbar, F2 is wrap).
- **Behavior (`saveDocumentThen(d, d.Path, false, nil)` → `saveDocument`):**
  1. `ReadOnly` → `editor is read-only`.
  2. Encoding (§7.2); if nothing changed — `SavedRaw`.
  3. The path equals `d.Path` and there is `Info` (the file existed) → `fsops.Save(path, data, Info)`: a check that the file on disk has not changed (`SameFile` by inode, size, mtime), otherwise error `file changed on disk; reopen it before saving`; write to a temporary `.farshmac-save-*` in the same directory, `chmod` to the original's permissions, `fsync`, `rename` (atomic replacement; ACL/xattr/hardlink topology are not preserved).
  4. A new file (`Info == nil`, created via Shift+F4) → `fsops.Create` (temporary `.farshmac-new-*`, permissions 0644, `os.Link` — error if the path is already taken).
  5. After success: `Path`, `Info = Stat`, `Raw = data`, `Size`, `markSaved()`, status `Saved`.
  6. Write error → a status with its text; the file on disk is not changed.
  - In Async: a clone is saved (`Saving file`), on completion `*d = *copy`, `Saved`; `done` (closing) runs only if the operation was not cancelled.
  - A remote document with `path == d.Path` → `saveRemoteDocument` (below).
- **Code:** `document.go: save, saveDocument`, `document_tasks.go: saveDocumentThen`, `fsops/files.go: Save, Create`.

#### Shift+F2 / Cmd+Shift+S — Save as
- **Dialog:** prompt `Save as`, label `Local file name`, value — `d.Path`; for a remote document — `<active panel path>/<basename of the remote file>`. A relative path is expanded from the active panel's path.
- **Behavior:** the same path → an ordinary save. An existing file → confirm `Overwrite` / `<basename> already exists. Replace it?` / buttons `Overwrite`, `Cancel`; on confirmation `fsops.Save` with `Lstat` of the target (overwrite preserving its permissions). Non-existent → `fsops.Create`. After a successful Save as of a remote document into a local file the document becomes local (`Remote/RemoteInfo/RemotePath` are cleared; the temporary cache is deleted on close).
- **Code:** `document.go: saveAs`, `document_tasks.go: saveDocumentThen`.

#### Saving to a remote server
- **Invocation:** F2/Cmd+S, Shift+F10, `Save` in the close dialogs for a document with `Remote != nil`.
- **Behavior (`remote_document.go: saveRemoteDocument`), background task `Saving remote file`:**
  1. A clone of the document; staged file `<TempDir>/save-<random>`.
  2. `Dial`, `Lstat(RemotePath)`; `sameRemoteInfo(current, RemoteInfo)` — size, mtime and mode must match the moment of opening/last save; otherwise error `remote file changed; reopen before saving`.
  3. Local save of the clone into the staged file (nested operation `Preparing upload`, cancelled together with the main one) — the encoding/ReadOnly checks happen here.
  4. `metadataQuestions`, then `remotefs.Copy` (1 worker, permissions = the permissions of the current remote file, `ExpectedDestination = current`, conflict → overwrite).
  5. A repeated `Lstat` → new `RemoteInfo`.
  6. Apply: `*d = *copy` (the document's local `Path` is now the staged file), the old cache file is deleted, status `Remote file saved`, then `done`.
- **Code:** `remote_document.go: saveRemoteDocument, sameRemoteInfo`.

#### Shift+F10 — save and close
- In the editor: `saveDocumentThen(d, d.Path, false, finishCloseDocument)` — on a save error the document stays open. In the viewer Shift+F10 = an ordinary close (§10).
- **Code:** `document.go: documentKey`.

---

## 10. Closing, multiple documents, help, terminal

#### Esc / F10 / F3 (viewer) / F4 (editor) — close
- **Invocation:** Esc, F10, key bar `3Close`/`10Close` (viewer) or `4Close`/`10Close` (editor). Esc additionally cancels a running highlighting task.
- **Behavior (`closeDocument`):** if `Dirty` → confirm `Save changes` / `<basename displayPath> has changed. Save it?` / buttons `Save`, `Discard`, `Cancel` (Esc = Cancel). `Save` → save to `d.Path` (locally or remotely), then close; on a save error the document remains.
- **`finishCloseDocument`:** deletes `TempDir` (remote/archive caches), stops highlighting, closes `File`, restores the previous document from the `Documents` stack (LIFO) or the panels; `refresh()` of the panels.
- **Code:** `document.go: closeDocument, finishCloseDocument, documentKey`.

#### Cmd+Q — quit with a check of all documents
- **Behavior (`RequestQuit`):** `Busy` → `Wait for the file operation to finish`. If any open document (including those hidden in the stack) is Dirty → confirm `Save changes` / `Save changes in open documents before quitting?` / `Save all`, `Discard`, `Cancel`. `Save all` saves them one by one (`saveAllThen`; an error aborts the quit). Then, if a command is running in the terminal — confirm `Quit` / `A command is running. Close the application?` / `Yes`, `No`. With several windows `WindowGroup` walks the windows one by one; in the other windows the status is `Finish the quit dialog in the other window`.
- **Code:** `app.go: RequestQuit, confirmQuitShell`, `document_tasks.go: saveAllThen`, `windows.go`.

#### F12 — screen list
- **Invocation:** F12 in a document and in the panels.
- **Dialog:** choose `Screens`: `Panels`, then for each document (by `ScreenID`) `View ` or `Edit `, `* ` if Dirty, the basename of `displayPath`. Focus is on the current document.
- **Behavior:** selecting makes the document current (`selectScreen`), the rest go onto the stack; `Panels` → all documents onto the stack, `Doc = nil`; the terminal is hidden.
- **Code:** `document.go: screenMenu, screenDocuments, selectScreen`.

#### Ctrl+Tab / Ctrl+Shift+Tab, Cmd+Shift+] / Cmd+Shift+[ — cyclic screen switching
- Works everywhere (except open forms/dialogs): the order is the panels, then the documents by `ScreenID`; `]`/Tab — forward, `[`/Shift+Tab — backward, cyclically.
- **Code:** `app.go: key`, `document.go: cycleScreen`.

#### F1 — help
- In a document: opens a new viewer screen `Viewer / editor help` with the `documentHelp` text (`Help=true`); inside the help F1 and F6 have no effect, highlighting is disabled. In the panels F1 opens `FarshMac — Help` with the general `help` text (the same viewer).
- **The `documentHelp` text (verbatim):**

```
FarshMac viewer / editor

Esc / F10   Close this document, return to previous screen / panels
Cmd+Q       Quit application (checks ALL unsaved documents)
F6          Switch viewer / editor, retaining text and cursor
F12         Switch open document screens
F7 / Cmd+F  Search: literal / regex, case, whole words, backwards, hex
Shift+F7    Next match      Alt+F7 Previous match
Cmd+F7 / Cmd+H  Search and replace; confirmation or Replace all
Alt+F8 / Cmd+G  Go to line:column, byte offset, or percentage
Shift+F8    Codepage chooser     F8 CP866 / Windows-1251
F9          Encoding-related options, line endings, BOM, read-only
Cmd+Option+0..9 Save bookmark    Cmd+0..9 Go to bookmark
Cmd+B       Toggle key bar      Cmd+Shift+B Toggle status
Cmd+O       Terminal / document

Viewer:
F2          Toggle wrap         Shift+F2 Word / character wrap
F4          Text / hex          Shift+F4 Text / hex / dump
Arrows      Scroll              Cmd+Left/Right Scroll 20 columns
Home/End    Start/end           PgUp/PgDn Page
Alt+Left/Right  Adjust hex bytes per row
Cmd+Z       Previous viewing position
+ / -       Next / previous file in active panel

Editor:
F2 / Cmd+S  Save                Shift+F2 / Cmd+Shift+S Save as
Shift+F4    Open/create another file
Shift+F10   Save and close this document
Cmd+Z / Cmd+Shift+Z Undo / redo
Insert      Insert / overwrite
Shift+arrows Stream selection   Alt+Shift+arrows Rectangular selection
Cmd+C/X/V   Copy / cut / paste  Cmd+A Select all
Cmd+Left/Right  Word movement   Cmd+Home/End File start/end
Cmd+Backspace/Delete Delete word   Cmd+Y Delete line
Cmd+K       Delete to line end  Alt+U / Alt+I Unindent / indent
Enter       Newline with auto-indent    Tab Insert tab

Limits: Go/RE2 regex, not Far's full regex dialect. No fuzzy/style-preserving
replacement, plugin APIs, macros or persistent bookmarks yet. Text editing
and text decoding are limited to 64 MiB; larger files use paged hex/dump.
Legacy encoding guesses are heuristic: choose an explicit codepage if needed.
```
- **Code:** `document.go: documentKey, documentHelp`, `app.go: function(1), help`.

#### Cmd+O — terminal over the document
- Cmd+O (Ctrl+O) without Shift/Alt with forms closed toggles `TermVisible`. The document stays open; while the terminal is visible, keys and clicks go to the terminal. Cmd+O again shows the document.
- **Code:** `app.go: key, showTerminal`.

---

## 11. Syntax highlighting (Chroma)

#### Scheduler
- **Conditions:** `Settings.EditorSyntax` is enabled, the document is not help, not paged, `Mode == "text"` (works in both the viewer and the editor).
- **Behavior (`pollSyntax`, called from `Poll`):** on a change of document/text/path — the old task is cancelled, the result is reset, 200 ms debounce. Text > `highlight.MaxBytes` (1 MiB) is not sent, status `syntax off: >1 MiB`. Otherwise `highlight.Run(ctx, displayPath, text)` is started in a goroutine; on completion the result is applied only if the generation has not changed (stale results are discarded). Success → status = the language name (`Result.Language`); error/limit → `syntax paused (limit/error)`. Esc in the document cancels the current task; closing the document/window — `stopSyntax`.
- **`highlight.Run`:** globally no more than 2 concurrent helper processes (across all windows); timeout 2 s; runs `<executable> --syntax-helper <basename>` with the text on stdin; output is limited to 8 MiB; offsets are validated; `MaxSpans = 100000`.
- **Helper (`HelperMain`/`Parse`):** Go memory limit 64 MiB; lexer — `lexers.Match(basename)` (for remote files — the basename of the remote path), otherwise `lexers.Analyse(first 8192 bytes)`; no lexer → an empty result (no status). Errors: `syntax size limit`, `syntax token limit` (> 400 000 tokens), `lexer changed input`, `syntax span limit`, `incomplete lexer output`, a lexer panic → `lexer: …`.
- **Colors (`tokenColor`):** Comment `0xaaaaaa`; Keyword `0xffffff`; LiteralString `0xffff55`; LiteralNumber `0x55ff55`; NameFunction/NameClass/NameBuiltin `0x55ff55`; NameTag/NameAttribute `0xff88ff`; Operator `0xffffff`; Error `0xff8888`; everything else — `Plain = 0x55ffff` (not written into spans). The selection is drawn over the highlighting.
- **Rendering:** `documentSyntax(d)` returns the result only if it belongs to the current text and path (otherwise — no colors), so while editing, until recomputation, the highlighting disappears.
- **Code:** `document_syntax.go`, `highlight/highlight.go`, `cmd/farshmac/main.go` (dispatch of `--syntax-helper`).

---

## 12. Summary of status messages (document)

`Choose a regular file` · `Editor limit is 64 MiB. Large files open in paged hex/dump viewer.` · `File grew while opening; reopen it` · `Encoding is not lossless; inspect in viewer and choose a codepage before editing` · `Binary data: open with F3 in hex/dump mode` · `Editor limit is 64 MiB` · `Binary data cannot be edited as text` · `Encoding is not lossless; choose a codepage before editing` · `Copy the file out with F5 before editing` · `Return to the archive panel to choose another file` · `No next file` / `No previous file` · `Large files currently support paged hex/dump only` · `Search in paged files larger than 64 MiB is not implemented yet` · `Hex replacement is not available in text editor` · `Enter pairs of hex digits, e.g. 48 65 6c 6c 6f` · `Not found` · `Match found` · `Replaced %d matches` · `enter search text` · `Save edited text before reinterpreting bytes` · `Tab width must be 1–16` · `Line endings must be LF, CRLF or CR` · `Enter a percentage from 0 to 100` · `Offset outside file` · `Invalid line / column` · `Bookmark %d saved` · `Read-only mode (⌘L to unlock)` · `editor is read-only` · `Saved` · `Remote file saved` · `file changed on disk; reopen it before saving` · `remote file changed; reopen before saving` · `remote file changed during download` · `viewer requires a regular remote file` · `text cannot be represented in %s: …` · `invalid UTF-8; choose another codepage` · `incomplete UTF-16 code unit` · `unsupported codepage %s` · `syntax off: >1 MiB` · `syntax paused (limit/error)` · `View/edit history is empty` · `Archive is read-only. Use F5 to copy files out.` · `Use F5 to copy the archive link` · `This command requires a local panel. Use F5 to copy remote files first.` · `Wait for the file operation to finish`.

---

## Discrepancies between the documentation (VIEWER_EDITOR_PARITY.md, README, F1 help) and the code

> **Closed on the `backlog-fixes` branch:** the decisions on each item are in the "Decisions on discrepancies" section of `docs/IMPLEMENTATION_BACKLOG.md`.

1. **Alt+F3 / Alt+F4 (internal/external inversion) and Cmd+Shift+F3 / Cmd+Shift+F4 (force internal)** are implemented in `launch.go`/`app.go` but are not mentioned in the README, the F1 help, `VIEWER_EDITOR_PARITY.md`, or the Files menu.
2. **Menu Commands → `File view/edit history`** does not show a key (empty string), although Alt+F11 works and is listed in the panel help (`Alt+F11/F12 View/edit history / folder history`).
3. **Space in the viewer = next match** — exists in the code, described nowhere.
4. **Cmd+D (delete selection), Cmd+U (clear selection), Cmd+L in the viewer, Shift+Enter (insert the panel's file name), Ctrl+Tab / Cmd+Shift+[ ] (screen switching), double/triple click, scrollbar dragging, Shift+F10 in the viewer (simply closes)** — not described in the help/parity.
5. **The F1 help describes only Cmd combinations**, while the code also accepts Ctrl (`command := m.Meta || m.Ctrl`) for all the document's "Cmd+…" combinations, including bookmarks.
6. **Insert ignores modifiers:** Ctrl+Insert in the editor toggles insert/overwrite rather than copying (in Far, Ctrl+Insert is copy; in the FarshMac panels Ctrl+Insert copies names). Shift+Delete in the editor is deletion, not cut (in the panels — permanent delete). The help is silent about this.
7. **F9 in a document** — the help says "Encoding-related options, line endings, BOM, read-only", but the form also contains `Scrollbar`, `Syntax highlighting (Chroma)`, `Auto indent`, `Tab width (1–16)`, `Expand tabs to spaces`, and the last four are **global** settings that change all documents (parity mentions them as "F9: autoindent, tab width…").
8. **Alt+Left/Right "Adjust hex bytes per row"** also limits the width of dump mode (`BytesPerLine` applies in dump too, with the hex ceiling `(Cols-14)/4`); not described.
9. **Parity: "Shift+F2 selects word/character wrapping"** — in the code Shift+F2 toggles `WordWrap` and always enables `Wrap` (there is no separate selection), which matches the help "Word / character wrap" but not "selects".
10. **F8 "CP866 / Windows-1251"**: from any other codepage the first press gives CP866 (not a "toggle" between the two); in the viewer, for a Dirty document and for paged files F8 silently does nothing (no message), whereas Shift+F8 gives `Save edited text before reinterpreting bytes`.
11. **`+`/`-` always open the adjacent file in the viewer** (even from the editor) and always internally (`openDocument`), bypassing external settings — parity says only "Adjacent files: + / - in viewer".
12. **View history (Alt+F11)** opens the file directly with `openDocument` — also bypassing external programs and without the ability to open a remote/archive file; parity says "Persistent view/edit history … absent", whereas `viewHistory` is saved in the setup (`Viewed`) — i.e. the history is persistent, only the positions are not.
13. **Remote editing and saving to the server** (`openRemoteDocument`, `saveRemoteDocument`, the `sameRemoteInfo` check) are entirely absent from `VIEWER_EDITOR_PARITY.md` and the README (the README only mentions FTP/SFTP panels).
14. **Viewing from an archive** (`ReadOnly`, `ArchiveSource`, F6 prohibition) is described only in ARCHIVE_SUPPORT/README in one phrase ("editing an archive member requires extracting it first"); the messages are not documented.
15. **Parity "Highlighting: Chroma 2.20.0"** — matches `go.mod` (`chroma/v2 v2.20.0`). But parity does not mention that highlighting also applies in the viewer (text mode) and that the language status is shown in the title.
16. **The light theme** replaces `0xffffff` and `0xffff55` (keywords/operators and strings) with dark colors, but leaves `0x55ff55` (numbers/functions), `0xaaaaaa`, `0xff88ff` bright on a light background — the theme is not described in parity.
17. **Panel help:** "The text editor saves atomically, but does not preserve ACLs/xattrs/hardlinks" — matches `fsops.Save`; however, new files (`fsops.Create`) are created with permissions 0644 rather than per umask — not described.
18. **README "Keyboard essentials"** contains no viewer/editor key except `F3 / F4`; all the others are only in the document's F1 help.
19. **Parity/help: "Cmd+Shift+0..9 Save bookmark"** — document bookmarks parse `m.Key` via `strconv.Atoi` (`document.go: documentKey`), while the renderer sends `Key: e.key` as is (`app.js: keyMessage`), i.e. with Shift on the US layout `!@#…` arrives and the bookmark is not saved. The panel folder shortcuts (Ctrl+Shift+1..9) use `m.Code == "DigitN"` for the same case (`folder_shortcuts.go`). The test `TestWrapGotoBookmarksAndRendering` sends `Key:"3", Shift:true` directly and does not cover this path. Probably a bug in the code, not in the documentation.


<a id="part-4"></a>
# Part 4. Terminal, command line, windows, native shell, renderer

The source of truth is the repository code `/Users/ikim/src/remote-codex/farshmac` (branch `main`, commit `b91313a`). Interface strings are quoted verbatim (English), as in the code. At the end is a list of discrepancies between the documentation/help and the code.

Conventions: "panel mode" — `App.TermVisible == false`, `Update.Mode == "panels"`; "terminal mode" — `TermVisible == true`, `Mode == "terminal"`. `PanelKeys` (a field of `Update`) is true only when there is no document, form, dialog or menu and no background operation is running; the native layer additionally clears it in terminal mode (`panelKeys = state.panelKeys && mode != "terminal"`).

---
## 1. Panel command line

#### Typing characters into the command line
- **Invocation:** any printable key (a single rune) without Ctrl/Alt in panel mode; Space when the line is non-empty; text paste (⌘V / Shift+Insert / IME composition).
- **Where available:** panel mode, no menu/dialog/form/document, not `Busy`. Printable characters are first checked by `fastFindKey` (Alt+character is fast find; if a fast-find query is already active, printable characters go into it), then `selectionKey` (Numpad +/−/*), then `folderShortcutKey` and `farPanelKey`.
- **Behavior:**
  1. `a.Command += m.Key` for single runes without Ctrl and without Alt (Meta combinations are handled separately).
  2. Space: if `a.Command == ""`, marks the file (`mark`); otherwise appends a space.
  3. Backspace: removes the last rune; on an empty line does nothing (does not go up to the parent directory).
  4. Escape: first `cancelReads()` (cancels the panel read); if there is nothing to cancel, clears `a.Command` and `a.Status`.
  5. Paste (`paste` message): control characters are replaced with a space (`unicode.IsControl → ' '`), then all `\n` are removed; the result is appended to `a.Command`. With an active fast-find the paste goes to fast-find; with an open form, into the form field; in the editor, into the document; in a dialog, into the input field (if the field has focus).
  6. `compositionend` (IME) in panel mode is sent as `paste` with `e.data`.
- **Rendering:** the second-to-last screen line: `<active panel path>>` + command, cursor `▌` at the end (`draw.go`). For a remote panel the prefix is `local:<local path>`. A long line is truncated from the tail (`tail(prompt, g.W-1)`). The status line above it: `a.Status` or by default `Tab Switch panel   Space Select   ⌘O Terminal   ⌘⌥0…9 Bookmark`.
- **Special cases:** Ctrl+U and ⌘U swap the panels without touching the command. Ctrl+Left/Right with an empty command change the panel widths (step 2 %, range 20–80), Ctrl+Num5 resets.
- **Code:** `internal/ui/app.go` `key()` (the `default`, `Backspace`, `Escape`, `" "` branches), `Handle()` case `"paste"`, `internal/ui/draw.go`.

#### Command history: Ctrl+E / Ctrl+X, ⌘↑ / ⌘↓, Ctrl+↑ / Ctrl+↓
- **Invocation:** Ctrl+E (previous), Ctrl+X (next); ⌘ArrowUp / ⌘ArrowDown; Ctrl+ArrowUp / Ctrl+ArrowDown.
- **Where available:** panel mode without modal layers.
- **Behavior:** `a.history` is a slice of strings, `a.historyAt` is the index (equal to `len(history)` after execution). "Back" is `historyAt = max(0, historyAt-1)`, the command is replaced with `history[historyAt]`. "Forward" is `historyAt = min(len, historyAt+1)`; if the index equals `len`, the command becomes empty. Ctrl+E/X: Ctrl only (not ⌘). ⌘↑/↓ and Ctrl+↑/↓ do the same.
- **Limit:** `Settings.HistoryLimit` (1–10000, default 200); `appendUnique` removes duplicates and keeps the last N.
- **Persistence:** `savedSetup.Commands` in `~/Library/Application Support/FarshMac/setup.json` (via `os.UserConfigDir()`), written by `saveSetup` (Shift+F9, Options → Save setup, or on exit if `AutoSave`). On startup `LoadSetup` restores `a.history`, `historyAt = len`.
- **Code:** `internal/ui/far_keys.go` (`case "e", "x"`), `internal/ui/app.go` `key()` (`arrowup/arrowdown`, `ArrowUp/ArrowDown` with Ctrl), `internal/ui/settings.go`.

#### Clearing the command line: Ctrl+Y / ⌘Y
- **Invocation:** Ctrl+Y or ⌘Y (`cmd && k == "y"`).
- **Behavior:** `a.Command = ""`. The history does not change.
- **Code:** `far_keys.go` `case "y"`.

#### Command history menu (Alt+F8 / Commands → Command history)
- **Invocation:** Alt+F8; F9 → Commands → `Command history` (shortcut label `AltF8`).
- **Behavior:** the `a.history` list in reverse order (newest on top) in a list form titled `Command history`; selecting an item puts its text into `a.Command` (does not execute it).
- **Messages:** with an empty history, the status `Command history is empty`.
- **Code:** `internal/ui/commands.go` `historyMenu("Commands")`, `internal/ui/form.go` `choose`.

#### Filename completion: ⌘Space
- **Invocation:** ⌘Space (`Meta` + `" "`). Tab does NOT complete; Tab switches the active panel.
- **Where available:** panel mode; only if `Settings.CompleteFiles` (default true; turned off in Terminal settings / AutoComplete settings).
- **Behavior:**
  1. Only the last word of the command is completed (after the last space/tab).
  2. If the token contains `' " \` $ \`, the status is `Completion: use an unquoted path prefix` and nothing is done.
  3. The base is the active panel path; `dir = filepath.Dir(token)`, `prefix = filepath.Base(token)`; if the token is empty or ends with `/`, the prefix is empty and `dir = token`. Relative paths and `~` are expanded via `expand(dir, base)`.
  4. The directory is read with `fsops.ReadDirectory` (in the async application, as a background task labelled `Completing filename`, cancellable with Esc; in sync, synchronously). `Settings.CompleteIgnoreCase` is honored. Directories get a `/` suffix. The result is sorted.
  5. One match is inserted immediately; several open the `Complete filename` list form, and the selection is inserted. Insertion: `a.Command[:start] + terminal.Quote(value)` (always single quotes).
  6. No matches: the status `No matching paths`.
- **Code:** `internal/ui/completion.go` `completeCommand`, `applyCompletion`.

#### Inserting names/paths into the command line
- **Invocation and behavior:**
  - **⌘Enter / Ctrl+Enter / Ctrl+J / ⌘J** → `insertFilename()`: the name of the file under the cursor of the active panel. If the name contains control characters, the status is `Cannot insert a filename containing control characters`. A name starting with `-` gets the `./` prefix. If the name contains anything other than `[A-Za-z0-9_./-]`, it is wrapped in `terminal.Quote` (single quotes, `'` → `'\''`). Before insertion, if the command is non-empty and has no trailing space/tab, a space is added; after the name, a space. (Menu Commands → `Insert filename` `⌘Enter`.)
  - **Ctrl+Shift+Enter / ⌘Shift+Enter** → the name of the file under the cursor of the **inactive** panel via `appendPanelValue` (always quoted).
  - **Ctrl+F / ⌘F** → the full path of the current item of the active panel (`p.Current()`), quoted.
  - **Ctrl+; / ⌘;** → the full path of the current item of the inactive panel.
  - **Ctrl+[ / ⌘[** → the left panel path; **with Shift**, the active panel path. **Ctrl+] / ⌘]** → the right panel path; **with Shift**, the inactive panel path.
  - `appendPanelValue`: an empty value is ignored; control characters → the status `Cannot insert a path containing control characters`; with a non-empty command without a trailing space, a space is added; the value in `terminal.Quote` + a space.
- **Code:** `internal/ui/app.go` `insertFilename`, `internal/ui/far_keys.go` `appendPanelValue`, `farPanelKey` (`case "j"`, `"f"`, `";"`, `Enter`, `BracketLeft/Right`).

#### Copying the command line / names to the clipboard: Ctrl+Insert, ⌘Insert, Alt+Shift+Insert, Ctrl+Alt+Insert
- **Invocation:** Insert with Ctrl or ⌘ (`cmd`), or Alt+Shift.
- **Behavior:**
  - Without Shift/Alt and with a non-empty command, the command text is copied.
  - If the command is empty, or Shift or Alt is pressed, the selected items are copied (`p.Selected()`: marked ones, otherwise the current one, except `..`): without Alt, base names; with Alt, full paths; with Alt+Ctrl, paths after `filepath.EvalSymlinks` (error → `a.error`), for a remote panel `Remote.Label() + path`. If nothing is selected, the panel path. Names are joined with `\n`.
  - The value goes to `a.Clipboard` → `Update.clipboard` → the renderer sends `{Type:'clipboard'}` → the native layer writes to `NSPasteboard` (`NSPasteboardTypeString`).
- **Code:** `far_keys.go` (`m.Key == "Insert" && (cmd || m.Alt && m.Shift)`), `app.js` (`state.clipboard`), `window_darwin.m` (`clipboard`).

#### ⌘C / ⌘X / ⌘V / ⌘A in panel mode
- **Invocation:** the Edit menu (Copy ⌘C, Cut ⌘X, Paste ⌘V, Select All ⌘A). In panel mode the renderer does not intercept ⌘C/⌘V/⌘X (`if(e.metaKey&&['c','v','x']...)return;`); they reach the native menu, which calls `window.farshmacCopy/Cut/Paste/SelectAll`.
- **Behavior:** `copy` → `copyDocument(false)`: in a form, the field value; in a dialog, the input; without a document, `copyPanelCommand`: the command, or (if the command is empty) the marked names, or the name under the cursor (joined with `\n`). `cut` → the same, but clears the field/command (with an empty command, cut copies nothing). `selectAll` without a document → the equivalent of `⌘A` in `key()` (in a dialog selects the input). `paste`: see "Typing characters".
- **Code:** `app.go` `Handle` (`copy`, `cut`, `selectAll`, `paste`), `document.go` `copyDocument`, `mouse.go` `copyPanelCommand`.

---

## 2. Command execution and shell integration

#### Executing a command (Enter)
- **Invocation:** Enter with a non-empty `a.Command` in panel mode; also `Apply command…` (Files), `User menu` (F2), `runUserCommand`: they build `a.Command` and call `execute()`.
- **Behavior (`App.execute`)**:
  1. `a.Term == nil` → the status `Shell unavailable`, return.
  2. `!a.ShellReady` → the status `Shell is busy; ⌘O to return to it`, `TermVisible = true` (switches to the terminal), the command stays in the line.
  3. `path = active panel.filesystemPath()` (for archive/remote panels, the local path); `a.Term.Run(path, command)`; an error → `a.error`.
  4. On success: the command is added to `a.history` (`appendUnique`, limit `HistoryLimit`), `historyAt = len`, `Command = ""`, `TermVisible = true`, `ShellReady = false`.
- **Transport (`Session.Run` → `request`)**: validation: `cwd` is absolute, no NUL in cwd/command, `len(cwd)+len(command) <= 1 MiB`, otherwise the error `invalid panel command or directory`. The file `<sessiondir>/panel-command` is created `O_EXCL` 0600 (if it already exists, an EEXIST error), content `cwd\0command\0run\0`. Then the private key `commandKey = ESC [777;<token>~` is written to the PTY (token is 32 hex from 16 random bytes per session). On error the file is removed.
- **On the shell side:**
  - zsh: the ZLE widget `_farshmac_panel_command` (bound in the keymaps `main emacs viins vicmd` to commandKey, historyKey, directoryKey) reads three NUL-separated fields via `builtin read -d ''`, removes the file, and if `$PWD` does not match does `builtin cd -- target`; on failure `zle -M "Cannot enter directory: <target>"`, a status marker 1, the command is not executed. Otherwise `BUFFER=$line; CURSOR=${#BUFFER}; zle reset-prompt; zle .accept-line`: the text goes into the regular zsh history, and the user's unfinished input is replaced entirely.
  - bash/sh (Bash 3.2): the readline macro commandKey = `\C-x\C-k` (kill-whole-line) + `\C-x\C-g` (`bind -x` → `_farshmac_panel_command`). The function prints `\n<line>\n`, restores the previous history entry (a workaround for the Bash 3.2 bug with `history -s` from `bind -x`), `history -s -- line`, prints the busy marker, `builtin eval -- "$line"`, `history -a`, prints the ready marker with the status. A failed `cd` → a status marker 1 without execution.
  - Multi-line commands (with `\n`) are passed as is (Apply command joins commands with `\n`).
- **Messages:** `Shell unavailable`, `Shell is busy; ⌘O to return to it`, after completion `Exit <N> · ⌘O panels / terminal`.
- **Special cases:** the command is executed in the active panel's directory even if the shell is in another one; for a remote panel, in `RemoteLocalPath`. Integration sequences are never sent while `ShellReady == false`.
- **Code:** `internal/ui/app.go` `execute`, `internal/terminal/session.go` `Run`, `request`, zsh rc; `internal/terminal/bourne.go`.

#### Keyboard input into the terminal (`input` / `inputBinary`)
- **Invocation:** xterm `onData` → `{Type:'input',Text}`; `onBinary` (legacy mouse reports with bytes ≥128) → `{Type:'inputBinary',Text: base64}`.
- **Behavior:** `input`: if the text contains `\r` or `\n`, `ShellReady = false` immediately (reserving the shell until the preexec marker arrives); then `Term.Write`. `inputBinary` is decoded from base64 and written as is. `Session.Write`: a 1 MiB limit (`paste exceeds 1 MiB`), a non-blocking queue of 32 buffers (`terminal input queue is full`), a closed session → `shell closed`. These messages go to `Status` via `a.error`.
- **Special cases:** `input`/`inputBinary`/`historyAppend`/`historyRead`/`commandHistory*`/`terminalError`/`mouseUp` are handled even while `Busy` (a file operation).
- **Code:** `app.go` `Handle`, `session.go` `Write`.

#### Supported shells and selection
- **Setting:** F9 → Options → `Terminal settings…`: a form: `Shell: bash / sh / zsh (new windows)` (text), `Command history entries (1–10000)`, `Complete filenames`, `Completion ignores case`, `Use custom shell prompt / hooks (new windows, may be slow)`. Validation: `Choose bash, sh or zsh`; `History entries must be 1–10000`. After applying, the status `Shell applies to new windows; Save setup to keep settings after restart`. Defaults: `Shell: "bash"`, `ShellCustomPrompt: false`. An invalid value in setup.json is normalized to `bash`.
- **Startup (`terminal.StartConfigured(cwd, cols, rows, shell, customPrompt)`)**:
  - A temporary directory `farshmac-shell-*` (0700) is created with private startup files (0600).
  - **zsh:** `/bin/zsh -il`, `ZDOTDIR=<tempdir>`, `FARSHMAC_INTEGRATION_DIR=<tempdir>`, `FARSHMAC_USER_ZDOTDIR=<the user's ZDOTDIR or $HOME>`. The `.zshenv`/`.zprofile` wrappers first source the user's files of the same name, then set `ZDOTDIR` back to the integration directory; `.zshrc` sources the user's `.zshrc`, registers `_farshmac_precmd` (in `precmd_functions`) and `_farshmac_preexec` (in `preexec_functions`), the widget, `unsetopt PROMPT_SP`, and at the end `export ZDOTDIR="$FARSHMAC_USER_ZDOTDIR"` (so child zsh processes see the user's configuration).
  - **bash:** `/bin/bash --noprofile --rcfile <tempdir>/shellrc -i`. `shellrc` sources `/etc/profile`, then the first of `~/.bash_profile`, `~/.bash_login`, `~/.profile`; if none, `~/.bashrc`. The user's `PROMPT_COMMAND` is saved in `_farshmac_user_prompt_command` and executed BEFORE the ready marker (`_farshmac_before_prompt`), then `PROMPT_COMMAND=_farshmac_before_prompt`.
  - **sh:** `/bin/sh -i`, `ENV=<tempdir>/shellrc`, `FARSHMAC_USER_ENV=<the old $ENV>`; shellrc sources the user's `$FARSHMAC_USER_ENV` and at the end restores `ENV` for child shells (`export ENV="$FARSHMAC_USER_ENV"` or `unset ENV`).
  - bash/sh bindings in the keymaps `emacs-standard vi-insert vi-command`: `\C-x\C-k` kill-whole-line, `\C-x\C-g` → the function, plus the macros `\C-x\C-e` end-of-line, `\C-x\C-i` quoted-insert, `\C-x\C-b` backward-delete-char, `\C-x\C-j` accept-line, `\C-x\C-y` yank, used by directoryKey to change directory while preserving the line being edited (Bash 3.2 caches PS1 during `bind -x`).
  - **Simple prompt (customPrompt=false, the default):** zsh: `precmd_functions=()`, `preexec_functions=()`, `unfunction precmd preexec`, `unsetopt PROMPT_SUBST`, `PROMPT='%n@%m %~ %# '`, `RPROMPT=''` (after loading the user's .zshrc, i.e. the user's prompt hooks are disabled but aliases/variables remain); bash/sh: `unset PROMPT_COMMAND`, `PS1='\u@\h \w \$ '`.
  - Environment: `TERM=xterm-256color`, `COLORTERM=truecolor`, `SHELL=/bin/<shell>`, `BASH_SILENCE_DEPRECATION_WARNING=1`, `CLICOLOR=1` (if not set and `NO_COLOR` is empty). PTY size: `cols × (rows-1)` (one line for the footer).
  - The user's files are not modified.
- **Startup probe:** `TestLiveStartupProbe` is an optional diagnostic test (`FARSHMAC_PROBE_STARTUP=1`, `FARSHMAC_PROBE_CUSTOM_PROMPT`) that measures the time until the first bash ready marker and until a command is accepted, with a 20 s limit. Not part of `check.sh`.
- **Code:** `internal/terminal/session.go`, `bourne.go`, `startup_probe_test.go`, `internal/ui/settings.go` (`Terminal settings`), `app.go` `startTerminalAt`.

#### Readiness protocol (Ready / Running / Exit markers, cwd, exit status)
- **Behavior:** the shell prints OSC `ESC ] 777;<token>;<status|busy>;<PWD> BEL`. The Go parser (`parser.feed`) cuts only this marker out of the PTY stream; everything else (alternate screen, mouse, colors, DSR) goes to xterm. A marker split at a read boundary is buffered (a tail of up to `len(prefix)-1` bytes is kept; an unfinished marker > 64 KiB is flushed as data). `status != "busy"` → `Event{Ready:true, Status, Cwd}`; `busy` → `Event{Running:true, Cwd}`. On every Ready event `readCommands()` picks up the `command-history` file if present. A read EOF/EIO → `Event{Exit:true, Err:nil}`, any other error → `Err`. The `Events` channel has capacity 64; the PTY reader blocks if the UI does not drain it.
- **UI (`App.Poll`)**: a single Poll reads up to 8 events and only while `len(pending) < 256 KiB`; draining happens only if `terminalCanDrain()`. `Running` → `ShellReady=false`. `Ready` → `ShellReady=true`, `shellStarted=true`, `ShellCwd=e.Cwd`, `finishPanelReturn()`, the status `Exit <N> · ⌘O panels / terminal`, `shellCommands` (if a history snapshot arrived). `Exit` → `scheduleShellRestart(e.Err)`.
- **Terminal footer (`Update.Footer` in terminal mode):**
  - with a scheduled restart: `⌘O  Panels    ·    <Status>`;
  - ready: `⌘O  Panels    ·    Shell ready    ·    <ShellCwd>`;
  - no first prompt yet: `⌘O  Panels    ·    Starting <shell> (loading shell configuration)…    ·    ⌘. Interrupt`;
  - a command is running: `⌘O  Panels    ·    ⌘.  Interrupt`.
  - The renderer appends ` · ⌘⇧H Commands` and, on a history error, the prefix `History: <error> · `.
- **Code:** `session.go` `parser`, `read`; `app.go` `Poll`.

#### Switching panels ↔ terminal (⌘O / Ctrl+O)
- **Invocation:** ⌘O or Ctrl+O (without Alt/Shift); the menu Commands → `Panels on/off / terminal` `⌘O` (calls only `showTerminal`). In the terminal the renderer intercepts the combination and sends `key`.
- **Where available:** no dialog and no form (`a.Dialog == nil && a.Form == nil`); works with an open document (then the terminal is shown on top, and returning does not sync the panels) and during `Busy`? No: while `Busy`, `key()` first handles only Escape/Alt+C/the form and returns.
- **Behavior:** closes the menu and fast-find. If the terminal is hidden, `showTerminal()`; otherwise `TermVisible=false`, and if there is no document, `returnFromTerminal()` + `refresh()` (re-reading both panels).
- **Code:** `app.go` `key()`.

#### Directory sync panel → shell (`showTerminal`, `syncShellCwd`, `pendingShellCwd`)
- **Behavior:**
  1. `showTerminal`: `TermVisible=true`. If there is a deferred "return from terminal" `shellPanelReturn` for the same panel and the same path, it is discarded and the cwd is NOT sent (the user just glanced at the panels and came back). Otherwise, if the active panel is local (`Remote == nil`): `pendingShellCwd = filesystemPath()`, `syncShellCwd()`.
  2. `syncShellCwd` (also called at the end of every Poll): does nothing if `Term == nil`, `!ShellReady` or `pendingShellCwd == ""`. If the target matches `ShellCwd`, only clears it. Otherwise `Term.ChangeDir(target)` → `ShellReady=false`.
  3. `Session.ChangeDir` writes `cwd\0\0cd\0` to `panel-command` and sends `directoryKey`. zsh: the widget does `cd`, `_farshmac_precmd`, `zle reset-prompt`; the line being edited is preserved, no history entry is added. bash/sh: the macro directoryKey = `\C-x\C-g` (cd, `_farshmac_cd_status`) + accepting an empty line to recompute PS1 + restoring the line from the kill ring.
  4. If the shell is busy (a program is reading stdin), the request stays in `pendingShellCwd` and is sent on the next Ready (test `TestPanelDirectorySyncWaitsForShell`: the keys do not end up in `read`).
  5. `pendingShellCwd` is overwritten by the latest request; on a shell restart it is used as the starting cwd.
- **Code:** `app.go` `showTerminal`, `syncShellCwd`; `session.go` `ChangeDir`.

#### Directory sync shell → panel (`returnFromTerminal`, `finishPanelReturn`, `syncCwd`)
- **Behavior:**
  1. On ⌘O from the terminal to the panels (without a document): if `ShellReady`, `syncCwd()`: for a local active panel with `ShellCwd != ""` and `pendingShellCwd == ""` → `Panels[Active].Load(ShellCwd)`.
  2. If the shell is not ready (a command is running, or `cd` was just typed and Ready is still on its way): `shellPanelReturn{Active, Path}` is remembered (only for a local panel and with an empty `pendingShellCwd`).
  3. On the next Ready, `finishPanelReturn`: if the terminal is still hidden, the active panel is the same and its path has not been changed by the user, the panel is local and `ShellCwd != ""` → `Load(ShellCwd)`. If the user has meanwhile moved to another directory, their choice is not overwritten (`TestShellCDReturnsToActivePanelBeforePromptEvent`).
  4. The passive panel does not change. Remote panels are not synced in either direction.
- **Code:** `app.go` `returnFromTerminal`, `finishPanelReturn`, `syncCwd`.

#### Interrupting a command: ⌘. / Ctrl+C
- **Invocation:** ⌘. in terminal mode (renderer: `e.metaKey && e.key==='.'` → `{Type:'input',Text:'\x03'}`); Ctrl+C goes to xterm as ordinary input.
- **Behavior:** the byte 0x03 is written to the PTY (SIGINT via the terminal driver). Does not terminate the shell.
- **Code:** `app.js` `attachCustomKeyEventHandler`.

#### Restarting the shell after exit (`scheduleShellRestart`, `restartShellIfDue`)
- **Invocation:** the `Exit` event from the session (`exit`, Ctrl+D/EOF, a crash, `exec` of another program and its termination).
- **Behavior:**
  1. `scheduleShellRestart(err)`: the cwd for the restart = `ShellCwd`, overridden by `pendingShellCwd`, otherwise the active panel path. `Term.Close()` (SIGHUP to the process group, closing the PTY, removing the tempdir), `Term=nil`, `ShellReady=false`, `shellStarted=false`, `shellCommands=nil`. If `closed` or `Quit`, return (no restart during shutdown).
  2. `shellRestartAt = now + 1s` (protection against an endless spawn loop with a broken rc). The status `Shell exited; restarting…` or `Shell stopped: <err>; restarting…`.
  3. A reset sequence is appended to the terminal stream: `ESC[?1049l ESC[0m ESC[?25h ESC[?1000l ESC[?1002l ESC[?1003l ESC[?1006l ESC[?2004l \r\n[Restarting shell]\r\n` (leaving the alternate screen, resetting attributes, showing the cursor, disabling mouse tracking and bracketed paste; the scrollback is kept).
  4. `restartShellIfDue` (every Poll): when the time comes, if the cwd has been deleted, the nearest existing parent is taken; if there is none, `$HOME`. `startTerminalAt(cwd)` with this window's current `Settings.Shell`/`ShellCustomPrompt`. An error → `shellRestartAt = now + 3s`, the status `Cannot restart shell: <err>; retrying…`; success → `Shell restarting…`.
  5. The terminal visibility does not change. A nested shell (`/bin/sh` → `exit`) does not trigger a restart (Exit only when the parent's PTY is closed).
- **Code:** `internal/ui/shell_restart.go`, tests `shell_restart_test.go`.

#### Terminal start at launch/new window
- **Behavior:** `native.Run`, after `EnableSession`, calls `a.StartTerminal()` for the first window if `Term == nil`; `EnableSession`/`NewWindow` start the shell for the other windows when `g.StartShell`. An error → the status `Shell: <err>`. The starting cwd is the active panel's `filesystemPath()`. PTY size `Cols × Rows-1` (Cols 60–300, Rows 15–120).
- **Code:** `native_darwin.go` `Run`, `windows.go`, `session.go`, `app.go` `StartTerminal`.

#### Terminal resize
- **Behavior:** the renderer measures the cell (`measureCells` on the panel's `.xterm-screen`), computes `cols = clamp(60..300, floor((innerWidth-8)/cellW))`, `rows = clamp(15..120, floor((innerHeight-8)/cellH))`; the `resize` event is coalesced by a 60 ms timer and sent only on change. Go: a `resize` with the same result is ignored before any other handling; otherwise `Cols/Rows` are updated and `Term.Resize(Cols, Rows-1)` (`pty.Setsize`). The renderer resizes the panels to `cols×rows` and the terminal to `cols×(rows-1)` via `archive.resize` (capturing lines before and after). On a font change, re-measurement after 100 ms.
- **Code:** `app.js` `dimensions`, `farshmacUpdate`; `app.go` `Handle` `resize`; `history.js` `resize`; test `resize.cjs`.

---

## 3. Terminal (renderer, xterm.js)

#### Emulation, colors, mouse
- **Behavior:** xterm.js 5.5.0 + addon-unicode11 0.8.0 (vendored, offline, CSP `default-src 'none'`). Terminal options: `fontFamily 'Menlo, Monaco, monospace'`, `fontSize 14` (from settings), `lineHeight 1.15`, `cursorBlink`, `scrollback 4096`, `macOptionIsMeta: true` (Option+key → ESC prefix), `macOptionClickForcesSelection: true` (Option+drag selects even with mouse tracking), `rightClickSelectsWord: true`, the theme `FarshMacTerminalTheme` (black background `#000000`, text `#c0c0c0`, cursor `#c0c0c0`, selection `#465b73`, the standard 16-color xterm palette). Supported: 16/256/RGB colors, SGR, DSR, alternate screen, bracketed paste, mouse reports (SGR and legacy via `inputBinary`), Unicode 11 widths (checked by `terminal.cjs`).
- **Focus:** on entering terminal mode `term.focus()`, on leaving `document.activeElement.blur()`. History overlays are closed.
- **Code:** `app.js`, `history.js` (theme), `index.html`.

#### Keyboard in the terminal (what the renderer intercepts)
- **Behavior (`term.attachCustomKeyEventHandler`, keydown only):**
  - ⌘O / Ctrl+O → `key` to Go (switching).
  - ⌘. → `\x03`.
  - ⌘C or Ctrl+Shift+C → `copyText()`.
  - ⌘V, Ctrl+Shift+V, Shift+Insert → `pasteRequest` (the native layer reads `NSPasteboard` and calls `window.farshmacPaste`).
  - ⌘A → `term.selectAll()`.
  - Any other combination with ⌘ is **not** passed to xterm (`return !e.metaKey`); it goes to the native key equivalents (menu: ⌘N, ⌘W, ⌘Q, ⌘⇧H, ⌃⌘F, ⌘M, ⌘`, ⌘⇧`).
  - All other keys (including Fn/F1–F12, Option as Meta, Escape, Ctrl+*) are handled by xterm and sent to the PTY.
  - Document-level capture listener: ⌘⇧H in terminal mode → `toggleCommands()`; with a visible history overlay, its own keys (see below); otherwise in terminal mode the listener does nothing (`if(mode==='terminal')return;`).
  - Natively: `performKeyEquivalent` in `terminalMode` on ⌘⇧H (keyCode 4 or the character `h`) calls `terminalHistory:` directly. Option translation (`handlePanelOptionKey`) is not applied in the terminal (`panelKeys` false).
- **Code:** `app.js`, `window_darwin.m`.

#### Mouse in the terminal
- **Behavior:** xterm handles selection and mouse tracking. Context menu (`contextmenu`): in terminal mode, if the application has enabled mouse tracking (`term.modes.mouseTrackingMode !== 'none'`) and Shift/Alt are not held, the menu is not shown (the event goes to the application). Otherwise its own `.clipboard-menu` is shown with the buttons `Copy`, `Paste`, `Select all` (the position is clamped to the window: `min(clientX, innerWidth-130)`, `min(clientY, innerHeight-100)`); closed by a click outside. In panel mode without a document the context menu is not shown (the right button marks files).
- **Wheel up** in the terminal: if the buffer is normal, the viewport is at the very top (`viewportY===0`), the archive has rows older than the resident scrollback (`history.total > baseY`) and (no mouse tracking or Shift/Alt held) → the output history overlay opens at the position `max(first, total - baseY - rows)`.
- **Code:** `app.js` (`contextmenu`, `termEl wheel`).

#### Copy / paste / select all in the terminal
- **⌘C / Ctrl+Shift+C / Edit → Copy / context menu Copy:** if the commands overlay is open, the selection in the textarea is copied; if the history overlay is open, the selected rows of the page (`\n` line breaks between rows, except soft-wrapped ones; `​` is removed); otherwise, if `term.hasSelection()`, `term.getSelection()`. The text → `{Type:'clipboard'}` → `NSPasteboard`. Without a selection nothing is copied.
- **⌘V / Ctrl+Shift+V / Shift+Insert / Edit → Paste:** `pasteRequest` → native reads the string from the pasteboard → `window.farshmacPaste(text)`: in the commands overlay, insertion into the textarea; in the terminal, closes the history overlay, `term.paste(data)` (with bracketed paste under `?2004h`, `\n`→`\r`), `term.focus()`. Go limit 1 MiB.
- **⌘A / Edit → Select All:** in the commands overlay, select the textarea; in the history overlay, select the page; in the terminal, `term.selectAll()` (the whole resident buffer, up to 4096 rows + the screen).
- **⌘X / Edit → Cut:** in the commands overlay, cut from the textarea; in the terminal, nothing; in the panels, `cut`.
- **Code:** `app.js` `copyText`, `farshmacPaste`, `farshmacSelectAll`, `farshmacCut`; `commands.js`; `history_view.js` `copy`, `selectAll`; `window_darwin.m` `pasteText:`, `copyText:` and the `clipboard` handler.

#### Scrollback and spooling output history to disk
- **Behavior (renderer, `TerminalArchive`):** rows of the normal buffer that scroll above the top of the screen (between the marker and `baseY`) are serialized (`{c: cols, w: isWrapped, r: [[text, fgRGB, bgRGB, flags], …]}`; flags: 1 bold, 2 dim, 4 italic, 8 underline, 16 blink, 32 inverse, 64 invisible, 128 strikethrough; trailing spaces without background/flags are trimmed). Capture happens after every write of a chunk ≤ `min(4094, scrollback-2)` bytes, before `ESC[3J`, `ESC c` and switching to the alternate screen (`?1049h/?1047h/?47h`), and before/after a resize. The alternate screen is not archived. Window growth does not duplicate rows already archived. The archive is disabled on `state.history.error`.
- **Transfer:** after processing a frame the renderer sends `{Type:'historyAppend', History: lines}`; Go (`appendTerminalHistory`) queues the batch: at most 8 batches and at most `MaxHistoryBatch = 8 MiB` in the queue, otherwise `historyStatus.Error = "History writer is overloaded; live output continues"`. Writing happens in a goroutine one batch at a time (`startHistoryWrite`).
- **Backpressure:** `terminalCanDrain()` = a history error already exists OR (`len(queue) < 2` and `queuedBytes < 4 MiB`). While false, PTY events are not read and `Update.Terminal` is not sent; a terminal frame is ≤ 16 KiB of base64 data per Poll; the `pending` buffer is limited to 256 KiB per read cycle. The renderer acknowledges the frame with `frameDone{More}` only after xterm parsing and sending the batch; native does not call the next `goPoll` while `evaluating`; with `More` the tick is invoked immediately.
- **Storage (`terminal.History`):** a temp directory `farshmac-history-*` (0700), segments `<total>.rows`/`.idx` (0600), a segment ≈ `HistorySegmentBytes = 16 MiB`, a total budget `HistoryDiskBytes = 1 GiB` per window; old segments are deleted, `First` grows ("oldest rows expired"). Batch limits: a row ≤ 256 KiB, a batch ≤ 8 MiB and ≤ 20000 rows, valid JSON, otherwise `terminal history batch exceeds limits or is invalid` and the history enters the error state (previously written pages remain readable). Only segment metadata is kept in RAM (test: 200,000 rows, heap growth < 4 MiB).
- **Reading:** `Page(start, count)`: `count` is limited to `HistoryPageLines = 256`; at most one request in flight at a time (`historyRead`).
- **Window close:** `App.Close` closes the history in a goroutine and removes the directory; `WindowGroup.CloseAll` waits for all cleanups to finish. The history is not restored between launches.
- **Code:** `internal/native/assets/history.js`, `internal/ui/terminal_history.go`, `internal/terminal/history.go`, tests `terminal.cjs`, `throughput.cjs`, `terminal_throughput_test.go`, `history_test.go`.

#### Output history overlay (Window → Terminal Output History)
- **Invocation:** the menu Window → `Terminal Output History` (no shortcut; enabled only in terminal mode, `validateMenuItem`); wheel up from the top of the scrollback (see above). There is no separate keyboard shortcut (⌘⇧? is not bound).
- **Behavior:** `document.body` gets the class `archive`: the terminal is hidden, `#history` is shown with a toolbar: the buttons `↑ Older`, `↓ Newer`, `Live · Esc` and a caption. Opens at `max(first, total - rows)` (the last page), `rows = max(1, term.rows-2)`. The request `{Type:'historyRead', Start, Count: rows, Request}`; Go replies with `historyPage` (in `Update`), the renderer applies only the page with a matching `request`. Rows are rendered as spans with colors/attributes (inverse swaps fg/bg, dim is opacity .5, invisible is hidden). An empty row is `​`.
- **Caption:** `History <start+1>–<end> / <total>` + ` · oldest rows expired` if `first > 0`; `History is empty`; or the page error text.
- **Navigation:** ArrowUp/ArrowDown ±1, PageUp/PageDown ±rows, Home is the oldest page, End/Escape closes (`Live`), the wheel is ±1..12 rows (by `deltaY/20`). If the reply is not for the desired position, a repeated request. ⌘O closes the overlay and switches the mode. ⌘C/⌘A/⌘V copy/select the page/paste (paste closes the overlay and goes to the terminal).
- **Special cases:** history search is not implemented. Selection/copying are limited to one page. On a mode switch the overlay closes.
- **Code:** `history_view.js`, `app.js` `toggleHistory`, `window_darwin.m` `terminalOutputHistory:`, `terminal_history.go` `readTerminalHistory`.

#### Shell command history overlay (⌘⇧H / Window → Command History)
- **Invocation:** ⌘⇧H (the renderer capture listener and the native `performKeyEquivalent` in terminalMode); the menu Window → `Command History` `⇧⌘H` (enabled only in terminal mode). Pressing again closes it.
- **Behavior:**
  1. The `#commands` dialog opens (caption `Command history`, a `<select size=12>` list, the field `Edit command (Shift+Enter: new line)`, a 3-row textarea, the buttons `Run · Enter`, `Cancel · Esc`). The output history overlay closes.
  2. The caption `Loading command history…`; if the shell is not ready, `Waiting for shell prompt… · Esc: cancel`; the request `{Type:'commandHistory'}` is sent as soon as `shellReady` (once per opening).
  3. Go: `Term.RequestCommands(ShellCwd)`, the `history` mode via `historyKey`: zsh writes up to the 500 most recent `$history` entries (newest first) NUL-separated into `command-history`; bash/sh: `history 500` with `HISTTIMEFORMAT='FARSHMAC_HISTORY_ENTRY:'`. The line being edited is preserved, the request itself does not enter the history. `ShellReady=false` until the next Ready.
  4. On Ready, `readCommands()`: the file is read (limit 4 MiB + 1; when exceeded the last entry is dropped), parsed, the bash order is reversed (newest first), empty entries and duplicates are removed; the file is deleted. The result → `Update.commands` (once).
  5. Renderer: the list (multi-line commands are shown with ` ↵ `), the first is selected, its text in the textarea; the caption `Command history — Enter: run · F4: edit · Esc: cancel` or `Command history is empty`. The Run button is disabled while `!shellReady`.
  6. Keys: Enter (without Shift) / double click / the button → `run`: if the shell is ready and the text is non-empty → `{Type:'commandHistoryRun', Text}` and close; F4 focuses the textarea; Escape closes; ⌘C/⌘X/⌘V/⌘A are clipboard operations in the textarea. Other keys go to the select/textarea (arrows select, `onchange` updates the textarea).
  7. Go `commandHistoryRun`: `Term.Run(ShellCwd, text)` when `ShellReady` and the text is non-empty → the command is executed in the shell's current cwd and enters the shell history (not the panels' `a.history`).
- **Code:** `commands.js`, `app.js` `toggleCommands`, `app.go` `Handle` (`commandHistory`, `commandHistoryRun`), `internal/terminal/commands.go`, the rc scripts in `session.go`/`bourne.go`, test `commands.cjs`, `commands_test.go`.

#### "commands" in Update
- `Update.Commands *[]string` is a snapshot of the shell history for the overlay; sent once after Ready with `Commands != nil`. This is not command autocompletion: completion from the shell is not implemented.

#### Font size
- **Invocation:** F9 → Options → `General settings…` (`Font size (10–24)`) or `Interface settings` (`Font size (10–24)`, `Key bar`). There are no keyboard ⌘= / ⌘− / ⌘0.
- **Behavior:** `Settings.FontSize` (10–24, default 14; the error `Font size must be 10–24`) → `Update.fontSize` → the renderer changes `fontSize` of both xterms and `#history-lines`, after 100 ms re-measures the cells and resends `resize`. Saved via Save setup.
- **Code:** `settings.go`, `app.js`.

#### Full screen mode and Zoom
- **Invocation:** Window → `Enter Full Screen` / `Exit Full Screen` (`⌃⌘F`, the title changes with the window state); F9 → Commands → `Full screen` `⌃⌘F` (sets `a.Fullscreen = true` → `Update.fullscreen` → `[window toggleFullScreen:]`). Alt+F9 → `a.Zoom = true` → `[window zoom:]` (Cocoa window zoom, not the font).
- **Special cases:** during fullscreen the window frame is not saved to the session (`captureFrame` skips under `NSWindowStyleMaskFullScreen` and during the transition).
- **Code:** `window_darwin.m` (`toggleFullScreenActive:`, `validateMenuItem`, `tick:`), `menu.go`, `far_keys.go`.

---

## 4. Windows, session, quitting

#### New window (⌘N)
- **Invocation:** ⌘N (in panel and terminal mode, if not `Busy`), File → `New Window` `⌘N`, Commands → `New window` `⌘N`; a Dock click with no visible windows (`applicationShouldHandleReopen` → `newWindowFrom:0`).
- **Behavior:** `a.NewWindow=true` → `Update.newWindow` → native `newWindowFrom:session` → `goNewWindow(parent)` → `WindowGroup.NewWindow`: the refusal `finish quitting before opening a new window` during quit; the left/right folders are inherited from the parent (`filesystemPath()`), otherwise `$HOME`; settings are copied via JSON (independent copies), `panelOptions` of both panels are copied, `refresh()`; without a parent, `LoadSetup()`. Starts its own shell (`StartTerminal`); PTYs are independent (`TestIndependentPTYWindows`). The window cascades from the point `(100+24*session, 850-24*session)`, size 1120×740, minimum content 700×400, title `FarshMac — <session>`; after the first Poll, `FarshMac — <session> — <active panel path | document name>`.
- **Code:** `windows.go` `NewWindow`, `window_darwin.m` `newWindowFrom:`, `open`.

#### Switching windows
- Window → `Next Window` `⌘``, `Previous Window` `⇧⌘``, `Minimize` `⌘M`, `Bring All to Front`. Windows cycle through the list of controllers.

#### Saving and restoring the session
- **File:** `<UserConfigDir>/FarshMac/session.json` (0600 via temp+rename), a read fallback from `<UserConfigDir>/Far Go/session.json` if the new one does not exist. Format: `{"Version":1,"Windows":[{"Frame":{X,Y,Width,Height},"Panels":[{"Path","Options":panelOptions,"Cursor","Top"},…],"Active":0|1}],"Focused":N}`.
- **When it is saved:** every `WindowGroup.Poll` (no more often than every 500 ms and only when the bytes change; failed writes are throttled too), on window close (force), at the start of quitting (`advanceQuit` → `SaveSession(true)`), `finishQuit` → `goSaveSession`, `CloseAll` (if not yet Terminating). With zero windows the last closed one is saved (`lastWindow`). Window frames come from Cocoa (`windowDidMove/Resize/EndLiveResize`), coordinates in points; `Valid()` requires 100 ≤ W,H ≤ 20000, |X|,|Y| < 100000, no NaN/Inf.
- **Restoring (`EnableSession(path, restore)`):** at startup, if `-left`/`-right` were not passed (`flag.Visit` → `restore=false`). Errors: `cannot read window session: …`, `unsupported window session version N`, `too many saved windows` (>100) go to `a.Status`, and the application starts with defaults. For each window: folders are checked by `accessibleFolder` (climbing to the nearest readable parent, then a fallback to the first window's folder, `$HOME`, `/`); `panelOptions` are applied; the cursor/top are restored by names (`pendingPosition` for async loading); `Active` is clamped to 0..1; an invalid frame is ignored (the window is centered/cascaded); focus is `Focused`. On restore the native layer fits the frame to the screen with the largest intersection: width ≥700, height ≥428, no larger than `visibleFrame`, origin inside the screen.
- **Special cases:** restoring does not depend on `AutoSave`; an archive panel saves `Cursor = archive name`; the command, the terminal history and the shell contents are not saved.
- **Code:** `internal/ui/session.go`, `windows.go`, `native_darwin.go`, `window_darwin.m` `open`, `cmd/farshmac/main.go`, tests `session_test.go`.

#### Closing a window (⌘W / the red button)
- **Invocation:** File → `Close Window` `⌘W`; `windowShouldClose` → `{Type:"close"}` (the window does not close by itself).
- **Behavior:** `WindowGroup.Handle` → `a.RequestQuit()` for this window (the same confirmation sequence as on quit, see below); on `a.Quit` → `Update.quit` → native `closeWindow:` → `goCloseWindow` → `CloseWindow`: a snapshot of the window into `lastWindow`, `App.Close()` (cancelling tasks, closing documents/temp, `saveSetup` if `AutoSave`, `Term.Close()`), removal from the group, the session is saved. The other windows and their shells keep working. The application does not terminate after the last window closes (`applicationShouldTerminateAfterLastWindowClosed → NO`).
- **Code:** `windows.go` `Handle`, `CloseWindow`; `window_darwin.m`.

#### Quitting the application (⌘Q): the group sequence
- **Invocation:** ⌘Q (intercepted by `WindowGroup.Handle` on the `key` Meta+q, or the native menu `Quit FarshMac` ⌘Q → `goQuitAll`), Commands → `Quit` `⌘Q`, `applicationShouldTerminate` (Dock/system) → `requestQuit:` (returns `NSTerminateCancel`, termination proceeds through the group). F10 does not close the application.
- **Behavior (`BeginQuit`/`advanceQuit`):**
  1. The windows are walked in `Order`. For each, `a.RequestQuit()`:
     - if `a.Busy` → the status `Wait for the file operation to finish`, the quit is aborted;
     - if there are unsaved documents → the terminal is hidden and the confirmation `Save changes` / `Save changes in open documents before quitting?` is shown with the buttons `Save all`, `Discard`, `Cancel`; `Save all` saves everything and proceeds to the shell check; `Discard` goes straight to the shell; `Cancel` cancels the whole sequence (windows approved earlier stay open);
     - `confirmQuitShell`: if `Term != nil && !ShellReady` (a command is running or the shell is still starting) → the terminal is hidden, the dialog `Quit` / `A command is running. Close the application?` with the buttons `Yes`, `No`; `Yes` → `a.Quit=true`;
     - otherwise `a.Quit=true` immediately.
  2. While a window waits for an answer, `Update.attentionWindow = id` makes native bring that window to the front; input to other windows is rejected with the status `Finish the quit dialog in the other window` (except `resize`).
  3. When all windows have approved: `Terminating=true`, `SaveSession(true)` (an error goes to the status of all windows), `Update.quitApplication` → native `finishQuit`: capturing frames, `goSaveSession`, closing all windows, `[NSApp stop]`; `native.Run` → `CloseAll` (waits for the history cleanups).
- **Special cases:** in a single window, ⌘W while a command is running shows the same text `Close the application?`. `NewWindow` is impossible during quit.
- **Code:** `windows.go` `BeginQuit`, `advanceQuit`, `Handle`, `Poll`; `app.go` `RequestQuit`, `confirmQuitShell`; `window_darwin.m` `requestQuit:`, `finishQuit`; test `TestIndependentWindowsAndQuitTransaction`.

#### Full Disk Access
- **Startup dialog (`native.PrepareDiskAccess` → `farshmac_prepare_disk_access`):** shown once (the `NSUserDefaults` flag `FarshMacDiskAccessIntroductionShown`), only if the bundle id is `local.farshmac.app` and the launch is from the `.app` (CLI/`go run`/tests skip it). NSAlert: title `Full Disk Access for FarshMac`, text: `To browse protected folders without separate folder prompts, enable FarshMac in System Settings → Privacy & Security → Full Disk Access.\n\nIf it is missing, use '+' or drag the app selected in Finder:\n<bundlePath>\n\nOpen Settings closes this launch before folders are loaded. Enable access, then reopen FarshMac. If already enabled, choose Continue. You can also continue with limited access and use FarshMac → Full Disk Access later.` Buttons: `Open Settings` (first), `Continue` (Esc). The flag is set regardless of the choice. `Open Settings` → `farshmac_open_disk_access()`; on success the process exits (`main` returns before the `App` is created), and the user relaunches manually; there is no automatic relaunch. The application neither checks nor grants the access.
- **`farshmac_open_disk_access`:** selects the bundle in Finder (`activateFileViewerSelectingURLs`), opens `x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles`, fallback is System Settings; if that fails too, the NSAlert `Open Full Disk Access in System Settings` / `Open Privacy & Security → Full Disk Access, add this copy of FarshMac and enable it. Then quit and reopen FarshMac.` / `OK`.
- **Later:** the application menu `FarshMac → Full Disk Access…` (native, immediate) and F9 → Options → `Full Disk Access…` (sets `DiskAccessSettings=true`, the status `Enable this copy of FarshMac in Full Disk Access (+ or drag from Finder), then quit and reopen.`).
- **Documentation:** PUBLISHING.md: the bundle id must be stable; after a signature change (ad-hoc → self-signed) add the copy to FDA again; TCC is not checked by code.
- **Code:** `internal/native/disk_access_darwin.m`, `native_darwin.go`, `cmd/farshmac/main.go`, `menu.go`, `window_darwin.m` `diskAccessSettings:`.

---

## 5. Native macOS menu (window_darwin.m `startWindows`)

| Menu | Item | Shortcut | Action |
| --- | --- | --- | --- |
| FarshMac | `About FarshMac` | — | the standard About panel |
| FarshMac | `Full Disk Access…` | — | `farshmac_open_disk_access()` |
| FarshMac | `Quit FarshMac` | ⌘Q | `requestQuit:` → `goQuitAll` (group quit); with no windows, `finishQuit` |
| File | `New Window` | ⌘N | `newWindowFrom:` of the active window |
| File | `Close Window` | ⌘W | sends `{Type:"close"}` to the active window |
| Edit | `Copy` | ⌘C | `window.farshmacCopy()` |
| Edit | `Cut` | ⌘X | `window.farshmacCut()` |
| Edit | `Paste` | ⌘V | reads `NSPasteboard` → `window.farshmacPaste(text)` |
| Edit | `Select All` | ⌘A | `window.farshmacSelectAll()` |
| Window | `Command History` | ⇧⌘H | `window.farshmacToggleHistory()`; enabled only in terminal mode |
| Window | `Terminal Output History` | — | `window.farshmacToggleOutputHistory()`; only in terminal mode |
| Window | `Enter Full Screen` / `Exit Full Screen` | ⌃⌘F | `toggleFullScreen:` of the active window; title follows the state |
| Window | `Minimize` | ⌘M | `performMiniaturize:` |
| Window | `Next Window` | ⌘` | cycle through windows +1 |
| Window | `Previous Window` | ⇧⌘` | cycle −1 |
| Window | `Bring All to Front` | — | `arrangeInFront:` |

The "active window" is `NSApp.keyWindow`, otherwise the last one. The application icon is `FarshMac.icns` from the bundle. Window: background `#000080`, `NSWindowStyleMaskTitled|Closable|Resizable|Miniaturizable`, a WKWebView with `nonPersistentDataStore`, navigation allowed only to `index.html`; on a web-process crash, `reload`. A 30 Hz timer (`NSRunLoopCommonModes`) calls `goPoll` on the serial `engineQueue`, the result is `window.farshmacUpdate(json)`.

---

## 6. Keyboard: native and renderer level (panel mode)

#### Option+letter (dead keys), Numpad, ⌘⇧.
- **Behavior (native, `panelKeys` only):**
  - A local event monitor (`NSEventMaskKeyDown`) ahead of AppKit and input methods: `FarshMacPanelOptionMessage`: for a keyDown with Option and without ⌘/Ctrl the character is taken from `charactersByApplyingModifiers:(Shift|CapsLock)`, i.e. the base character of the current layout without Option accents/dead keys (Option+E → `e`, Option+- → `-`, Option+Shift+- → `_`). Control (<32, 127) and function (0xF700–0xF8FF) characters are ignored. `{Type:"key", Key, Code:(NumpadMultiply|NumpadAdd|NumpadSubtract|""), Alt:true, Shift}` is sent; the event does not reach WebKit (fast find via Alt+letter).
  - `performKeyEquivalent`: keyCode 46 (M) with ⌘/Ctrl → `Key:"m", Code:"KeyM"`; keyCode 67/69/78 (Numpad * + −) → `Key "*","+","-"` with the codes `NumpadMultiply/Add/Subtract` and flags; keyCode 47 with ⌘⇧ (without Ctrl/Opt) → `Key ".", Code "Period", Meta, Shift` (show hidden). Everything else: Option translation, then `super`.
  - In terminalMode and under modal layers (`panelKeys=false`) Option translation is not performed: xterm receives Option as Meta, the editor receives ordinary characters.
- **Renderer (document keydown, capture) in panel mode:** ignores `isComposing`; Shift+Insert → `pasteRequest`; ⌘C/⌘V/⌘X are passed through to the native menu; everything else is `preventDefault` + `{Type:'key', Key:e.key, Code:e.code, Ctrl, Alt, Meta, Shift}`. Fn/F1–F12 arrive as `Key:"F1"…` (on keyboards with media keys, hold Fn, see README). Escape is an ordinary key. Go in `key()`: Alt+Key* with an open form/dialog/Busy is mapped from `Code` (`KeyC` → `c`) for Alt accelerators.
- **Test:** `panel_keys.m` (clang): Option+a/e/./-/Shift+-, refusal under `panelKeys=NO` and with ⌘; `input.cjs`: `ç`/KeyC with Alt.
- **Code:** `panel_keys_darwin.h`, `window_darwin.m`, `app.js`.

#### Mouse in the panels (renderer → Go)
- **Behavior:** `pointerdown` (buttons 0 and 2): `preventDefault`, clearing the xterm selection, `{Type:'click', X, Y (in cells), Clicks:1, Button, Buttons, Shift, Alt, Meta, Ctrl}`; **Control+left = right button (`Button:2`)** marks the file as in Far. In a document, right/Ctrl-click is ignored. `dblclick` (left without Ctrl) → `Clicks:2`; a triple click in a document → `Clicks:3`. Drag: `pointermove` with buttons held is coalesced by a 25 ms timer into `{Type:'drag'}`; auto-repeat of 60 ms at the edges (rows < 3 / ≥ rows-6 for panels, < 1 / ≥ rows-1 for a document). `pointerup`/`pointercancel`/`blur` → `mouseUp` (handled while Busy). Wheel → `{Type:'wheel', Delta: sign·clamp(1..12, ceil(|deltaY|/20))}`. Pointer capture on `#panels`. Panel text is `user-select:none`.
- **Go `click`:** clicks on the menu bar (Y=0) open sections; with `TermVisible` clicks are ignored; a click on the key bar (the last row, if `ShowKeys`) → `function(min(10, X*10/Cols+1))`.
- **File drag&drop (from Finder/to Finder):** not implemented; "drag" is only mouse selection inside the panels/document.
- **Code:** `app.js` (`pointer`, `flushDrag`), `app.go` `click`, `mouse.go`; test `input.cjs`.

---

## 7. F1 help (the terminal fragment, verbatim)
`Type a shell command directly while panels are visible, then press Enter. The panels disappear and the SAME window becomes an interactive terminal. Commands run in the active panel's directory. zsh keeps its environment, history, aliases and jobs. Your .zshrc is loaded at shell startup. The output stays visible after completion; ⌘O restores the panels. ⌘O also works during a command; returning does not stop the job. Use Cmd+. (or Ctrl+C) to interrupt, Cmd+C / Cmd+V to copy / paste. Scroll the terminal to review output. vim/less use the alternate screen.`

---

## Discrepancies between documentation/help and code

> **Closed on the `backlog-fixes` branch:** the decisions on each item are in the "Decisions on discrepancies" section of `docs/IMPLEMENTATION_BACKLOG.md`.

1. **F1 help** (`app.go`, `help`): the heading `persistent zsh terminal`, "zsh keeps its environment…", "Your .zshrc is loaded at shell startup", while the default shell is `bash` (`defaultSettings`, `TestDefaultShellIsBash`); zsh only when selected in Terminal settings.
2. **F1 help**: `Ctrl+E/X/Y`: in the code Ctrl+Y and ⌘Y clear the line, but Ctrl+E/X work only with Ctrl (not ⌘); additionally the undocumented Ctrl+↑/Ctrl+↓ and ⌘↑/⌘↓ work (the latter are in the help).
3. **F1 help**: `Ctrl+F / Ctrl+;`: in the code also ⌘F / ⌘; (any `cmd`). `Ctrl+Insert … Alt+Shift+Insert full paths`: ⌘Insert works too; Ctrl+Alt+Insert (paths with symlink resolution / the remote server label) is undocumented. Ctrl+[ / Ctrl+] (left/right panel paths, with Shift active/inactive) and Ctrl+Shift+Enter (the name from the inactive panel) are missing from the help.
4. **F1 help** mentions `F10 closes only viewer/editor`; in panel mode `function(10)` does nothing (comment: exit only via ⌘Q); closing a document with F10 is in `document_keys.go` (outside this area).
5. **TERMINAL_PARITY.md**: "xterm parses 1024-byte chunks before collecting history"; in `history.js` the chunk size is `min(4094, scrollback-2)` = 4094 bytes.
6. **TERMINAL_PARITY.md**: "⌘⇧H Command history (output archive in the original 0.13 implementation)" and the navigation table "in the archive": current state: ⌘⇧H = command history, the output archive is only via the Window menu without a shortcut. The document itself notes this in its preamble.
7. **Terminal settings**: the labels `Shell: … (new windows)` and `Use custom shell prompt / hooks (new windows…)`, the status `Shell applies to new windows`; but `restartShellIfDue` uses the current `Settings.Shell`/`ShellCustomPrompt` of the same window, i.e. after `exit` in the current window the new shell already starts with the changed settings.
8. **Window close dialog**: ⌘W / the close button while a command is running shows `A command is running. Close the application?`; only the window closes, the others keep working.
9. **The task mentions ⌘= / ⌘− / ⌘0 (font zoom) and ⌘⇧? (output history)**: there are no such bindings in the code: the font size only via settings (10–24), the output history only via the Window menu / wheel up from the scrollback edge.
10. **README "Keyboard essentials"**: `Cmd+Shift+H Shell command history: Enter to run, F4 to edit` is correct, but the item is available only in terminal mode (the menu is disabled in the panels); README does not say so.
11. **CLAUDE.md**: "Session wraps a PTY running bash/sh/zsh" and "Directory sync … only when the shell is at a prompt" match the code. CLAUDE.md describes the `Message` types "key, click, drag, wheel, resize, input, historyRead"; the actual set is wider (`mouseUp`, `terminalError`, `commandHistory`, `commandHistoryRun`, `historyAppend`, `inputBinary`, `copy`, `cut`, `selectAll`, `scroll`, `save`, `paste`, `quit`, `close`, `redraw`).
12. **F1 help**: "Shift+Insert Paste text": in panel mode it is implemented in the renderer (`pasteRequest`); Go has no separate Shift+Insert handling; it depends on the renderer, which is correct for the application but not for headless tests.
13. **TERMINAL_PARITY.md**: "Pages … the UI asks for at most the window height": `term.rows-2` rows (minimum 1) are requested, not the window height.


<a id="part-5"></a>
# Part 5. F9 menu, dialogs, settings, network connections, help

The source of truth is the code in `internal/ui` and `internal/remotefs` (version 0.20.0, the brand string `FarshMac 0.20.0` in `draw.go`). Interface strings are given verbatim in English. References "see the file operations / search / user menu section" refer to the specifications of other sections.

---

## 1. General mechanics of modal windows

The UI engine keeps exactly four kinds of modal state in `App` (`internal/ui/app.go`): `Menu *MenuState`, `Dialog *Dialog`, `Form *Form`, and the "busy" state `Busy` with a progress window (`Operation`). While a document, form, dialog, or menu is open, or a background operation is running, `Update.PanelKeys == false` and the renderer routes keys differently (`App.Poll`).

Key handling order in `App.key` (simplified): `Alt+letter` while a form/dialog/Busy is open is normalised to the Latin letter by `m.Code` → if `Busy`: `Escape`/`Alt+C` cancel the operation, otherwise keys go to the form (operation questions) → `⌘N` new window → `⌘Q` quit → `Ctrl+Tab`/`⌘⇧[ ]` screen switching → `⌘O`/`Ctrl+O` (only without a dialog/form) closes the menu and toggles the terminal → if the terminal is visible — return → `Form` → `Dialog` → `Doc` → … → `Menu`.

Drawing (`Draw`, `draw.go`): panels → status line → command line → key bar → document → fast find → menu → progress → Dialog → Form → application of the colour theme.

### Shared primitives (`internal/ui/modal.go`)

- `modalBounds(cols, rows, width, height)` — a centred window; width `min(width, max(12, cols-4))`, height `min(height, max(8, rows-2))`.
- `drawModal` — shadow (black fill offset by +2/+1), grey body (`Gray`), `Box` frame with an inset of 2 on X and 1 on Y, title centred in the frame, truncated with `tail(title, W-10)` and wrapped in spaces.
- `modalRule` — a horizontal separator `─` with `╟`/`╢` ends.
- `mnemonicKeys(labels)` — for each label the first not-yet-taken letter (lower case) becomes the accelerator; `modalLabel` draws that letter in yellow (`Yellow`).
- `modalInput` — input field: background `Cyan`, text `LightCyan`; when `selected` (everything selected) — `White` on `Blue`; the caret is the inverted cell (`White` on `Black`) only in the focused field; control characters are replaced with a space; a window of text around the caret is shown (`editWindow`).
- `buttonLayout(labels, width)` — buttons in a row with a step of 1, width `len+4`, wrapping to a new row when space runs out, each row is centred.
- `drawModalButtons` — the first button is drawn as `{ OK }`, the others as `[ Cancel ]`; the focused one has background `Cyan`, the others `Gray`; accelerators are highlighted.

### Dialog (single-line prompt, `App.prompt`, `app.go`)

#### Dialog (prompt with one field)
- **Invocation:** `a.prompt(title, msg, value, fn)` — used by: Make folder (F7), Change directory (⌘L), Select/Deselect group, File panel filter, Create archive, Extract archive, Tree settings, Command line settings, File descriptions, Sort groups, Folder description files, Describe files, Apply command, Save folder shortcut, Remote move / rename, Make remote folder, Move / rename (F6).
- **Behaviour:**
  - If `value == ""` and `RememberDialogValues` is enabled, the last value entered for this title is substituted (`a.dialogValues[title]`, in process memory only).
  - `Selected = Settings.SelectDialogInput` — with the setting enabled, the initial text is selected in full; the first printable character/Backspace replaces it.
  - Focus: `0` field, `1` OK, `2` Cancel. `Tab`/`Shift+Tab` cycle through the three; `ArrowLeft/Right/Home/End` in the field clear the selection (the cursor is always at the end of the line — Dialog has no separate cursor), on the buttons they switch OK↔Cancel.
  - `Enter`: closes; if focus is not on Cancel — calls `Confirm(Input)` (and remembers the value when `RememberDialogValues`). `Space` on a button = Enter. `Escape` — close without calling.
  - `Alt+O` → OK, `Alt+C` → Cancel. `⌘A` — select all. Backspace deletes the last character (or the whole selection). Printable characters (without ⌘/Ctrl) are appended at the end.
  - Paste (`paste`): only when focus is on the field; control characters → space, line breaks are removed.
  - Mouse (`promptClick`): a click on the field row sets focus 0; a click on a button activates it.
- **Drawing** (`drawDialog`): width `min(88, Cols-4)`, message up to 3 lines (wrapped at width `W-8`), input field, separator, `OK`/`Cancel` buttons (accelerators `o`/`c`).
- **Code:** `app.go` (`Dialog`, `prompt`, `key`), `draw.go` (`promptBounds`, `drawDialog`, `promptClick`).

### Form (multi-field form / choice list / confirmation, `internal/ui/form.go`)

A single `Form` structure serves three modes:
1. **Form with fields** (`a.form(title, fields, submit)`): `Fields []Field{Label, Value, Toggle, Checked, Secret}`, buttons `Primary`/`Cancel`.
2. **Choice list** (`a.choose(title, choices, select)`): `Choice` is non-empty, `Buttons=false`. If the list is empty, the form does not open; status `"<title>: empty"`.
3. **Confirmation with buttons** (`a.confirm(title, message, buttons, select)`): `Buttons=true`, `Choice` = the button labels, `Cancel` calls `select(len(buttons)-1)` (i.e. Esc = the last button). An extra field `Remember` — a `Remember choice` checkbox (used by operation questions).

#### Form with fields
- **Behaviour:**
  - Initial focus is field 0, cursor at the end of its value. `Selected` (select all) is enabled by the calling code (for example, the copy dialog sets `Form.Selected = Settings.SelectDialogInput`).
  - Navigation: `Tab`/`ArrowDown` → next element, `Shift+Tab`/`ArrowUp` → previous; elements = fields + the Primary button + the Cancel button (`count = len(Fields)+2`), navigation is cyclic. `ArrowLeft/Right` inside a text field move the cursor, not the focus; on a toggle field they flip the checkbox.
  - Text field: `Home/End`, `Backspace`, `Delete`, printable characters (without ⌘/Ctrl); `⌘A` selects all; with a selection, Backspace/Delete/a character clear the field. Paste `formPaste` — at the cursor position.
  - Toggle: `Space`, `ArrowLeft`, `ArrowRight` switch `[ ]`/`[x]`.
  - `Secret`: the value is shown as `•` per character (passwords). The test `TestRemoteUnknownHostConfirmationAndMasking` verifies that the password does not reach the grid.
  - `Enter`: closes the form; if focus is on Cancel (`len(Fields)+1`) — calls `Cancel` (if set), otherwise `Submit(Fields)` (including when focus is on any field). `Space` on a button = Enter.
  - `Escape`: closes, calls `Cancel` if set.
  - `Alt+letter` accelerators: mnemonics are computed as `mnemonicKeys(buttons ++ labels)` — i.e. first the button letters (`OK`/`Primary` and `Cancel`), then the field labels. `Alt+letter` of a field: focus on the field; a toggle is flipped, a text field gets the cursor at the end. `Alt+letter` of a button — a press.
  - A separator is drawn between the group of text fields and the group of toggle fields (`Rules`). With `CheckColumns == 2` the toggle fields are laid out in two columns with a vertical line `│` (`┬`/`┴`).
  - Geometry: width 88 (or less), height `rows+8`; a text field takes 2 rows (label with an appended `:` + the field), a toggle takes 1. Scrolling (`Scroll`) follows the focus; a `▲` indicator at the top right if the list is scrolled.
  - Buttons: `[Primary|OK]` and `Cancel` (`Form.buttons()`); a choice list has `Select`/`Cancel` without focus.
  - Mouse (`formClick`): a click on a button — Primary: focus on the button + Enter (submit), Cancel → Escape. A click on a field — focus, cursor at the end, a toggle is flipped.
- **Code:** `form.go` (`form`, `formKey`, `formPaste`, `formGeometry`, `drawForm`, `formClick`).

#### Choice list (`choose`)
- **Behaviour:** `ArrowUp/Down/Tab` move the focus; with `Settings.MenuWrap` — cyclically, otherwise clamped; `Home`/`End`; `Enter` → `Select(Focus)`; `Escape` closes (without a callback, `Cancel` is not set). Accelerators by the first letters of the items (`Alt+letter` sets the focus, does not select). A click on a row selects immediately. Visible rows = `rows+8` limited by the window height, with scrolling.
- **Used by:** Sort mode, Volumes and home, Folder shortcuts, Network connections, Advanced settings, Colors, Code pages, Groups of file masks, Command/Folder/View-edit history, Screens, Archive commands, User menu, Edit user menu, File associations, Find results, Folder tree.

#### Confirmation (`confirm`)
- **Behaviour:** the message is wrapped at width `W-8` (multi-line via `\n`); `PageUp/PageDown` scroll long text by 3 lines; buttons via `ArrowLeft/Right/Tab`, `Enter`/`Space`, `Escape` = the last button; `Alt+letter` presses a button; with `Remember` — a `Remember choice` checkbox (`Space`/`Enter` on it, `Alt+M`). A click on a button presses it; a click on the checkbox row toggles it.
- **Used by:** Save changes (quit), Quit (shell busy), Unknown SSH host key, Delete remote files permanently, Move to Trash / Delete permanently, Error (Make folder retry), `<label> failed` (background task errors with `reportError`), operation questions (overwrite/skip/retry; described in the file operations section).

### Progress window (`internal/ui/progress.go`)
- Shown while `a.Operation != nil` (Busy). Size 88×15. Title = the operation label; rows: the path of the current item, `Working / calculating… ▰▱▱▱  N items` (scanning/streaming) or `Total  xx.x%  n / m items` + bar, `File  ` + bar, with `Workers > 1` — `Parallel transfers: A active / W workers`, statistics `done / total · rate/s · Ns  ETA Ns` or `… processed · rate/s · Ns`. A `Cancel` button (accelerator `c`); `Escape`/`Alt+C` or a click on the button → `cancelOperation`.
- On completion (`pollTask`): `Status = ""`, an error → `a.error(err)` (into the status), and if the task has `reportError` and the error is not a cancellation — additionally `confirm("<Label> failed", err, ["OK"])`.

### Status line and key bar (`draw.go`, `Poll`)
- Row `Rows-3`: `a.Status`; if empty — the hint `Tab Switch panel   Space Select   ⌘O Terminal   ⌘⌥0…9 Bookmark`.
- Row `Rows-2`: `<path>>` + the command line; for a remote panel the path is shown as `local:<local panel path>` (commands run locally).
- Row `Rows-1` (if `ShowKeys`): 10 cells `1Help 2UserMenu 3View 4Edit 5Copy 6RenMov 7MkFold 8Trash 9Menu 10` (empty); for a remote panel 8 → `Delete`. A click on a cell calls `function(n)`.
- In terminal mode the `Footer` is replaced: `⌘O  Panels    ·    Shell ready    ·    <cwd>` / `⌘O  Panels    ·    Starting <shell> (loading shell configuration)…    ·    ⌘. Interrupt` / `⌘O  Panels    ·    ⌘.  Interrupt` / on restart `⌘O  Panels    ·    <Status>`.
- Clearing the status: `Escape` in the panels (if there are no active reads: `Command = ""; Status = ""`), completion of a background task (`task.go`), and every shell readiness event overwrites the status with `Exit N · ⌘O panels / terminal`. Other messages live until the next overwrite.
- `a.error(err)` — if `err != nil`, puts `err.Error()` into the status and marks `dirty`. There is no separate error window (except for `reportError` tasks and `Error` on folder creation).

---

## 2. The F9 menu

### Opening and navigation

#### Main menu F9
- **Invocation:** `F9`, `⌘9` (`function(9)`); `Shift+F10` — open the menu on the last executed item (`a.lastMenu`, remembered on `Enter`); mouse — a click on a section name in row 0.
- **Where available:** panels (not the terminal, not a document, not a form/dialog, not Busy). With `Active == 0` the `Left` section opens (column 0), with `Active == 1` — `Right` (column 4).
- **Behaviour (`menuKey`):**
  - `Escape`, `F9`, `⌘9` — close.
  - `ArrowLeft`/`ArrowRight` — the adjacent section, wrapping around, row 0.
  - `Tab` — from `Left` to `Right`, from `Right` to `Left`, from any other — to `Right` (or to `Left` if the right panel is active); row 0.
  - `ArrowDown`/`ArrowUp` — with `Settings.MenuWrap` cyclically, otherwise clamped; `Home`/`End`; `PageDown`/`PageUp` — step `Rows-5`.
  - `Enter` — remembers `lastMenu`, closes the menu, then executes the item's `Run`.
  - After any key the cursor "jumps over" separators (empty `menuItem`s) in the direction of movement.
  - There are **no** letter accelerators in the menu.
  - `⌘O` with the menu open closes it and toggles the terminal. The keys `Alt+F1/F2/F7/F8/F10`, `Shift+F10`, `Shift+F4`, `F12`, `⌘N`, `⌘Q` are intercepted before `menuKey` and work even while the menu is open (the menu stays open in that case, except on explicit closing).
- **Mouse (`menuClick`):** a click inside the frame on an item (not a separator) — execute; on a separator — nothing; outside the frame — close the menu. A click on another section's title in row 0 switches the section.
- **Drawing (`drawMenu`):** row 0 — section names (`Left` x=2, `Files` x=11, `Commands` x=21, `Options` x=34, `Right` x=46), the active section is inverted (`White` on `Black`); on the right the brand `FarshMac 0.20.0` (at width ≥ 70). The drop-down block: width `min(44, Cols-2)`, height `min(items+2, Rows-3)`, scrolling via `Menu.Top`, background `Cyan`, white frame, the current row has background `Black`; separators `╟───╢`; the shortcut text in yellow in the rightmost 9 columns; the `✓ ` prefix is drawn as a check mark in the first column (the current panel view).
- **Code:** `menu.go` (`menuSections`, `menuKey`, `menuBounds`, `menuClick`, `drawMenu`), `app.go` (`function(9)`, `click`).

### The Left / Right section (identical, the parameter is the panel index)

| Item | Shortcut in the menu | Action |
| --- | --- | --- |
| `Brief` | — | `setView(i, "Brief")` |
| `Medium` | — | `setView(i, "Medium")` |
| `Full` | — | `setView(i, "Full")` (also checked when `View` is empty) |
| `Wide` | — | `setView(i, "Wide")` |
| `Detailed` | — | `setView(i, "Detailed")` |
| *(separator)* | | |
| `Info panel` | — | `setView(i, "Info panel")` |
| `Tree panel` | — | `setView(i, "Tree panel")` |
| `Quick view` | — | `setView(i, "Quick view")` |
| *(separator)* | | |
| `Sort modes…` | — | `sortMenu(i)` |
| `Panel on/off` | `⌘F1` / `⌘F2` | `togglePanel(i)` |
| `Re-read` | `⌘R` | `rereadPanel(i)` |
| `Change volume…` | `AltF1` / `AltF2` | `volumes(i)` |

The current view is marked with `✓ `. The test `TestCleanMenuOrderAndSeparators` pins exactly this order.

#### Changing the panel view (setView)
- **Behaviour:** for a remote panel `Tree panel`/`Info panel`/`Quick view` are forbidden → status `Choose a file view for remote panels`; inside an archive `Tree panel` → `Tree view is unavailable inside archives`. Otherwise `View = view`, `Hidden = false`, the panel is reloaded, the quick view cache is reset. When loading old setups the views `Descriptions`, `Long descriptions`, `File owners`, `File links`, `Alternative full` are normalised to `Full` (`configurePanel`).
- **Code:** `panel_options.go` (`setView`), `settings.go` (`configurePanel`).

#### Sort modes…
- **Dialog:** the `Sort mode` list: `Name`, `Extension`, `Size`, `Modified`, `Reverse order`, `Case sensitive`, `Directories first`, `Selected first`, `Unsorted`, `Created`, `Accessed`, `Description`, `Owner`.
- **Behaviour:** the mode items set `panelOptions.Sort`; `Reverse order`/`Case sensitive`/`Directories first`/`Selected first` toggle the flags `Reverse`/`CaseSensitive`/`MixDirectories`/`SelectedFirst` (the `Directories first` item inverts `MixDirectories`; the current values are not shown in the list). After the choice the panel is reloaded.
- **Keys outside the menu:** `Ctrl/⌘+F12` — the same list for the active panel; `Ctrl/⌘+F3…F11` — direct choice of a mode (`Name, Extension, Modified, Size, Unsorted, Created, Accessed, Description, Owner`; repeating inverts the order); on a remote panel `Created/Accessed/Owner/Description` → `This sort mode requires local file metadata`; `Shift+F11` — sort groups off/on (`DisableSortGroups`), `Shift+F12` — `Selected first`.
- **Code:** `panel_options.go` (`sortMenu`, `sortEntries`), `far_keys.go`.

#### Panel on/off
- **Invocation:** menu, `⌘F1`/`⌘F2` (`Ctrl+F1/F2` too), `Ctrl/⌘+P` — hide the inactive panel.
- **Behaviour:** `Hidden` is inverted; if the active panel is being hidden and the other one is visible, the other one becomes active.
- **Code:** `archive_panel.go` (`togglePanel`).

#### Re-read
- **Invocation:** menu (a specific panel), `⌘R`/`Ctrl+R` (the active panel).
- **Behaviour:** resets the info panel keys, for an archive — reopens the archive, otherwise `Load(Path)`; an error goes to the status.
- **Code:** `archive_panel.go` (`rereadPanel`).

#### Change volume… / Mounted volumes
- **Invocation:** the Left/Right menu (`AltF1`/`AltF2`), Commands → `Mounted volumes` (the active panel).
- **Dialog:** the `Volumes and home` list: `/`, the home folder, all entries of `/Volumes/*`.
- **Behaviour:** the chosen panel becomes active, `Remote = nil` (the panel's remote connection is dropped without a message), the path is loaded.
- **Code:** `panel_options.go` (`volumes`).

### The Files section

| Item | Shortcut | Action / conditions |
| --- | --- | --- |
| `Open with macOS` | `⇧Enter` | `openSystemSelected`: on a remote panel → status `Copy remote files to a local panel before opening them with macOS`; an item inside an archive → `Use F5 to copy this archive item out before opening it with macOS`; otherwise `open` via the Finder association |
| `View / folder size` | `F3` | `function(3)`: on a folder — size calculation; a remote file — download and view; an archive — view a member |
| `Calculate all folder sizes` | `⌘⌥F3` | `calculateFolderSizes(true)` (also works over SFTP, see the test) |
| `Edit` | `F4` | `function(4)`; in an archive → `Archive is read-only. Use F5 to copy files out.`; on a remote panel — download and edit |
| `New file…` | `ShiftF4` | `newFile` (blocked on a remote panel/in an archive) |
| `Copy…` | `F5` | `transferDialog(false)` |
| `Rename or move…` | `F6` | `transferDialog(true)` → for remote panels `remoteMoveDialog` |
| `Link…` | — (`Alt+F6` in the code) | `createLink` |
| `Make folder…` | `F7` | `function(7)` → locally the `Make folder` prompt / remotely `Make remote folder` |
| `Move to Trash…` | `F8` | `deleteSelected(false)`; on a remote panel — `remoteDelete` |
| `Delete permanently…` | `⇧F8` | `deleteSelected(true)` (also `Shift+Delete`; `Delete` without Shift = Trash) |
| `Create archive…` | — (`Shift+F1` in the code) | `archiveCreate` |
| `Extract archive…` | — (`Shift+F2` in the code) | `archiveExtract` |
| `Archive commands…` | — (`Shift+F3` in the code) | `archiveCommands` |
| `File attributes…` | — | `fileAttributes` |
| `Apply command…` | — | `applyCommand` |
| `Describe files…` | — | `describeFiles` |
| `Select group…` | `Num +` | `selectGroup("Select")` |
| `Deselect group…` | `Num −` | `selectGroup("Deselect")` |
| `Invert selection` | `Num *` | `selectGroup("Invert")` |
| `Restore selection` | `⌘M` | `selectGroup("Restore")` |

The items `Link…`, `File attributes…`, `Apply command…`, `Describe files…`, `New file…`, `Create archive…` on a remote panel write `This command requires a local panel. Use F5 to copy remote files first.` to the status and open nothing (test `TestRemoteCommandsDoNotModifyLocalPaths`); inside an archive — `Archive is read-only. Use F5 to copy files out.`.

#### Link…
- **Dialog:** `Create link`: `Link name` (default `<other panel>/<name>`), toggle `Symbolic link` (on). OK → `os.Symlink` or `os.Link`, errors go to the status, the panels are refreshed.

#### File attributes…
- **Dialog:** `File attributes`: `Permissions (octal)` (the current permissions `%03o` of the first selected item), `Modified (local)` (`2006-01-02 15:04:05`). Checks: `Permissions must be 000–777`; the time is parsed in the local zone. Applied to all selected items via `workProgress("Updating attributes")`; symbolic links → error `attributes of symlinks are not changed: <path>`.

#### Apply command…
- **Dialog:** prompt `Apply command` / `Use {} for a shell-quoted filename`. For each selected file a command is built (`{}` is replaced with the escaped path, otherwise the path is appended at the end), the commands are joined with `\n` and executed in the terminal (`execute`).

#### Describe files…
- **Dialog:** prompt `Describe files` / `Description (empty removes it)`, the initial value is the description of the first selected item. Writes the file `Settings.DescriptionFile` (default `descript.ion`) in the current folder atomically; names with spaces are quoted; names containing `\n\r"` → error `this filename cannot be stored in descript.ion`.

#### Select group… / Deselect group… / Invert / Restore
- **Dialog:** prompt `Select group` / `Deselect group`, hint `Masks: *.go;*.md | *_test.go (exclude)`, value `*`. Masks are validated (`validateMasks`, `<name>` groups from `MaskGroups`); folders are included when `Settings.SelectFolders`. `Restore` swaps `Marked` and `RestoreMarked`.

### The Commands section

| Item | Shortcut | Action |
| --- | --- | --- |
| `Network connections…` | — | `networkMenu` (section 5) |
| `Quick view on/off` | `Ctrl+Q` | `toggleOtherView("Quick view")` for the inactive panel; returns to the previous view on repeat |
| `New window` | `⌘N` | `NewWindow = true` → Cocoa opens a window, copying the settings |
| `Find file…` | `AltF7` | `findFiles(false)` — see the search section |
| `Command history` | `AltF8` | `historyMenu("Commands")` |
| `Find folder…` | — | `findFiles(true)` |
| `Folder tree` | `AltF10` | `treeMenu` — background scan, the `Folder tree` list (`(partial)` when limits are hit) |
| `File view/edit history` | — (`Alt+F11` in the code) | `historyMenu("Files")` |
| `Folder history` | — (`Alt+F12` in the code) | `historyMenu("Folders")` |
| `Same folder in other panel` | `⌘=` | `matchOtherPanel` (also `⌘⇧\`) |
| `Swap panels` | `⌘U` | swaps `Panels[0]`/`Panels[1]` (also `Ctrl+U`) |
| `Panels on/off / terminal` | `⌘O` | `showTerminal` |
| `Compare folders` | — | `compareFolders` |
| `User menu` | `F2` | `userMenu` — see the user menu section |
| `Edit user menu…` | — | `editUserMenu` |
| `File associations…` | — | `fileAssociations` |
| `Bookmarks…` | ⌘⌥0…9 | `bookmarksMenu` |
| `Edit sort groups…` | — | `settingsMenu("Sort groups")` |
| `File panel filter…` | — | `filterPanel(Active)` |
| `Screens list` | `F12` | `screenMenu` |
| `Full screen` | `⌃⌘F` | `Fullscreen = true` (Cocoa toggleFullScreen) |
| `Task list` | — | `taskList` |
| `Mounted volumes` | — | `volumes(Active)` |
| `Insert filename` | `⌘Enter` | `insertFilename` |
| `Quit` | `⌘Q` | `RequestQuit` |

#### Command history / View-edit history / Folder history
- **Dialogs:** the lists `Command history` (newest on top; a choice puts the command into the command line without executing it), `View/edit history` (`View <path>` / `Edit <path>`; a choice opens the document), `Folder history` (the union of `folderHistory` and the history of both panels, limited by `HistoryLimit`; a choice loads it into the active panel).
- **Messages:** `Command history is empty`, `View/edit history is empty`; for an empty folder history — `Folder history: empty` (via `choose`).
- Histories are saved to `setup.json` only via Save setup.

#### Compare folders
- Clears the selection (saving it to `RestoreMarked`) and marks in both panels the files that are missing on the opposite side, differ in size, or are newer. Status: `Marked files missing, different in size, or newer than the opposite panel (non-recursive)`.

#### Edit user menu… / File associations…
- `Edit user menu`: the list `Add command…` + command names; the form `User command — {} inserts filename` with the fields `Label`, `Command (empty deletes)`; after OK — `saveSetup()` (immediate write of setup.json).
- `File associations`: the list `Add association…`, `Open current file with macOS default app`, then `<mask> → <command>`; the form `File association — {} inserts filename`: `Masks`, `Run command`, `Viewer command`, `Editor command`; masks are validated; after OK — `saveSetup()`. The Viewer/Editor commands of associations override the external viewer/editor from the settings (`viewPlan`).

#### Bookmarks…
- **Dialog:** the `Bookmarks` list: `Save current folder…`, then `0  <label>` … `9  <label>` (a folder — `<path>/<file>`, for remote ones prefixed with `<label>`; a document — `Edit|View <path>:<line>`). Choosing a slot → `goBookmark`; `Save current folder…` → prompt `Save bookmark` / `Slot 0–9` (value `1`), error `Choose 0–9`.
- **Keys:** `⌘⌥0…9` — save (in the panels the folder and the file under the cursor, in a document the position), `⌘0…9` — go to (not Numpad). There are no Ctrl or Shift variants.
- **Storage:** `App.Bookmarks map[int]bookmark` — in window memory only; not saved and not visible to other windows. Details — section 9.
- **Code:** `folder_shortcuts.go`, `panel_options.go` (`folderShortcuts`).

#### File panel filter…
- prompt `File panel filter` / `Include masks | exclude masks; empty shows all`, the current value is `panelOptions.Filter`; mask validation; the filter applies to files (not folders) and is stored in `panelOptions` (and in setup/session).

#### Screens list
- The `Screens` list: `Panels`, then `View <name>`/`Edit <name>` (`* ` when there are unsaved changes); focus on the current document. `Ctrl+Tab`/`⌘⇧[`/`]` — cyclic switching.

#### Task list
- The background task `Reading processes` (`/bin/ps -axo pid,ppid,%cpu,%mem,comm`), the result is the report document `Processes` (Help mode, read-only).

#### Insert filename
- `⌘Enter` (and `Ctrl+J`): the name of the current file is appended to the command line with escaping (`terminal.Quote`), names starting with `-` get `./`; names with control characters → `Cannot insert a filename containing control characters`.

#### Quit
- `⌘Q` is handled by `WindowGroup.BeginQuit` for all windows. `RequestQuit`: when Busy → `Wait for the file operation to finish`; with unsaved documents — confirm `Save changes` / `Save changes in open documents before quitting?` with the buttons `Save all`, `Discard`, `Cancel`; with a busy shell — confirm `Quit` / `A command is running. Close the application?` (`Yes`/`No`). Other windows during the group quit get the status `Finish the quit dialog in the other window`. Before exiting, session.json is saved; `AutoSave` → `saveSetup()` in `App.Close`.

### The Options section

| Item | Shortcut | Action |
| --- | --- | --- |
| `General settings…` | — | the `General settings` form |
| `Panel settings…` | — | the `Panel settings` form |
| `Terminal settings…` | — | the `Terminal settings` form |
| *(separator)* | | |
| `Viewer settings…` | — | the `Viewer settings` form |
| `Editor settings…` | — | the `Editor settings` form |
| `Advanced…` | — | the `Advanced settings` list |
| *(separator)* | | |
| `Full Disk Access…` | — | `DiskAccessSettings = true` + status |
| *(separator)* | | |
| `Save setup` | — (`Shift+F9` in the code) | `saveSetup` |
| `Keyboard reference` | `F1` | `function(1)` — help |

---

## 3. Settings dialogs (`internal/ui/settings.go`, `settingsMenu`)

All settings forms apply the values in memory immediately after OK; they reach the disk only via `Save setup` (Shift+F9 / menu), `AutoSave` on window close, or indirectly through actions that call `saveSetup()` (user menu, associations, folder shortcuts). A validation error is written to the status, while the form is already closed (the values are not applied).

#### General settings
- **Dialog:** `General settings`
  - `Font size (10–24)` — text, default `14` → `FontSize` (passed to the renderer in every `Update.FontSize`).
  - `Key bar` — toggle, default on → `ShowKeys` (also `Ctrl/⌘+B`).
  - `Wrap menus` — toggle, default on → `MenuWrap` (cyclic navigation in menus and lists).
  - `Select initial dialog text` — toggle, default on → `SelectDialogInput`.
  - `Remember dialog values` — toggle, default off → `RememberDialogValues`.
  - `Confirm deletion` — toggle, default on → `ConfirmDelete`.
  - `Confirm overwrite` — toggle, default on → `ConfirmOverwrite`.
  - `Save setup on exit` — toggle, default off → `AutoSave`.
- **Messages:** `Font size must be 10–24`.

#### Panel settings
- **Dialog:** `Panel settings`
  - `Show hidden files (Cmd+Shift+.)` — default on → `ShowHidden` (the panels get `HideHidden = !ShowHidden`); keys `⌘⇧.`, `Ctrl+H`.
  - `Highlight files` — default on → `Highlight` (colouring by type in the panels).
  - `Human-readable sizes (off = bytes)` — default off → `HumanReadableSizes` (also `Ctrl/⌘+Shift+S`).
  - `Hide and ignore .DS_Store` — default off → `IgnoreDSStore` (hides in the panels, including remote ones, and skips during copying/archiving: `Operation.IgnoreDSStore`).
  - `Select folders with file masks` — default off → `SelectFolders`.
- **Effect:** reset of `ForcedVisiblePath`, `configurePanel` for both panels, `refresh()`.

#### Terminal settings
- **Dialog:** `Terminal settings`
  - `Shell: bash / sh / zsh (new windows)` — text, default `bash` → `Shell`.
  - `Command history entries (1–10000)` — default `200` → `HistoryLimit`.
  - `Complete filenames` — default on → `CompleteFiles` (⌘Space).
  - `Completion ignores case` — default off → `CompleteIgnoreCase`.
  - `Use custom shell prompt / hooks (new windows, may be slow)` — default off → `ShellCustomPrompt`.
- **Messages:** `Choose bash, sh or zsh` (validation `terminal.ValidShell`, protects against injections such as `zsh; echo bad`), `History entries must be 1–10000`, on success: `Shell applies to new windows; Save setup to keep settings after restart`.

#### Viewer settings
- **Dialog:** `Viewer settings`: `Wrap lines` (default off → `ViewerWrap`), `External by default` (off → `ExternalViewerDefault`), `Command (blank=system)` (text → `ExternalViewer`).

#### Editor settings
- **Dialog:** `Editor settings`: `Auto indent` (on → `EditorAutoIndent`), `Syntax highlighting (Chroma)` (on → `EditorSyntax`), `Tab width (1–16)` (`4` → `EditorTabSize`), `Expand tabs to spaces` (off → `EditorExpandTabs`), `External by default` (off → `ExternalEditorDefault`), `Command (blank=system)` (→ `ExternalEditor`).
- **Messages:** `Tab width must be 1–16`.

#### Advanced…
- **Dialog:** the `Advanced settings` list: `Colors`, `Info panel settings`, `Tree settings`, `Folder description files`, `File descriptions`, `Groups of file masks`, `Sort groups`, `Code pages`.

#### Colors
- The `Colors` list: `Classic Far`, `Dark`, `Light` → `ColorTheme` (default `""` = the classic palette). `Dark`: `Blue→#151b24`, `LightCyan→#c7d7e9`, `Cyan→#486987`; `Light`: `Blue→#f1f3f5`, `LightCyan→#182b49`, `White→#000000`, `Yellow→#885000`, `Cyan→#80b8dd`. Applied to the whole grid at drawing time (`Draw`). There is no separate font (typeface) setting — only the size.

#### Info panel settings
- **Invocation:** Advanced → `Info panel settings`, or `Ctrl/⌘+F12` while the info panel is active.
- **Dialog:** `Info panel settings`: `Computer and user` (on → `InfoHost`), `Drive information` (on → `InfoVolume`), `Memory information` (on → `InfoMemory`), `Folder description` (on → `InfoDescription`), `Power status` (on → `InfoPower`). After OK the info panel cache is reset.

#### Tree settings
- prompt `Tree settings` / `Maximum levels for folder tree (1–12)`, default `4` → `TreeDepth`; error `Enter a number from 1 to 12`; the panels are reconfigured and refreshed.

#### Folder description files
- prompt `Folder description files` / `Names separated by commas`, default `file_id.diz,README.md,README,readme.txt` → `FolderDescriptions` (shows the folder description in the info panel); names with a path → `Use filenames without paths`.

#### File descriptions
- prompt `File descriptions` / `Description file name`, default `descript.ion` → `DescriptionFile`; `Enter a file name without a path`.

#### Groups of file masks
- The `Groups of file masks` list: `Add group…`, then `<name> = <masks>` (keys sorted). The `Mask group` form: `Name`, `Masks (empty deletes)`. Errors: `Invalid group name` (empty or containing `<>,;|`), `Use literal masks in groups` (nested `<>`), `filepath.Match` errors. Renaming removes the old key. Default groups: `arc` (`*.zip,*.tar,*.gz,*.bz2,*.xz,*.tgz,*.tbz,*.tbz2,*.txz,*.7z,*.rar,*.jar,*.war,*.cpio`), `source` (`*.go,*.c,*.h,*.js,*.ts,*.py`), `exec` (`*.sh,*.command`). Groups are substituted as `<name>` in the masks of the filter, selection, search, associations, and sort groups.

#### Sort groups (Advanced → Sort groups; Commands → Edit sort groups…)
- Two entry points into the same prompt — intentional duplication (as in Far: Commands → "Edit sort groups", Options → "Sorting").
- prompt `Sort groups` / `Masks in priority order, separated by semicolons`, default empty → `SortGroups`. Validation with `;`→`,` replacement; the status carries the error text (`unknown mask group` etc.). Files matching earlier masks sort higher (except in `Unsorted` mode and with `DisableSortGroups`/Shift+F11).

#### Code pages
- The `Default encoding for new files` list: `UTF-8`, `UTF-16LE`, `UTF-16BE`, `Windows-1251`, `CP866`, `Windows-1252`, `ISO-8859-1`, `KOI8-R` → `DefaultCodepage` (default `UTF-8`).

#### Full Disk Access…
- **Invocation:** the Options menu, and also the native application menu `FarshMac → Full Disk Access…`.
- **Behaviour:** `DiskAccessSettings = true` (a one-shot flag in `Update`, test `TestDiskAccessMenuEmitsOneNativeRequest`), status `Enable this copy of FarshMac in Full Disk Access (+ or drag from Finder), then quit and reopen.`. The native side reveals the bundle in Finder and opens `x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles` (fallback — System Settings, then an NSAlert `Open Full Disk Access in System Settings`). On first launch `native.PrepareDiskAccess` shows the alert `Full Disk Access for FarshMac` with the buttons `Open Settings`/`Continue` (the flag `FarshMacDiskAccessIntroductionShown` in NSUserDefaults). Details — in the platform section.

#### Save setup
- **Invocation:** the Options menu → `Save setup`; `Shift+F9`; indirectly from the user menu/associations/folder shortcuts.
- **Behaviour (`saveSetup`):** `normalizeSettings()`; for remote panels updates the `Directory` of the corresponding profile in `Settings.RemoteConnections` (in memory); the folder histories of both panels are added to `folderHistory`; a `savedSetup{Settings, Panels[2] panelOptions, Paths[2] (filesystemPath), Commands, Folders, Viewed}` is assembled; indented JSON is written atomically (temp file `.setup-*` + rename, directory `0700`). Status `Setup saved`; a write error is shown on top via `a.error`.
- **Code:** `settings.go`.

#### Keyboard reference (F1)
- **Invocation:** `F1`, `⌘1`, the Options menu.
- **Behaviour:** opens the document `FarshMac — Help` with the `help` constant in `Help` mode (read-only, `markSaved`). Inside the document a repeated `F1` does nothing (for the Help document); for ordinary documents `F1` opens `Viewer / editor help`. Closing: `Escape`, `F10`, `F3` (in the viewer). `F12` — the screens list. `F6` (viewer/editor switch) is ignored for Help.
- **Content (verbatim):**

```
FarshMac — native macOS window, Go file manager, persistent zsh terminal

Tab                 Switch active panel
Arrows / PgUp/PgDn   Navigate files
Ctrl+Q             Toggle inactive Quick view (restore previous view)
Ctrl+L / Ctrl+T    Toggle inactive Info / Tree
Ctrl+P / Ctrl+B    Toggle inactive panel / key bar
Ctrl+F3..F11       Sort (name, ext, modified, size, unsorted, created, accessed, description, owner)
Ctrl+F12           Sort menu; Shift+F12 selected first
Shift+F5/F6        Copy/move only the current item
Shift+F1/F2/F3     Archive create/extract/commands
Shift+F9           Save setup
Alt+F11/F12        View/edit history / folder history
Ctrl+PgUp/PgDown   Parent / enter folder or archive
Ctrl+Left/Right    Panel width (empty command); Ctrl+Num5 reset
Ctrl+E/X/Y         Previous / next command / clear
Ctrl+F / Ctrl+;    Insert full path from active / inactive panel
Ctrl+Insert        Copy command or selected names; Alt+Shift+Insert full paths
Shift+Insert       Paste text
Num + / Num -      Select / deselect by masks
Num *              Invert files; Cmd/Ctrl+Num * includes folders
Cmd/Ctrl+Num +/-   Select / deselect same extension
Alt+Num +/-        Select / deselect same base name
Shift+Num +/-      Select / deselect all files
Cmd/Ctrl+M         Restore previous selection
Ctrl+Shift+1..9     Save folder shortcut (panels)
Ctrl+1..9           Go to folder shortcut (panels)
Network connections: F9 > Commands > Network connections
FTP / FTPS / SFTP    Remote panel; terminal stays local
F5 Parallel transfers: 1-8 workers, Esc cancels

Enter               Open directory / view file / execute command
Shift+Enter         Open selected item with macOS (folders in Finder)
Cmd+Shift+.         Show / hide hidden files (display only)
Backspace           Delete command-line character; never leave directory
Space / Insert      Mark file; operations apply to marked files
F3 on a folder      Calculate marked/current folder sizes; participates in Size sort
⌘Option+F3         Calculate all folder sizes (~ = cached, possibly outdated)
Info: Ctrl+F12      Configure sections; PgUp/PgDn/wheel scroll; F3/F4 description
F3 / F4             Viewer / editor (F1 inside for full key reference)
F5 / F6             Copy / move or rename (no overwriting)
F7                  New directory
F8 / Delete         Move selected items to Trash
Shift+F8 / Shift+Del Delete permanently
F9 / Cmd+9          Main menu (arrows, Enter, Esc)
Cmd+L              Change directory
Cmd+Enter          Insert current filename in command line
F2 / Cmd+2          User menu
Cmd+R              Refresh panels
Cmd+Space          Complete filename in command line
⌘U              Swap panels
⌘F1 / ⌘F2     Hide/show left / right panel
Alt+F1 / Alt+F2    Choose left / right volume
⌘Shift+\ or ⌘=  Open active folder in the other panel
Enter on archive  Browse archive; .. returns to parent
F5 in archive     Copy selected files/folders out; F3 previews
File links view   Names, sizes and hard-link counts (names sharing one inode)
⌘Up / Down      Command history
⌘O              Toggle panels and persistent terminal
Cmd+1 … Cmd+9       Alternatives to F1 … F9 on Mac keyboards
Cmd+N / Cmd+W      New / close native window
Cmd+Q              Quit ALL windows; F10 closes only viewer/editor
Alt+characters     Fast find (Cmd+Enter next; Cmd+Shift+Enter previous)
F12                Panels and document screens
Esc during operation  Cancel

Type a shell command directly while panels are visible, then press Enter.
The panels disappear and the SAME window becomes an interactive terminal.
Commands run in the active panel's directory. zsh keeps its environment,
history, aliases and jobs. Your .zshrc is loaded at shell startup.
The output stays visible after completion; ⌘O restores the panels.
⌘O also works during a command; returning does not stop the job.
Use Cmd+. (or Ctrl+C) to interrupt, Cmd+C / Cmd+V to copy / paste.
Scroll the terminal to review output. vim/less use the alternate screen.

This is an initial independent implementation, not full Far compatibility.
Read-only archive panels are supported. No Windows DLL plugins, macros or network VFS yet.
Cross-volume move is refused. Copy, verify, then delete explicitly.
Copies preserve permissions, timestamps and links, not ACLs/xattrs.
The text editor saves atomically, but does not preserve ACLs/xattrs/hardlinks.
Text editing/decoding: 64 MiB limit. Larger files: paged hex/dump viewer.
Esc returns to the file panels.
```
- **Code:** `app.go` (`help`, `function(1)`), `document.go` (`documentKey`), `commands.go` (`report`).

#### settingsMenu branches not used from the menu (dead code)
`settingsMenu` contains forms that cannot be reached from the current menu: `Dialog settings` (`Select initial input`, `Remember values`), `AutoComplete settings` (`Files (⌘Space)`, `Ignore case`), `System settings` (`Save setup on exit`), `Interface settings` (`Font size (10–24)`, `Key bar`), `Menu settings` (`Wrap at list ends`), `Confirmations` (`Delete files/folders`, `Overwrite files`), `Command line settings` (prompt `Maximum history entries (1–10000)`). An unknown `kind` → status `Settings: <kind>`.

#### Settings edited outside Options
- `CopyWorkers` (default `4`, range 1–8), `CopyPermissions` (on), `CopyAttributes` (on) — fields of the F5 copy dialog: `Preserve permissions (mode, owner, group, ACL)`, `Preserve attributes (xattrs, flags, timestamps)`, `Parallel transfers (1–8)` (error `parallel transfers must be between 1 and 8`). The values are remembered in `Settings` on every copy.
- `EditorExpandTabs`/`EditorTabSize` — also from the editor settings dialog inside a document (`document.go`).
- `ShowHidden`, `ShowKeys`, `HumanReadableSizes` — via hotkeys (see above).
- `UserMenu`, `Associations`, `Shortcuts*` — via the corresponding dialogs (with an immediate `saveSetup`).
- `RemoteConnections` — `connections.json` (section 5).

---

## 4. The Settings structure, default values, storage and compatibility

### `Settings` fields (`settings.go`) and defaults (`defaultSettings`)

| Field | Default | Where it is changed |
| --- | --- | --- |
| `InfoMemory`, `InfoPower`, `InfoDescription` | true | Info panel settings |
| `ShellCustomPrompt` | false | Terminal settings |
| `SelectFolders` | false | Panel settings |
| `CopyWorkers` | 4 (normalised to 1–8) | the Copy dialog |
| `RemoteConnections []remotefs.Config` | nil | connections.json / connectRemote |
| `CopyPermissions`, `CopyAttributes` | true | the Copy dialog |
| `Shell` | `bash` (normalised by `ValidShell`) | Terminal settings |
| `HumanReadableSizes` | false | Panel settings, Ctrl+Shift+S |
| `ExternalViewer`, `ExternalEditor` | "" | Viewer/Editor settings |
| `ExternalViewerDefault`, `ExternalEditorDefault` | false | Viewer/Editor settings |
| `CompleteFiles` | true | Terminal settings |
| `CompleteIgnoreCase` | false | Terminal settings |
| `SelectDialogInput` | true | General settings |
| `RememberDialogValues` | false | General settings |
| `InfoHost`, `InfoVolume` | true | Info panel settings |
| `FolderDescriptions` | `file_id.diz,README.md,README,readme.txt` | Folder description files |
| `AutoSave` | false | General settings |
| `RestoreFolders` | false | **used nowhere** |
| `MenuWrap` | true | General settings |
| `ShowKeys` | true | General settings, Ctrl+B |
| `FontSize` | 14 (normalised to 10–24) | General settings |
| `ColorTheme` | "" (Classic Far) | Colors |
| `ConfirmDelete` | true | General settings |
| `ConfirmOverwrite` | true | General settings |
| `ShowHidden` | true | Panel settings, ⌘⇧., Ctrl+H |
| `IgnoreDSStore` | false | Panel settings |
| `Highlight` | true | Panel settings |
| `ViewerWrap` | false | Viewer settings |
| `EditorAutoIndent` | true | Editor settings |
| `EditorSyntax` | true | Editor settings |
| `EditorExpandTabs` | false | Editor settings / document |
| `EditorTabSize` | 4 (normalised to 1–16) | Editor settings / document |
| `DefaultCodepage` | `UTF-8` | Code pages |
| `HistoryLimit` | 200 (normalised to 1–10000) | Terminal settings |
| `TreeDepth` | 4 (normalised to 1–12) | Tree settings |
| `UserMenu []UserCommand{Name, Command}` | nil | Edit user menu |
| `Associations []Association{Mask, Command, Viewer, Editor}` | nil | File associations |
| `Shortcuts [10]string`, `ShortcutRemotes [10]*remotefs.Config`, `ShortcutArchives [10]string` | empty | Legacy folder shortcuts; read for compatibility, not used (bookmarks live in the window) |
| `SortGroups` | "" | Sort groups |
| `MaskGroups map[string]string` | `arc`, `source`, `exec` (see above) | Groups of file masks |
| `DescriptionFile` | `descript.ion` (normalised — base name only) | File descriptions |

`savedSetup` also stores the `panelOptions` of both panels (`DisableSortGroups`, `View`, `Sort`, `Filter`, `Reverse`, `CaseSensitive`, `MixDirectories`, `SelectedFirst`, `Hidden`), `Paths[2]`, `Commands` (command history), `Folders` (folder history), `Viewed []viewRecord{Path, Edit}`.

#### File locations
- Root: `os.UserConfigDir()` → on macOS `~/Library/Application Support`. The `FarshMac/` directory:
  - `setup.json` — settings + histories (`setupPath`).
  - `connections.json` — connection profiles (`profilesPath`).
  - `session.json` — windows/folders/cursors (`SessionPath`, `WindowGroup.SaveSession`).
- Secrets (`Password`, `KeyPassphrase`, `HostKey`) carry the tag `json:"-"` and never reach the disk (tests `TestHostKeysAndSecrets`, `TestConnectionProfilesPersistWithoutSaveSetup`).

#### Loading the setup (`LoadSetup`)
- Called in `main` after the `App` is created. Reads `setup.json` via `readConfigFile`; on a JSON error — default settings. Applies `normalizeSettings`, the `panelOptions` of both panels and `configurePanel`; **panel paths are not restored from the setup** (comment in the code: folder restoration goes through session.json); the command/folder/view histories come from the setup. At the end always `loadConnectionProfiles()` (defer), so the profiles are available even without setup.json.

#### Autosave
- `AutoSave` (`Save setup on exit`) — `saveSetup()` in `App.Close()` for each window (on window close and on quit). Off by default.
- The session (`session.json`) is saved independently of Save setup: in every `WindowGroup.Poll` at most once per 500 ms and only when the content has changed; forced on window close, on quit (`BeginQuit`), `CloseAll`. Format: `{Version: 1, Windows: [{Frame{X,Y,Width,Height}, Panels: [{Path, Options, Cursor, Top}], Active}], Focused}`. When started with the `-left/-right` flags session restore is disabled. On restore, unavailable folders are replaced by the nearest readable parent, then the default folder, home, `/` (`accessibleFolder`); version ≠ 1 → `unsupported window session version N`; > 100 windows → `too many saved windows`.

#### Configuration compatibility (`config_compat.go`)
- `readConfigFile(path)`: if the file in `<UserConfigDir>/FarshMac/` is missing, the file of the same name is read from the directory of the project's former name `<UserConfigDir>/Far Go/` (setup.json, session.json, connections.json). Writing always goes to `FarshMac/`; the old files are neither modified nor deleted. For paths outside the standard directory (for example, an explicit session path in tests) the fallback is not applied. Tests: `TestRenamedConfigFallsBackWithoutOverwriting`, `TestRenamedConfigDoesNotFallbackForCustomPath`.
- Migration of old panel views (`Descriptions`, `Long descriptions`, `File owners`, `File links`, `Alternative full` → `Full`) in `configurePanel`.
- `normalizeSettings` repairs out-of-range values (`CopyWorkers`, `Shell`, `EditorTabSize`, `FontSize`, `HistoryLimit`, `TreeDepth`, `MaskGroups == nil`, `DescriptionFile`).
- The test `TestSettingsRoundtrip` checks the round-trip of FontSize/MaskGroups/View/Shortcuts.

---

## 5. Network connections (FTP / FTPS / SFTP)

#### Network connections…
- **Invocation:** the Commands menu → `Network connections…` (no hotkey; F1 help: `Network connections: F9 > Commands > Network connections`).
- **Dialog:** before showing, `connections.json` is re-read (`loadConnectionProfiles`). The `Network connections` list: `New FTP / SFTP connection…`, then each profile as `<Label><Directory>` (Label = `sftp://user@host:port`), the last item `Disconnect active panel`.
- **Behaviour:** `New…` → a form with the template `{Protocol: sftp, Port: 22, Directory: "/", User: $USER}`; a profile → a form with its values; `Disconnect active panel` → `disconnectRemote(Active)` (if the panel is not remote — nothing).
- **After the backlog (P5-13 and others):** F8/Delete on a profile deletes it after the confirmation `Delete connection profile`; several profiles for one server are distinguished by the server + folder pair; a corrupted `connections.json` is renamed to `.broken-<time>`. Unknown SFTP key: `Trust once` / `Trust and save` (appends to `~/.ssh/known_hosts`) / `Cancel`; a changed or revoked key — `SSH host key changed` / `revoked` with fingerprints and `file:line`, OK only; other errors — `Connection failed` with `Edit` / `Retry` / `Cancel`. The test with three menu items for a single profile matches the code and is updated together with profile deletion.
- **Code:** `remote.go` (`networkMenu`), `connection_profiles.go`.

#### The "FTP / SFTP connection" form
- **Fields (`connectionDialog`):**
  1. `Server URL (sftp://, ftp://, ftps://)` — text; for a new connection `sftp://`, for a profile `c.ServerURL()` (scheme://host:port/path, without the user).
  2. `User` — text (from the profile / `$USER`).
  3. `Password (memory only)` — secret, always empty.
  4. `SSH private key (blank = default keys + agent)` — text (`KeyFile`).
  5. `Key passphrase (memory only)` — secret, always empty.
  - Buttons: `Connect` (Primary) / `Cancel`.
- **URL parsing (`remotefs.Parse`):** `strings.TrimSpace`; scheme `ssh` → `sftp`; only `sftp`/`ftp`/`ftps` are allowed (otherwise `use sftp://host, ftp://host or ftps://host`); a host is required, query/fragment are forbidden (`invalid server address`); a user in the URL (`sftp://user@host`) overrides the `User` field; a password in the URL → `enter passwords in the password field, not the URL`; the default port is 22 for sftp, 21 for ftp/ftps, range 1–65535 (`invalid port`); an empty path → `/`, otherwise `path.Clean`; control characters `\x00\r\n` → `invalid control character in address`.
- **After OK:** `Password`, `KeyPassphrase` are taken from the fields; `KeyFile` is expanded via `expand` (supports `~`, a relative path is resolved from the local path of the active panel). `connectRemote(Active, config)` is called.

#### The connection process (`connectRemote`)
- **Behaviour:** the background task `Connecting to <label>` (progress window, Esc cancels; cancellation closes all sockets via `socketSet`): `Dial` + `ReadDir(Directory)` as a check; the connection is closed.
  - The error `remotefs.UnknownHost` → confirm `Unknown SSH host key` with the text `<host> <SHA256:fingerprint>. Trust for this connection?`, buttons `Trust once` / `Cancel`. `Trust once` puts the key into `c.HostKey` (memory only, this connection only) and repeats `connectRemote`.
  - Other errors — into the status (`a.error`).
  - Success (`apply`): the panel is reconfigured (`configurePanel`), background loads are cancelled, the panel's local path `RemoteLocalPath` (or `$HOME`) is remembered, `Remote = &c`, `Archive = nil`, `Async = true`, `View = "Full"`, states/selection are reset, `Path = Directory`, cursor 0. The profile is saved (`saveConnectionProfile`), the panel is loaded. Status `Connected: <label> (terminal is local)`; on a profile save error `; profile not saved: <err>` is appended.
- **Code:** `remote.go` (`connectRemote`), `remotefs/fs.go`, `sftp.go`, `ftp.go`.

#### Host key verification and SFTP authentication (`remotefs/sftp.go`, `ssh_defaults.go`)
- known_hosts: `~/.ssh/known_hosts`, `~/.ssh/known_hosts2`, `/etc/ssh/ssh_known_hosts`, `/etc/ssh/ssh_known_hosts2` (only existing ones). An unknown host (no entries) → `UnknownHost`; **a mismatch with a known key or a revoked key is never accepted automatically** (the knownhosts error is returned). If `HostKey` is set (after `Trust once`) — `ssh.FixedHostKey`. Host key algorithm preference is as in OpenSSH: first the types already known for the host (`hostKeyAlgorithms`, test `TestKnownHostAlgorithmPreference`).
- Keys: if `KeyFile` is specified — only it (error `cannot read SSH private key <path>: …`); otherwise `~/.ssh/id_rsa`, `id_ecdsa`, `id_ed25519` (missing ones are skipped; encrypted ones without a passphrase produce the hint `<name> requires a passphrase or ssh-agent`); the entered passphrase is also applied to the default keys. A key file > 1 MiB → `SSH key file is too large`. Plus all identities from `SSH_AUTH_SOCK` (ssh-agent). The password is added as `ssh.Password` if non-empty. No method at all → `no usable SSH identity: provide a password, private key or ssh-agent. <hint>`. On a handshake error with a hint — `<err> (<hint>)`.
- Timeouts: dial 15 s, SSH handshake 15 s, a read/write deadline of 30 s on each socket. SFTP without concurrent reads/writes. `Rename` uses `posix-rename@openssh.com` when available. `Create` — `O_CREATE|O_EXCL|O_WRONLY`.

#### FTP / FTPS (`remotefs/ftp.go`)
- An empty user → `anonymous`. Credentials containing `\x00\r\n` → `FTP credentials contain a protocol control character`.
- FTPS = explicit TLS only (`AUTH TLS`) with `MinVersion TLS1.2`, `ServerName = host`, system roots (a self-signed certificate is rejected — test `TestExplicitFTPSProtectsDataConnection`); data connections are also wrapped in TLS. Implicit FTPS is not supported.
- Dial timeout 15 s; deadlines 30 s. `Lstat`: `GetEntry` (MLST), on 550 with the text `no such file`/`not found`/`does not exist` → `ErrNotExist`, otherwise a listing of the parent; `/` is treated as a folder. `Chmod` → `FTP does not provide portable permission preservation`; `Readlink`/`Symlink` → `FTP symbolic links are not supported`. Size > int64 → `invalid FTP size`.

#### The remote panel: loading and display (`loadRemote`)
- Every load opens a new session (`Dial`) and closes it; > 100000 entries → `remote folder exceeds 100000 entries`; `.`/`..` are skipped; a name containing `/\\\x00\r\n` or an empty one → `server returned an unsafe filename`; `IgnoreDSStore` (`.DS_Store`), `HideHidden` (dot files), and the panel filter (files) are applied. Sorting is standard. `InfoText = "<label>\nRemote filesystem; terminal remains local"` (shown in the info panel opposite).
- In the command line header the path is shown as `local:<RemoteLocalPath>`; key bar F8 → `Delete`. Quick view opposite a remote panel: `Remote file: use F3 to download and view`.
- The views `Tree panel`/`Info panel`/`Quick view` are unavailable for a remote panel (`Choose a file view for remote panels`; `Ctrl+L/T` → `This panel view requires a local folder`).

#### Operations in the remote panel (briefly; details — in the file operations section)
- `Enter` on a folder — navigate; on a file / `F3` / `F4` — the background task `Downloading remote file` into a temporary directory `farshmac-remote-view-*`, then view/edit; the document title is `<label><remote path>`; a non-regular file → `viewer requires a regular remote file`; changed during the process → `remote file changed during download`. Saving (`Saving remote file`): the check `remote file changed; reopen before saving`, upload via a temporary file `.farshmac-part-*` + rename; status `Remote file saved`.
- `F5` between a local and a remote panel, and also between two servers (through a local stream) — `remotefs.Copy` with 1–8 workers, each worker has its own session; limits of 1 000 000 items / depth 256 (`transfer tree limit exceeded`), 100000 entries per directory (`remote directory exceeds 100000 entries`); metadata is transferred "portably" (chmod/chtimes), with `Cannot preserve metadata` questions.
- `F6`: only within one server (`Label` matches) — prompt `Remote move / rename` / `Destination on the same server`; otherwise status `Remote move is available within the same server. Between servers use F5 Copy, then Delete.`; several items into a non-folder → `destination must be an existing directory`; nesting → `destination is inside source`.
- `F7`: prompt `Make remote folder` / `Folder name`; a name with `/` and the like → `enter one folder name`; errors are also shown in the window `Creating remote folder failed` (`reportError`); `Folder creation skipped` on skip.
- `F8`/`Shift+F8`/`Delete`: confirm `Delete remote files permanently` / `Delete N selected item(s) on <label>? No remote Trash is available.` with the buttons `Delete`/`Cancel`; deletion is recursive (`RemoveTree`, depth ≤ 256: `remote tree depth exceeds 256`). There is no Trash.
- `Shift+Enter`, `New file`, `Link`, `File attributes`, `Apply command`, `Describe files`, `Create/Extract archive`, `Find`, `Folder tree`, the user menu — blocked with the status `This command requires a local panel. Use F5 to copy remote files first.` (or `Copy remote files to a local panel before opening them with macOS`).
- The terminal is always local: `showTerminal`/`syncCwd` do not synchronise the cwd for a remote panel; commands run in `filesystemPath()` (the panel's local path).
- `Ctrl+Alt+Insert` copies full paths in the form `<label><path>`.

#### Disconnect active panel
- **Behaviour (`disconnectRemote`):** cancels loads, `Remote = nil`, loads `RemoteLocalPath` (the local folder before the connection), status `Disconnected`. Also any action that sets `Remote = nil` (Change volume, a local folder shortcut, Same folder in other panel from a local source panel).

#### Connection profiles (`connection_profiles.go`)
- The file `connections.json` — a JSON array of `remotefs.Config` (`Protocol, Host, User, Directory, KeyFile, Port`; no password/passphrase/host key). Written after every successful connection (`saveConnectionProfile`): re-reads the file, replaces the profile with the same `Label()` (`scheme://user@host:port`) or adds a new one, writes atomically (`.connections-*` + rename, `0700`), updates `Settings.RemoteConnections`. Access under `profilesMu` (shared between windows). Independent of Save setup (test `TestConnectionProfilesPersistWithoutSaveSetup`).
- `Save setup` additionally updates the `Directory` of the profiles of connected panels, but only in memory (`setup.json` contains a copy of `RemoteConnections` in `Settings`; at startup `connections.json` takes priority, since `loadConnectionProfiles` is called after the setup is read).
- There is no profile deletion/editing from the UI (only reconnecting with new parameters under the same label overwrites `Directory`/`KeyFile`).

---

## 6. The native macOS menu (for completeness)

Created in `window_darwin.m`: `FarshMac` → `About FarshMac`, `Full Disk Access…`, `Quit FarshMac ⌘Q`; `File` → `New Window ⌘N`, `Close Window ⌘W`; `Edit` → `Copy ⌘C`, `Cut ⌘X`, `Paste ⌘V`, `Select All ⌘A`; `Window` → `Command History ⌘⇧H` and `Terminal Output History` (active only in terminal mode), `Enter Full Screen ⌃⌘F`, `Minimize ⌘M`, `Next Window ⌘\``, `Previous Window ⌘⇧\``, `Bring All to Front`.

Note: `⌘M` in the panels is handled by the Go code as "Restore selection" (the Files menu item), but at the Cocoa level the same `⌘M` is assigned to `Minimize` — which handler wins is determined by the native key monitor; there is no exception in the Go code.

---

## 7. Discrepancies between the code, MENU_PARITY.md, README and the F1 help

> **Closed on the `backlog-fixes` branch:** the decisions on each item are in the "Decisions on discrepancies" section of `docs/IMPLEMENTATION_BACKLOG.md`.

1. **`Save setup` has no shortcut in the menu**, although `Shift+F9` is implemented (`far_keys.go`) and listed in the F1 help. Not mentioned in the README.
2. **Items without a shortcut in the menu but with hotkeys in the code and the help:** `Link…` (`Alt+F6` — not mentioned in the help either), `Create archive…`/`Extract archive…`/`Archive commands…` (`Shift+F1/F2/F3` in the help), `File view/edit history`/`Folder history` (`Alt+F11/F12` in the help), `Sort modes…` (`Ctrl+F12` in the help), `Find folder…` has no key anywhere.
3. **The F1 help calls `Ctrl+L / Ctrl+T` the Info/Tree toggle, and `Cmd+L` — Change directory**: the code confirms this (`toggleOtherView` only with `Ctrl` without `Meta`; `⌘L` → `gotoPrompt`). The README writes `Ctrl+Q / Ctrl+L` — consistent. But the Commands menu has only `Quick view on/off`; there is no item for `Info panel on/off`/`Tree` (they are in Left/Right as views).
4. **The menu shows `Ctrl+Q`**, while the other shortcuts in the menu are written with symbols (`⌘`, `⇧`) — mixed notation (`AltF1`, `ShiftF4`, `⇧F8`, `Ctrl+Q`, `⌃⌘F`).
5. **MENU_PARITY.md: "Folder shortcuts provide ten slots"** — in the code the slots are 1–9 (`Shortcuts[10]`, index 0 is unused; the dialog says `Slot 1–9`).
6. **MENU_PARITY.md: "Options → General: … dialogs, confirmations"** and the Advanced list "colors, Info panel, tree depth, descriptions, masks, sorting and encodings" — matches; but the separate dialogs `Dialog settings`, `Confirmations`, `System/Interface/Menu/AutoComplete/Command line settings` exist in the code and are unreachable from the menu.
7. **MENU_PARITY.md: "Settings are stored in `~/Library/Application Support/FarshMac/setup.json`"** — correct; the document does not mention `connections.json` and the fallback to `Far Go/` (described only in the code/tests).
8. **F1 help first line: "persistent zsh terminal"** and the paragraph "zsh keeps its environment… Your .zshrc is loaded" — the default shell in the code is `bash` (`defaultSettings`, `normalizeSettings`), the README says "bash, sh, or zsh".
9. **F1 help: "Cross-volume move is refused"** — MENU_PARITY.md states "Cross-volume moves copy before removing the source"; the file operations section is responsible for the code, here the contradiction between the documents is recorded.
10. **F1 help: "F5 / F6 Copy / move or rename (no overwriting)"** — yet `Confirm overwrite` and the buttons `Overwrite / All / Skip…` exist (MENU_PARITY.md, tests). The help wording is outdated.
11. **F1 help: "Copies preserve permissions, timestamps and links, not ACLs/xattrs"** — the copy dialog offers `Preserve permissions (mode, owner, group, ACL)` and `Preserve attributes (xattrs, flags, timestamps)`.
12. **The F1 help mentions `File links view`** (`Names, sizes and hard-link counts`), but the `File links` view has been removed from the menu and is normalised to `Full` (`configurePanel`, MENU_PARITY.md).
13. **README "Ctrl+Shift+1…9 / Ctrl+1…9 Save / open folder bookmarks"** — correct; the `Folder shortcuts…` menu item shows no shortcut.
14. **Help: "Cmd+1 … Cmd+9 Alternatives to F1 … F9"** — correct (`function(n)` with `Meta`+digit); `⌘9` opens the menu (and also closes it in `menuKey`).
15. **`Full screen ⌃⌘F`** in the Commands menu duplicates the native item Window → Enter Full Screen; not mentioned in the F1 help.
16. **The notation `AltF1`/`AltF2`, `ShiftF4`** in the menu versus `Alt+F1 / Alt+F2` in the help — cosmetic only.
17. **The test `TestConnectionProfilesPersistWithoutSaveSetup` expects 3 items** in `Network connections` with one profile — matches the code (`New…`, the profile, `Disconnect active panel`).
18. **`RestoreFolders`** in `Settings` is neither read nor edited anywhere (a dead field; it ends up in setup.json).
19. **`Describe files…`, `Edit sort groups…`** — in MENU_PARITY.md "Edit sort groups" is described under Commands; in the code the item leads to the same prompt form as Advanced → `Sort groups` (duplication).


<a id="discrepancy-summary"></a>
# Summary of discrepancies

> **Closed.** All discrepancies below have been resolved on the `backlog-fixes` branch (see "Decisions on discrepancies" in `docs/IMPLEMENTATION_BACKLOG.md`). The section is kept as history.

The lists of discrepancies between the code and the README / F1 help / parity documents are given at the end of each part:

- Part 1. File panels and navigation: 13 items.
- Part 2. File operations, archives, launching programs: 13 items.
- Part 3. Built-in viewer and editor: 19 items.
- Part 4. Terminal, command line, windows, native shell, renderer: 13 items.
- Part 5. The F9 menu, dialogs, settings, network connections, help: 19 items.

The most important ones for the user (appearing in several parts):

- The F1 help calls the terminal "persistent zsh" and promises to load `.zshrc`; the default shell is bash, selectable in the settings.
- The F1 help claims "Cross-volume move is refused" and "not ACLs/xattrs"; the code moves between volumes via copy-and-delete and preserves ACLs and xattrs.
- A number of working keys are undocumented: Alt+F3/Alt+F4, Cmd+Shift+F3/F4, Ctrl+J, Ctrl+[ ], Ctrl+Shift+Enter, Shift+F11, Alt+F6, Alt+F11/F12, Cmd+Shift+S.
- A number of menu items are shown without a shortcut although the shortcut exists in the code: Save setup (Shift+F9), Create link (Alt+F6), archive commands (Shift+F1–F3), histories (Alt+F11/F12).
- Seven dialogs from `settingsMenu` (Dialog, Confirmations, System, Interface, Menu, AutoComplete, Command line settings) are unreachable from the menu.
- Cmd+=/−/0 and Cmd+Shift+? are absent from the code: the font size changes only through the settings, the output history opens through the Window menu or the wheel.
- A probable defect: document bookmarks Cmd+Shift+0..9 parse `m.Key` via `Atoi`, whereas the renderer sends `!@#…` with Shift; panel bookmarks use `m.Code`.
- Ctrl+PgDn treats `.gz/.bz2/.xz` as archives by the `isArchiveName` list, but `ArchiveFormat` does not open them.


<a id="appendices"></a>
# Appendices. Additions from the gap analysis

Behaviour variants that the code handles but the main text of the parts did not describe. The tables are grouped by part; the "Code" column indicates where to check. Cases the code does not handle are moved to `docs/IMPLEMENTATION_BACKLOG.md`.

## Appendix 1. File panels and navigation

The code handles the case, the specification does not describe it.

| Function | Case | What the code does | Code (file:function) |
|---|---|---|---|
| Panel.Load | Esc during an asynchronous read | After cancellation the panel rolls back to the previous state; `operation cancelled` (the text of `fsops.ErrCancelled`) stays in the footer until the next load; "Stopping folder scan…" is shown only until the result arrives | `internal/ui/panel_load.go:pollPanelLoad`, `internal/fsops/progress.go:Check` |
| Panel.Load | Error on the very first load (the `-left`/`-right` start folder is unavailable) | `job.previous.Path == ""` → no rollback: the panel stays on the unavailable path with a single `..` entry and the error text in the footer | `internal/ui/panel_load.go:pollPanelLoad` |
| Panel.Load | No read permission (EACCES/EPERM) | `readDirectory` goes through `o.Attempt("read folder", …)`; without a question handler the error is returned as is: `open <path>: permission denied` in the footer, the cursor stays in the previous folder | `internal/fsops/walk.go:readDirectory` |
| Panel.Load | `e.Info()` error for an individual entry (the file disappeared between ReadDir and stat) | The entry is silently skipped | `internal/ui/app.go:Load` (`if info, err := e.Info(); err != nil { return nil }`) |
| Panel.Load | Esc with a non-empty command line during a read | `cancelReads()` returns true → the command line is not cleared, the status is not reset | `internal/ui/app.go:key` (`case "Escape"`) |
| Session | Folders from session.json do not exist / are not readable | `accessibleFolder` climbs to the nearest readable parent; then falls back to the first window's folder, the home directory, `/` | `internal/ui/session.go:accessibleFolder` |
| Save setup | Corrupt setup.json | `LoadSetup` silently returns on a `json.Unmarshal` error; default values are used, the user is told nothing | `internal/ui/settings.go:LoadSetup` |
| Save setup | Write failed (no permission, disk full) | `a.Status = "Setup saved"` then `a.error(err)` overrides the status with the error text; the in-memory values are kept | `internal/ui/settings.go:saveSetup` |
| Bookmarks | saveSetup failed on Ctrl+Shift+N | The slot is filled in memory anyway; the status shows the write error rather than `Folder shortcut N saved` | `internal/ui/folder_shortcuts.go:saveFolderShortcut` |
| Bookmarks | Ctrl+Shift+N on a layout where Shift+digit produces a symbol (`!`, `@`) | The number is taken from `m.Code` (`Digit1`…`Digit9`) | `internal/ui/folder_shortcuts.go:folderShortcutKey` |
| Bookmarks | Remote bookmark when some panel is already connected to the same server | The **active** panel is reconnected with the live configuration of the other panel (including the password in memory) | `internal/ui/folder_shortcuts.go:goFolderShortcut` |
| Folder history | History is empty | `choose` shows the status `Folder history: empty` (the generic `<title>: empty` mechanism) | `internal/ui/form.go:choose` |
| F3 on a folder | Cursor on `..` with no marks | The size of the **current** folder is calculated (`name == ".." → root = base`), the result is shown in the `..` row instead of `<DIR>`; the specification wrongly says "not `..`" | `internal/ui/folder_sizes.go:calculateFolderSizes, measureFolders` |
| F3 on a folder | The panel changed its path/source before the calculation finished | The result is silently discarded, the status stays empty | `internal/ui/folder_sizes.go:calculateFolderSizes` (apply: `if p.Path != base … return`) |
| F3 on a folder | Tree panel is active | Node names are relative paths; `filepath.Join(base, "a/b")` computes correctly; marks in the tree are taken into account | `internal/ui/folder_sizes.go:calculateFolderSizes` |
| Human-readable sizes | Toggling ⌘Shift+S | The panel footer is recalculated **immediately** (`drawPanel` reads `a.Settings.HumanReadableSizes`); only `InfoText`, which is not displayed anywhere, becomes stale. The specification claims the opposite | `internal/ui/panel_draw.go:drawPanel` |
| Quick view | Source cursor on `..` | The listing of the **parent** folder is shown (`Current()` = `Join(Path, "..")`) | `internal/ui/commands.go:updatePreview` |
| Quick view | Broken/cyclic symbolic link | `os.Stat` follows the link → error text (`too many levels of symbolic links` etc.); a link to a folder → folder listing | `internal/ui/commands.go:updatePreview` |
| Quick view | Helper process missing/crashed/exceeded 4 s | `preview.Run` returns an error → `Preview unavailable (unsupported, damaged or limit exceeded)` | `internal/preview/preview.go:Run` |
| Quick view | Esc while the preview is loading | `cancelReads` cancels the preview and writes `Preview cancelled`; the command/status are not cleared; the text stays until the cursor moves | `internal/ui/panel_load.go:cancelReads`, `commands.go:updatePreview` |
| Quick view | Content with control characters/binary | `Grid.Text` replaces control characters with a space; `\r` also becomes a space; a single long line is truncated to the width | `internal/ui/grid.go:Text` |
| Quick view / Info | Ctrl+Q/Ctrl+L when the mode was restored from setup.json and `viewReturns` is empty | Returns to the zero pair `{View:"", Hidden:false}`; an empty View is treated as Full | `internal/ui/far_keys.go:toggleOtherView` |
| Info panel | Source is a remote panel | `sysinfo.Read` is called for `RemoteLocalPath` (the terminal's local folder); the `Filesystem panel` section shows `Label()+Path` | `internal/ui/info_panel.go:updateInfoPanels` |
| Info panel | The source folder disappeared | `statfs` fails silently: a `Disk  ()` section with empty `Mount`/`Filesystem` and zero sizes | `internal/sysinfo/info_darwin.go:Read` |
| Info panel | No description file | `infoDescriptionPaths` = first candidate; F4 creates it, F3 puts an open error in the status | `internal/ui/info_panel.go:readInfoPanel, infoPanelKey` |
| Info panel | Description > 4096 bytes / truncated UTF-8 sequence | Read via `LimitReader(4096)`; invalid bytes are drawn as U+FFFD | `internal/ui/info_panel.go:readInfoPanel`, `grid.go:Text` |
| Tree panel | Root unavailable/not a folder/does not exist | `result.err` → footer with the error text, the list contains only `..`, the panel path = the unavailable one (no rollback, see P1-08) | `internal/ui/tree.go:scanTree, pollTree` |
| Tree panel | Marks | The footer in tree mode **never** shows `N selected`/`[filter]`: the `Err` field is occupied by the tree status line | `internal/ui/tree.go:pollTree`, `panel_draw.go:drawPanel` |
| Tree panel | matchOtherPanel from the tree | The other panel loads the tree root; the cursor does not match (the node name is a relative path `a/b`) | `internal/ui/archive_panel.go:matchOtherPanel` |
| Enter | File/folder disappeared | `os.Stat` → error in the status, the cursor does not move, the panel is not re-read | `internal/ui/launch.go:enterFile` |
| Enter | Executable file, shell busy | `execute()` → `Shell is busy; ⌘O to return to it` and the terminal is shown | `internal/ui/app.go:execute` |
| Enter | Symbolic link to a folder | The link path is loaded (not the target); `..` returns to the link's folder | `internal/ui/launch.go:enterFile`, `app.go:beginLoad` |
| Ctrl+PgDn | Cursor on `..` | `Dir == true` → `enterFile` → goes to the parent | `internal/ui/far_keys.go:farPanelKey` |
| Ctrl+PgUp | In `/` or in the root of a remote | `filepath.Dir("/") == "/"` → re-reads the same folder | `internal/ui/far_keys.go:farPanelKey` |
| Cmd+L | Remote panel, entering `~` or a relative path | `expand` substitutes the **local** home directory / resolves relative to the remote path; the result is sent to the server | `internal/ui/app.go:gotoPrompt, expand` |
| Cmd+L | Archive panel | A path inside the archive → archive directory; outside → leaves the archive into a local folder | `internal/ui/app.go:Load` |
| Remote | Every navigation | Every `Load` opens a new connection (`Remote.Dial`) and closes it after reading | `internal/ui/remote.go:loadRemote` |
| Remote | Server returned an unsafe name / > 100000 entries | The whole load fails with `server returned an unsafe filename` / `remote folder exceeds 100000 entries`, the panel rolls back | `internal/ui/remote.go:loadRemote` |
| Remote | Connection dropped during navigation | Asynchronous rollback to the previous listing, error in the footer; ⌘R retries | `internal/ui/panel_load.go:pollPanelLoad` |
| Archive | The archive file was modified/deleted while being browsed | `ArchiveIndex.Check` → `archive changed on disk; leave and reopen it` on the next navigation/F3 | `internal/fsops/archive_index.go:Check` |
| Archive | A file with an archive extension is corrupt | The `Opening archive` task ends with an error in the status (no dialog: `reportError` false), the panel does not change | `internal/ui/task.go:pollTask`, `archive_panel.go:openArchiveAt` |
| Archive | Upper-case extension (`.ZIP`) | `ArchiveFormat` lower-cases it | `internal/fsops/formats.go:ArchiveFormat` |
| Archive | Opening in a panel in Tree/Info/Quick view mode | The mode is forced to `Full` | `internal/ui/archive_panel.go:openArchiveAt` |
| Alt+F1/F2 | `/Volumes` is not readable | The `os.ReadDir` error is ignored: the list contains only `/` and the home directory | `internal/ui/panel_options.go:volumes` |
| Mouse | Click on Quick view | `panelItem` returns false → the panel is **not** activated (unlike the Info panel) | `internal/ui/mouse.go:panelItem`, `info_panel.go:infoPanelClick` |
| Mouse | Shift+click | Always marks the range (`markRange(…, true)`); a Shift+click cannot unmark | `internal/ui/mouse.go:panelMouse` |
| Mouse | Wheel over a Form/Menu | Moves the focus/selection by `delta` | `internal/ui/mouse.go:scrollMouse` |
| Status line | Long message | Truncated on the right to `Cols` without wrapping; long paths in errors are not fully available | `internal/ui/draw.go:Draw` |
| Names | Longer than the column / CJK / combining marks | Truncated without `…`; wide runes take 2 cells (`runewidth`); zero-width runes are glued to the previous cell | `internal/ui/grid.go:Text` |
| Names | Control characters (`\n`) | In file panels they are replaced with a space; in the Tree panel they are escaped (`\n`, `\uXXXX`) — the modes behave differently | `internal/ui/grid.go:Text`, `tree.go:treeDisplayName` |
| Geometry | Column width < 12 (narrow window + `panelSplit` 20%) | The metadata column is not drawn, names only; Brief at w=12 yields 1–2 characters of the name | `internal/ui/panel_draw.go:drawPanel` |
| Window title | Remote panel | `Update.Title` = the remote path without the server name | `internal/ui/app.go:Poll` |
| Selection | Quick view/Info/Tree active, `+`/`-`/`*` pressed (including Numpad) | `selectionKey` returns false → the character is appended to the command line | `internal/ui/selection_keys.go:selectionKey`, `app.go:key` |
| Sorting | Broken mask in `SortGroups` (from setup.json) | `matchesMask` ignores the `filepath.Match` error → the group never matches, no message | `internal/ui/panel_options.go:matchesMask, sortEntries` |
| Panels hidden | ⌘F1 when the right panel is already hidden | Both panels are hidden, `Active` does not change; keys keep acting on the invisible panel (see P1-01) | `internal/ui/archive_panel.go:togglePanel` |
| Panel settings | The `Show hidden files` form | Also resets `ForcedVisiblePath` of both panels | `internal/ui/settings.go:settingsMenu("Panel settings")` |
| Multiple windows | Cmd+N | The new window gets a JSON copy of the parent's `Settings` and `panelOptions`; `LoadSetup` is called only for a window without a parent | `internal/ui/windows.go:NewWindow` |
| Closing the window/Cmd+Q during a read | — | `Close()` cancels the panel loads, the tree, the preview, the operation; no confirmation (a panel read does not count as `Busy`) | `internal/ui/app.go:Close, RequestQuit` |
| Busy | Keys during a background task | Only Esc / Alt+C (cancel) and the question form's keys are accepted; left click on the progress `Cancel` | `internal/ui/app.go:key, click` |
| Session | Snapshot during a load/inside an archive | The cursor is taken from the pending state; for an archive `Cursor = Base(Archive.Path)`, `Top = ""` is saved | `internal/ui/session.go:windowSnapshot` |
| Fast find | Empty panel | The first character is accepted, the `Find` window appears, the cursor does not change | `internal/ui/fastfind.go:fastFindText, findPrefix` |
| ⌘C (copyPanelCommand) | Cursor on `..`, no marks | The string `..` is copied; for an empty panel the clipboard is not changed (unlike Ctrl+Insert, which copies the panel path) | `internal/ui/mouse.go:copyPanelCommand` |

## Appendix 2. File operations, archives, launching programs

| Function | Case | What the code does | Code (file:function) |
|---|---|---|---|
| F5 Copy | Disk full (ENOSPC), file disappeared, no read permission — on every file | For local→local every file goes through a separate `fsops.Transfer` → `Attempt("copy")` → `Error` dialog (`Cannot copy\n<src>\nTo: <dst>\n\n<err>`) Retry/Skip/Skip all/Cancel; the temporary `.farshmac-copy-*` is removed by `defer os.Remove`; on Skip the remaining files continue | `remotefs/transfer.go:Copy` (worker loop), `fsops/files.go:copyRegularStream` |
| F5 Copy | `fclonefileat` returned an error not in the ENOTSUP/EXDEV/EINVAL/ENOSYS/EPERM list (for example ENOSPC, EACCES) | The error is returned as is without attempting a streaming copy → `Error` dialog | `fsops/files.go:copyEntry` |
| F5 Copy | The source disappeared between the walk and the copy (`read attributes`) | `Attempt("read attributes")` → Skip skips the item, the walk continues (`IgnoreSkipped`) | `remotefs/transfer.go:Copy.walk` |
| F5 Copy | Error in one worker with `workers > 1` | After a `Cancel` answer the first error cancels the `ctx` of all workers via `fail()`; the other workers finish their current file (temp is removed) and exit; the status shows the first error with the suffix ` (copy <src> to <dst>)` | `remotefs/transfer.go:Copy.fail` |
| F5 Copy | File onto itself (both panels in the same folder, the suggested path) | `remotefs.Copy` for the same endpoint compares `path.Clean` and returns `destination is inside source` even for a file (the local `fsops.transfer` would return `source and destination are the same file`) | `remotefs/transfer.go:Copy` (loop over `pairs`) |
| F5 Copy | Hard links inside the copied tree (not move) | Copied as independent files (`copyHardlink` works only when `o.moving`) | `fsops/hardlinks.go:copyHardlink` |
| F5 Copy | remote → local, Permissions checkbox off | The file is created with `O_EXCL 0666` minus umask; `Chmod` is not called; the parent's local ACL is applied | `remotefs/local.go:Local.Create`, `transfer.go:portableMetadata` |
| F5 Copy | Progress: percentages/ETA | `o.Streaming()` until the walk ends, `o.Ready()` right after `close(queue)` — for a large tree the percentages appear when the walk is done while files are still being copied; for a single large file — almost immediately | `remotefs/transfer.go:Copy` |
| F5 from an archive | Progress | The extraction counters (`o.Add/Complete` by member names) and the subsequent copy are added together → `N items` in the window is roughly twice the real number | `fsops/formats.go:extractArchive`, `transfers.go:transferDialog` |
| F5 Copy | Enter on an archive in a **remote** panel | Does not open an archive panel: the file is downloaded to a temporary directory and opened by the internal viewer as a binary document | `launch.go:enterFile` → `remote_document.go:openRemoteDocument` |
| F6 Move | A directory is moved onto itself | The message `destination is inside source` (not "same file") — the `rel == "."` check fires before `SameFile` | `fsops/transfer.go:transfer` |
| F6 Move | Esc while merging a directory (same volume) | Already moved items remain in the destination, the source keeps the rest; `delete source` is not performed | `fsops/transfer.go:transfer` (the `si.IsDir() && di.IsDir()` branch) |
| F6 Move | Esc at the copy stage of a cross-volume move | The unfinished destination directory is removed entirely (`RemoveAll(dst)` in the `copyEntry` defer), the source is untouched | `fsops/files.go:copyEntry` |
| F6 Move | Esc at the `delete source` stage of a cross-volume move | The copy in the destination is complete, the source is partially deleted (interrupted `Remove`); there is no message about this, only `operation cancelled` | `fsops/transfer.go:moveAcrossVolumes` |
| F6 Move | The source is a symlink | The link itself is renamed; symlink → existing directory gives a collision dialog (`Existing: Folder`) and after `Overwrite` the error `cannot replace a file with a directory or a directory with a file` | `fsops/transfer.go:transfer` |
| Collision | Esc/empty name in the `Rename` form | Cancels the **whole** operation (`Resolution{Cancel}`) rather than returning to the collision dialog | `operation_questions.go:collisionQuestions` |
| Collision | Overwrite when the destination has changed | `destination changed during copy/move/append` → `Error` dialog; Retry runs `transfer` again → a new collision dialog with current data | `fsops/transfer.go`, `append.go` |
| F7 | Name `.`/`..`/`~`, absolute path | `expand` → `MkdirAll` on an existing directory is not an error; the panel goes to the parent of that directory and places the cursor; a name matching a file → `Cannot create folder … not a directory` Retry/Cancel | `app.go:function case 7` |
| F8/Shift+F8 | Symlink to a directory | The link itself is deleted/sent to the Trash (`Lstat`, no walk) | `fsops/progress.go:removeTreeStep`, `trash_darwin.go` |
| Shift+F8 | File with the `uchg` flag, file on a read-only volume | `os.Remove` → EPERM/EROFS → `Error` dialog Retry/Skip; flags are not bypassed (policy) | `fsops/progress.go:removeTreeStep` |
| Shift+F8 | Esc in the middle | Already deleted items are not restored; the parent directory remains | `fsops/progress.go:removeTreeStep` |
| Remote delete | The server returned an unsafe name in a subdirectory | `unsafe server filename` aborts the whole operation (not Skip) | `remotefs/transfer.go:removeTreeStep` |
| General | `Cmd+Q` / closing the window during an operation (including from another window) | `WindowGroup.advanceQuit`: `RequestQuit` sets the status `Wait for the file operation to finish`, the group quit is interrupted; other windows get `Finish the quit dialog in the other window`; the real `Close()` cancels the operation and waits for `cleanup` in a goroutine | `windows.go:advanceQuit`, `app.go:Close` |
| General | A worker question after cancellation | `operationChoice` checks `o.Check()` inside the queued closure — the dialog is not opened if the operation is already cancelled | `operation_questions.go:operationChoice` |
| Shift+Enter / Enter | Esc during `Opening <name>` | `exec.CommandContext` kills `/usr/bin/open`, but an already launched application is not closed | `launch.go:launch` |
| Enter | Association with a non-existent program; the external editor exited with an error | The command goes to the terminal; "command not found"/the exit code is visible only in the terminal, there is no dialog | `launch.go:launch` (`plan.Command`) |
| Enter | File without an extension and without the exec bit | `/usr/bin/open` → when there is no application `open` returns code 1 → `Opening <name> failed` dialog | `launch.go:enterFile` |
| Shift+F1 | Upper-case extension (`.ZIP`) | `ArchiveFormat` lower-cases it — a zip is created | `fsops/formats.go:ArchiveFormat` |
| Shift+F1 | Cancel | The temporary `.farshmac-zip-*`/`.farshmac-archive-*` is removed, bsdtar is killed with `SIGKILL` by process group | `fsops/formats.go:CreateArchive`, `archive.go:CreateZIP` |
| Shift+F2 | Destination — a non-existent parent | `MkdirTemp` error → `Error` Retry/Skip (Skip → `Done — 1 item(s) skipped`) | `fsops/formats.go:extractArchive` |
| Archive F3 | ZIP with a non-standard compression method (bzip2/lzma) | `withArchiveSelection` falls back to a full bsdtar pass | `fsops/archive_zip_selection.go` |
| Apply command | Empty/whitespace-only command | Nothing is executed, the dialog closes | `commands.go:applyCommand` |

## Appendix 3. Internal viewer and editor

| Function | Case | What the code does | Code (file:function) |
|---|---|---|---|
| F3/F4 open | File exactly 64 MiB / 64 MiB + 1 | `info.Size() > maxTextBytes` — exactly 64 MiB is read into memory (`LimitReader(maxTextBytes+1)`, bytes read = limit — OK); one byte more — paged hex | `document.go: openDocument` |
| F3/F4 open | FIFO, socket, device, folder via `Alt+F11` | `!info.Mode().IsRegular()` → `Choose a regular file`; a broken symbolic link → `lstat …: no such file or directory` from `EvalSymlinks` | `document.go: openDocument` |
| F3/F4 open | 0-byte file | `Detect` → `UTF-8` (an empty slice is valid), a buffer of one empty line; hex/dump draw nothing, the header is `Offset 0 / 0`; Go to `0` is allowed (`v >= max(1, Size)`), End = 0 | `document.go: openDocument, gotoDocument`; `document_keys.go: viewerKey` |
| F3/F4 open | UTF-16 without BOM | NUL is valid for `utf8.Valid` → `Codepage = UTF-8`, `binary = true` → the viewer in hex, the editor `Binary data: open with F3 in hex/dump mode`. The text is visible only via Shift+F8 → `UTF-16LE/BE` | `textfile/encoding.go: Detect`; `document.go: openDocument` |
| F3/F4 open | Invalid UTF-8 (a single "bad" byte in a UTF-8 file) | `Detect` → `Windows-1251` always; the charmap decoder never fails (undefined bytes → U+FFFD), the file is shown as Cyrillic "garbage"; for F4 the roundtrip does not match → `Encoding is not lossless; …` | `textfile/encoding.go: Detect, Decode`; `document.go: openDocument` |
| Saving | Mixed EOL after an edit | Byte-exact saving (`SavedRaw`) only if nothing changed; after any edit `Endings(Text, EOL)` unifies all line endings to the EOL detected at open (CRLF if there was at least one CRLF) | `document.go: saveDocument`; `textfile/encoding.go: Normalize, Endings, DetectEOL` |
| Saving | File without a trailing `\n` | Saved as is (Normalize adds nothing); `Lines()` yields the last line without `\n` | `textfile/buffer.go: New, Lines` |
| Saving | The file became 0444 after opening (folder writable) | `fsops.Save` compares only inode/size/mtime, not mode → temporary file + `chmod 0444` + `rename` — **the save succeeds**, permissions are preserved. A file owned by another user in a sticky-bit directory → `rename …: operation not permitted` | `fsops/files.go: Save` |
| Saving | Disk full | `copyProgress` → `write …: no space left on device`, the temporary file is removed by `defer os.Remove`, the target file is untouched, the buffer/Dirty are preserved | `fsops/files.go: Save`; `document_tasks.go: saveDocumentThen` |
| Saving | File with `uchg` / quarantine / hardlink / xattr | `uchg` → `rename …: operation not permitted` (the buffer is intact); the quarantine xattr **is lost** (new inode), i.e. saving removes the quarantine; the hardlink is broken; xattr/ACL are lost (see B P3-10) | `fsops/files.go: Save` |
| Saving (Async) | Esc after the actual `rename` but before apply | `applyCompleted = true` → apply runs anyway (`Saved`, `markSaved`), but `done` (close/quit) is skipped when `o.Check() != nil` | `document_tasks.go: saveDocumentThen`; `task.go: pollTask`; test `cancel_test.go: TestAsyncDocumentReadReplaceSaveAndCancel` |
| Saving | Paste/selectAll during a background task | `Handle` discards all messages except key/click/resize/quit/input while `Busy`; keys while `Busy` — only Esc/Alt+C; clicks — `click` while `Busy` → return. Therefore there is no "edit during `*d = *copy`" race | `app.go: Handle (line 546), key (632), click (1136)` |
| Save as | The active panel is remote | `base = p.filesystemPath()` = `RemoteLocalPath` (the panel's last local path); saving to the server via Save as is impossible (label `Local file name`) | `document.go: saveAs`; `archive_panel.go: filesystemPath` |
| Save as | The same file under a different spelling (relative path, `..`) | `path != d.Path` → `Lstat` → confirm `Overwrite` → `fsops.Save` with `SameFile` → succeeds; `d.Path` becomes the new spelling | `document.go: saveAs, saveDocument` |
| Shift+F4 | The path of an existing folder is entered | `os.Stat` OK → `openDocument(path, true)` → `Choose a regular file` | `document.go: newFile, openDocument` |
| Shift+F4 | New file, folder without write permission | Error only on save (`fsops.Create` → `open <dir>/.farshmac-new-…: permission denied`) | `document.go: newFile`; `fsops/files.go: Create` |
| Remote document | Connection dropped during download | Task error → status; `cleanup` removes the temporary directory; the document is not opened | `remote_document.go: openRemoteDocument` |
| Remote document | Connection drop/server error during save | `Dial`/`Lstat`/`remotefs.Copy` → error → status; the clone is discarded, the buffer and Dirty are intact, F2 retries. The upload goes to `.farshmac-part-<random>` + `Rename` — the replacement on the server is atomic; `ExpectedDestination` re-checks the target | `remote_document.go: saveRemoteDocument`; `remotefs/transfer.go: copyFile` |
| Remote document | No write permission on the server | There is no preliminary check; the error comes from the server at `Create(tmp)`/`Rename` (`550 …`, `permission denied`) and goes into the truncated status | `remote_document.go: saveRemoteDocument` |
| Alt+F11 history | Remote and archive documents | Only local opens (`openDocument`/`openDocumentAsync`) get into the history; `openRemoteDocument`/`viewArchiveMember` have their own apply without a record | `document_tasks.go: openDocumentAsync`; `remote_document.go`; `archive_panel.go` |
| Closing the window | The red window button / Cmd+W with unsaved documents | `windowShouldClose` → `{"Type":"close"}` → `WindowGroup.Handle` → `RequestQuit()` → the same dialog `Save changes` / `Save all`, `Discard`, `Cancel`; `App.Close` closes `File`, removes the `TempDir` of all documents | `window_darwin.m:150`; `windows.go:150`; `app.go: Close` |
| Cmd+Q | `Save all` ended with an error | `saveAllThen` breaks the chain, the quit is cancelled, the reason is only the truncated status | `document_tasks.go: saveAllThen`; `app.go: RequestQuit` |
| Text input | Pasting large blocks | There is no upper limit on the buffer size in the editor (it can be grown beyond 64 MiB and saved); an edit > 32 MiB clears the whole undo and cannot itself be undone; > 1 MiB disables highlighting | `textfile/buffer.go: Replace`; `document_syntax.go: pollSyntax` |
| Text input | Typing "past the end of the line" | There is no virtual space: `At(row, col)` clamps the column to the line length; in Overwrite at the end of the line characters are inserted (they do not overwrite `\n`) | `textfile/buffer.go: At, Insert` |
| Undo | Undo after saving | The stack is not cleared on `markSaved`; undo makes the document Dirty, and returning to text equal to `SavedText` clears Dirty (`modified()`) | `textfile/buffer.go: Undo`; `document.go: documentKey ("z")` |
| Bookmarks / Cmd+Z positions | After deleting/inserting lines | Offsets are not adjusted; `restore` clamps only `Cursor` by `len(Text)`; `Offset` is clamped at draw time, `ViewStart` — in `visualRows`. The bookmark points "somewhere" but does not crash | `document.go: restore`; `document_draw.go: drawDoc (157), visualRows` |
| Search | Regex with an empty match (`^`, `a*`) | `Matches` returns zero-length matches; `Find` with an empty selection starts from `Next(start)` → Shift+F7 advances through positions; `Replace all` inserts the replacement at every position (`Replaced N matches`) | `textfile/buffer.go: Find, ReplaceAll` |
| Search | Empty string; `Hex bytes` in dump | `enter search text`; in dump mode `Hex bytes` is off by default (only `Mode == "hex"`), a hex search from dump switches to hex | `textfile/buffer.go: Compile`; `document.go: searchDocument, findDocument` |
| Hex search | Odd-length/non-hex string, `0x` prefix | `hex.DecodeString` after removing spaces → `Enter pairs of hex digits, e.g. 48 65 6c 6c 6f` (the `0x` prefix is not supported — same error) | `document.go: searchDocument` |
| Encodings | BOM with a single-byte encoding | `Encode` writes a BOM only for UTF-8/UTF-16; F9 `Write BOM` for `Windows-1251` and the like silently has no effect; when switching UTF-8+BOM → 1251 the BOM is lost without a message | `textfile/encoding.go: Encode` |
| Encodings | Saving as UTF-16 without BOM | On the next open the file is detected as UTF-8 with NUL → "binary", hex mode | `textfile/encoding.go: Detect` |
| Encodings | F8 in the editor vs Shift+F8 | F8 checks `Encode(d.Text, …)` without `Endings`, Shift+F8 — with `Endings`; this does not affect the result (`\r` is encodable everywhere) | `document.go: documentKey (F8), chooseCodepage` |
| External programs | `/usr/bin/open` returned an error | `workProgress("Opening <name>")` → status `<err>: <open output>` (for example `exit status 1: Unable to …`) | `launch.go: launch` |
| External programs | Command not found / shell busy / no shell | A non-empty command goes to the shell: "not found" is visible only in the terminal, the application does not know about it; shell busy → `Shell is busy; ⌘O to return to it` + the terminal is shown; `Term == nil` → `Shell unavailable`. After external editing the file in the panel is not re-read automatically | `launch.go: launch, submitCommand`; `app.go: execute` |
| Highlighting | Helper crashed / non-zero exit / hung | `cmd.Run` error (including kill by the 2 s timeout via `CommandContext`) → `syntax paused (limit/error)`; no retry is made until the text/path changes (`attempted = true`) | `highlight/highlight.go: Run`; `document_syntax.go: pollSyntax` |
| Highlighting | No free slot (2 helpers for all windows) | `Run` blocks in `select` on `slots` without a timeout until a slot frees up or the ctx is cancelled (Esc/text change/close) | `highlight/highlight.go: Run` |
| Highlighting | Language not detected | `lexers.Match`/`Analyse` → nil → empty `Result`, empty status (no `[…]`) | `highlight/highlight.go: Parse` |
| Highlighting | Theme change during the task | Colors are substituted on every `Draw` across the whole grid; the highlighting task is not restarted | `draw.go: Draw` |
| UI | Narrow window | `Cols` is clamped to 60..300, `Rows` to 15..120; key bar cells are `W/10` → at 60 columns the labels are truncated (`9Optio`, `8Codep`); the title is truncated with `g.Text(…, g.W)`; the status message — up to `min(32, W/3)` runes | `app.go: Handle (resize)`; `document_draw.go: drawDoc` |
| UI | Scrollbar with 1 line / click in column W-1 without a scrollbar | The scrollbar is drawn only when `lines > height`; but a click at `X == Cols-1` with `Scrollbar == true` is always treated as a scrollbar drag (`Offset = 0`, `ManualScroll = true`) even if there is no scrollbar | `document_draw.go: drawDoc, documentClick` |
| UI | Click below the last line / in hex / right button | `row >= len(rows)` → ignored (the cursor is not placed at the end); hex/dump — ignored (except the key bar); right button with a document open — ignored | `document_draw.go: documentClick`; `app.go: click (1128)` |
| Help (F1) | `+`/`-`, Cmd+L, F12 in the help | `+`/`-` replace the help document with the adjacent panel file; Cmd+L sets `RO` (pointless but harmless); in `Screens` the help appears as `View Viewer / editor help` | `document_keys.go: adjacentFile`; `document.go: screenMenu` |
| Multiple documents | The same file opened twice | Duplicates are not merged; the second screen's `Info` becomes stale after the first is saved → `file changed on disk; reopen it before saving` (see B P3-04) | `document.go: showDocument, saveDocument` |

## Appendix 4. Terminal, command line, windows, native shell, renderer

| Function | Case | What the code does | Code (file:function) |
| --- | --- | --- | --- |
| Command line input (paste) | Pasting multi-line text | Control characters (including `\n`) are replaced with a space BEFORE `ReplaceAll("\n","")`, i.e. the lines are joined with a space rather than the line breaks being removed; the second step is dead code. | `app.go:Handle` case `"paste"` |
| Command line input (paste) | Pasting while `Busy` | The `paste` message is silently discarded (the gate `a.Busy && m.Type != key/click/resize/quit/input`). | `app.go:Handle` |
| Command history Ctrl+E/X | Empty history | Ctrl+E — no-op; Ctrl+X clears `Command` (`a.Command = ""` before the index check). ⌘↑/Ctrl+↑ with an empty history change nothing. | `far_keys.go` case `"e","x"`; `app.go:key` |
| Command history | Changing `HistoryLimit` in settings | The existing history is not truncated until the next `appendUnique` (on the next command execution). | `settings.go` Terminal settings; `commands.go:appendUnique` |
| Command history | setup.json write error (disk/permissions) | `saveSetup` sets `Setup saved`, then `a.error(err)` overwrites the status with the error text; the history remains only in memory. | `settings.go:saveSetup` |
| Completion ⌘Space | Very many matches | No limit: the `Complete filename` list form receives all matches (thousands of entries in a large directory). | `completion.go:applyCompletion`, `form.go:choose` |
| Insert name ⌘Enter | Cursor on `..` | The literal `.. ` is inserted (the name passes the `[A-Za-z0-9_./-]` filter, no quotes). | `app.go:insertFilename` |
| Command execution | Command longer than 1 MiB | `Session.request` returns `invalid panel command or directory` (the same message as for NUL/non-absolute cwd); the command stays in the line. | `session.go:request` |
| Command execution | Enter in the terminal with an unfinished line (PS2: `echo "` + Enter) | `input` with `\r` sets `ShellReady=false`; preexec/precmd do not fire, so until completion/Ctrl+C a panel Enter gives `Shell is busy; ⌘O to return to it`, the footer — `⌘.  Interrupt`. Ctrl+C returns the prompt → Ready. | `app.go:Handle` case `"input"` |
| Command execution | Input during the restart pause (`Term == nil`) | `input`/`inputBinary` are silently ignored (the pressed keys are lost); the footer status is `Shell exited; restarting…`. | `app.go:Handle` |
| Readiness protocol | `$PWD` contains `;` | `SplitN(…, ";", 2)` — a cwd with `;` is parsed correctly. A BEL character (0x07) in a directory name would cut the marker (the cwd gets truncated, the tail ends up in the output) — not handled, but practically never occurs. | `session.go:parser.feed` |
| Readiness protocol | cwd is a symlink (`cd /tmp` → `$PWD=/tmp`, panel `/private/tmp`) | The string comparison `target == a.ShellCwd` misses → an extra `ChangeDir` on every ⌘O (harmless; PWD becomes `/private/tmp`). | `app.go:syncShellCwd` |
| Readiness protocol | Shell killed externally (`kill -9`) | PTY read → `EIO` → `Event{Exit:true, Err:nil}` → `Shell exited; restarting…` (as for a normal `exit`). | `session.go:read` |
| Readiness protocol | Shell does not read stdin (SIGSTOP, XOFF/Ctrl+S) | The `input` queue (32 buffers) overflows → status `terminal input queue is full`; the writer goroutine blocks in `f.Write` until the PTY is closed. | `session.go:Write`, `StartConfigured` writer goroutine |
| Readiness protocol | `Session.Write` after `Close` | The `select` between `s.input <-` (a buffered channel) and `<-s.done` is chosen randomly — a write may "successfully" land in the buffer and be lost; in `App` after `Close` it is always `Term=nil`, so this does not manifest. | `session.go:Write` |
| Terminal start | `/bin/<shell>` does not exist/is not executable | The path is hard-coded `/bin/bash|sh|zsh`; a `pty.StartWithSize` error → `Shell: fork/exec …`, the temp directory is removed. No retry (see P4-03). | `session.go:StartConfigured`; `native_darwin.go:Run`; `windows.go:NewWindow`; `session.go:EnableSession` |
| Terminal start | Hooks of other tools (starship, direnv, nvm) in a custom prompt | bash: the user's `PROMPT_COMMAND` runs inside `_farshmac_before_prompt` before the marker, `$?` is preserved in `farshmac_status` before eval; zsh: `_farshmac_precmd` is appended to the end of `precmd_functions` after `.zshrc`, i.e. it runs after the user's hooks. A multi-line PS1 in bash is printed twice on directoryKey (accepting an empty line to recompute PS1). | `bourne.go`, `session.go` rc |
| Terminal start | cwd longer than `PATH_MAX` / not enterable with `cd` | The panel cannot be in such a directory; on `Run` the shell does `builtin cd` → failure → `Cannot enter directory: …` (zle -M) / bash: only the status marker 1, no message in the terminal. | `session.go` rc `_farshmac_panel_command`; `bourne.go` |
| Shell restart | While `Busy` (file operation) or with a document/dialog open | `Poll` does not depend on `Busy`/document: Exit is processed, `[Restarting shell]` is appended, `a.Status` is overwritten (`Shell exited; restarting…`) even with a form open. | `app.go:Poll`, `shell_restart.go` |
| Shell restart | Exit while the `Quit` / `A command is running. Close the application?` dialog is open | The dialog stays although the command is gone; `No` → the shell restarts, `Yes` → `Quit=true`, `restartShellIfDue` does not start the shell. | `app.go:confirmQuitShell`, `shell_restart.go` |
| Shell → panel sync | The shell entered a directory the panel cannot access (`x` without `r`) | `Panel.Load` fails in `fsops.ReadDirectory` before `beginLoad` → the panel does not change, the error is in `a.Status`. | `app.go:syncCwd`, `finishPanelReturn`; `app.go:Panel.Load` |
| Scrollback/spooling | Temp directory unavailable/full | `NewHistory` or `Append` → `historyStatus.Error` forever for this window (footer `History: <err> · …`, `archive.enabled=false`); no recovery, already written pages are readable. | `terminal_history.go:appendTerminalHistory`, `history.go:Append` |
| Scrollback/spooling | A `.rows/.idx` segment deleted externally | `Append` keeps writing to the open (unlinked) fd — the bytes are counted, the lines are unavailable; `Page` returns `open …: no such file or directory` in the overlay caption. There is no detection. | `history.go:Append`, `Page`; `history_view.js:update` |
| Scrollback/spooling | `History writer is overloaded` | Practically unreachable: frames are sent only when `len(queue) < 2`, so `len(queue) >= 8` is impossible; but the error state is permanent. | `terminal_history.go:terminalCanDrain` |
| Keyboard in the terminal | Option+letter (dead keys, `€`, `ß`, `~` on some layouts) | `macOptionIsMeta: true` — Option is always an ESC prefix; characters cannot be entered via Option in the terminal (only via IME/paste). | `app.js` `term` options |
| Keyboard in the terminal | Fn/media keys with "Use F1, F2… as standard function keys" turned off | The system media functions intercept the key; xterm receives F1–F12 only with Fn held (README). There is no code. | — |
| Keyboard in the terminal | Caps Lock | Go compares with `EqualFold`/`ToLower` for ⌘O/N/Q/E/X/Y; the renderer — `e.key.toLowerCase()`; character input into the command line — as is. | `app.go:key`, `far_keys.go`, `app.js` |
| Copy/paste | ⌘V with non-text clipboard content (file, image) | `stringForType:NSPasteboardTypeString` → nil → `return` without a message. | `window_darwin.m:pasteText:` |
| Copy/paste | Pasting > 1 MiB into the terminal | xterm `paste` (bracketed) → a single `input` → `Session.Write` → status `paste exceeds 1 MiB`; the text is not pasted at all (no partial paste). | `app.js:farshmacPaste`; `session.go:Write` |
| Mouse in the terminal | Control+click | Generates `contextmenu` → the `.clipboard-menu` is shown (if there is no mouse tracking); xterm with `rightClickSelectsWord` selects the word. | `app.js` `contextmenu` |
| Renderer | WebKit web process crash | `webViewWebContentProcessDidTerminate` → `reload`; the shell keeps running, but the xterm screen content and the resident scrollback are lost (the screen is empty until the next output/Ctrl+L); the on-disk archive is intact, `history.total` comes from `Update`. | `window_darwin.m` |
| New window | ⌘N with 10+ windows | There is no limit (the limit of 100 applies only to session restore). The cascade point `(100+24*n, 850-24*n)` goes off-screen at n > 35 — Cocoa's `cascadeTopLeftFromPoint` adjusts it itself. | `windows.go:NewWindow`, `window_darwin.m:open` |
| Session | Panel on a remote server | `filesystemPath()` = `RemoteLocalPath` is written to `session.json`; after a restart the window opens on the local path, the connection is not restored. | `session.go:windowSnapshot` |
| Session | Corrupt `session.json` | Besides the status `cannot read window session: …`, the file is overwritten with default values on the very first `Poll` (≤ 500 ms), no backup copy is made. | `windows.go:Poll` → `SaveSession` |
| Quit ⌘Q | Cancel at any step | `quitting=false`, `approved` is not cleared until the next `BeginQuit`, where it is created anew → on a repeated ⌘Q all windows are asked again (including previously approved ones). | `windows.go:BeginQuit`, `advanceQuit` |
| Native menu | Edit → Copy/Cut/Paste/Select All with a dialog/form open | Always enabled (`validateMenuItem` returns YES); the action goes to `copyDocument`/`formPaste` etc. Services and Zoom are absent from the menu; `Minimize` is the standard `performMiniaturize:` through the responder chain. | `window_darwin.m:validateMenuItem`, `startWindows` |
| Full Disk Access | Granted without a restart | TCC applies to new processes; the code does not check access and does not re-read the panels; the dialog text says "quit and reopen". | `disk_access_darwin.m` |

## Appendix 5. F9 menu, dialogs, settings, network connections, help

| Function | Case | What the code does | Code (file:function) |
| --- | --- | --- | --- |
| Main menu F9 | `Shift+F10` when no item has been executed yet | `lastMenu` is a zero `MenuState` → the `Left` section opens, row 0 | `app.go:key` (`F10 && Shift`) |
| Main menu F9 | `F12`, `Alt+F1/F2/F7/F8/F10`, `Shift+F4` with the menu open | Open a list/form on top of the menu; the form receives keys first; after it closes the menu stays open | `app.go:key` (the checks come before `if a.Menu != nil`) |
| Main menu F9 | `F1`, `⌘1…9`, printable keys with the menu open | Go to `menuKey` and are ignored (not in the `switch`); help via `F1` is not available from the menu | `menu.go:menuKey` |
| Main menu F9 | Long item label | Truncated to `w-4-len(shortcut)` columns; the shortcut is drawn in the fixed 9 rightmost columns | `menu.go:drawMenu` |
| Main menu F9 | Click outside the frame / on a separator / on another section's title | Outside the frame — close; on a separator — nothing; on a title in row 0 — switch section (handled in `click` before `menuClick`) | `menu.go:menuClick`, `app.go:click` |
| Main menu F9 | `PageUp/PageDown` | Step `Rows-5`, then the cursor "jumps over" the separator | `menu.go:menuKey` |
| Main menu F9 | Opening with a document/form/dialog/Busy/terminal | Unreachable: `key` routes to `Form`/`Dialog`/`Doc`/`Busy` earlier; a `click` on row 0 is blocked by `a.Doc != nil`; a mouse click while `Busy`/`Doc` is rejected | `app.go:key`, `app.go:click` |
| Form (fields) | `Enter` on a toggle field or in any text field | Submits the form (`Submit`), does not toggle the checkbox | `form.go:formKey` |
| Form (fields) | `Esc` with modified fields | Closes without asking, the changes are lost | `form.go:formKey` |
| Form / Dialog | Mouse click outside the window | Ignored, the window stays (no close on outside click) | `form.go:formClick`, `draw.go:promptClick` |
| Form (fields) | `Alt+letter` not matching a mnemonic | `App.key` normalizes `m.Key` to a Latin letter by `m.Code` → the Latin letter is inserted into the field; Option characters (`ø`, `ß`, `€`) cannot be typed into a form/dialog field | `app.go:key` (first line), `form.go:formKey` |
| Form (fields) | `⌘C`/`⌘X` in a text field | Copies/cuts the **entire** field value to the system clipboard (ignoring the selection); in a Dialog — the whole `Input` | `document.go:copyDocument` |
| Form (fields) | Very long value | The field shows a window around the caret (`editWindow`); there is no word-wise scrolling | `modal.go:editWindow`, `modalInput` |
| Form (fields) | Fields do not fit in height | Scrolling follows the focus, `▲` indicator; separators outside the visible area are not drawn | `form.go:formGeometry`, `drawForm` |
| Form (list) | More than 26 items / repeated first letters | Accelerators are assigned to the first letter not yet taken; when the letters run out — an item without an accelerator (`key == 0`) | `modal.go:mnemonicKeys` |
| Form (list) | Click on a row | A single click selects immediately (`Enter`), a double click is not needed | `form.go:formClick` |
| Form (confirm) | Text wrapping | By rune count, not by words and not by CJK width | `form.go:confirmationLayout` |
| Dialog (prompt) | Message longer than 3 lines | Truncated silently; wrapped by characters | `draw.go:drawDialog`, `promptBounds` |
| Dialog (prompt) | Pasting multi-line text | Control characters → space (line breaks also become spaces) | `app.go:Handle` (`paste`) |
| Make folder (F7) | Empty/whitespace-only name | Silently ignored, no message | `app.go:function(7)` |
| General settings | `Font size` = `+14`, ` 14` | `strconv.Atoi` accepts `+14`, rejects spaces (`Font size must be 10–24`) | `settings.go:settingsMenu` |
| Terminal settings | `Shell` with spaces; decreasing `HistoryLimit` | `TrimSpace`; the history is not trimmed immediately, only on the next `appendUnique` | `settings.go`, `commands.go:appendUnique` |
| Editor settings | `Tab width` outside 1–16 in setup.json | `normalizeSettings` resets it to 4 (not clamped, unlike FontSize/HistoryLimit/TreeDepth, which are clamped) | `settings.go:normalizeSettings` |
| All settings | Missing fields in setup.json | `Unmarshal` on top of `defaultSettings()` → missing fields get defaults rather than zeros | `settings.go:LoadSetup` |
| All settings | A string instead of a number / an array >10 for `Shortcuts` | `json.Unmarshal` returns an error → **all** settings and histories are replaced with defaults, silently (see P5-04) | `settings.go:LoadSetup` |
| Colors / Code pages | Unknown `ColorTheme`/`DefaultCodepage` value in setup.json | Not normalized: `Draw` applies the palette only for `Dark`/`Light` (otherwise classic); `textfile.codec` returns `nil` for an unknown name | `draw.go:Draw`, `textfile/encoding.go:codec` |
| General settings | Applying `FontSize`/`ColorTheme`/`ShowKeys` | Immediately in this window (`Update.FontSize` on every `Poll`, the palette — on every `Draw`); other windows are not affected | `app.go:Poll`, `draw.go:Draw` |
| New window (⌘N) | Copying settings | `Settings` are serialized to JSON and deserialized into the new window → secrets (`json:"-"`) are not copied; `dialogValues` and histories are not copied | `windows.go:NewWindow` |
| Save setup | The configuration directory cannot be created / the temporary file cannot be written | Status = the OS error text (`a.error` on top of `Setup saved`); `saveFolderShortcut` checks `Status == "Setup saved"` | `settings.go:saveSetup`, `folder_shortcuts.go` |
| Save setup | `DescriptionFile` = `.`/`..` | They pass `filepath.Base(s) == s` and are saved; writing `descript.ion` later fails with a rename error | `settings.go` (`File descriptions`, `normalizeSettings`) |
| Network connections | URL without a scheme, `host:22`, `user@host` | `url.Parse` yields an empty scheme → `use sftp://host, ftp://host or ftps://host` | `remotefs/fs.go:Parse` |
| Network connections | A space inside the URL, `@` in the user name inside the URL, `sftp://::1` without brackets | `url.Parse` → `invalid server address`; IPv6 in brackets (`sftp://[::1]:22/`) is supported via `JoinHostPort` | `remotefs/fs.go:Parse`, `Address` |
| Network connections | Key without 0600 permissions | Not checked (unlike OpenSSH); any file ≤ 1 MiB is read | `remotefs/ssh_defaults.go:readPrivateKey` |
| Network connections | `SSH_AUTH_SOCK` not set / agent unavailable | Silently skipped; when no other methods exist — `no usable SSH identity…` | `remotefs/sftp.go:dialSFTP` |
| Network connections | known_hosts without read permission | `os.Stat` succeeds → `knownhosts.New` returns an open error → to the status, no dialog | `remotefs/sftp.go:HostKeyCallback` |
| Network connections | Several host keys in known_hosts | Algorithms of already known types are preferred (`hostKeyAlgorithms`), RSA is expanded to `rsa-sha2-256/512` | `remotefs/ssh_defaults.go` |
| Remote panel | The server went down after connecting | Every load is a new `Dial`; with `Async` the error goes into `p.Err` and is drawn in the panel status line, the panel content rolls back to the previous snapshot | `panel_load.go:pollPanelLoad`, `panel_draw.go` |
| FTP | Transfer mode | Passive only (the library's EPSV/PASV); there is no active mode | `remotefs/ftp.go:dialFTPWithTLS` |
| FTP | Password with spaces/non-ASCII | Sent as is; only `\x00\r\n` are rejected | `remotefs/ftp.go` |
| Change volume… | The panel is remote/archive | `Remote = nil` without `cancelPanelLoad`/`cancelTree`/resetting `states`/`RemoteLocalPath`; there is no race, since `loadPanel` itself cancels the previous load | `panel_options.go:volumes` |
| Disconnect active panel | The panel is not remote | Silently does nothing | `remote.go:disconnectRemote` |
| Profiles | connections.json is corrupt | `loadConnectionProfiles` silently keeps the profiles from setup.json; `saveConnectionProfile` returns an error → `; profile not saved: <err>` on every connection | `connection_profiles.go` |
| Help F1 | Inside a document | `F1` in the Help document — no-op; in a regular document — `Viewer / editor help`; `F9` in a document opens `documentSettings`, not the main menu | `document.go:documentKey` |
| Status line | Truncation / overwriting | Truncated to the width without "…"; overwritten by `backgroundTask` (label), `pollTask` (`""`), by the shell readiness event (`Exit N · ⌘O panels / terminal`) even in panels mode | `draw.go:Draw`, `task.go`, `app.go:Poll` |
