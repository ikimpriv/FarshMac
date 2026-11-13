package ui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"farshmac/internal/fsops"
)

// openRefusal is an expected "cannot open this way" answer, not a failure:
// it stays a status line instead of a dialog.
type openRefusal string

func (e openRefusal) Error() string { return string(e) }

// saveRefusal is a save that Retry cannot fix (read-only mode, encoding).
type saveRefusal string

func (e saveRefusal) Error() string { return string(e) }

var errRemoteChanged = errors.New("remote file changed")

func isCancelled(err error) bool {
	return errors.Is(err, fsops.ErrCancelled) || errors.Is(err, context.Canceled)
}

// errorReason is the human part of an error: the path is shown separately.
func errorReason(err error) string {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err.Error()
	}
	return err.Error()
}

// saveReason names the folder rather than the hidden temporary file a failed
// save tried to create, so the message says what to fix.
func saveReason(err error) string {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		path := pathErr.Path
		if strings.HasPrefix(filepath.Base(path), ".farshmac-") {
			return fmt.Sprintf("cannot write in folder %s: %v", filepath.Dir(path), pathErr.Err)
		}
		return fmt.Sprintf("%s %s: %v", pathErr.Op, path, pathErr.Err)
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		if errors.Is(linkErr.Err, fs.ErrExist) {
			return linkErr.New + " already exists"
		}
		return fmt.Sprintf("%s %s: %v", linkErr.Op, linkErr.New, linkErr.Err)
	}
	return err.Error()
}

// openFailed reports why a document did not open. The status is set too, so
// worker Apps that cannot show dialogs still return a meaningful error.
func (a *App) openFailed(path string, edit bool, err error) {
	a.dirty = true
	var refusal openRefusal
	if errors.As(err, &refusal) || isCancelled(err) {
		a.Status = err.Error()
		return
	}
	a.Status = err.Error()
	message := path + "\n" + errorReason(err)
	if errors.Is(err, fs.ErrPermission) {
		message += "\nCheck the file permissions or Full Disk Access."
	}
	a.confirm("Cannot open file", message, []string{"OK"}, func(int) {})
}

// checkSaveAccess predicts, before editing starts, whether the save strategy
// can work. It runs on the worker that loads the document.
func (d *Document) checkSaveAccess() {
	d.saveChecked = true
	d.saveBlocked = ""
	if d.Help || d.ArchiveSource {
		return
	}
	if d.Source != nil {
		if blocked := d.Source.Blocked(d.SourceInfo); blocked != "" {
			d.SaveStrategy = fsops.SaveNone
			d.saveBlocked = blocked
		} else {
			d.SaveStrategy = fsops.SaveReplace
		}
		return
	}
	d.SaveStrategy, d.saveBlocked = fsops.SaveAccess(d.Path)
}

// saveWarning asks how to open a document that is not expected to be
// saveable. readOnly is the label of the first button: "View" when opening,
// "Read-only" when switching from the viewer, "" when there is no file yet.
func (a *App) saveWarning(d *Document, first string, choose func(int)) {
	buttons := []string{"Edit anyway", "Cancel"}
	if first != "" {
		buttons = append([]string{first}, buttons...)
	}
	title := "Read-only file"
	message := filepath.Base(d.Path) + " cannot be saved here: " + d.saveBlocked + "\nOpen it read-only?"
	if first == "" {
		message = filepath.Base(d.Path) + " cannot be saved here: " + d.saveBlocked
	}
	if d.Source != nil {
		title = "Remote file is read-only"
		message = d.Source.Display() + " has " + d.saveBlocked + ". Saving will probably fail."
	}
	a.confirm(title, message, buttons, func(i int) {
		if first == "" {
			i++
		}
		choose(i)
	})
}

// presentDocument shows a freshly loaded document, first asking when the
// editor is not expected to be able to save it.
func (a *App) presentDocument(d *Document, show func(*Document)) {
	note := d.notice
	d.notice = ""
	if !d.Edit || d.SaveStrategy != fsops.SaveNone || d.saveBlocked == "" {
		show(d)
		if note != "" {
			a.Status = note
		}
		return
	}
	first := "View"
	if d.Info == nil && d.Source == nil {
		first = "" // A new file has nothing to view.
	}
	a.saveWarning(d, first, func(i int) {
		switch i {
		case 0:
			d.Edit = false
			d.saveChecked = false // F6 asks again, offering a read-only editor.
			show(d)
		case 1:
			show(d)
			a.Status = "Saving may fail: " + d.saveBlocked
		default:
			if d.File != nil {
				d.File.Close()
			}
			if d.TempDir != "" {
				os.RemoveAll(d.TempDir)
			}
		}
	})
}

// focusDocument makes d the visible screen so a question about it is not
// asked over another document or the terminal.
func (a *App) focusDocument(d *Document) {
	if a.Doc == d {
		a.TermVisible = false
		return
	}
	all := a.screenDocuments()
	for i, other := range all {
		if other == d {
			a.selectScreen(all, i)
			return
		}
	}
}

// saveFailed turns a failed save into a decision. The buffer and its dirty
// state are untouched: only a later successful save marks it saved.
func (a *App) saveFailed(d *Document, path string, overwrite bool, done func(), err error) {
	a.dirty = true
	if isCancelled(err) {
		a.Status = "Not saved: " + err.Error()
		return
	}
	a.focusDocument(d)
	var refusal saveRefusal
	if errors.As(err, &refusal) {
		a.Status = refusal.Error()
		return
	}
	retry := func() { a.saveDocumentThen(d, path, overwrite, done) }
	notSaved := func() { a.Status = "Not saved"; a.dirty = true }
	name := filepath.Base(path)
	display := path
	if path == d.Path {
		display = d.displayPath()
		name = filepath.Base(display)
	}
	remote := d.Source != nil && path == d.Path
	switch {
	case !remote && errors.Is(err, fsops.ErrInPlaceRequired):
		a.confirm("Save in place", fmt.Sprintf("%s is not writable, so %s will be rewritten in place (not atomically). Continue?", filepath.Dir(path), name), []string{"Save", "Cancel"}, func(i int) {
			if i != 0 {
				notSaved()
				return
			}
			d.inplaceConfirmed = path
			retry()
		})
	case remote && (errors.Is(err, errRemoteChanged) || strings.Contains(err.Error(), "remote file changed")):
		a.confirm("File changed on disk", name+" was modified on the server.\nOverwrite it with your text?", []string{"Overwrite", "Save as…", "Cancel"}, func(i int) {
			switch i {
			case 0:
				a.saveSourceDocument(d, done, true)
			case 1:
				a.saveAsThen(d, done)
			default:
				notSaved()
			}
		})
	case !remote && errors.Is(err, fsops.ErrChangedOnDisk):
		a.confirm("File changed on disk", name+" was modified by another program (size/mtime).\nOverwrite it with your text?", []string{"Overwrite", "Save as…", "Cancel"}, func(i int) {
			switch i {
			case 0:
				current, err := os.Lstat(path)
				if err != nil {
					a.saveFailed(d, path, overwrite, done, err)
					return
				}
				if path == d.Path {
					d.Info, d.infoLost = current, false
				}
				retry()
			case 1:
				a.saveAsThen(d, done)
			default:
				notSaved()
			}
		})
	case !remote && path == d.Path && d.Info != nil && errors.Is(err, fs.ErrNotExist) && missing(path):
		a.confirm("File was deleted", name+" no longer exists on disk. Create it again?", []string{"Create", "Save as…", "Cancel"}, func(i int) {
			switch i {
			case 0:
				d.Info, d.infoLost = nil, false
				a.saveDocumentThen(d, path, false, done)
			case 1:
				a.saveAsThen(d, done)
			default:
				notSaved()
			}
		})
	default:
		a.confirm("Save failed", display+"\n"+saveReason(err), []string{"Retry", "Save as…", "Cancel"}, func(i int) {
			switch i {
			case 0:
				retry()
			case 1:
				a.saveAsThen(d, done)
			default:
				notSaved()
			}
		})
	}
}

func missing(path string) bool {
	_, err := os.Lstat(path)
	return errors.Is(err, fs.ErrNotExist)
}

// pagedReadFailed reports, once, that a paged file can no longer be read; if
// the file itself is gone, offers to close the document.
func (a *App) pagedReadFailed(d *Document) {
	a.Status = d.notice
	d.notice = ""
	a.dirty = true
	if d.File == nil || a.Form != nil || a.Dialog != nil {
		return
	}
	if _, err := d.File.Stat(); err != nil {
		a.confirm("Cannot read file", d.displayPath()+"\n"+errorReason(err), []string{"OK"}, func(int) {
			if a.Doc == d {
				a.finishCloseDocument()
			}
		})
	}
}

// reloadDocument rereads the file into the same screen, after confirming that
// unsaved edits may be discarded. Remote documents are downloaded again.
func (a *App) reloadDocument() {
	d := a.Doc
	if d == nil || d.Help {
		return
	}
	if d.ArchiveSource {
		a.Status = "Return to the archive panel to reopen the file"
		return
	}
	reload := func() {
		replace := func(n *Document) { a.replaceReloaded(d, n) }
		if d.Source != nil {
			a.openSourceDocument(d.Source, d.Edit, replace, nil)
			return
		}
		if a.Async {
			a.openDocumentAsync(d.Path, d.Edit, replace)
			return
		}
		n, err := a.loadDocument(d.Path, d.Edit)
		if err != nil {
			a.openFailed(d.Path, d.Edit, err)
			return
		}
		a.presentDocument(n, replace)
	}
	if !d.Dirty {
		reload()
		return
	}
	a.confirm("Reload", "Discard changes and reload "+filepath.Base(d.displayPath())+"?", []string{"Reload", "Cancel"}, func(i int) {
		if i == 0 {
			reload()
		}
	})
}

// replaceReloaded swaps fresh content into an open document, keeping its
// screen, view settings, bookmarks and approximately its position.
func (a *App) replaceReloaded(d, n *Document) {
	old := *d
	at := old.Pos(old.Cursor)
	releaseDocument(d)
	*d = *n
	d.ScreenID = old.ScreenID
	d.Positions, d.Search = old.Positions, old.Search
	d.Wrap, d.WordWrap, d.Scrollbar = old.Wrap, old.WordWrap, old.Scrollbar
	d.HideKeys, d.HideStatus, d.BytesPerLine = old.HideKeys, old.HideStatus, old.BytesPerLine
	d.ReadOnly = old.ReadOnly
	if d.File == nil && n.Mode == "text" {
		d.Mode = old.Mode
	}
	d.Cursor = d.At(at.Row, at.Col)
	d.Offset = max(0, min(old.Offset, len(d.Lines())-1))
	d.Column = old.Column
	d.ViewStart = d.At(d.Offset, 0)
	d.ByteOffset = max(0, min(old.ByteOffset, max(int64(0), d.Size-1)))
	d.ClearSelection()
	if a.Doc != d {
		a.showDocument(d)
	}
	a.Status = "Reloaded " + filepath.Base(d.displayPath())
	a.dirty = true
}
