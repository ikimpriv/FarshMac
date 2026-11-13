package ui

import "farshmac/internal/remotefs"

// kubePanel is the state of a panel connected to a Kubernetes cluster. The
// behaviour lives in kubeBackend; see docs/KUBERNETES_PANEL_SPEC.md.
type kubePanel struct {
	label string
}

// kubeBackend: a Kubernetes cluster shown as namespaces, resource types and
// objects. This is the phase-0 placeholder; the levels follow.
type kubeBackend struct{}

func (kubeBackend) Kind() backendKind              { return backendKube }
func (kubeBackend) Label(p *Panel) string          { return p.Kube.label }
func (kubeBackend) Caps(*Panel) panelCaps          { return 0 }
func (kubeBackend) FilesystemPath(p *Panel) string { return p.RemoteLocalPath }
func (kubeBackend) Virtual(*Panel, string) bool    { return true }
func (kubeBackend) Endpoint(*Panel) remotefs.Endpoint {
	return remotefs.Endpoint{}
}
func (kubeBackend) HistoryEntry(p *Panel, abs string) string { return p.Kube.label + abs }
func (kubeBackend) SizeSource(p *Panel) string               { return p.Kube.label }
func (kubeBackend) InfoPlugin(p *Panel) string               { return p.Kube.label + p.Path }
func (kubeBackend) KeyBar(*Panel, modifiers) [10]string {
	return [10]string{"Help", "UserMn", "", "", "", "", "", "", "Menu", ""}
}
func (kubeBackend) DocumentSource(*Panel, string) documentSource { return nil }
func (kubeBackend) Open(*App, *Panel, bool) bool                 { return true }
func (kubeBackend) Enter(*App, *Panel) bool                      { return true }
func (kubeBackend) Mkdir(*App, *Panel) bool                      { return true }
func (kubeBackend) Delete(*App, *Panel, bool) bool               { return true }
func (kubeBackend) Move(*App, *Panel) bool                       { return true }
func (kubeBackend) MenuSection(*App, *Panel) *menuSection        { return nil }

// loadKube lists the current level of the cluster; phase 1 fills it in.
func (p *Panel) loadKube(path string) error {
	state := p.beginLoad(path)
	p.cancelPanelLoad()
	p.cancelTree()
	var files []Entry
	p.sortEntries(files)
	p.installEntries(files, state)
	p.TotalBytes, p.ItemCount = 0, 0
	p.Descriptions = nil
	p.InfoText = p.Kube.label
	return nil
}
