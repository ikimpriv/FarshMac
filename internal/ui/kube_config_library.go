package ui

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"farshmac/internal/kube"
)

type storedKubeconfig struct{ ID, Name string }

func kubeConfigLibraryPath() string {
	return filepath.Join(filepath.Dir(setupPath()), "kubeconfigs.json")
}
func (c storedKubeconfig) path() string {
	return filepath.Join(filepath.Dir(setupPath()), "kubeconfigs", c.ID+".yaml")
}
func readKubeConfigLibrary() ([]storedKubeconfig, error) {
	b, e := readConfigFile(kubeConfigLibraryPath())
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var entries []storedKubeconfig
	if e = json.Unmarshal(b, &entries); e != nil {
		return nil, fmt.Errorf("cannot read saved kubeconfig catalog: %w", e)
	}
	for _, entry := range entries {
		if entry.ID == "" || filepath.Base(entry.ID) != entry.ID || strings.ContainsAny(entry.ID, "/\\\x00") {
			return nil, fmt.Errorf("invalid saved kubeconfig identifier")
		}
	}
	return entries, nil
}
func savePastedKubeconfig(name, text, id string) (storedKubeconfig, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return storedKubeconfig{}, fmt.Errorf("give this kubeconfig a name")
	}
	if _, e := kube.InspectConfig([]byte(text)); e != nil {
		return storedKubeconfig{}, fmt.Errorf("paste kubeconfig YAML only (starting with apiVersion/kind), with at least one context: %w", e)
	}
	profilesMu.Lock()
	defer profilesMu.Unlock()
	entries, e := readKubeConfigLibrary()
	if e != nil {
		return storedKubeconfig{}, e
	}
	for _, entry := range entries {
		if strings.EqualFold(entry.Name, name) && entry.ID != id {
			return storedKubeconfig{}, fmt.Errorf("a kubeconfig named %q already exists", name)
		}
	}
	result := storedKubeconfig{ID: id, Name: name}
	found := false
	for i, entry := range entries {
		if entry.ID == id && id != "" {
			entries[i] = result
			found = true
		}
	}
	if id != "" && !found {
		return result, fmt.Errorf("saved kubeconfig no longer exists")
	}
	if !found {
		result.ID = rand.Text()
		entries = append(entries, result)
	}
	// Preserve YAML verbatim; CreateTemp + rename gives an atomic 0600 file.
	if e = writeConfigAtomically(result.path(), ".kubeconfig-*", []byte(text)); e != nil {
		return result, e
	}
	b, _ := json.MarshalIndent(entries, "", "  ")
	if e = writeConfigAtomically(kubeConfigLibraryPath(), ".kubeconfigs-*", b); e != nil {
		if !found {
			os.Remove(result.path())
		}
		return result, e
	}
	return result, nil
}
func (a *App) kubeConfigLibrary(index int, p kube.Profile) {
	profilesMu.Lock()
	entries, err := readKubeConfigLibrary()
	profilesMu.Unlock()
	if err != nil {
		a.error(err)
		return
	}
	labels := []string{"Paste new kubeconfig…"}
	for _, entry := range entries {
		labels = append(labels, entry.Name)
	}
	a.choose("Custom kubeconfig files", labels, func(i int) {
		if i == 0 {
			a.pasteKubeconfigForm(index, p, storedKubeconfig{})
			return
		}
		entry := entries[i-1]
		if p.Kubeconfig != entry.path() {
			p.Context = ""
		}
		p.Kubeconfig = entry.path()
		p.StoredConfig = true
		p.RemoteConfig = false
		a.storedKubeConnectForm(index, p)
	})
	menu := a.Form
	menu.Message = "Choose a config, then any context. F4: rename / replace YAML. F8: delete unused config."
	menu.OnKey = func(m Message) bool {
		i := menu.choiceIndex(menu.Focus) - 1
		if i < 0 || i >= len(entries) {
			return false
		}
		entry := entries[i]
		if m.Key == "F4" {
			a.pasteKubeconfigForm(index, p, entry)
			return true
		}
		if m.Key == "F8" || m.Key == "Delete" {
			a.confirm("Delete saved kubeconfig", entry.Name+" — delete the stored credentials?", []string{"Delete", "Cancel"}, func(i int) {
				if i == 0 {
					if e := deleteStoredKubeconfig(entry); e != nil {
						a.error(e)
						return
					}
				}
				a.kubeConfigLibrary(index, p)
			})
			return true
		}
		return false
	}
}
func deleteStoredKubeconfig(entry storedKubeconfig) error {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	// Do not silently break other contexts that share this config.
	b, e := readConfigFile(kubeProfilesPath())
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	var profiles []kube.Profile
	if len(b) > 0 {
		if e = json.Unmarshal(b, &profiles); e != nil {
			return e
		}
	}
	for _, p := range profiles {
		if p.Kubeconfig == entry.path() {
			return fmt.Errorf("remove the connection profiles using this kubeconfig first")
		}
	}
	entries, e := readKubeConfigLibrary()
	if e != nil {
		return e
	}
	var kept []storedKubeconfig
	found := false
	for _, c := range entries {
		if c.ID == entry.ID {
			found = true
		} else {
			kept = append(kept, c)
		}
	}
	if !found {
		return fmt.Errorf("saved kubeconfig no longer exists")
	}
	// Check the catalog first, so a crafted external path cannot be deleted.
	if e = os.Remove(entry.path()); e != nil && !os.IsNotExist(e) {
		return e
	}
	b, _ = json.MarshalIndent(kept, "", "  ")
	return writeConfigAtomically(kubeConfigLibraryPath(), ".kubeconfigs-*", b)
}
func (a *App) pasteKubeconfigForm(index int, p kube.Profile, existing storedKubeconfig) {
	a.form("Paste kubeconfig", []Field{{Label: "Config name", Value: existing.Name}, {Label: "Kubeconfig YAML (Cmd+V)", PasteDocument: true}}, func(f []Field) {
		text := f[1].Value
		if text == "" && existing.ID != "" {
			b, e := os.ReadFile(existing.path())
			if e != nil {
				a.error(e)
				return
			}
			text = string(b)
			clear(b)
		}
		entry, err := savePastedKubeconfig(f[0].Value, text, existing.ID)
		if err != nil {
			a.reopenForm(f, 0, err.Error())
			return
		}
		p.Kubeconfig = entry.path()
		p.StoredConfig = true
		p.RemoteConfig = false
		p.Context = ""
		a.storedKubeConnectForm(index, p)
	})
	a.Form.Primary = "Save and choose context"
	a.Form.Message = "YAML is preserved, including line breaks. Stored privately (0600). Credentials are not shown in the dialog or copied into connection profiles."
	if existing.ID != "" {
		a.Form.Message += " Leave YAML blank to keep the current contents."
	}
}
func (a *App) storedKubeConnectForm(index int, p kube.Profile) {
	a.choose("API access", []string{"Direct", "Through SSH"}, func(i int) {
		if i == 0 {
			p.SSHHost = ""
			p.SSHUser = ""
			p.SSHPort = ""
			p.SSHAuth = ""
			p.SSHSecret = ""
			p.SSHKeyFile = ""
			p.SSHConfigFile = ""
			a.discoverKubeContexts(index, p)
		} else {
			a.kubeSSHServerForm(index, p)
		}
	})
	if p.SSHHost != "" {
		a.Form.Focus = 1
	}
}

func kubeConfigDisplayName(p kube.Profile) string {
	if !p.StoredConfig {
		return p.Kubeconfig
	}
	profilesMu.Lock()
	entries, err := readKubeConfigLibrary()
	profilesMu.Unlock()
	if err == nil {
		for _, entry := range entries {
			if entry.path() == p.Kubeconfig {
				return entry.Name
			}
		}
	}
	return "Saved kubeconfig"
}
