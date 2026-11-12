package ui

import (
	"encoding/json"
	"farshmac/internal/remotefs"
	"os"
	"path"
	"path/filepath"
	"sync"
)

var profilesMu sync.Mutex

func profilesPath() string { return filepath.Join(filepath.Dir(setupPath()), "connections.json") }

// A profile is identified by server label and start folder, so one server may
// have several profiles for different folders.
func profileKey(c remotefs.Config) string { return c.Label() + "\x00" + path.Clean("/"+c.Directory) }

func (a *App) loadConnectionProfiles() {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	profiles, err := a.readConnectionProfiles()
	if err == nil && profiles != nil {
		a.Settings.RemoteConnections = profiles
	}
}

// readConnectionProfiles returns nil, nil when there is no file. A damaged
// file is moved aside once (never deleted) so the next save starts afresh
// from the profiles copied in setup.json. Callers hold profilesMu.
func (a *App) readConnectionProfiles() ([]remotefs.Config, error) {
	b, err := readConfigFile(profilesPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var profiles []remotefs.Config
	if err = json.Unmarshal(b, &profiles); err != nil {
		if _, statErr := os.Stat(profilesPath()); statErr != nil {
			// Only the read-only pre-rename fallback is damaged; leave it alone.
			return nil, nil
		}
		broken, renameErr := preserveBrokenConfig(profilesPath())
		if renameErr != nil {
			return nil, err
		}
		a.Status = "connections.json is damaged; saved as " + filepath.Base(broken)
		a.dirty = true
		return nil, nil
	}
	return profiles, nil
}
func (a *App) saveConnectionProfile(profile remotefs.Config) error {
	return a.updateConnectionProfile(profile, profile)
}

// updateConnectionProfile replaces the profile identified by origin with
// profile (for example after the folder changed), or adds it.
func (a *App) updateConnectionProfile(origin, profile remotefs.Config) error {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	return a.editConnectionProfiles(func(profiles []remotefs.Config) []remotefs.Config {
		profile.Password, profile.KeyPassphrase, profile.HostKey, profile.CertFingerprint = "", "", nil, ""
		var out []remotefs.Config
		placed := false
		for _, old := range profiles {
			switch profileKey(old) {
			case profileKey(origin):
				if !placed {
					out = append(out, profile)
					placed = true
				}
			case profileKey(profile):
				if !placed {
					out = append(out, profile)
					placed = true
				}
			default:
				out = append(out, old)
			}
		}
		if !placed {
			out = append(out, profile)
		}
		return out
	})
}
func (a *App) deleteConnectionProfile(profile remotefs.Config) error {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	return a.editConnectionProfiles(func(profiles []remotefs.Config) []remotefs.Config {
		var out []remotefs.Config
		for _, old := range profiles {
			if profileKey(old) != profileKey(profile) {
				out = append(out, old)
			}
		}
		return out
	})
}

// editConnectionProfiles re-reads connections.json so other windows and
// instances keep their profiles. Callers hold profilesMu.
func (a *App) editConnectionProfiles(edit func([]remotefs.Config) []remotefs.Config) error {
	profiles, err := a.readConnectionProfiles()
	if err != nil {
		return err
	}
	if profiles == nil {
		profiles = append([]remotefs.Config(nil), a.Settings.RemoteConnections...)
	}
	profiles = edit(profiles)
	if profiles == nil {
		profiles = []remotefs.Config{}
	}
	b, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	if err = writeConfigAtomically(profilesPath(), ".connections-*", b); err == nil {
		a.Settings.RemoteConnections = profiles
	}
	return err
}
