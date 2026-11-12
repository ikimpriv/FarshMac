package ui

import (
	"farshmac/internal/fsops"
	"fmt"
)

func cloneDocument(d *Document) *Document {
	c := *d
	c.Buffer = d.Buffer.Clone()
	c.Positions = append([]docPosition(nil), d.Positions...)
	return &c
}

func (a *App) openDocumentAsync(path string, edit bool, opened func(*Document)) {
	settings := a.Settings
	a.backgroundTask("Opening file", func(o *fsops.Operation) (func(), func(), error) {
		worker := &App{Settings: settings, Operation: o}
		d, err := worker.loadDocument(path, edit)
		if err != nil {
			if isCancelled(err) {
				return nil, nil, err
			}
			return func() { a.openFailed(path, edit, err) }, nil, nil
		}
		d.ScreenID = 0 // Allocate the screen ID in the owning window, not the worker.
		retained := false
		cleanup := func() {
			if !retained && d.File != nil {
				d.File.Close()
			}
		}
		return func() {
			retained = true
			a.recordView(d.Path, edit)
			show := a.showDocument
			if opened != nil {
				show = opened
			}
			a.presentDocument(d, show)
		}, cleanup, nil
	})
}

func (a *App) saveDocumentThen(d *Document, path string, overwrite bool, done func()) {
	if d.Remote != nil && path == d.Path {
		a.saveRemoteDocument(d, done, false)
		return
	}
	if !a.Async {
		if err := a.saveDocument(d, path, overwrite); err != nil {
			a.saveFailed(d, path, overwrite, done, err)
		} else {
			if d.Remote != nil {
				d.Remote = nil
				d.RemoteInfo = nil
				d.RemotePath = ""
			}
			if done != nil {
				done()
			}
		}
		return
	}
	copy := cloneDocument(d)
	if copy.Remote != nil {
		copy.Remote = nil
		copy.RemoteInfo = nil
		copy.RemotePath = ""
	}
	a.backgroundTask("Saving file", func(o *fsops.Operation) (func(), func(), error) {
		worker := &App{Operation: o}
		err := worker.saveDocument(copy, path, overwrite)
		if err != nil && !isCancelled(err) {
			return func() { a.saveFailed(d, path, overwrite, done, err) }, nil, nil
		}
		status := worker.Status
		return func() {
			*d = *copy
			a.Status = status
			if done != nil && o.Check() == nil {
				done()
			}
		}, nil, err
	})
	if a.task != nil {
		a.task.applyCompleted = true
	}
}

func (a *App) findDocumentThen(backward bool, found func()) {
	if !a.Async {
		if a.findDocument(backward) && found != nil {
			found()
		}
		return
	}
	d := a.Doc
	copy := cloneDocument(d)
	a.backgroundTask("Searching document", func(o *fsops.Operation) (func(), func(), error) {
		if err := o.Check(); err != nil {
			return nil, nil, err
		}
		worker := &App{Doc: copy, Operation: o}
		hit := worker.findDocument(backward)
		return func() {
			*d = *copy
			a.Status = worker.Status
			if hit && found != nil {
				found()
			}
		}, nil, nil
	})
}

func (a *App) replaceAllAsync(d *Document, replacement string) {
	copy := cloneDocument(d)
	a.backgroundTask("Replacing matches", func(o *fsops.Operation) (func(), func(), error) {
		if err := o.Check(); err != nil {
			return nil, nil, err
		}
		n, err := copy.ReplaceAll(copy.Search, replacement, o.Check)
		copy.modified()
		copy.Lines()
		return func() { *d = *copy; a.Status = fmt.Sprintf("Replaced %d matches", n) }, nil, err
	})
}

func (a *App) saveAllThen(documents []*Document, done func()) {
	for i, d := range documents {
		if d != nil && d.Dirty {
			a.saveDocumentThen(d, d.Path, false, func() { a.saveAllThen(documents[i+1:], done) })
			return
		}
	}
	done()
}
func (a *App) transformDocument(label string, transform func(*App)) {
	d := a.Doc
	copy := cloneDocument(d)
	settings := a.Settings
	a.backgroundTask(label, func(o *fsops.Operation) (func(), func(), error) {
		if err := o.Check(); err != nil {
			return nil, nil, err
		}
		worker := &App{Doc: copy, Settings: settings}
		transform(worker)
		return func() { *d = *copy; a.Status = worker.Status }, nil, nil
	})
}
