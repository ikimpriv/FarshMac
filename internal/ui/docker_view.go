package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"farshmac/internal/fsops"
	"go.yaml.in/yaml/v3"
)

// Docker reports are read-only: inspect output is not an update manifest.
// Keep object resolution separate from filesystem resolution so a container
// row can be inspected without entering it or starting a toolbox.
func (d *dockerPanel) describe(ctx context.Context, target string) (string, error) {
	parts := strings.Split(strings.Trim(target, "/"), "/")
	kind := map[string]string{"Volumes": "volume", "Networks": "network", "Images": "image"}[parts[0]]
	var args []string
	title := "Docker · " + target
	if kind != "" && len(parts) == 2 {
		args = []string{kind, "inspect", parts[1]}
	} else if c, prefix, _, ok := d.location(target); ok && prefix == target {
		args = []string{"container", "inspect", c.ID}
	} else if len(parts) == 2 && parts[0] == "Compose" {
		list, err := d.client.Containers(ctx)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Compose project: %s\nContext: %s\n\nContainers\n", parts[1], d.client.Context)
		for _, c := range list {
			if c.Label("com.docker.compose.project") == parts[1] {
				fmt.Fprintf(&b, "\n%s\n  Service: %s\n  State: %s\n  Status: %s\n  Image: %s\n", c.Names, c.Label("com.docker.compose.service"), c.State, c.Status, c.Image)
			}
		}
		return b.String(), nil
	} else {
		return title + "\n\nChoose a container, Compose project, volume, network or image.\nF3: details · F4: edit files inside containers and volumes.", nil
	}
	data, err := d.client.Run(ctx, args...)
	if err != nil {
		return "", err
	}
	return dockerInspectReport(title, d.client.Context, data)
}

func dockerInspectReport(title, connection string, data []byte) (string, error) {
	var objects []map[string]any
	if err := json.Unmarshal(data, &objects); err != nil {
		return "", fmt.Errorf("invalid Docker inspect response: %w", err)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%s\nContext: %s\nRead-only Docker object\n", title, connection)
	groups := []struct {
		title string
		keys  []string
	}{
		{"Identity", []string{"Name", "Id", "ID", "Created", "RepoTags", "RepoDigests", "Driver", "Scope", "Architecture", "Os", "Size", "VirtualSize"}},
		{"State", []string{"State", "RestartCount"}},
		{"Storage", []string{"Mounts", "Mountpoint", "Volumes", "Options", "RootFS", "GraphDriver"}},
		{"Network", []string{"NetworkSettings", "IPAM", "Containers", "Internal", "Attachable", "Ingress", "EnableIPv6"}},
		{"Configuration", []string{"Config", "Path", "Args", "HostConfig"}},
		{"Labels", []string{"Labels"}},
	}
	for _, obj := range objects {
		for _, key := range []string{"Size", "VirtualSize"} {
			if n, ok := obj[key].(float64); ok {
				obj[key] = fmt.Sprintf("%s (%d bytes)", humanSize(int64(n)), int64(n))
			}
		}
		for _, g := range groups {
			var section strings.Builder
			for _, key := range g.keys {
				if v, ok := obj[key]; ok {
					b, err := yaml.Marshal(map[string]any{key: v})
					if err != nil {
						return "", err
					}
					section.Write(b)
					delete(obj, key)
				}
			}
			if section.Len() > 0 {
				fmt.Fprintf(&out, "\n%s\n%s\n%s", g.title, strings.Repeat("─", len(g.title)), section.String())
			}
		}
		if len(obj) > 0 {
			b, err := yaml.Marshal(obj)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&out, "\nOther details\n─────────────\n%s", b)
		}
	}
	return out.String(), nil
}

func (a *App) updateDockerPreview(source *Panel) {
	target := source.Current()
	if len(source.Entries) == 0 || source.Entries[source.Cursor].Name == ".." {
		target = source.Path
	}
	d := source.Docker
	key := source.label() + "/preview/" + target
	if a.previewPending {
		if key != a.previewKey && a.previewOperation != nil {
			a.previewOperation.Cancel()
			a.previewKey = ""
		}
		return
	}
	if key == a.previewKey {
		return
	}
	a.previewKey, a.previewPath = key, target
	a.previewOffset = 0
	a.previewImage = ""
	a.previewText = "Loading…"
	a.previewPending = true
	op := fsops.NewOperation("Docker Quick view")
	a.previewOperation = op
	// A filesystem panel previews bounded file contents; a catalog inspects objects.
	endpoint := source.endpoint()
	file := len(source.Entries) > 0 && !source.Entries[source.Cursor].Dir
	go func() {
		var text string
		var err error
		if endpoint.Opener != nil {
			if !file {
				text = target + "\nDirectory"
			} else {
				fs, e := endpoint.Opener.Open(op.Context())
				err = e
				if e == nil {
					func() {
						defer fs.Close()
						f, e := fs.Open(target)
						if e != nil {
							err = e
							return
						}
						defer f.Close()
						b, e := io.ReadAll(op.Reader(io.LimitReader(f, 16*1024+1)))
						err = e
						if len(b) > 16*1024 {
							b = b[:16*1024]
							text = string(b) + "\n… (F3 for full file)"
						} else {
							text = string(b)
						}
					}()
				}
			}
		} else {
			text, err = d.describe(op.Context(), target)
		}
		if err != nil {
			text = target + "\n" + err.Error()
		}
		a.jobs <- func() {
			if a.previewOperation != op {
				return
			}
			if a.previewOperation == op && a.previewKey == key && op.Check() == nil {
				if len(text) > 64*1024 {
					text = text[:64*1024] + "\n… (F3 for full details)"
				}
				a.previewText = text
			}
			a.previewPending = false
			a.previewOperation = nil
			a.dirty = true
		}
	}()
}
