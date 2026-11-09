package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func (a *App) folderShortcutKey(m Message) bool {
	if !m.Ctrl || m.Meta || m.Alt || strings.HasPrefix(m.Code, "Numpad") {
		return false
	}
	key := m.Key
	if len(m.Code) == 6 && strings.HasPrefix(m.Code, "Digit") {
		key = m.Code[5:]
	}
	n, err := strconv.Atoi(key)
	if err != nil || n < 1 || n > 9 {
		return false
	}
	a.FastFind = nil
	if m.Shift {
		a.saveFolderShortcut(n)
	} else {
		a.goFolderShortcut(n)
	}
	return true
}
func (a *App) saveFolderShortcut(n int) {
	p := &a.Panels[a.Active]
	a.Settings.Shortcuts[n] = p.Path
	a.Settings.ShortcutRemotes[n] = nil
	a.Settings.ShortcutArchives[n] = ""
	if p.Remote != nil {
		c := *p.Remote
		c.Directory = p.Path
		c.Password = ""
		c.KeyPassphrase = ""
		c.HostKey = nil
		a.Settings.ShortcutRemotes[n] = &c
	}
	if p.Archive != nil {
		a.Settings.ShortcutArchives[n] = p.Archive.Path
	}
	a.saveSetup()
	if a.Status == "Setup saved" {
		a.Status = fmt.Sprintf("Folder shortcut %d saved: %s", n, p.Path)
	}
}
func (a *App) goFolderShortcut(n int) {
	path := a.Settings.Shortcuts[n]
	if path == "" {
		a.Status = fmt.Sprintf("Folder shortcut %d is empty", n)
		return
	}
	p := &a.Panels[a.Active]
	if c := a.Settings.ShortcutRemotes[n]; c != nil {
		for i := range a.Panels {
			other := &a.Panels[i]
			if other.Remote != nil && other.Remote.Label() == c.Label() {
				live := *other.Remote
				live.Directory = path
				a.connectRemote(a.Active, live)
				return
			}
		}
		a.connectionDialog(*c)
		return
	}
	if archive := a.Settings.ShortcutArchives[n]; archive != "" {
		a.openArchiveAt(a.Active, archive, path)
		return
	}
	// Validate before leaving a remote/archive panel, preserving it on errors.
	if info, err := os.Stat(path); err != nil {
		a.error(err)
		return
	} else if !info.IsDir() {
		a.Status = "Shortcut is not a directory"
		return
	}
	p.cancelPanelLoad()
	p.cancelTree()
	p.Remote = nil
	p.Archive = nil
	p.states = nil
	a.error(p.Load(path))
}
