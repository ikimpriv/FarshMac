package ui

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
)

// documentSource is where a document that is not a local file comes from: a
// file on a server, a file in a container, a Kubernetes manifest. The viewer
// and editor work on a private local copy; the source fetches and stores it.
type documentSource interface {
	// Display names the document for titles and messages (label + path).
	Display() string
	// Name is the file name for the local copy and Save as.
	Name() string
	// Fetch copies the document into dir and returns the local path with the
	// attributes that later detect a change on the source (nil if none).
	Fetch(o *fsops.Operation, dir string) (local string, info os.FileInfo, err error)
	// Store uploads the staged local file. expected is what Fetch or the last
	// Store returned (nil = unknown); unless force is set, a source that
	// changed since then fails with errRemoteChanged. It returns the new
	// attributes, or nil when they could not be examined.
	Store(o *fsops.Operation, staged string, expected os.FileInfo, force bool) (os.FileInfo, error)
	// Blocked predicts why saving will fail ("" when it should work).
	Blocked(info os.FileInfo) string
}

// remoteSource is a file on a server (FTP/FTPS/SFTP) or in a container:
// anything reachable through a remotefs.Opener.
type remoteSource struct {
	Opener remotefs.Opener
	Label  string
	Path   string
}

func (s *remoteSource) Display() string { return s.Label + s.Path }
func (s *remoteSource) Name() string    { return path.Base(s.Path) }
func (s *remoteSource) Blocked(info os.FileInfo) string {
	if info != nil && info.Mode().Perm()&0222 == 0 {
		return fmt.Sprintf("mode %v", info.Mode().Perm())
	}
	return ""
}
func (s *remoteSource) Fetch(o *fsops.Operation, dir string) (string, os.FileInfo, error) {
	f, err := s.Opener.Open(o.Context())
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	info, err := f.Lstat(s.Path)
	if err != nil {
		return "", nil, err
	}
	source := s.Path
	if source, info, err = resolveRemoteLink(f, s.Path, info); err != nil {
		return "", nil, err
	}
	s.Path = source // Saving replaces the link target, never the link.
	local := filepath.Join(dir, path.Base(source))
	out, err := os.OpenFile(local, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", nil, err
	}
	in, err := f.Open(source)
	if err != nil {
		out.Close()
		return "", nil, err
	}
	o.Add(source, info.Size())
	o.Ready()
	o.Begin(source, info.Size())
	n, err := io.CopyBuffer(struct{ io.Writer }{out}, o.Reader(in), make([]byte, 256*1024))
	readErr, writeErr := in.Close(), out.Close()
	if err == nil {
		err = readErr
	}
	if err == nil {
		err = writeErr
	}
	if err == nil && n != info.Size() {
		err = fmt.Errorf("remote file changed during download")
	}
	if err != nil {
		return "", nil, err
	}
	o.Complete(source)
	return local, info, nil
}
func (s *remoteSource) Store(o *fsops.Operation, staged string, expected os.FileInfo, force bool) (os.FileInfo, error) {
	f, err := s.Opener.Open(o.Context())
	if err != nil {
		return nil, err
	}
	defer f.Close()
	current, err := f.Lstat(s.Path)
	if err != nil {
		return nil, err
	}
	// Unknown attributes (an earlier upload whose result could not be
	// examined) cannot be compared; that upload is what is on the server.
	if !force && expected != nil && !sameRemoteInfo(current, expected) {
		return nil, errRemoteChanged
	}
	mode := current.Mode().Perm()
	options := remotefs.CopyOptions{Workers: 1, Metadata: fsops.MetadataOptions{Permissions: true}, PermissionsMode: &mode, ExpectedDestination: current, Conflict: func(string, string) int { return 0 }}
	if err = remotefs.Copy(remotefs.Endpoint{}, remotefs.Endpoint{Opener: s.Opener}, []remotefs.Pair{{Source: staged, Destination: s.Path}}, options, o); err != nil {
		return nil, err
	}
	if o.Check() != nil {
		return nil, nil
	}
	info, err := f.Lstat(s.Path)
	if err != nil {
		return nil, nil
	}
	return info, nil
}

// openSourceDocument copies a document from its source into a private cache
// and opens it. show receives the document on the engine queue; failed runs
// on failure.
func (a *App) openSourceDocument(src documentSource, edit bool, show func(*Document), failed func()) {
	settings := a.Settings
	display := src.Display()
	a.backgroundTask("Downloading "+src.Name(), func(o *fsops.Operation) (func(), func(), error) {
		var doc *Document
		var dir string
		retained := false
		cleanup := func() {
			if !retained {
				if doc != nil && doc.File != nil {
					doc.File.Close()
				}
				if dir != "" {
					os.RemoveAll(dir)
				}
			}
		}
		fail := func(err error) (func(), func(), error) {
			if isCancelled(err) {
				return nil, cleanup, err
			}
			return func() {
				a.openFailed(display, edit, err)
				if failed != nil {
					failed()
				}
			}, cleanup, nil
		}
		var err error
		if dir, err = os.MkdirTemp("", "farshmac-remote-view-*"); err != nil {
			return fail(err)
		}
		local, info, err := src.Fetch(o, dir)
		if err != nil {
			return fail(err)
		}
		worker := &App{Settings: settings, Operation: o}
		doc, err = worker.loadDocument(local, edit)
		if err != nil {
			return fail(err)
		}
		doc.Source = src
		doc.SourceInfo = info
		doc.TempDir = dir
		doc.ScreenID = 0
		if edit {
			doc.checkSaveAccess()
		}
		return func() {
			retained = true
			a.presentDocument(doc, func(d *Document) {
				show(d)
				a.Status = src.Display()
			})
		}, cleanup, nil
	})
}

func sameRemoteInfo(a, b os.FileInfo) bool {
	return a != nil && b != nil && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && a.Mode() == b.Mode()
}

// saveSourceDocument stores the document through a staged local file. force
// skips the "changed on the source" check after the user chose to overwrite.
func (a *App) saveSourceDocument(d *Document, done func(), force bool) {
	copy := cloneDocument(d)
	oldPath := d.Path
	src := d.Source
	staged := filepath.Join(d.TempDir, "save-"+rand.Text())
	a.backgroundTask("Saving "+src.Name(), func(o *fsops.Operation) (func(), func(), error) {
		retained := false
		cleanup := func() {
			if !retained {
				os.Remove(staged)
			}
		}
		fail := func(err error) (func(), func(), error) {
			if isCancelled(err) {
				return nil, cleanup, err
			}
			return func() { a.saveFailed(d, oldPath, false, done, err) }, cleanup, nil
		}
		stageOperation := fsops.NewOperation("Preparing upload")
		stop := context.AfterFunc(o.Context(), stageOperation.Cancel)
		defer stop()
		defer stageOperation.Cancel()
		worker := &App{Operation: stageOperation}
		if err := worker.saveDocument(copy, staged, false); err != nil {
			return fail(err)
		}
		a.metadataQuestions(o, false)
		info, err := src.Store(o, staged, copy.SourceInfo, force)
		if err != nil {
			return fail(err)
		}
		// The source now holds the new text. Whatever happens next, the
		// document must not stay dirty against a version that is gone.
		status := "Remote file saved"
		copy.SourceInfo = info
		if info == nil {
			status = "Remote file saved; attributes unknown"
		}
		return func() {
			retained = true
			*d = *copy
			os.Remove(oldPath)
			a.Status = status
			if done != nil && o.Check() == nil {
				done()
			}
		}, cleanup, nil
	})
	if a.task != nil {
		a.task.applyCompleted = true
	}
}

// Display and syntax detection use the source name, never the temporary cache.
func (d *Document) displayPath() string {
	if d.Source != nil {
		return d.Source.Display()
	}
	return d.Path
}
