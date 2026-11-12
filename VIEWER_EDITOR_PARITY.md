# Viewer/editor: FarshMac 0.10 port audit

November 7, 2026. This is a substantial expansion of the earlier prototype,
**not a claim of complete one-to-one compatibility**. The C++ implementation is
not bundled; behavior is reimplemented in Go. The HTML text area was removed:
both viewer and editor use a character grid backed by a Go text model.

References: official [help](https://github.com/FarGroup/FarManager/blob/master/far/FarEng.hlf.m4)
(`Viewer`, `ViewerMode`, `ViewerSearch`, `Editor`, `EditorSearch`),
[viewer.cpp](https://github.com/FarGroup/FarManager/blob/master/far/viewer.cpp),
[editor.cpp](https://github.com/FarGroup/FarManager/blob/master/far/editor.cpp), and
[fileedit.cpp](https://github.com/FarGroup/FarManager/blob/master/far/fileedit.cpp).
This is a historical audit; see [CHANGELOG.md](CHANGELOG.md) for later changes.

## Deliberate macOS adaptations

- Backspace in panels only deletes command text. Use `..` to go up.
- F10 does nothing in panels. In a document, F10/Esc closes it and returns to the
  preceding document or panels, leaving the application window open.
- ⌘Q quits after checking all documents for unsaved changes.
- Primary shortcuts use ⌘: C/X/V for the system clipboard, F for search, H for
  replace, G for goto, S for save and R for reload. Ctrl works as ⌘ in the
  viewer/editor, which matters for external keyboards.
- Bookmarks are set with ⌘⌥0…9 (Far's Ctrl+Shift+digit, which also works) and
  restored with ⌘0…9 or Ctrl+0…9. ⌘⇧digit is not used because macOS takes
  ⌘⇧3/4/5 for screenshots. Bookmark digits use the physical key on every layout.
- F9 opens document settings in a document and the main menu in panels.

## Implemented features

| Feature | Controls / behavior |
| --- | --- |
| Text viewing | Vertical/horizontal scrolling, PgUp/PgDn, Home/End; ⌘←/→ moves 20 columns. |
| Text / Hex / Dump | F4 toggles Text/Hex; Shift+F4 selects a mode. Hex displays offsets, bytes, and ASCII; Dump uses the selected single-byte encoding. |
| Wrapping | F2 toggles wrapping; Shift+F2 toggles word/character wrap and enables wrap. |
| Large binary files | Files over 64 MiB use paged ReadAt without loading the whole file. |
| Damaged text | Text that fails to decode (for example truncated UTF-16) opens as hex in the viewer; the editor refuses it and suggests F3. |
| Search | F7 / ⌘F: text or hex bytes (hex search is viewer-only), case, whole words, regex, direction. Shift+F7 or Space finds next; Alt+F7 finds previous. |
| Replace | ⌘F7 / ⌘H: confirm individual matches or Replace all. Regex replacement supports `$1`, `$2`, etc. Replace all is one undo operation. |
| Goto | Alt+F8 / ⌘G: one-based line:column or percentage; Hex/Dump accepts byte offsets, including `0x…`. |
| Encodings | Shift+F8: UTF-8, UTF-16LE/BE, Windows-1251/1252, CP866, ISO-8859-1, KOI8-R. F8 toggles CP866/1251 (from any other code page the first press selects CP866, as Far's DOS/Win). On paged large files F8 changes only the code page; a viewer holding unsaved edits reports "Save edited text before reinterpreting bytes". |
| Encoding on save | Viewer reinterprets original bytes; editor selects output encoding. Unrepresentable characters prevent saving. F9 selects BOM and LF/CRLF/CR. |
| Viewer ↔ editor | F6 preserves text, position, and dirty state. Encoding failure does not display stale content. |
| Bookmarks | ⌘⌥0…9 (or Ctrl+Shift+0…9) sets; ⌘0…9 jumps; scoped to the open document. |
| Viewer position undo | ⌘Z restores the position before search, goto, or a bookmark jump. |
| Adjacent files | `+` / `-` in the viewer only (in the editor they are ordinary input); always opens the internal viewer. Unsaved changes require a decision before switching. |
| Alternate / internal | Alt+F3 / Alt+F4 open the alternate viewer/editor (external if internal is the default, and vice versa); ⌘⇧F3 / ⌘⇧F4 force the internal one. Both are also in the Files menu. |
| View/edit history | Alt+F11 (Commands → File view/edit history) opens the internal viewer/editor directly. The history is saved with the setup; positions are not. Local files only. |
| Editor input/navigation | Arrows, pages, words, line/file boundaries; Insert toggles insert/overwrite; Enter preserves indentation. |
| Selection | Shift+arrows selects a normal block; Alt+Shift+arrows selects a rectangle; mouse supports normal selection. |
| Clipboard | ⌘C/X/V, Ctrl+Insert copy, Shift+Delete cut; without selection, editor copies/cuts the current line. ⌘U clears the selection, ⌘D deletes it. Double/triple click selects a word/line; dragging the scrollbar scrolls. Dialog fields handle clipboard separately without changing the document. |
| Highlighting | Chroma 2.20.0 in both viewer and editor: filename/content detection, multiline constructs, cancellable background processes, 1 MiB / 2-second limits; the title shows the detected language. Not FarColorer/HRC. |
| Indentation settings | F9 has eight fields: code page, line endings, BOM and read-only belong to the document; Syntax, Auto indent, Tab width (1–16) and Expand tabs change the application settings. Enter respects cursor/selection; Tab/Shift+Tab preserves block selection. |
| Light theme | Bright greens, greys and pinks are replaced with darker variants on the light background. |
| Undo/redo | ⌘Z / ⌘Shift+Z; Go edit history capped at 32 MiB / 10,000 actions. An over-budget edit clears history and cannot be undone. A new edit after undo discards redo. |
| Deletion/indentation | ⌘Y deletes line, ⌘K deletes to line end, ⌘Backspace/Delete deletes word; Alt+U/I unindents/indents. |
| Save | F2 / ⌘S; Shift+F2 / ⌘Shift+S opens Save As; existing targets require confirmation. Saving through a symbolic link (local or remote) writes the link target. Saves keep xattrs, ACLs and hidden/nodump flags; partial loss reports "Saved; some attributes were not preserved". New files are created with mode 0644. |
| Save problems | "Save failed" offers Retry / Save as… / Cancel; "File changed on disk" offers Overwrite; "File was deleted" offers Create; in an unwritable folder "Save in place" rewrites the file non-atomically and keeps hard links. A file that probably cannot be saved asks View / Edit anyway / Cancel when opened (or when switching from the viewer with F6). |
| Reload | ⌘R rereads the file into the same screen, asking first if there are unsaved changes. |
| Remote files | F3/F4 on an FTP/SFTP panel download the file; F2 uploads it back, asking when the server copy changed ("remote file changed"). Save as writes a local file only; there is no Save as to the server. |
| Archive members | Read-only viewer only: F6, `+`/`-` and reload are refused with a hint to copy the file out with F5. |
| New file | Shift+F4 from panels/editor; created on save. |
| Multiple documents | F12 selects open documents; F1 opens help as another screen. Closing restores the previous screen. |
| Read-only/status | ⌘L toggles read-only; ⌘B key bar; ⌘Shift+B status; ⌘S in viewer toggles scrollbar. |
| Screens | Ctrl+Tab / Ctrl+Shift+Tab cycle screens; F12 lists them. |
| Closing | Esc/F10/F4 in editor, Esc/F10/F3 in viewer; Shift+F10 saves and closes editor. Every close checks for changes. |

## Remaining parity gaps

1. **Large text:** text decoding/editing is limited to 64 MiB. There is no streaming
   text viewer, search over larger files, or automatic following of growing files.
2. **Search:** Go/RE2 differs from Far's regex dialect: no lookaround, pattern
   backreferences, fuzzy search, or Preserve style replacement. Multiline pattern
   behavior also differs.
3. **Encodings/lines:** eight encodings; detection without BOM is heuristic.
   Unmodified files save byte-for-byte. After editing, mixed line endings normalize
   to the selected EOL rather than Far's full per-line model. Dump does not fully
   reproduce the original's multibyte encodings.
4. **Blocks:** persistent-block operations (copy/move within the document, append
   to clipboard), rectangular clipboard metadata, and virtual columns beyond line
   end are incomplete. Rectangles with tabs count characters, not display columns.
5. **Undo/cursor:** input undo follows events; continuous typing groups differ
   from Far. Fixed-cursor scrolling, deletion combinations, and exact preferred
   column semantics are incomplete.
6. **Unicode/input:** ordinary Cyrillic and wide characters work in the model;
   complex emoji/ZWJ, all grapheme clusters, and IME lack end-to-end confirmation.
7. **State:** bookmarks and screen lists are not retained after closing. The
   view/edit history is persistent, but reopening positions, all Far settings, and
   per-field contextual help are absent.
8. **Extensions:** no viewer/editor plugin API, F11, Lua macros, FarColorer/HRC/HRD,
   search-result filter, or custom keyboard macros. UI alone does not establish
   that these features have been ported.
9. **Saving:** external-change protection checks inode/mtime/size without locking
   other processes. Atomic replacement keeps xattrs, ACLs and hidden/nodump flags
   but not backup copies or hardlink topology (use Save in place for hard links).
   Other open copies of the same file are not synchronized after a save.

## Validation

`go test -race ./...`, `go vet ./...`, and JavaScript syntax checks cover:

- Backspace/F10/⌘Q and safe closing of hidden documents;
- Unicode paste, selection, overwrite, undo/redo, and rectangular blocks;
- forward/backward search, whole words, regex groups, Replace all, and undo;
- encoding round trips, BOM/CRLF, and rejection of lossy saves;
- Save As without implicit overwrite, new files, and external modifications;
- hex search, paged reading of large sparse files, wrapping, goto, and bookmarks;
- cut/paste, model-level mouse selection, and clipboard in modal dialogs;
- data-loss prevention during F6, adjacent-file navigation, and closing screens.

These validate the model and build, not Cocoa visual/keyboard parity. At the time
of this audit, application automation returned `Invalid app`. Intel builds were
produced without a separate Intel runtime test. Full one-to-one parity remains
unfinished.
