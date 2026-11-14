package ui

import (
	"os"
	"path/filepath"
	"strings"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
)

// Folder history entries are plain strings so setup.json stays compatible:
//
//	/Users/x                      local folder (also every legacy entry)
//	sftp://user@host:22/home/x    remote folder; the label never has a password
//	[Archive] /x/a.zip/dir        folder inside an archive file
const archiveHistoryPrefix = "[Archive] "

func (p *Panel) historyEntry(abs string) string {
	if entry := p.backend().HistoryEntry(p, abs); entry != abs {
		return entry
	}
	return abs
}

// parseRemoteHistory splits "scheme://user@host:port/path" into a connection
// (without credentials) and its folder.
func parseRemoteHistory(entry string) (remotefs.Config, string, bool) {
	scheme, rest, ok := strings.Cut(entry, "://")
	if !ok || strings.ContainsAny(scheme, "/ ") {
		return remotefs.Config{}, "", false
	}
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return remotefs.Config{}, "", false
	}
	label, dir := scheme+"://"+rest[:slash], rest[slash:]
	c, err := remotefs.Parse(label, "")
	if err != nil {
		return remotefs.Config{}, "", false
	}
	c.Directory = dir
	return c, label, true
}

// archiveContaining finds the archive file that path lies in.
func archiveContaining(path string) string {
	for dir := path; ; dir = filepath.Dir(dir) {
		if info, err := os.Stat(dir); err == nil && info.Mode().IsRegular() && fsops.ArchiveFormat(dir) != "" {
			return dir
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

// goFolderHistory opens a folder history entry in the active panel with the
// source it was visited from.
func (a *App) goFolderHistory(entry string) {
	index := a.Active
	if path, ok := strings.CutPrefix(entry, archiveHistoryPrefix); ok {
		archive := archiveContaining(path)
		if archive == "" {
			a.Status = "Archive not found: " + path
			return
		}
		a.openArchiveAt(index, archive, path, "")
		return
	}
	if c, label, ok := parseRemoteHistory(entry); ok {
		for i := range a.Panels {
			if other := a.Panels[i].Remote; other != nil && other.Label() == label {
				live := *other
				live.Directory = c.Directory
				a.connectRemote(index, live)
				return
			}
		}
		a.connectionDialog(c)
		return
	}
	if info, err := os.Stat(entry); err != nil {
		a.error(err)
		return
	} else if !info.IsDir() {
		a.Status = "Not a directory: " + entry
		return
	}
	p := &a.Panels[index]
	p.cancelPanelLoad()
	p.cancelTree()
	previous := *p
	p.Remote, p.RemoteLocalPath, p.Archive = nil, "", nil
	a.error(p.loadSource(entry, previous, ""))
}
