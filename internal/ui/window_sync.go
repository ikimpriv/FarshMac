package ui

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// windowSync is the WindowGroup state that keeps windows of one process (and
// several processes) from overwriting each other's settings and session.
type windowSync struct {
	settingsJSON []byte   // shared Settings last seen in any window
	sessionLock  *os.File // advisory lock owning session.json
}

var errSessionOwned = errors.New("Another FarshMac instance owns the window session; this instance will not save it")

// syncSettings propagates Settings changed in src to every other window, so
// any window that later saves writes the current values (P1-06, P5-10).
// It runs on the engine queue, which owns every App of the group.
func (g *WindowGroup) syncSettings(src *App) {
	current, err := json.Marshal(src.Settings)
	if err != nil {
		return
	}
	if bytes.Equal(current, g.settingsJSON) {
		return
	}
	g.settingsJSON = current
	for _, id := range g.Order {
		b := g.Apps[id]
		if b == nil || b == src || b.closed {
			continue
		}
		old := b.Settings
		var fresh Settings
		if json.Unmarshal(current, &fresh) != nil {
			continue
		}
		b.Settings = fresh
		if panelSettingsChanged(old, b.Settings) {
			b.refresh()
		} else {
			for i := range b.Panels {
				b.configurePanel(i)
			}
		}
		b.dirty = true
	}
}

// primeSettings records the shared settings before the first change, so the
// first edit in any window is recognized and propagated.
func (g *WindowGroup) primeSettings(a *App) {
	if g.settingsJSON == nil {
		g.settingsJSON, _ = json.Marshal(a.Settings)
	}
}
func panelSettingsChanged(a, b Settings) bool {
	if a.ShowHidden != b.ShowHidden || a.IgnoreDSStore != b.IgnoreDSStore || a.SortGroups != b.SortGroups || a.DescriptionFile != b.DescriptionFile || a.HumanReadableSizes != b.HumanReadableSizes || a.FolderDescriptions != b.FolderDescriptions || a.TreeDepth != b.TreeDepth || a.InfoHost != b.InfoHost || a.InfoVolume != b.InfoVolume || len(a.MaskGroups) != len(b.MaskGroups) {
		return true
	}
	for k, v := range a.MaskGroups {
		if b.MaskGroups[k] != v {
			return true
		}
	}
	return false
}

// quitAutoSave runs "Save setup on exit" once for the whole group, from the
// focused window, before any window closes.
func (g *WindowGroup) quitAutoSave() error {
	a := g.Apps[g.focused]
	if a == nil && len(g.Order) > 0 {
		a = g.Apps[g.Order[len(g.Order)-1]]
	}
	if a == nil {
		return nil
	}
	err := a.autoSaveSetup()
	for _, other := range g.Apps {
		other.autoSaveDone = true
	}
	return err
}
func (g *WindowGroup) askQuitWithoutSetup(err error) {
	a := g.Apps[g.focused]
	if a == nil {
		for _, id := range g.Order {
			if a = g.Apps[id]; a != nil {
				break
			}
		}
	}
	a.confirm("Settings were not saved", err.Error()+"\nQuit anyway?", []string{"Quit", "Cancel"}, func(i int) {
		if i == 0 {
			g.finishQuit()
			return
		}
		for _, other := range g.Apps {
			other.autoSaveDone = false
		}
		a.Status = "Setup not saved: " + err.Error()
	})
	a.dirty = true
}

// lockSession takes an advisory lock next to session.json. Only the owner
// restores and writes the session; the lock is released by the OS if the
// owner crashes. Lock failures other than contention are ignored.
func (g *WindowGroup) lockSession(path string) bool {
	if g.sessionLock != nil {
		return true
	}
	dir := filepath.Dir(path)
	if os.MkdirAll(dir, 0700) != nil {
		return true
	}
	f, err := os.OpenFile(filepath.Join(dir, "session.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return true
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return !errors.Is(err, syscall.EWOULDBLOCK)
	}
	g.sessionLock = f
	return true
}
func (g *WindowGroup) unlockSession() {
	if g.sessionLock != nil {
		_ = syscall.Flock(int(g.sessionLock.Fd()), syscall.LOCK_UN)
		g.sessionLock.Close()
		g.sessionLock = nil
	}
}
