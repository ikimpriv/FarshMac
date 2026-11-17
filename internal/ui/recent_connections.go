package ui

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"farshmac/internal/kube"
	"farshmac/internal/remotefs"
)

type recentConnection struct {
	Kind, Key, Label, Path, Cursor, View string
	Remote                               *remotefs.Config
	Cluster                              *kube.Profile
	Docker                               string
	Used                                 int64
}

func recentConnectionsPath() string {
	return filepath.Join(filepath.Dir(setupPath()), "recent-connections.json")
}
func readRecentConnections() []recentConnection {
	var records []recentConnection
	b, err := readConfigFile(recentConnectionsPath())
	if err == nil {
		_ = json.Unmarshal(b, &records)
	}
	return records
}
func panelConnection(p *Panel) (recentConnection, bool) {
	if p.Archive != nil {
		return recentConnection{}, false
	}
	r := recentConnection{Path: p.Path, View: p.View}
	if p.Cursor >= 0 && p.Cursor < len(p.Entries) {
		r.Cursor = p.Entries[p.Cursor].Name
	}
	if p.Docker != nil {
		r.Kind = "docker"
		r.Key = p.Docker.client.Context
		r.Label = "docker:" + r.Key
		r.Docker = r.Key
	} else if p.Kube != nil {
		c := p.Kube.profile
		r.Kind = "kubernetes"
		r.Key = kubeProfileKey(c)
		r.Label = c.Label()
		r.Cluster = &c
	} else if p.Remote != nil {
		c := *p.Remote
		c.Password, c.KeyPassphrase, c.HostKey, c.CertFingerprint = "", "", nil, ""
		r.Kind = "remote"
		r.Key = c.Label()
		r.Label = c.Label()
		r.Remote = &c
	} else {
		return r, false
	}
	return r, true
}

// The list shown in menus is limited to five; stored locations are not.
func saveRecentConnection(r recentConnection, touch bool) error {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	records := readRecentConnections()
	found := false
	for i, old := range records {
		if old.Kind == r.Kind && old.Key == r.Key {
			r.Used = old.Used
			if touch {
				r.Used = time.Now().UnixNano()
			}
			records[i] = r
			found = true
			break
		}
	}
	if !found {
		r.Used = time.Now().UnixNano()
		records = append(records, r)
	}
	b, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	old, _ := readConfigFile(recentConnectionsPath())
	if bytes.Equal(old, b) {
		return nil
	}
	return writeConfigAtomically(recentConnectionsPath(), ".recent-connections-*", b)
}
func (a *App) rememberConnection(index int, touch bool) {
	p := &a.Panels[index]
	if p.load != nil || p.Err != "" {
		return
	}
	if r, ok := panelConnection(p); ok {
		a.error(saveRecentConnection(r, touch))
	}
}
func (a *App) rememberConnections() {
	for i := range a.Panels {
		a.rememberConnection(i, false)
	}
}
func (a *App) connectionMenuItems(index int) []menuItem {
	items := []menuItem{
		{"Docker…", "", func() { a.dockerConnections(index) }},
		{"Kubernetes cluster…", "", func() { a.Active = index; a.kubeConnectionForm(index, kube.Profile{}) }},
		{"FTP / SFTP connections…", "", func() { a.Active = index; a.networkMenu() }},
	}
	records := readRecentConnections()
	sort.SliceStable(records, func(i, j int) bool { return records[i].Used > records[j].Used })
	if len(records) > 0 {
		items = append(items, menuItem{})
		// Choose the five most recent first, then group their presentation.
		records = records[:min(5, len(records))]
		rank := func(kind string) int {
			switch kind {
			case "docker":
				return 0
			case "kubernetes":
				return 1
			default:
				return 2
			}
		}
		sort.SliceStable(records, func(i, j int) bool { return rank(records[i].Kind) < rank(records[j].Kind) })
		for _, r := range records {
			r := r
			items = append(items, menuItem{r.Label, "", func() { a.Active = index; a.openRecentConnection(index, r) }})
		}
	}
	return items
}
func (a *App) openRecentConnection(index int, r recentConnection) {
	a.rememberConnection(index, false)
	a.requestedRestore = &r
	if r.Kind == "docker" {
		a.connectDocker(index, r.Docker)
	} else if r.Remote != nil {
		c := *r.Remote
		// Credentials stay in the current in-memory profile, never in this file.
		for _, known := range a.Settings.RemoteConnections {
			if known.Label() == c.Label() {
				c = known
				break
			}
		}
		for i := range a.Panels {
			if live := a.Panels[i].Remote; live != nil && live.Label() == c.Label() {
				c = *live
				break
			}
		}
		a.connectCursor = r.Cursor
		a.restoreNextConnection = true
		a.connectRemote(index, c)
	} else if r.Cluster != nil {
		a.restoreNextConnection = true
		a.connectKube(index, *r.Cluster)
	}
}
func (a *App) restoreConnection(index int, requested ...*recentConnection) {
	p := &a.Panels[index]
	current, ok := panelConnection(p)
	if !ok {
		return
	}
	recordsToRestore := readRecentConnections()
	if len(requested) > 0 && requested[0] != nil {
		recordsToRestore = append([]recentConnection{*requested[0]}, recordsToRestore...)
	}
	for _, r := range recordsToRestore {
		if r.Kind != current.Kind || r.Key != current.Key {
			continue
		}
		copy := r
		p.restoreLocation = &copy
		if p.load == nil {
			p.restoreConnectionLocation()
		}

		break
	}
	// Restoring runs with an async listing in flight. Touch the current source,
	// but preserve the stored path until that listing has completed.
	profilesMu.Lock()
	records := readRecentConnections()
	if !slices.ContainsFunc(records, func(r recentConnection) bool { return r.Kind == current.Kind && r.Key == current.Key }) {
		records = append(records, current)
	}
	for i := range records {
		if records[i].Kind == current.Kind && records[i].Key == current.Key {
			records[i].Used = time.Now().UnixNano()
		}
	}
	b, err := json.MarshalIndent(records, "", "  ")
	if err == nil {
		err = writeConfigAtomically(recentConnectionsPath(), ".recent-connections-*", b)
	}
	profilesMu.Unlock()
	a.error(err)
}

// Apply only after the initial listing succeeds, so a missing saved directory
// falls back to a usable root listing instead of an unfinished async snapshot.
func (p *Panel) restoreConnectionLocation() {
	r := p.restoreLocation
	p.restoreLocation = nil
	if r == nil {
		return
	}
	if r.View != "" && r.View != "Quick view" && r.View != "Info panel" {
		p.View = r.View
	}
	if r.Path != "" && r.Path != p.Path {
		previous := *p
		if err := p.loadSource(r.Path, previous, "Previous location unavailable: "); err != nil {
			return
		}
	}
	p.placeCursor(r.Cursor)
}
