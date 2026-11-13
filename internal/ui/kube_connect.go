package ui

import (
	"os"
	"slices"
	"strings"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
)

// kubeConnectionForm asks for the kubeconfig file and then the context; the
// profile is saved once the connection works (5.6).
func (a *App) kubeConnectionForm(index int, p kube.Profile) {
	if p.Kubeconfig == "" {
		p.Kubeconfig = kube.DefaultKubeconfig()
	}
	a.form("Kubernetes cluster", []Field{
		{Label: "Kubeconfig file", Value: p.Kubeconfig},
		{Label: "Profile name (blank = context name)", Value: p.Name},
		{Label: "Extra PATH for auth plugins (optional)", Value: p.ExtraPath},
	}, func(fields []Field) {
		profile := p
		profile.Kubeconfig = expand(strings.TrimSpace(fields[0].Value), a.Panels[a.Active].filesystemPath())
		profile.Name = strings.TrimSpace(fields[1].Value)
		profile.ExtraPath = strings.TrimSpace(fields[2].Value)
		names, current, err := kube.Contexts(profile.Kubeconfig)
		if err != nil {
			a.reopenForm(fields, 0, err.Error())
			return
		}
		if len(names) == 0 {
			a.reopenForm(fields, 0, "kubeconfig has no contexts")
			return
		}
		if slices.Contains(names, profile.Context) {
			current = profile.Context
		}
		labels := make([]string, len(names))
		for i, name := range names {
			labels[i] = name
			if name == current {
				labels[i] = "✓ " + name
			}
		}
		a.choose("Context", labels, func(i int) {
			profile.Context = names[i]
			a.connectKube(index, profile)
		})
		if i := slices.Index(names, current); i >= 0 && a.Form != nil {
			a.Form.Focus = i
		}
	})
	a.Form.Primary = "Next"
}

// connectKube connects in the background: the auth plugin PATH, client,
// discovery and the namespace list. Success switches the panel to the
// cluster root and saves the profile.
func (a *App) connectKube(index int, profile kube.Profile) {
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
			if p.Kube != nil {
				p.Kube.close()
			}
			p.Remote, p.Archive = nil, nil
			p.Kube = newKubePanel(profile, client, namespaces, a.Settings)
			p.RemoteLocalPath = local
			p.Async = true
			p.View = "Full"
			p.states = nil
			p.Marked = map[string]bool{}
			p.Path = "/"
			p.Cursor, p.Offset = 0, 0
			saveErr := a.saveKubeProfile(profile)
			a.error(p.Load("/"))
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
