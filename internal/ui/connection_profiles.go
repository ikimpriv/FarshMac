package ui

import (
	"encoding/json"
	"farshmac/internal/remotefs"
	"os"
	"path/filepath"
	"sync"
)

var profilesMu sync.Mutex

func profilesPath() string { return filepath.Join(filepath.Dir(setupPath()), "connections.json") }
func (a *App) loadConnectionProfiles() {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	b, err := readConfigFile(profilesPath())
	if err != nil {
		return
	}
	var profiles []remotefs.Config
	if json.Unmarshal(b, &profiles) == nil {
		a.Settings.RemoteConnections = profiles
	}
}
func (a *App) saveConnectionProfile(profile remotefs.Config) error {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	profiles := append([]remotefs.Config(nil), a.Settings.RemoteConnections...)
	b, err := readConfigFile(profilesPath())
	if err == nil {
		if err = json.Unmarshal(b, &profiles); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	profile.Password, profile.KeyPassphrase, profile.HostKey = "", "", nil
	found := false
	for i, old := range profiles {
		if old.Label() == profile.Label() {
			profiles[i] = profile
			found = true
			break
		}
	}
	if !found {
		profiles = append(profiles, profile)
	}
	b, err = json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(profilesPath()), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(profilesPath()), ".connections-*")
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
		err = os.Rename(f.Name(), profilesPath())
	}
	if err == nil {
		a.Settings.RemoteConnections = profiles
	}
	return err
}
