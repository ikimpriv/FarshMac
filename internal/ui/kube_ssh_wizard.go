package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
)

func kubeSSHPreferencesPath() string {
	return filepath.Join(filepath.Dir(setupPath()), "kubernetes-ssh.json")
}
func sshPreferenceKey(p kube.Profile) string {
	return strings.Join([]string{p.SSHHost, p.SSHUser, p.SSHPort}, "\x00")
}
func loadSSHPreference(p kube.Profile) kube.Profile {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	b, e := readConfigFile(kubeSSHPreferencesPath())
	if e != nil {
		return p
	}
	var prefs map[string]kube.Profile
	if json.Unmarshal(b, &prefs) != nil {
		return p
	}
	if saved, ok := prefs[sshPreferenceKey(p)]; ok {
		p.SSHTrustFile = saved.SSHTrustFile
		p.SSHAuth = saved.SSHAuth
		p.SSHKeyFile = saved.SSHKeyFile
		p.SSHConfigFile = saved.SSHConfigFile
	}
	return p
}
func saveSSHPreference(p kube.Profile) error {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	prefs := map[string]kube.Profile{}
	b, e := readConfigFile(kubeSSHPreferencesPath())
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if len(b) > 0 {
		if e = json.Unmarshal(b, &prefs); e != nil {
			return e
		}
	}
	prefs[sshPreferenceKey(p)] = kube.Profile{SSHHost: p.SSHHost, SSHUser: p.SSHUser, SSHPort: p.SSHPort, SSHAuth: p.SSHAuth, SSHTrustFile: p.SSHTrustFile, SSHKeyFile: p.SSHKeyFile, SSHConfigFile: p.SSHConfigFile}
	b, e = json.MarshalIndent(prefs, "", "  ")
	if e != nil {
		return e
	}
	return writeConfigAtomically(kubeSSHPreferencesPath(), ".ssh-preferences-*", b)
}
func (a *App) kubeSSHServerForm(index int, p kube.Profile) {
	address := p.SSHHost
	if p.SSHPort != "" {
		address = netSSHAddress(p.SSHHost, p.SSHPort)
	}
	a.form("Kubernetes · SSH server", []Field{{Label: "Server", Value: address}, {Label: "Username (optional)", Value: p.SSHUser}}, func(f []Field) {
		parsed, err := kube.ParseSSHAddress(f[0].Value, f[1].Value)
		if err != nil {
			a.reopenForm(f, 0, err.Error())
			return
		}
		if p.SSHHost != parsed.SSHHost || p.SSHUser != parsed.SSHUser || p.SSHPort != parsed.SSHPort {
			p.SSHSecret = ""
			p.SSHAuth = ""
			p.SSHKeyFile = ""
			p.SSHConfigFile = ""
			p.SSHTrustFile = ""
		}
		p.SSHHost = parsed.SSHHost
		p.SSHPort = parsed.SSHPort
		p.SSHUser = parsed.SSHUser
		p = loadSSHPreference(p)
		a.kubeSSHAuthenticate(index, p)
	})
	a.Form.Primary = "Connect"
	a.Form.Message = "Uses SSH config, keys and agent automatically. Server may include :port."
}
func netSSHAddress(host, port string) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return host + ":" + port
}
func (a *App) kubeSSHAuthenticate(index int, p kube.Profile) {
	a.backgroundTask("Connecting to "+p.SSHHost, func(o *fsops.Operation) (func(), func(), error) {
		err := kube.ProbeSSH(o.Context(), p)
		if o.Check() != nil {
			return nil, nil, o.Check()
		}
		return func() {
			if err != nil {
				a.kubeSSHAuthFailed(index, p, err)
				return
			}
			kube.RememberSSHAuthentication(p)
			if e := saveSSHPreference(p); e != nil {
				a.Status = "Connected; authentication settings not saved: " + e.Error()
			}
			a.discoverKubeContexts(index, p)
		}, nil, nil
	})
}
func (a *App) kubeSSHAuthFailed(index int, p kube.Profile, err error) {
	text := err.Error()
	if strings.Contains(text, "REMOTE HOST IDENTIFICATION HAS CHANGED") || strings.Contains(text, "REVOKED HOST KEY") {
		a.confirm("SSH host key changed", text, []string{"Edit server", "Cancel"}, func(i int) {
			if i == 0 {
				a.kubeSSHServerForm(index, p)
			}
		})
		return
	}
	if strings.Contains(text, "Host key verification failed") {
		a.backgroundTask("Reading SSH host identity", func(o *fsops.Operation) (func(), func(), error) {
			keys, e := kube.ScanSSHHostKeys(o.Context(), p)
			if e != nil {
				return nil, nil, e
			}
			return func() {
				a.confirm("Trust SSH server", keys.Address+"\nVerify these fingerprints before trusting this server:\n"+strings.Join(keys.Fingerprints, "\n"), []string{"Trust and continue", "Cancel"}, func(i int) {
					if i != 0 {
						return
					}
					file, e := kube.TrustSSHHostKeys(keys)
					if e != nil {
						a.error(e)
						return
					}
					p.SSHTrustFile = file
					a.kubeSSHAuthenticate(index, p)
				})
			}, nil, nil
		})
		if a.task != nil {
			a.task.reportError = true
		}
		return
	}

	if !strings.Contains(text, "Permission denied") && !strings.Contains(text, "Authentication failed") && !strings.Contains(text, "Host key verification failed") {
		a.confirm("SSH connection", text, []string{"Edit server", "Retry", "Cancel"}, func(i int) {
			if i == 0 {
				a.kubeSSHServerForm(index, p)
			} else if i == 1 {
				a.kubeSSHAuthenticate(index, p)
			}
		})
		return
	}
	a.choose("SSH · choose authentication", []string{"Private key…", "Password…", "SSH settings file…", "Retry automatic authentication", "Edit server…"}, func(i int) {
		switch i {
		case 0:
			a.form("SSH private key", []Field{{Label: "Private key file", Value: p.SSHKeyFile}, {Label: "Passphrase (optional; session only)", Secret: true}}, func(f []Field) {
				if strings.TrimSpace(f[0].Value) == "" {
					a.reopenForm(f, 0, "Choose a private key")
					return
				}
				p.SSHKeyFile = expand(strings.TrimSpace(f[0].Value), a.Panels[index].filesystemPath())
				p.SSHSecret = f[1].Value
				p.SSHAuth = "key"
				a.kubeSSHAuthenticate(index, p)
			})
		case 1:
			a.form("SSH password", []Field{{Label: "Password (session only)", Secret: true}}, func(f []Field) { p.SSHAuth = "password"; p.SSHSecret = f[0].Value; a.kubeSSHAuthenticate(index, p) })
		case 2:
			a.prompt("SSH settings", "SSH config file", p.SSHConfigFile, func(name string) {
				p.SSHConfigFile = expand(name, a.Panels[index].filesystemPath())
				p.SSHAuth = ""
				p.SSHSecret = ""
				a.kubeSSHAuthenticate(index, p)
			})
		case 3:
			p.SSHAuth = ""
			p.SSHKeyFile = ""
			p.SSHSecret = ""
			a.kubeSSHAuthenticate(index, p)
		case 4:
			a.kubeSSHServerForm(index, p)
		}
	})
	a.Form.Message = text
}
func (a *App) kubeOtherFile(index int, p kube.Profile) {
	label := "Kubeconfig file"
	if p.RemoteConfig {
		label = "Kubeconfig path on " + p.SSHHost
	}
	a.prompt("Other configuration", label, p.Kubeconfig, func(name string) {
		p.Kubeconfig = strings.TrimSpace(name)
		if p.Kubeconfig == "" {
			return
		}
		if !p.RemoteConfig {
			p.Kubeconfig = expand(p.Kubeconfig, a.Panels[index].filesystemPath())
		}
		a.discoverKubeContexts(index, p)
	})
}
