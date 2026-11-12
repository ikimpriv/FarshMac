package ui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// setup.json is shared by every window of this process and by other FarshMac
// instances. Windows of one process keep identical Settings (WindowGroup
// propagates changes); histories are merged on save; a write by another
// instance since this process last read or wrote the file is never
// overwritten silently.
var (
	setupMu    sync.Mutex
	setupFiles = map[string]*setupFileState{}
)

type setupFileState struct {
	exists     bool
	modTime    time.Time
	size       int64
	unreadable bool // could not be read at startup; overwriting needs consent
}

var (
	errSetupUnread  = errors.New("setup.json could not be read at startup")
	errSetupChanged = errors.New("setup.json was changed by another FarshMac instance")
)

// rememberSetupFile records the on-disk version this process knows about.
// Callers hold setupMu.
func rememberSetupFile(path string) {
	state := setupFiles[path]
	if state == nil {
		state = &setupFileState{}
		setupFiles[path] = state
	}
	info, err := os.Stat(path)
	state.exists = err == nil
	if err == nil {
		state.modTime, state.size = info.ModTime(), info.Size()
	}
}
func setupChangedOnDisk(path string) bool {
	state := setupFiles[path]
	if state == nil {
		return false // never loaded here (tests, first run): nothing to protect
	}
	info, err := os.Stat(path)
	if err != nil {
		return state.exists && !os.IsNotExist(err)
	}
	return !state.exists || !info.ModTime().Equal(state.modTime) || info.Size() != state.size
}

// preserveBrokenConfig moves a damaged config file aside instead of letting
// the next save overwrite it.
func preserveBrokenConfig(path string) (string, error) {
	broken := path + ".broken-" + time.Now().Format("20060102-150405")
	for i := 1; ; i++ {
		if _, err := os.Lstat(broken); os.IsNotExist(err) {
			break
		}
		broken = fmt.Sprintf("%s.broken-%s-%d", path, time.Now().Format("20060102-150405"), i)
	}
	return broken, os.Rename(path, broken)
}
func writeConfigAtomically(path, pattern string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), pattern)
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	return err
}

// readSetupFile decodes setup.json for LoadSetup. It reports problems in the
// status line and never leaves a damaged file in place to be overwritten.
func (a *App) readSetupFile() (savedSetup, bool) {
	path := setupPath()
	s := savedSetup{Settings: defaultSettings()}
	setupMu.Lock()
	defer setupMu.Unlock()
	b, err := readConfigFile(path)
	if os.IsNotExist(err) {
		rememberSetupFile(path)
		return s, false
	}
	if err != nil {
		rememberSetupFile(path)
		setupFiles[path].unreadable = true
		a.Status = "Cannot read setup.json: " + err.Error() + "; using defaults, Save setup is disabled"
		return s, false
	}
	if len(bytes.TrimSpace(b)) == 0 {
		rememberSetupFile(path)
		return s, false
	}
	err = json.Unmarshal(b, &s)
	var typeErr *json.UnmarshalTypeError
	if err != nil && !errors.As(err, &typeErr) {
		s = savedSetup{Settings: defaultSettings()}
		if _, statErr := os.Stat(path); statErr != nil {
			// Only the read-only pre-rename fallback is damaged; leave it alone.
			rememberSetupFile(path)
			a.Status = "Previous setup.json is damaged; defaults loaded"
			return s, false
		}
		broken, renameErr := preserveBrokenConfig(path)
		rememberSetupFile(path)
		if renameErr != nil {
			setupFiles[path].unreadable = true
			a.Status = "Cannot read setup.json: " + err.Error() + "; using defaults, Save setup is disabled"
			return s, false
		}
		a.Status = "setup.json is damaged; saved as " + filepath.Base(broken) + ", defaults loaded"
		return s, false
	}
	rememberSetupFile(path)
	if typeErr != nil {
		field := typeErr.Field
		if field == "" {
			field = "a value"
		}
		a.Status = "setup.json: " + field + " has wrong type; default used"
	}
	return s, true
}

// writeSetup writes setup.json without asking. force overrides the
// unreadable-at-startup and changed-by-another-instance protections.
func (a *App) writeSetup(force bool) error {
	path := setupPath()
	setupMu.Lock()
	defer setupMu.Unlock()
	if !force {
		if state := setupFiles[path]; state != nil && state.unreadable {
			return errSetupUnread
		}
		if setupChangedOnDisk(path) {
			return errSetupChanged
		}
	}
	s := a.setupSnapshot()
	if b, err := os.ReadFile(path); err == nil {
		var old savedSetup
		if json.Unmarshal(b, &old) == nil {
			s.Commands = mergeHistory(old.Commands, s.Commands, a.Settings.HistoryLimit)
			s.Folders = mergeHistory(old.Folders, s.Folders, a.Settings.HistoryLimit)
			s.Viewed = mergeViewHistory(old.Viewed, s.Viewed, a.Settings.HistoryLimit)
		}
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err == nil {
		err = writeConfigAtomically(path, ".setup-*", b)
	}
	if err == nil {
		rememberSetupFile(path)
		setupFiles[path].unreadable = false
	}
	return err
}

// saveSetup is the interactive save (Shift+F9, menus, shortcuts). It asks
// before overwriting a file it could not read or another instance changed.
func (a *App) saveSetup() error {
	profileErr := a.syncRemoteProfiles()
	defer func() {
		if profileErr != nil && a.Form == nil {
			a.Status += "; profile not saved: " + profileErr.Error()
		}
	}()
	err := a.writeSetup(false)
	question := ""
	switch {
	case errors.Is(err, errSetupUnread):
		question = "setup.json could not be read at startup. Overwrite it with current settings?"
	case errors.Is(err, errSetupChanged):
		question = "setup.json was changed by another FarshMac instance. Overwrite?"
	}
	if question != "" {
		a.confirm("Save setup", question, []string{"Overwrite", "Cancel"}, func(i int) {
			if i == 0 {
				a.reportSetupSaved(a.writeSetup(true))
			} else {
				a.Status = "Setup not saved"
			}
		})
		return err
	}
	a.reportSetupSaved(err)
	return err
}
func (a *App) reportSetupSaved(err error) {
	a.Status = "Setup saved"
	a.error(err)
}

// autoSaveSetup is the non-interactive save used when a window closes.
func (a *App) autoSaveSetup() error {
	if !a.Settings.AutoSave || a.autoSaveDone {
		return nil
	}
	a.autoSaveDone = true
	err := a.writeSetup(false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "FarshMac: setup not saved:", err)
	}
	return err
}
func mergeHistory(old, mine []string, limit int) []string {
	var out []string
	for _, list := range [][]string{old, mine} {
		for _, s := range list {
			out = appendUnique(out, s, limit)
		}
	}
	return out
}
func mergeViewHistory(old, mine []viewRecord, limit int) []viewRecord {
	var out []viewRecord
	for _, list := range [][]viewRecord{old, mine} {
		for _, v := range list {
			for i := range out {
				if out[i] == v {
					out = append(out[:i], out[i+1:]...)
					break
				}
			}
			out = append(out, v)
		}
	}
	if limit = max(1, limit); len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// copySettings gives dst an independent copy of src's shared settings.
func copySettings(dst *App, src *App) {
	b, _ := json.Marshal(src.Settings)
	s := Settings{}
	if json.Unmarshal(b, &s) == nil {
		dst.Settings = s
	}
}

// inheritWindowState gives a new window the parent's settings and histories
// (Far keeps one command/folder/view history per user, not per window).
func (a *App) inheritWindowState(parent *App) {
	copySettings(a, parent)
	a.history = append([]string(nil), parent.history...)
	a.historyAt = len(a.history)
	a.folderHistory = append([]string(nil), parent.folderHistory...)
	a.viewHistory = append([]viewRecord(nil), parent.viewHistory...)
	if parent.dialogValues != nil {
		a.dialogValues = map[string]string{}
		for k, v := range parent.dialogValues {
			a.dialogValues[k] = v
		}
	}
}

// syncRemoteProfiles stores the current folder of connected panels in the
// connection profile they were opened from (connections.json is what is
// read at startup, so updating only setup.json would have no effect).
func (a *App) syncRemoteProfiles() error {
	var first error
	for i := range a.Panels {
		p := &a.Panels[i]
		if p.Remote == nil || p.Path == "" || p.Path == p.Remote.Directory {
			continue
		}
		updated := *p.Remote
		updated.Directory = p.Path
		if err := a.updateConnectionProfile(*p.Remote, updated); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		p.Remote.Directory = p.Path
	}
	return first
}
