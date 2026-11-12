package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"farshmac/internal/remotefs"
)

// bookmark is one of the ten window-scoped bookmarks 0…9. A folder bookmark
// remembers a panel folder (local, remote or inside an archive) and the entry
// under the cursor; a document bookmark remembers a file open in the viewer
// or editor and the position in it. Bookmarks belong to one window instance:
// they are not shared between windows and are not saved.
type bookmark struct {
	Kind    string           // "folder" or "document"
	Path    string           // Folder, or the document file.
	Cursor  string           // Folder: entry under the cursor.
	Remote  *remotefs.Config // Folder on a server (no secrets).
	Archive string           // Folder inside this archive.

	Edit, Help bool        // Document: editor rather than viewer; the F1 help text.
	Row, Col   int         // Document: text position; survives the file changing length.
	View       docPosition // Document: exact view when the text there is unchanged.
	ScreenID   int         // Document: its screen while open.
}

func (b bookmark) label() string {
	switch {
	case b.Kind == "document" && b.Help:
		return "Help"
	case b.Kind == "document":
		mode := "View "
		if b.Edit {
			mode = "Edit "
		}
		return mode + b.Path + ":" + strconv.Itoa(b.Row+1)
	case b.Remote != nil:
		return b.Remote.Label() + filepath.Join(b.Path, b.Cursor)
	default:
		return filepath.Join(b.Path, b.Cursor)
	}
}

func (a *App) setBookmark(n int, b bookmark) {
	if a.Bookmarks == nil {
		a.Bookmarks = map[int]bookmark{}
	}
	a.Bookmarks[n] = b
}

// bookmarkDigit is the 0…9 of a bookmark key, from the physical key because
// Shift and Option change the key symbol (and the layout may be non-Latin).
// Keypad digits are not bookmark keys.
func bookmarkDigit(m Message) (int, bool) {
	if strings.HasPrefix(m.Code, "Numpad") {
		return 0, false
	}
	key := m.Key
	if len(m.Code) == 6 && strings.HasPrefix(m.Code, "Digit") {
		key = m.Code[5:]
	}
	n, err := strconv.Atoi(key)
	if err != nil || len(key) != 1 || n < 0 || n > 9 {
		return 0, false
	}
	return n, true
}

// bookmarkKey recognises the bookmark keys, the same in the panels and in a
// document: ⌘⌥0…9 saves and ⌘0…9 jumps. Shift and Ctrl variants are not
// bookmark keys (macOS takes ⌘⇧3/4/5 for screenshots and Ctrl+digit for
// switching desktops). save reports which of the two was pressed.
func bookmarkKey(m Message) (n int, save, ok bool) {
	if !m.Meta || m.Ctrl || m.Shift {
		return 0, false, false
	}
	n, ok = bookmarkDigit(m)
	return n, m.Alt, ok
}

// panelBookmarkKey: ⌘⌥0…9 saves the current folder with the file under the
// cursor, ⌘0…9 jumps.
func (a *App) panelBookmarkKey(m Message) bool {
	n, save, ok := bookmarkKey(m)
	if !ok {
		return false
	}
	a.FastFind = nil
	if save {
		a.saveFolderBookmark(n)
	} else {
		a.goBookmark(n)
	}
	return true
}

// documentBookmarkKey: ⌘⌥0…9 saves the position in the document, ⌘0…9 jumps.
func (a *App) documentBookmarkKey(d *Document, m Message) bool {
	n, save, ok := bookmarkKey(m)
	if !ok {
		return false
	}
	if save {
		a.saveDocumentBookmark(d, n)
	} else {
		a.goBookmark(n)
	}
	return true
}

func (a *App) saveFolderBookmark(n int) {
	p := &a.Panels[a.Active]
	b := bookmark{Kind: "folder", Path: p.Path}
	if p.Cursor >= 0 && p.Cursor < len(p.Entries) && p.Entries[p.Cursor].Name != ".." {
		b.Cursor = p.Entries[p.Cursor].Name
	}
	if p.Remote != nil {
		c := *p.Remote
		c.Directory = p.Path
		c.Password = ""
		c.KeyPassphrase = ""
		c.HostKey = nil
		b.Remote = &c
	}
	if p.Archive != nil {
		b.Archive = p.Archive.Path
	}
	a.setBookmark(n, b)
	a.Status = fmt.Sprintf("Bookmark %d saved: %s", n, b.label())
	a.dirty = true
}

func (a *App) saveDocumentBookmark(d *Document, n int) {
	at := d.Pos(d.Cursor)
	a.setBookmark(n, bookmark{Kind: "document", Path: d.Path, Edit: d.Edit, Help: d.Help, ScreenID: d.ScreenID, Row: at.Row, Col: at.Col, View: d.pos()})
	a.Status = fmt.Sprintf("Bookmark %d saved", n)
}

// goBookmark jumps to bookmark n from the panels or from any document screen.
func (a *App) goBookmark(n int) {
	b, ok := a.Bookmarks[n]
	if !ok {
		a.Status = fmt.Sprintf("Bookmark %d is empty", n)
		return
	}
	if b.Kind == "document" {
		a.goDocumentBookmark(n, b)
		return
	}
	if a.Doc != nil {
		a.selectScreen(a.screenDocuments(), -1)
	}
	a.goFolderBookmark(b)
}

// goFolderBookmark shows the bookmarked folder in the active panel and puts
// the cursor on the remembered entry if it is still there.
func (a *App) goFolderBookmark(b bookmark) {
	p := &a.Panels[a.Active]
	if c := b.Remote; c != nil {
		for i := range a.Panels {
			other := &a.Panels[i]
			if other.Remote != nil && other.Remote.Label() == c.Label() {
				live := *other.Remote
				live.Directory = b.Path
				a.connectCursor = b.Cursor
				a.connectRemote(a.Active, live)
				return
			}
		}
		a.connectionDialog(*c)
		return
	}
	if b.Archive != "" {
		a.openArchiveAt(a.Active, b.Archive, b.Path, b.Cursor)
		return
	}
	// Validate before leaving a remote/archive panel, preserving it on errors.
	if info, err := os.Stat(b.Path); err != nil {
		a.error(err)
		return
	} else if !info.IsDir() {
		a.Status = "Bookmark is not a directory"
		return
	}
	p.cancelPanelLoad()
	p.cancelTree()
	// Remembered cursor positions and marks of visited folders survive the
	// jump; going up still focuses the folder just left (see beginLoad).
	previous := *p
	p.Remote = nil
	p.RemoteLocalPath = ""
	p.Archive = nil
	if err := p.loadSource(b.Path, previous, ""); err != nil {
		a.error(err)
		return
	}
	p.placeCursor(b.Cursor)
}

// placeCursor moves the cursor to the named entry of a panel that has just
// been loaded, or asks a load still in progress to do so.
func (p *Panel) placeCursor(name string) {
	if name == "" || p.pendingPosition(name, "") {
		return
	}
	for i, e := range p.Entries {
		if e.Name == name {
			p.Cursor = i
			return
		}
	}
}

// goDocumentBookmark jumps into the bookmarked document: its screen if still
// open (or another screen showing the same file), otherwise the file is
// opened again in the same mode. A missing file is reported, not opened.
func (a *App) goDocumentBookmark(n int, b bookmark) {
	all := a.screenDocuments()
	for _, d := range all {
		if d.ScreenID == b.ScreenID {
			a.jumpToBookmark(d, b)
			return
		}
	}
	if !b.Help {
		for _, d := range all {
			if d.Path == b.Path {
				a.jumpToBookmark(d, b)
				return
			}
		}
	}
	if b.Help {
		a.Status = fmt.Sprintf("Bookmark %d: help is closed", n)
		return
	}
	info, err := os.Stat(b.Path)
	switch {
	case err != nil:
		a.Status = fmt.Sprintf("Bookmark %d: %s: %s", n, b.Path, errorReason(err))
		return
	case !info.Mode().IsRegular():
		a.Status = fmt.Sprintf("Bookmark %d: %s is not a regular file", n, b.Path)
		return
	}
	a.openDocumentThen(b.Path, b.Edit, func(d *Document) {
		a.showDocument(d)
		b.ScreenID = d.ScreenID
		a.Bookmarks[n] = b
		d.restoreBookmark(b)
	})
}

func (a *App) jumpToBookmark(d *Document, b bookmark) {
	a.focusDocument(d)
	d.remember()
	d.restoreBookmark(b)
	a.dirty = true
}

// openDocumentThen opens a local file and, once it is loaded, runs shown with
// it; shown is responsible for showing the document (see openDocumentAsync).
func (a *App) openDocumentThen(path string, edit bool, shown func(*Document)) {
	if a.Async {
		a.openDocumentAsync(path, edit, shown)
		return
	}
	d, err := a.loadDocument(path, edit)
	if err != nil {
		a.openFailed(path, edit, err)
		return
	}
	a.recordView(d.Path, edit)
	a.presentDocument(d, shown)
}

// restoreBookmark returns to a bookmarked position. When the text at that
// spot is unchanged the exact view is restored; otherwise the cursor goes to
// the remembered line and column, or to the end of a file that shrank.
func (d *Document) restoreBookmark(b bookmark) {
	p := b.View
	if d.File != nil {
		p.ByteOffset = max(0, min(p.ByteOffset, max(int64(0), d.Size-1)))
		d.restore(p)
		return
	}
	lines := len(d.Lines())
	if p.Cursor <= len(d.Text) && b.Row < lines && d.At(b.Row, b.Col) == p.Cursor {
		d.restore(p)
		return
	}
	if b.Row >= lines {
		p.Cursor = len(d.Text)
	} else {
		p.Cursor = d.At(b.Row, b.Col)
	}
	p.Offset = max(0, min(p.Offset, lines-1))
	p.ViewStart = d.At(p.Offset, 0)
	p.ByteOffset = max(0, min(p.ByteOffset, max(int64(0), d.Size-1)))
	d.restore(p)
	d.ManualScroll = false
}

// bookmarksMenu lists the ten bookmarks (Commands → Bookmarks…).
func (a *App) bookmarksMenu() {
	names := []string{"Save current folder…"}
	for i := 0; i <= 9; i++ {
		label := ""
		if b, ok := a.Bookmarks[i]; ok {
			label = b.label()
		}
		names = append(names, strconv.Itoa(i)+"  "+label)
	}
	a.choose("Bookmarks", names, func(i int) {
		if i > 0 {
			a.goBookmark(i - 1)
			return
		}
		a.prompt("Save bookmark", "Slot 0–9", "1", func(s string) {
			n, e := strconv.Atoi(s)
			if e != nil || n < 0 || n > 9 {
				a.rejectPrompt(s, "Choose 0–9")
				return
			}
			a.saveFolderBookmark(n)
		})
	})
}
