package ui

import (
	"context"
	"encoding/json"
	"farshmac/internal/kube"
)

func (d *dockerPanel) storageMounts(ctx context.Context, id string) ([]kube.StorageMount, bool) {
	d.mu.Lock()
	cached, ok := d.mounts[id]
	d.mu.Unlock()
	if ok {
		return cached, true
	}
	b, err := d.client.Run(ctx, "inspect", "--format", "{{json .Mounts}}", id)
	if err != nil {
		return nil, false
	}
	var mounts []struct {
		Type, Source, Destination, Name string
		RW                              bool
	}
	if json.Unmarshal(b, &mounts) != nil {
		return nil, false
	}
	out := make([]kube.StorageMount, 0, len(mounts))
	for _, m := range mounts {
		kind, name := m.Type, m.Name
		switch m.Type {
		case "volume":
			kind = "Docker volume"
		case "bind":
			kind = "Bind mount"
			name = m.Source
		case "tmpfs":
			kind = "Memory (tmpfs)"
		}
		out = append(out, kube.StorageMount{Path: m.Destination, Kind: kind, Volume: name, ReadOnly: !m.RW})
	}
	d.mu.Lock()
	if d.mounts == nil {
		d.mounts = map[string][]kube.StorageMount{}
	}
	d.mounts[id] = out
	d.mu.Unlock()
	return out, true
}
