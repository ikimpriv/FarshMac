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
	a.openSourceDocument(&remoteSource{Config: *p.Remote, Path: p.Current()}, edit, a.showDocument, nil)
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
