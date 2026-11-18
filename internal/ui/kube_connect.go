package ui

import (
	"os"
	"strings"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
)

// Connection discovery reads metadata in the background; credentials remain in
// the source file (local or remote) and never enter saved connection profiles.
func (a *App) kubeConnectionForm(index int, p kube.Profile) {
	a.requestedRestore = nil
	a.choose("Kubernetes connection", []string{"Local kubeconfig files…", "SSH server · discover configs…", "Custom kubeconfig files…"}, func(i int) {
		if i == 2 {
			a.kubeConfigLibrary(index, p)
			return
		}
		changed := p.StoredConfig || p.RemoteConfig != (i == 1)
		p.StoredConfig = false
		p.RemoteConfig = i == 1
		if changed {
			p.Kubeconfig = ""
			p.Context = ""
		}
		if i == 0 {
			p.SSHAuth = ""
			p.SSHSecret = ""
			p.SSHTrustFile = ""
			p.SSHHost = ""
			p.SSHUser = ""
			p.SSHPort = ""
			p.SSHKeyFile = ""
			p.SSHConfigFile = ""
		}
		a.kubeSourceForm(index, p, i == 1)
	})
	if p.StoredConfig {
		a.Form.Focus = 2
	} else if p.RemoteConfig {
		a.Form.Focus = 1
	}
}
func (a *App) kubeSourceForm(index int, p kube.Profile, ssh bool) {
	if ssh {
		a.kubeSSHServerForm(index, p)
	} else {
		a.discoverKubeContexts(index, p)
	}
}
func (a *App) discoverKubeContexts(index int, p kube.Profile) {
	a.backgroundTask("Discovering Kubernetes contexts", func(o *fsops.Operation) (func(), func(), error) {
		var sources []kube.ConfigSource
		var warnings []string
		var err error
		if p.Kubeconfig != "" {
			var source kube.ConfigSource
			source, err = kube.ReadConfigSource(o.Context(), p)
			if err == nil {
				sources = []kube.ConfigSource{source}
			}
		} else {
			sources, warnings, err = kube.DiscoverConfigs(o.Context(), p)
		}
		if o.Check() != nil {
			return nil, nil, o.Check()
		}
		return func() {
			if err != nil || len(sources) == 0 {
				text := "No readable kubeconfigs with contexts found. Enter a file path explicitly."
				if err != nil {
					text = err.Error()
				}
				if len(warnings) > 0 {
					text += "\n" + strings.Join(warnings, "\n")
				}
				a.confirm("Kubernetes configuration", text, []string{"Edit", "Retry", "Cancel"}, func(i int) {
					if i == 0 {
						if p.StoredConfig {
							a.storedKubeConnectForm(index, p)
						} else {
							a.kubeOtherFile(index, p)
						}
					} else if i == 1 {
						a.discoverKubeContexts(index, p)
					}
				})
				return
			}
			a.chooseKubeContext(index, p, sources, warnings)
		}, nil, nil
	})
	if a.task != nil {
		a.task.cancelStatus = "Kubernetes discovery cancelled"
	}
}

// connectKube connects in the background: the auth plugin PATH, client,
// discovery and the namespace list. Success switches the panel to the
// cluster root and saves the profile.
func (a *App) connectKube(index int, profile kube.Profile) {
	requested := a.requestedRestore
	a.requestedRestore = nil
	restore := a.restoreNextConnection
	a.restoreNextConnection = false
	a.rememberConnection(index, false)
	label := profile.Label()
	a.backgroundTask("Connecting to "+label, func(o *fsops.Operation) (func(), func(), error) {
		kube.EnsureLoginPath(profile.ExtraPath)
		client, err := kube.Connect(o.Context(), profile)
		if err == nil {
			_, err = client.Resources(o.Context())
		}
		var namespaces []*kube.Object
		if err == nil {
			namespaces, err = client.Namespaces(o.Context())
		}
		if err != nil {
			if client != nil {
				client.Close()
			}
			if o.Check() != nil {
				return nil, nil, err
			}
			return func() { a.kubeConnectFailed(index, profile, err) }, nil, nil
		}
		retained := false
		cleanup := func() {
			if !retained {
				client.Close()
			}
		}
		return func() {
			retained = true
			p := &a.Panels[index]
			a.configurePanel(index)
			p.cancelPanelLoad()
			p.cancelTree()
			local := p.filesystemPath()
			if local == "" {
				local, _ = os.UserHomeDir()
			}
			if p.Kube != nil && p.Kube != a.Panels[1-index].Kube {
				p.Kube.close()
			}
			p.rememberFileView()
			p.Docker = nil
			p.Remote, p.Archive = nil, nil
			p.Kube = newKubePanel(profile, client, nil, a.Settings)
			_ = namespaces
			p.RemoteLocalPath = local
			p.Async = true
			p.View = "Full"
			p.states = nil
			p.Marked = map[string]bool{}
			p.Path = "/"
			p.Cursor, p.Offset = 0, 0
			saveErr := a.saveKubeProfile(profile)
			a.error(p.Load("/"))
			if restore {
				a.restoreConnection(index, requested)
			}
			a.Status = "Connected: " + label + " (terminal is local)"
			if saveErr != nil {
				a.Status += "; profile not saved: " + saveErr.Error()
			}
		}, cleanup, nil
	})
	if a.task != nil {
		a.task.cancelStatus = "Connection cancelled"
	}
}

// kubeConnectFailed shows client-go's message (including an auth plugin's
// stderr) and offers to edit the profile or try again.
func (a *App) kubeConnectFailed(index int, profile kube.Profile, err error) {
	if profile.SSHHost != "" && strings.Contains(err.Error(), "Permission denied") {
		a.kubeSSHAuthFailed(index, profile, err)
		return
	}
	if strings.Contains(err.Error(), "relative credential paths require") {
		a.prompt("Referenced credential files", "Original kubeconfig path on the SSH server", profile.RemoteBase, func(name string) { profile.RemoteBase = strings.TrimSpace(name); a.connectKube(index, profile) })
		return
	}

	a.confirm("Connection failed", profile.Label()+"\n"+err.Error(), []string{"Edit", "Retry", "Cancel"}, func(i int) {
		switch i {
		case 0:
			a.kubeConnectionForm(index, profile)
		case 1:
			a.connectKube(index, profile)
		default:
			a.Status = "Connection cancelled"
		}
	})
}
