package ui

import (
	"bytes"
	"encoding/hex"
	"errors"
	"farshmac/internal/remotefs"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"farshmac/internal/fsops"
	"farshmac/internal/textfile"
)

const maxTextBytes = 64 * 1024 * 1024

type Document struct {
	Remote        *remotefs.Config
	RemotePath    string
	RemoteInfo    os.FileInfo
	ArchiveSource bool
	TempDir       string
	TabSize       int
	ManualScroll  bool
	ScreenID      int
	*textfile.Buffer
	Path                            string
	Edit, Dirty                     bool
	Info                            os.FileInfo
	Offset, Column                  int
	BytesPerLine                    int
	Raw                             []byte
	SavedRaw                        []byte   // Immutable bytes of the last loaded or saved version.
	File                            *os.File // Large files are paged in hex/dump view, not loaded into memory.
	Size                            int64
	ByteOffset                      int64
	Codepage, EOL, Mode             string
	BOM, Wrap, WordWrap             bool
	HideKeys, HideStatus, Scrollbar bool
	SavedText, SavedCP, SavedEOL    string
	SavedBOM                        bool
	Bookmarks                       map[int]docPosition
	Positions                       []docPosition
	Search                          textfile.Search
	HexQuery                        []byte
	MatchStart, MatchEnd            int64
	ViewStart                       int
	Help                            bool

	SaveStrategy     fsops.SaveStrategy // Predicted when editing starts; a hint, never a ban.
	saveChecked      bool               // SaveStrategy/saveBlocked are current for this document.
	saveBlocked      string             // Why saving is expected to fail, if it is.
	inplaceConfirmed string             // Path the user agreed to rewrite non-atomically.
	infoLost         bool               // Saved, but the result could not be examined.
	notice           string             // Status to show when the document appears.
	readError        bool               // A paged read failed; reported once.
}
type docPosition struct {
	Cursor, Offset, Column int
	ViewStart              int
	ByteOffset             int64
}

func newDocument(path, text string) *Document {
	return &Document{Buffer: textfile.New(text), Path: path, Codepage: "UTF-8", EOL: "LF", Mode: "text", Bookmarks: map[int]docPosition{}, Scrollbar: true, SavedText: textfile.Normalize(text)}
}
func (d *Document) modified() {
	d.Dirty = d.Text != d.SavedText || d.Codepage != d.SavedCP || d.EOL != d.SavedEOL || d.BOM != d.SavedBOM
}
func (d *Document) markSaved() {
	d.SavedRaw = d.Raw
	d.SavedText = d.Text
	d.SavedCP = d.Codepage
	d.SavedEOL = d.EOL
	d.SavedBOM = d.BOM
	d.Dirty = false
}
func (d *Document) pos() docPosition {
	return docPosition{d.Cursor, d.Offset, d.Column, d.ViewStart, d.ByteOffset}
}
func (d *Document) restore(p docPosition) {
	d.Cursor = min(p.Cursor, len(d.Text))
	d.Offset = p.Offset
	d.Column = p.Column
	d.ByteOffset = p.ByteOffset
	d.ViewStart = p.ViewStart
	d.ClearSelection()
}
func (d *Document) remember() {
	d.Positions = append(d.Positions, d.pos())
	if len(d.Positions) > 100 {
		d.Positions = d.Positions[1:]
	}
}
func (d *Document) dataAt(at int64, n int) []byte {
	if at < 0 {
		return nil
	}
	if d.File != nil {
		b := make([]byte, n)
		read, err := d.File.ReadAt(b, at)
		if errors.Is(err, io.EOF) && at+int64(read) < d.Size {
			// The file shrank: page within what is really there.
			if info, e := d.File.Stat(); e == nil {
				d.Size = info.Size()
			}
		} else if err != nil && !errors.Is(err, io.EOF) && !d.readError {
			d.readError = true
			d.notice = "read error: " + errorReason(err)
		}
		return b[:read]
	}
	if at >= int64(len(d.Raw)) {
		return nil
	}
	return d.Raw[int(at):min(len(d.Raw), int(at)+n)]
}
func (a *App) showDocument(d *Document) {
	if d.ScreenID == 0 {
		a.nextScreenID++
		d.ScreenID = a.nextScreenID
	}
	if a.Doc != nil && a.Doc != d {
		a.Documents = append(a.Documents, a.Doc)
	}
	a.Doc = d
	a.Menu = nil
	a.dirty = true
}
func (a *App) closeDocument() {
	d := a.Doc
	if d == nil {
		return
	}
	if d.Dirty {
		a.confirm("Save changes", filepath.Base(d.displayPath())+" has changed. Save it?", []string{"Save", "Discard", "Cancel"}, func(i int) {
			switch i {
			case 0:
				a.saveDocumentThen(d, d.Path, false, a.finishCloseDocument)
			case 1:
				a.finishCloseDocument()
			}
		})
		return
	}
	a.finishCloseDocument()
}
func (a *App) finishCloseDocument() {
	if a.Doc != nil && a.Doc.TempDir != "" {
		defer os.RemoveAll(a.Doc.TempDir)
	}
	a.stopSyntax()
	if a.Doc != nil && a.Doc.File != nil {
		a.Doc.File.Close()
	}
	a.Doc = nil
	if len(a.Documents) > 0 {
		a.Doc = a.Documents[len(a.Documents)-1]
		a.Documents = a.Documents[:len(a.Documents)-1]
	}
	a.refresh()
}
func (a *App) open(edit bool) {
	path := a.Panels[a.Active].Current()
	if path != "" {
		a.openDocument(path, edit)
	}
}
func (a *App) openDocument(path string, edit bool) {
	if a.Async {
		a.openDocumentAsync(path, edit, nil)
		return
	}
	d, err := a.loadDocument(path, edit)
	if err != nil {
		a.openFailed(path, edit, err)
		return
	}
	a.recordView(d.Path, edit)
	a.presentDocument(d, a.showDocument)
}
func (a *App) recordView(path string, edit bool) {
	a.viewHistory = append(a.viewHistory, viewRecord{path, edit})
	if limit := a.Settings.HistoryLimit; limit > 0 && len(a.viewHistory) > limit {
		a.viewHistory = a.viewHistory[len(a.viewHistory)-limit:]
	}
}

// loadDocument reads a local file without touching the window, so it can run
// on a worker. Expected refusals are openRefusal; anything else is a failure
// worth a dialog. For the editor it also predicts whether saving can work.
func (a *App) loadDocument(path string, edit bool) (*Document, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, openRefusal("Choose a regular file")
	}
	d := newDocument(resolved, "")
	d.Info = info
	d.Size = info.Size()
	d.Edit = edit
	d.Wrap = a.Settings.ViewerWrap
	if edit {
		d.checkSaveAccess()
	}
	if info.Size() > maxTextBytes {
		if edit {
			f.Close()
			return nil, openRefusal("Editor limit is 64 MiB. Large files open in paged hex/dump viewer.")
		}
		d.File = f
		d.Mode = "hex"
		d.markSaved()
		return d, nil
	}
	raw, err := io.ReadAll(a.Operation.Reader(io.LimitReader(f, maxTextBytes+1)))
	f.Close()
	if err != nil {
		return nil, err
	}
	if len(raw) > maxTextBytes {
		return nil, openRefusal("File grew while opening; reopen it")
	}
	d.Raw = raw
	d.Codepage = textfile.Detect(raw)
	decoded, bom, decodeErr := textfile.Decode(raw, d.Codepage)
	if decodeErr != nil {
		if edit {
			return nil, openRefusal(decodeErr.Error() + "; open with F3")
		}
		// Truncated UTF-16 and similar damage must stay inspectable: every
		// byte is representable in ISO-8859-1, and hex shows the real bytes.
		d.Codepage = "ISO-8859-1"
		if decoded, bom, err = textfile.Decode(raw, d.Codepage); err != nil {
			return nil, decodeErr
		}
		d.Mode = "hex"
		d.notice = decodeErr.Error() + "; shown as hex"
	}
	binary := strings.ContainsRune(decoded, 0)
	if edit {
		roundTrip, e := textfile.Encode(decoded, d.Codepage, bom)
		if e != nil || !bytes.Equal(roundTrip, raw) {
			return nil, openRefusal("Encoding is not lossless; inspect in viewer and choose a codepage before editing")
		}
	}
	if edit && binary {
		return nil, openRefusal("Binary data: open with F3 in hex/dump mode")
	}
	d.BOM = bom
	d.EOL = textfile.DetectEOL(decoded)
	d.Buffer = textfile.New(decoded)
	if binary {
		d.Mode = "hex"
	}
	d.markSaved()
	d.Lines()
	return d, nil
}
func (a *App) save() {
	if a.Doc != nil && a.Doc.Edit {
		a.saveDocumentThen(a.Doc, a.Doc.Path, false, nil)
	}
}

// statSavedFile is replaced by tests to simulate a file that vanishes right
// after a successful save.
var statSavedFile = os.Stat

func (a *App) saveDocument(d *Document, path string, overwrite bool) error {
	if d.ArchiveSource && path == d.Path {
		return saveRefusal("Copy the file out with F5 before editing")
	}
	if d.ReadOnly {
		return saveRefusal("Read-only mode (⌘L to unlock)")
	}
	data, err := textfile.Encode(textfile.Endings(d.Text, d.EOL), d.Codepage, d.BOM)
	if err != nil {
		return saveRefusal(err.Error())
	}
	if d.Info != nil && d.Text == d.SavedText && d.Codepage == d.SavedCP && d.EOL == d.SavedEOL && d.BOM == d.SavedBOM {
		data = d.SavedRaw
	}
	var result fsops.SaveResult
	switch {
	case path == d.Path && (d.Info != nil || d.infoLost):
		original := d.Info
		if d.infoLost {
			// The last save succeeded but its result could not be examined;
			// whatever is on disk now is that save.
			if original, err = os.Lstat(path); err != nil {
				return err
			}
		}
		result, err = a.saveExisting(d, path, data, original)
	case overwrite:
		info, e := os.Lstat(path)
		if e != nil {
			return e
		}
		result, err = a.saveExisting(d, path, data, info)
	default:
		err = fsops.Create(path, data, 0644, a.Operation)
	}
	if err != nil {
		return err
	}
	// The data is on disk: never report this save as failed. Without a
	// fresh stat the next save re-reads the file instead of recreating it.
	info, statErr := statSavedFile(path)
	d.Path = path
	d.Info, d.infoLost = info, statErr != nil
	if statErr != nil {
		d.Info = nil
	}
	d.Raw = data
	d.Size = int64(len(data))
	d.markSaved()
	switch {
	case result.MetadataLost != nil:
		a.Status = "Saved; some attributes were not preserved"
	case result.InPlace:
		a.Status = "Saved (in place)"
	default:
		a.Status = "Saved"
	}
	a.dirty = true
	return nil
}
func (a *App) saveExisting(d *Document, path string, data []byte, original os.FileInfo) (fsops.SaveResult, error) {
	result, err := fsops.SaveWith(path, data, original, fsops.SaveOptions{}, a.Operation)
	if errors.Is(err, fsops.ErrInPlaceRequired) && d.inplaceConfirmed == path {
		result, err = fsops.SaveWith(path, data, original, fsops.SaveOptions{InPlace: true}, a.Operation)
	}
	return result, err
}
func (a *App) saveAs() {
	a.saveAsThen(a.Doc, nil)
}
func (a *App) saveAsThen(d *Document, done func()) {
	initial, base := d.Path, a.Panels[a.Active].filesystemPath()
	if d.Remote != nil {
		initial = filepath.Join(base, filepath.Base(d.RemotePath))
	}
	a.prompt("Save as", "Local file name", initial, func(s string) {
		path := expand(s, base)
		if path == d.Path {
			a.saveDocumentThen(d, path, false, done)
			return
		}
		info, err := os.Lstat(path)
		if err != nil {
			a.saveDocumentThen(d, path, false, done)
			return
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(path)
			if err != nil {
				a.Status = path + " is a broken symbolic link"
				return
			}
			if target == d.Path {
				a.saveDocumentThen(d, target, false, done)
				return
			}
			if info, err = os.Stat(target); err != nil {
				a.error(err)
				return
			}
			if info.IsDir() {
				a.Status = path + " is a directory"
				return
			}
			if !info.Mode().IsRegular() {
				a.Status = path + " is not a regular file"
				return
			}
			a.confirm("Overwrite", filepath.Base(path)+" is a link to "+target+". Replace the target file?", []string{"Overwrite", "Cancel"}, func(i int) {
				if i == 0 {
					a.saveDocumentThen(d, target, true, done)
				}
			})
			return
		}
		if info.IsDir() {
			a.Status = path + " is a directory"
			return
		}
		if !info.Mode().IsRegular() {
			a.Status = path + " is not a regular file"
			return
		}
		a.confirm("Overwrite", filepath.Base(path)+" already exists. Replace it?", []string{"Overwrite", "Cancel"}, func(i int) {
			if i == 0 {
				a.saveDocumentThen(d, path, true, done)
			}
		})
	})
}
func (a *App) newFile() {
	if a.remoteUnsupported() {
		return
	}
	if a.archiveReadOnly() {
		return
	}
	a.prompt("Edit file", "Existing file or new file name", "", func(s string) {
		if s == "" {
			return
		}
		path := expand(s, a.Panels[a.Active].Path)
		if _, err := os.Stat(path); err == nil {
			a.openDocument(path, true)
		} else if os.IsNotExist(err) {
			d := newDocument(path, "")
			d.Edit = true
			d.Codepage = a.Settings.DefaultCodepage
			d.markSaved()
			d.checkSaveAccess()
			a.presentDocument(d, a.showDocument)
		} else {
			a.openFailed(path, true, err)
		}
	})
}
func (a *App) chooseCodepage() {
	d := a.Doc
	a.choose("Code page", textfile.Codepages, func(i int) {
		if a.Async {
			a.transformDocument("Converting code page", func(worker *App) { worker.chooseCodepage(); worker.Form.Select(i) })
			return
		}
		cp := textfile.Codepages[i]
		if d.Edit {
			if _, err := textfile.Encode(textfile.Endings(d.Text, d.EOL), cp, d.BOM); err != nil {
				a.error(err)
				return
			}
			d.Codepage = cp
			d.modified()
			return
		}
		if d.Dirty {
			a.Status = "Save edited text before reinterpreting bytes"
			return
		}
		if d.File != nil {
			d.Codepage = cp
			return
		}
		s, bom, err := textfile.Decode(d.Raw, cp)
		if err != nil {
			a.error(err)
			return
		}
		d.Codepage = cp
		d.BOM = bom
		d.EOL = textfile.DetectEOL(s)
		d.Buffer = textfile.New(s)
		d.Offset = 0
		d.markSaved()
	})
}
func (a *App) documentSettings() {
	d := a.Doc
	a.form("Viewer / editor options", []Field{
		{Label: "Line endings", Value: d.EOL}, {Label: "Write BOM", Toggle: true, Checked: d.BOM},
		{Label: "Read only", Toggle: true, Checked: d.ReadOnly}, {Label: "Scrollbar", Toggle: true, Checked: d.Scrollbar},
		{Label: "Syntax highlighting (Chroma)", Toggle: true, Checked: a.Settings.EditorSyntax}, {Label: "Auto indent", Toggle: true, Checked: a.Settings.EditorAutoIndent},
		{Label: "Tab width (1–16)", Value: strconv.Itoa(a.editorTabWidth())}, {Label: "Expand tabs to spaces", Toggle: true, Checked: a.Settings.EditorExpandTabs},
	}, func(f []Field) {
		width, err := strconv.Atoi(f[6].Value)
		if err != nil || width < 1 || width > 16 {
			a.Status = "Tab width must be 1–16"
			return
		}
		eol := strings.ToUpper(f[0].Value)
		if eol != "LF" && eol != "CRLF" && eol != "CR" {
			a.Status = "Line endings must be LF, CRLF or CR"
			return
		}
		if d.Edit {
			d.EOL = eol
			d.BOM = f[1].Checked
			d.modified()
		}
		d.ReadOnly = f[2].Checked
		d.Scrollbar = f[3].Checked
		a.Settings.EditorSyntax = f[4].Checked
		a.Settings.EditorAutoIndent = f[5].Checked
		a.Settings.EditorTabSize = width
		a.Settings.EditorExpandTabs = f[7].Checked
	})
}
func (a *App) gotoDocument() {
	d := a.Doc
	label := "Line:column (1-based), or percentage e.g. 50%"
	if !d.Edit && d.Mode != "text" {
		label = "Byte offset (decimal / 0xHEX) or percentage"
	}
	a.prompt("Go to", label, "", func(s string) {
		d.remember()
		if strings.HasSuffix(s, "%") {
			v, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
			if err != nil || v < 0 || v > 100 {
				a.Status = "Enter a percentage from 0 to 100"
				return
			}
			if d.Mode != "text" && !d.Edit {
				d.ByteOffset = int64(float64(max(int64(0), d.Size-1)) * v / 100)
			} else {
				row := int(float64(len(d.Lines())-1) * v / 100)
				d.Cursor = d.At(row, 0)
				d.Offset = row
				d.ViewStart = d.Cursor
			}
			return
		}
		if !d.Edit && d.Mode != "text" {
			base := 10
			if strings.HasPrefix(s, "0x") {
				base = 0
			}
			v, err := strconv.ParseInt(s, base, 64)
			if err != nil || v < 0 || v >= max(int64(1), d.Size) {
				a.Status = "Offset outside file"
				return
			}
			d.ByteOffset = v
			return
		}
		parts := strings.Split(s, ":")
		row, err := strconv.Atoi(parts[0])
		col := 1
		if len(parts) > 1 {
			col, err = strconv.Atoi(parts[1])
		}
		if err != nil || row < 1 || col < 1 || row > len(d.Lines()) {
			a.Status = "Invalid line / column"
			return
		}
		d.Cursor = d.At(row-1, col-1)
		d.ClearSelection()
		d.Offset = row - 1
		d.ViewStart = d.Cursor
	})
}
func (a *App) searchDocument(replace bool) {
	d := a.Doc
	if d.File != nil {
		a.Status = "Search in paged files larger than 64 MiB is not implemented yet"
		return
	}
	if replace && d.ReadOnly {
		a.Status = "Read-only mode (⌘L to unlock)"
		return
	}
	fields := []Field{{Label: "Search", Value: d.Search.Query}, {Label: "Case sensitive", Toggle: true, Checked: d.Search.Case}, {Label: "Whole words", Toggle: true, Checked: d.Search.Whole}, {Label: "Regex (Go/RE2)", Toggle: true, Checked: d.Search.Regex}, {Label: "Backwards", Toggle: true}, {Label: "Hex bytes", Toggle: true, Checked: d.Mode == "hex" && !d.Edit}}
	if replace {
		fields = append(fields, Field{Label: "Replace with"}, Field{Label: "Replace all", Toggle: true})
	}
	a.form("Search", fields, func(f []Field) {
		d.Search = textfile.Search{Query: f[0].Value, Case: f[1].Checked, Whole: f[2].Checked, Regex: f[3].Checked}
		d.HexQuery = nil
		if f[5].Checked {
			if replace {
				a.Status = "Hex replacement is not available in text editor"
				return
			}
			if d.Edit {
				// d.Raw holds the bytes last loaded or saved, not the text
				// being edited, so a byte search here would be wrong.
				a.Status = "Hex search is available in the viewer (F6)"
				return
			}
			b, err := hex.DecodeString(strings.Join(strings.Fields(f[0].Value), ""))
			if err != nil || len(b) == 0 {
				a.Status = "Enter pairs of hex digits, e.g. 48 65 6c 6c 6f"
				return
			}
			d.HexQuery = b
			a.findDocument(f[4].Checked)
			return
		}
		if replace && f[7].Checked {
			if a.Async {
				a.replaceAllAsync(d, f[6].Value)
				return
			}
			n, err := d.ReplaceAll(d.Search, f[6].Value)
			a.error(err)
			if err == nil {
				a.Status = fmt.Sprintf("Replaced %d matches", n)
				d.modified()
			}
			return
		}
		a.findDocumentThen(f[4].Checked, func() {
			if replace {
				a.confirmReplacement(f[6].Value, f[4].Checked)
			}
		})
	})
}
func (a *App) confirmReplacement(replacement string, backward bool) {
	d := a.Doc
	a.confirm("Replace", "Replace this match?", []string{"Replace", "Skip", "Cancel"}, func(i int) {
		if i == 2 {
			return
		}
		if i == 0 {
			start, end := d.Bounds()
			value := replacement
			if d.Search.Regex {
				re, err := d.Search.Compile()
				if err != nil {
					a.error(err)
					return
				}
				value = re.ReplaceAllString(d.Text[start:end], replacement)
			}
			d.Replace(start, end, value)
			d.modified()
		}
		a.findDocumentThen(backward, func() { a.confirmReplacement(replacement, backward) })
	})
}
func (a *App) findDocument(backward bool) bool {
	if a.Async {
		a.findDocumentThen(backward, nil)
		return false
	}
	d := a.Doc
	if d.Edit && len(d.HexQuery) > 0 {
		d.HexQuery = nil
		a.Status = "Hex search is available in the viewer (F6)"
		return false
	}
	d.remember()
	if len(d.HexQuery) > 0 {
		at := int(min(d.ByteOffset, int64(len(d.Raw))))
		if d.MatchEnd > d.MatchStart {
			at = int(d.MatchEnd)
		}
		var hit int
		if backward {
			at = int(min(d.MatchStart, int64(len(d.Raw))))
			hit = bytes.LastIndex(d.Raw[:max(0, at)], d.HexQuery)
			if hit < 0 {
				hit = bytes.LastIndex(d.Raw, d.HexQuery)
			}
		} else {
			hit = bytes.Index(d.Raw[at:], d.HexQuery)
			if hit >= 0 {
				hit += at
			} else {
				hit = bytes.Index(d.Raw, d.HexQuery)
			}
		}
		if hit < 0 {
			a.Status = "Not found"
			return false
		}
		d.MatchStart = int64(hit)
		d.MatchEnd = int64(hit + len(d.HexQuery))
		d.ByteOffset = int64(hit)
		d.Mode = "hex"
		return true
	}
	if !d.Edit && d.Anchor < 0 {
		d.Cursor = d.At(d.Offset, 0)
	}
	found, err := d.Find(d.Search, backward, a.Operation.Check)
	if err != nil {
		a.error(err)
		return false
	}
	if !found {
		a.Status = "Not found"
		return false
	}
	d.Offset = d.Pos(d.Anchor).Row
	d.Column = max(0, d.Pos(d.Anchor).Col-5)
	d.ViewStart = d.Anchor
	if !d.Edit {
		d.Mode = "text"
	}
	a.Status = "Match found"
	return true
}
func (a *App) documentKey(m Message) {
	if a.Doc.ArchiveSource && m.Key == "F6" {
		a.Status = "Copy the file out with F5 before editing"
		return
	}
	a.Doc.ManualScroll = false
	if d := a.Doc; m.Key == "F6" && !d.Edit && !d.Help && d.File == nil && !d.saveChecked {
		// Switching to the editor is when saving starts to matter.
		d.checkSaveAccess()
		if d.SaveStrategy == fsops.SaveNone && d.saveBlocked != "" {
			a.saveWarning(d, "Read-only", func(i int) {
				switch i {
				case 0:
					d.ReadOnly = true
					a.documentKey(m)
				case 1:
					d.notice = "Saving may fail: " + d.saveBlocked
					a.documentKey(m)
				default:
					d.saveChecked = false
				}
			})
			return
		}
	}
	if a.Async && (m.Key == "F6" || m.Key == "F8" && !m.Alt && !m.Shift && (a.Doc.Edit || !a.Doc.Dirty && a.Doc.File == nil)) {
		a.transformDocument("Converting document", func(worker *App) { worker.documentKey(m) })
		return
	}
	d := a.Doc
	key := m.Key
	command := m.Meta || m.Ctrl
	if key == "F1" {
		if d.Help {
			return
		}
		helpDoc := newDocument("Viewer / editor help", documentHelp)
		helpDoc.Help = true
		helpDoc.markSaved()
		a.showDocument(helpDoc)
		return
	}
	if key == "Escape" && a.syntax.job != nil {
		a.syntax.job.cancel()
	}
	if key == "Escape" || key == "F10" || (!d.Edit && key == "F3") || (d.Edit && key == "F4" && !m.Shift) {
		if key == "F10" && m.Shift && d.Edit {
			a.saveDocumentThen(d, d.Path, false, a.finishCloseDocument)
			return
		}
		a.closeDocument()
		return
	}
	if key == "F12" {
		a.screenMenu()
		return
	}
	if key == "F6" {
		if d.Help {
			return
		}
		if d.File != nil {
			a.Status = "Editor limit is 64 MiB"
			return
		}
		if !d.Edit && strings.ContainsRune(d.Text, 0) {
			a.Status = "Binary data cannot be edited as text"
			return
		}
		if !d.Edit && !d.Dirty {
			decoded, bom, err := textfile.Decode(d.Raw, d.Codepage)
			if err != nil {
				a.error(err)
				return
			}
			roundTrip, err := textfile.Encode(decoded, d.Codepage, bom)
			if err != nil || !bytes.Equal(roundTrip, d.Raw) {
				a.Status = "Encoding is not lossless; choose a codepage before editing"
				return
			}
		}
		if d.Edit {
			data, err := textfile.Encode(textfile.Endings(d.Text, d.EOL), d.Codepage, d.BOM)
			if err != nil {
				a.error(err)
				return
			}
			if !d.Dirty && d.Info != nil {
				data = d.SavedRaw
			}
			d.Raw = data
			d.Size = int64(len(data))
		}
		d.Edit = !d.Edit
		if !d.Edit {
			d.Offset = d.Pos(d.Cursor).Row
			d.ViewStart = d.Cursor
		} else if d.notice != "" {
			a.Status = d.notice
			d.notice = ""
		}
		return
	}
	if key == "F7" {
		if m.Shift || m.Alt {
			a.findDocument(m.Alt)
		} else {
			a.searchDocument(command && d.Edit)
		}
		return
	}
	if key == "F8" {
		if m.Alt {
			a.gotoDocument()
		} else if m.Shift {
			a.chooseCodepage()
		} else {
			cp := "CP866"
			if d.Codepage == cp {
				cp = "Windows-1251"
			}
			if d.Edit {
				if _, err := textfile.Encode(d.Text, cp, d.BOM); err != nil {
					a.error(err)
					return
				}
				d.Codepage = cp
				d.modified()
			} else if d.Dirty {
				a.Status = "Save edited text before reinterpreting bytes"
			} else if d.File != nil {
				d.Codepage = cp
				a.Status = "Code page: " + cp
			} else {
				s, bom, err := textfile.Decode(d.Raw, cp)
				if err == nil {
					d.Buffer = textfile.New(s)
					d.Codepage = cp
					d.BOM = bom
					d.Offset = 0
					d.markSaved()
				} else {
					a.error(err)
				}
			}
		}
		return
	}
	if key == "F9" {
		a.documentSettings()
		return
	}
	if command {
		digit := key
		// With Shift or Option the key is a symbol ("#", "№", "¡", "™"); the
		// physical key code names the digit on every layout.
		if len(m.Code) == 6 && strings.HasPrefix(m.Code, "Digit") {
			digit = m.Code[5:]
		} else if len(m.Code) == 7 && strings.HasPrefix(m.Code, "Numpad") {
			digit = m.Code[6:]
		}
		if n, err := strconv.Atoi(digit); err == nil && len(digit) == 1 && n >= 0 && n < 10 {
			// Cmd+Option+digit saves (macOS takes Cmd+Shift+3/4/5 for
			// screenshots); Ctrl+Shift+digit is an alias. Cmd/Ctrl+digit restores.
			switch {
			case m.Meta && m.Alt && !m.Ctrl, m.Ctrl && m.Shift && !m.Meta && !m.Alt:
				d.Bookmarks[n] = d.pos()
				a.Status = fmt.Sprintf("Bookmark %d saved", n)
			case !m.Shift && !m.Alt:
				if p, ok := d.Bookmarks[n]; ok {
					d.remember()
					d.restore(p)
				}
			}
			return
		}
		switch strings.ToLower(key) {
		case "q":
			a.RequestQuit()
			return
		case "s":
			if d.Edit {
				if m.Shift {
					a.saveAs()
				} else {
					a.save()
				}
			} else {
				d.Scrollbar = !d.Scrollbar
			}
			return
		case "f":
			a.searchDocument(false)
			return
		case "h":
			if d.Edit {
				a.searchDocument(true)
			}
			return
		case "g":
			a.gotoDocument()
			return
		case "a":
			d.Anchor = 0
			d.Cursor = len(d.Text)
			d.Rect = false
			return
		case "c", "x":
			a.copyDocument(strings.EqualFold(key, "x"))
			return
		case "z":
			if d.Edit {
				if m.Shift {
					d.Redo()
				} else {
					d.Undo()
				}
				d.modified()
			} else if len(d.Positions) > 0 {
				p := d.Positions[len(d.Positions)-1]
				d.Positions = d.Positions[:len(d.Positions)-1]
				d.restore(p)
			}
			return
		case "l":
			d.ReadOnly = !d.ReadOnly
			return
		case "r":
			a.reloadDocument()
			return
		case "insert":
			a.copyDocument(false)
			return
		case "b":
			if m.Shift {
				d.HideStatus = !d.HideStatus
			} else {
				d.HideKeys = !d.HideKeys
			}
			return
		case "u":
			d.ClearSelection()
			return
		}
	}
	if d.Edit {
		a.editorKey(m)
	} else {
		a.viewerKey(m)
	}
}
func (a *App) copyDocument(cut bool) {
	if a.Form != nil {
		f := a.Form
		if len(f.Choice) > 0 {
			s := ""
			if f.Buttons {
				s = f.Message
			} else if f.Focus >= 0 && f.Focus < len(f.Choice) {
				s = f.Choice[f.Focus]
			}
			if s != "" {
				a.Clipboard = &s
			}
			return
		}
		if f.Focus < 0 || f.Focus >= len(f.Fields) || f.Fields[f.Focus].Toggle {
			return
		}
		field := &f.Fields[f.Focus]
		if field.Secret {
			if cut {
				field.Value = ""
				f.Cursor = 0
				f.Selected = false
			}
			a.Status = "Secret fields are not copied"
			return
		}
		s := field.Value
		a.Clipboard = &s
		if cut {
			field.Value = ""
			f.Cursor = 0
			f.Selected = false
		}
		return
	}
	if a.Dialog != nil {
		s := a.Dialog.Input
		a.Clipboard = &s
		if cut {
			a.Dialog.Input = ""
		}
		return
	}
	d := a.Doc
	if d == nil {
		a.copyPanelCommand(cut)
		return
	}
	s := d.Selected()
	if !d.Edit && s == "" {
		return
	}
	if s == "" {
		p := d.Pos(d.Cursor)
		start := d.At(p.Row, 0)
		end := start + len(d.Line(p.Row))
		if end < len(d.Text) {
			end++
		}
		s = d.Text[start:end]
		if cut && d.Edit {
			d.Replace(start, end, "")
		}
	} else if cut && d.Edit {
		d.Insert("")
	}
	a.Clipboard = &s
	d.modified()
}
func (a *App) screenDocuments() []*Document {
	all := []*Document{}
	for _, d := range a.Documents {
		if d != nil {
			all = append(all, d)
		}
	}
	if a.Doc != nil {
		all = append(all, a.Doc)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].ScreenID < all[j].ScreenID })
	return all
}
func (a *App) selectScreen(all []*Document, index int) {
	a.Doc = nil
	a.Documents = nil
	for i, d := range all {
		if i == index {
			a.Doc = d
		} else {
			a.Documents = append(a.Documents, d)
		}
	}
	a.TermVisible = false
	a.dirty = true
}
func (a *App) screenMenu() {
	all := a.screenDocuments()
	labels := []string{"Panels"}
	for _, d := range all {
		prefix := "View "
		if d.Edit {
			prefix = "Edit "
		}
		if d.Dirty {
			prefix += "* "
		}
		labels = append(labels, prefix+filepath.Base(d.displayPath()))
	}
	a.choose("Screens", labels, func(i int) { a.selectScreen(all, i-1) })
	for i, d := range all {
		if d == a.Doc {
			a.Form.Focus = i + 1
			break
		}
	}
}
func (a *App) cycleScreen(back bool) {
	if a.Form != nil || a.Dialog != nil {
		return
	}
	all := a.screenDocuments()
	if len(all) == 0 {
		return
	}
	index := -1
	if a.Doc != nil {
		for i, d := range all {
			if d == a.Doc {
				index = i
				break
			}
		}
	}
	if back {
		index--
	} else {
		index++
	}
	if index >= len(all) {
		index = -1
	}
	if index < -1 {
		index = len(all) - 1
	}
	a.selectScreen(all, index)
}

const documentHelp = `FarshMac viewer / editor

Ctrl works as Cmd in the viewer/editor (also for external keyboards).

Esc / F10   Close this document, return to previous screen / panels
Cmd+Q       Quit application (checks ALL unsaved documents)
Cmd+R       Reload from disk (asks first if there are unsaved changes)
F6          Switch viewer / editor, retaining text and cursor
F12         Switch open document screens
F7 / Cmd+F  Search: literal / regex, case, whole words, backwards;
            hex search is viewer-only
Shift+F7    Next match      Alt+F7 Previous match
Cmd+F7 / Cmd+H  Search and replace; confirmation or Replace all
Alt+F8 / Cmd+G  Go to line:column, byte offset, or percentage
Shift+F8    Codepage chooser     F8 CP866 / Windows-1251 (first press: CP866)
F9          Encoding, line endings, BOM, read-only; Syntax, Auto indent,
            Tab width and Expand tabs change app settings, not the document
Cmd+Option+0..9 Save bookmark    Cmd+0..9 Restore bookmark
            (Ctrl+Shift+0..9 also saves, Ctrl+0..9 also restores)
Cmd+L       Toggle read-only    Cmd+U Clear selection
Cmd+B       Toggle key bar      Cmd+Shift+B Toggle status
Cmd+O       Terminal / document
Ctrl+Tab    Next screen         Ctrl+Shift+Tab Previous screen

Viewer:
F2          Toggle wrap         Shift+F2 Word / character wrap
F4          Text / hex          Shift+F4 Text / hex / dump
Arrows      Scroll              Cmd+Left/Right Scroll 20 columns
Home/End    Start/end           PgUp/PgDn Page
Alt+Left/Right  Adjust hex bytes per row
Cmd+Z       Previous viewing position
Space       Next search match   Cmd+S Toggle scrollbar
+ / -       Next / previous file in active panel

Editor:
F2 / Cmd+S  Save                Shift+F2 / Cmd+Shift+S Save as
Shift+F4    Open/create another file
Shift+F10   Save and close this document
Cmd+Z / Cmd+Shift+Z Undo / redo
Insert      Insert / overwrite
Shift+arrows Stream selection   Alt+Shift+arrows Rectangular selection
Cmd+C/X/V   Copy / cut / paste  Cmd+A Select all
Ctrl+Insert Copy                Shift+Delete Cut
Cmd+D       Delete selection
Cmd+Left/Right  Word movement   Cmd+Home/End File start/end
Cmd+Backspace/Delete Delete word   Cmd+Y Delete line
Cmd+K       Delete to line end  Alt+U / Alt+I Unindent / indent
Enter       Newline with auto-indent    Tab Insert tab

Limits: Go/RE2 regex, not Far's full regex dialect. No fuzzy/style-preserving
replacement, plugin APIs, macros or persistent bookmarks yet. Text editing
and text decoding are limited to 64 MiB; larger files use paged hex/dump.
Legacy encoding guesses are heuristic: choose an explicit codepage if needed.

Saving keeps xattrs, ACLs and hidden/nodump flags; partial loss reports
"Saved; some attributes were not preserved". Save dialogs: "Save failed"
(Retry / Save as… / Cancel), "File changed on disk" (Overwrite), "File was
deleted" (Create), "Save in place" for unwritable folders (not atomic, keeps
hard links). A file that probably cannot be saved asks View / Edit anyway /
Cancel when opened. Saving through a symbolic link writes its target.`
