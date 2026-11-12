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

func (a *App) openRemoteDocument(edit bool) {
	p := &a.Panels[a.Active]
	if len(p.Entries) == 0 || p.Entries[p.Cursor].Dir {
		return
	}
	a.downloadRemoteDocument(*p.Remote, p.Current(), edit, a.showDocument, nil)
}

// resolveRemoteLink follows a symbolic link to the regular file it names.
// Saving then replaces the target, never the link itself.
func resolveRemoteLink(f remotefs.FS, remotePath string, info os.FileInfo) (string, os.FileInfo, error) {
	name := path.Base(remotePath)
	for hops := 0; info.Mode()&os.ModeSymlink != 0; hops++ {
		if hops == 8 {
			return "", nil, openRefusal(name + " is a link loop")
		}
		target, err := f.Readlink(remotePath)
		if err != nil {
			return "", nil, openRefusal(name + " is a symbolic link; copy it with F5 first")
		}
		if !path.IsAbs(target) {
			target = path.Join(path.Dir(remotePath), target)
		}
		remotePath = target
		if info, err = f.Lstat(remotePath); err != nil {
			if os.IsNotExist(err) {
				return "", nil, openRefusal(name + " is a broken link")
			}
			return "", nil, err
		}
	}
	if info.IsDir() {
		return "", nil, openRefusal(name + " is a link to a folder")
	}
	if !info.Mode().IsRegular() {
		return "", nil, openRefusal(name + " is not a regular file")
	}
	return remotePath, info, nil
}

// downloadRemoteDocument copies a remote file into a private cache and opens
// it. show receives the document on the engine queue; failed runs on failure.
func (a *App) downloadRemoteDocument(config remotefs.Config, remotePath string, edit bool, show func(*Document), failed func()) {
	settings := a.Settings
	display := config.Label() + remotePath
	a.backgroundTask("Downloading remote file", func(o *fsops.Operation) (func(), func(), error) {
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
		f, err := config.Dial(o.Context())
		if err != nil {
			return fail(err)
		}
		defer f.Close()
		info, err := f.Lstat(remotePath)
		if err != nil {
			return fail(err)
		}
		source := remotePath
		if source, info, err = resolveRemoteLink(f, remotePath, info); err != nil {
			return fail(err)
		}
		if dir, err = os.MkdirTemp("", "farshmac-remote-view-*"); err != nil {
			return fail(err)
		}
		local := filepath.Join(dir, path.Base(source))
		out, err := os.OpenFile(local, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return fail(err)
		}
		in, err := f.Open(source)
		if err != nil {
			out.Close()
			return fail(err)
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
			return fail(err)
		}
		worker := &App{Settings: settings, Operation: o}
		doc, err = worker.loadDocument(local, edit)
		if err != nil {
			return fail(err)
		}
		doc.Remote = &config
		doc.RemotePath = source
		doc.RemoteInfo = info
		doc.TempDir = dir
		doc.ScreenID = 0
		if edit {
			doc.checkSaveAccess()
		}
		o.Complete(source)
		return func() {
			retained = true
			a.presentDocument(doc, func(d *Document) {
				show(d)
				a.Status = config.Label() + source
			})
		}, cleanup, nil
	})
}

func sameRemoteInfo(a, b os.FileInfo) bool {
	return a != nil && b != nil && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && a.Mode() == b.Mode()
}

// saveRemoteDocument uploads through a staged local file. force skips the
// "changed on the server" check after the user chose to overwrite.
func (a *App) saveRemoteDocument(d *Document, done func(), force bool) {
	copy := cloneDocument(d)
	oldPath := d.Path
	config := *d.Remote
	staged := filepath.Join(d.TempDir, "save-"+rand.Text())
	a.backgroundTask("Saving remote file", func(o *fsops.Operation) (func(), func(), error) {
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
		f, err := config.Dial(o.Context())
		if err != nil {
			return fail(err)
		}
		defer f.Close()
		current, err := f.Lstat(copy.RemotePath)
		if err != nil {
			return fail(err)
		}
		// Unknown attributes (an earlier upload whose result could not be
		// examined) cannot be compared; that upload is what is on the server.
		if !force && copy.RemoteInfo != nil && !sameRemoteInfo(current, copy.RemoteInfo) {
			return fail(errRemoteChanged)
		}
		stageOperation := fsops.NewOperation("Preparing upload")
		stop := context.AfterFunc(o.Context(), stageOperation.Cancel)
		defer stop()
		defer stageOperation.Cancel()
		worker := &App{Operation: stageOperation}
		if err = worker.saveDocument(copy, staged, false); err != nil {
			return fail(err)
		}
		a.metadataQuestions(o, false)
		mode := current.Mode().Perm()
		options := remotefs.CopyOptions{Workers: 1, Metadata: fsops.MetadataOptions{Permissions: true}, PermissionsMode: &mode, ExpectedDestination: current, Conflict: func(string, string) int { return 0 }}
		err = remotefs.Copy(remotefs.Endpoint{}, remotefs.Endpoint{Remote: &config}, []remotefs.Pair{{Source: staged, Destination: copy.RemotePath}}, options, o)
		if err != nil {
			return fail(err)
		}
		// The server now holds the new text. Whatever happens next, the
		// document must not stay dirty against a version that is gone.
		status := "Remote file saved"
		if o.Check() == nil {
			copy.RemoteInfo, err = f.Lstat(copy.RemotePath)
		}
		if o.Check() != nil || err != nil {
			copy.RemoteInfo = nil
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

// Display and syntax detection use the remote name, never the temporary cache.
func (d *Document) displayPath() string {
	if d.Remote != nil {
		return d.Remote.Label() + d.RemotePath
	}
	return d.Path
}
