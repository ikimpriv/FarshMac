package ui

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
)

func (a *App) openRemoteDocument(edit bool) {
	p := &a.Panels[a.Active]
	if len(p.Entries) == 0 || p.Entries[p.Cursor].Dir {
		return
	}
	remotePath := p.Current()
	config := *p.Remote
	settings := a.Settings
	a.backgroundTask("Downloading remote file", func(o *fsops.Operation) (func(), func(), error) {
		f, err := config.Dial(o.Context())
		if err != nil {
			return nil, nil, err
		}
		defer f.Close()
		info, err := f.Lstat(remotePath)
		if err != nil {
			return nil, nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("viewer requires a regular remote file")
		}
		dir, err := os.MkdirTemp("", "farshmac-remote-view-*")
		if err != nil {
			return nil, nil, err
		}
		retained := false
		var doc *Document
		cleanup := func() {
			if !retained {
				if doc != nil && doc.File != nil {
					doc.File.Close()
				}
				os.RemoveAll(dir)
			}
		}
		local := filepath.Join(dir, filepath.Base(remotePath))
		out, err := os.OpenFile(local, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, cleanup, err
		}
		in, err := f.Open(remotePath)
		if err != nil {
			out.Close()
			return nil, cleanup, err
		}
		o.Add(remotePath, info.Size())
		o.Ready()
		o.Begin(remotePath, info.Size())
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
			return nil, cleanup, err
		}
		worker := &App{Settings: settings, Operation: o}
		worker.openDocument(local, edit)
		doc = worker.Doc
		if doc == nil {
			return nil, cleanup, fmt.Errorf("%s", worker.Status)
		}
		doc.Remote = &config
		doc.RemotePath = remotePath
		doc.RemoteInfo = info
		doc.TempDir = dir
		doc.ScreenID = 0
		o.Complete(remotePath)
		return func() { retained = true; a.showDocument(doc); a.Status = config.Label() + remotePath }, cleanup, nil
	})
}

func sameRemoteInfo(a, b os.FileInfo) bool {
	return a != nil && b != nil && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && a.Mode() == b.Mode()
}
func (a *App) saveRemoteDocument(d *Document, done func()) {
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
		f, err := config.Dial(o.Context())
		if err != nil {
			return nil, cleanup, err
		}
		defer f.Close()
		current, err := f.Lstat(copy.RemotePath)
		if err != nil {
			return nil, cleanup, err
		}
		if !sameRemoteInfo(current, copy.RemoteInfo) {
			return nil, cleanup, fmt.Errorf("remote file changed; reopen before saving")
		}
		stageOperation := fsops.NewOperation("Preparing upload")
		stop := context.AfterFunc(o.Context(), stageOperation.Cancel)
		defer stop()
		defer stageOperation.Cancel()
		worker := &App{Operation: stageOperation}
		if err = worker.saveDocument(copy, staged, false); err != nil {
			return nil, cleanup, err
		}
		a.metadataQuestions(o, false)
		mode := current.Mode().Perm()
		options := remotefs.CopyOptions{Workers: 1, Metadata: fsops.MetadataOptions{Permissions: true}, PermissionsMode: &mode, ExpectedDestination: current, Conflict: func(string, string) int { return 0 }}
		err = remotefs.Copy(remotefs.Endpoint{}, remotefs.Endpoint{Remote: &config}, []remotefs.Pair{{Source: staged, Destination: copy.RemotePath}}, options, o)
		if err != nil {
			return nil, cleanup, err
		}
		copy.RemoteInfo, err = f.Lstat(copy.RemotePath)
		if err != nil {
			return nil, cleanup, err
		}
		return func() {
			retained = true
			*d = *copy
			os.Remove(oldPath)
			a.Status = "Remote file saved"
			if done != nil {
				done()
			}
		}, cleanup, nil
	})
}

// Display and syntax detection use the remote name, never the temporary cache.
func (d *Document) displayPath() string {
	if d.Remote != nil {
		return d.Remote.Label() + d.RemotePath
	}
	return d.Path
}
