package ui

import (
	"farshmac/internal/docker"
	"path"
	"strings"
)

func (i dockerCatalogInfo) toastMarker() string {
	if i.toastCount == 0 {
		return " "
	}
	if i.toastCount < i.toastTotal {
		return "◇"
	}
	return "◆"
}

// Annotate the catalog snapshot without probing agents or changing object names.
// Network-only helpers do not imply filesystem/process management.
func (d *dockerPanel) markPodToastEntries(folder string, entries []Entry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	running := map[string]bool{}
	for _, c := range d.containers {
		running[c.ID] = c.State == "running"
	}
	type coverage struct{ managed, total int }
	counts := map[string]coverage{}
	containers := map[string]docker.Container{}
	if folder == "/Volumes" {
		for _, e := range entries {
			id := d.helpers["volume:"+e.Name]
			if id != "" && running[id] {
				counts[e.Name] = coverage{1, 1}
			}
		}
	} else {
		for _, c := range d.containers {
			if c.Label("io.farshmac.podtoast.target") != "" || c.Label("io.farshmac.podtoast.volume") != "" || c.Label("io.farshmac.podtoast.network") != "" {
				continue
			}
			project := c.Label("com.docker.compose.project")
			name := ""
			switch {
			case folder == "/Containers" && project == "":
				name = c.Names
			case folder == "/Compose" && project != "":
				name = project
			case path.Dir(folder) == "/Compose" && path.Base(folder) == project:
				name = c.Names
			}
			if name == "" {
				continue
			}
			if folder != "/Compose" {
				containers[name] = c
			}
			count := counts[name]
			count.total++
			if c.State == "running" && d.helpers[c.ID] != "" && running[d.helpers[c.ID]] {
				count.managed++
			}
			counts[name] = count
		}
	}
	// Filesystem entries and navigation rows never acquire a management badge.
	if folder != "/Volumes" && folder != "/Containers" && folder != "/Compose" && !(strings.HasPrefix(folder, "/Compose/") && path.Dir(folder) == "/Compose") {
		return
	}
	for i, e := range entries {
		info, ok := e.Info.(dockerCatalogInfo)
		if !ok || e.Name == ".." {
			continue
		}
		count := counts[e.Name]
		info.toastCount, info.toastTotal = count.managed, count.total
		if c, ok := containers[e.Name]; ok {
			info.container = true
			info.state = c.State
			info.status = c.Status
			info.created = dockerCreatedAt(c.CreatedAt)
		}
		entries[i].Info = info
	}
}
