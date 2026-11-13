package ui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"farshmac/internal/kube"
)

// Cluster profiles live in their own file so older versions, which only know
// connections.json, keep working. Profiles carry no secrets (G7).
func kubeProfilesPath() string { return filepath.Join(filepath.Dir(setupPath()), "kubernetes.json") }

func kubeProfileKey(p kube.Profile) string { return p.Kubeconfig + "\x00" + p.Context }

func (a *App) loadKubeProfiles() {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	if profiles, err := a.readKubeProfiles(); err == nil && profiles != nil {
		a.Settings.KubeProfiles = profiles
	}
}

// readKubeProfiles returns nil, nil when there is no file; a damaged file is
// moved aside once. Callers hold profilesMu.
func (a *App) readKubeProfiles() ([]kube.Profile, error) {
	b, err := readConfigFile(kubeProfilesPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var profiles []kube.Profile
	if err = json.Unmarshal(b, &profiles); err != nil {
		if _, statErr := os.Stat(kubeProfilesPath()); statErr != nil {
			return nil, nil
		}
		broken, renameErr := preserveBrokenConfig(kubeProfilesPath())
		if renameErr != nil {
			return nil, err
		}
		a.Status = "kubernetes.json is damaged; saved as " + filepath.Base(broken)
		a.dirty = true
		return nil, nil
	}
	return profiles, nil
}

func (a *App) saveKubeProfile(profile kube.Profile) error {
	return a.editKubeProfiles(func(profiles []kube.Profile) []kube.Profile {
		var out []kube.Profile
		placed := false
		for _, old := range profiles {
			if kubeProfileKey(old) == kubeProfileKey(profile) {
				if !placed {
					out = append(out, profile)
					placed = true
				}
				continue
			}
			out = append(out, old)
		}
		if !placed {
			out = append(out, profile)
		}
		return out
	})
}
func (a *App) deleteKubeProfile(profile kube.Profile) error {
	return a.editKubeProfiles(func(profiles []kube.Profile) []kube.Profile {
		var out []kube.Profile
		for _, old := range profiles {
			if kubeProfileKey(old) != kubeProfileKey(profile) {
				out = append(out, old)
			}
		}
		return out
	})
}

// editKubeProfiles re-reads kubernetes.json so other windows keep their
// profiles, applies edit and writes the file atomically.
func (a *App) editKubeProfiles(edit func([]kube.Profile) []kube.Profile) error {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	profiles, err := a.readKubeProfiles()
	if err != nil {
		return err
	}
	if profiles == nil {
		profiles = append([]kube.Profile(nil), a.Settings.KubeProfiles...)
	}
	profiles = edit(profiles)
	if profiles == nil {
		profiles = []kube.Profile{}
	}
	b, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	if err = writeConfigAtomically(kubeProfilesPath(), ".kubernetes-*", b); err == nil {
		a.Settings.KubeProfiles = profiles
	}
	return err
}
