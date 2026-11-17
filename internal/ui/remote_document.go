package ui

import (
	"os"
	"path"

	"farshmac/internal/remotefs"
)

func (a *App) openRemoteDocument(edit bool) {
	p := &a.Panels[a.Active]
	if len(p.Entries) == 0 || p.Entries[p.Cursor].Dir {
		return
	}
	a.openSourceDocument(p.backend().DocumentSource(p, p.Current()), edit, a.showDocument, nil)
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

// New files are staged locally and created remotely only when the editor saves.
func (a *App) newRemoteFile(p *Panel) {
	if !p.can(capNewFile) {
		a.Status = "File creation is unavailable in this panel"
		return
	}
	base := p.Path
	backend := p.backend()
	a.prompt("Edit file", "Existing file or new file name", "", func(name string) {
		if name == "" {
			return
		}
		destination := name
		if !path.IsAbs(name) {
			destination = path.Join(base, name)
		} else if p.Docker != nil {
			_, prefix, _, _ := p.Docker.location(p.Path)
			destination = path.Join(prefix, name)
		} else if p.Kube != nil {
			destination = path.Join(p.kubeLevel().Prefix, name)
		}
		src, ok := backend.DocumentSource(p, path.Clean(destination)).(*remoteSource)
		if !ok {
			a.Status = "File creation is unavailable in this panel"
			return
		}
		src.AllowCreate = true
		a.openSourceDocument(src, true, a.showDocument, nil)
	})
}
