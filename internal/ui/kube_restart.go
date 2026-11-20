package ui

import (
	"fmt"
	"path"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
)

func (a *App) kubeRestartItems(p *Panel) []menuItem {
	if p.Kube == nil {
		return nil
	}
	selected := p.Selected()
	var jobs []contextJob
	client := p.Kube.client
	for _, name := range selected {
		for _, entry := range p.Entries {
			if entry.Name != path.Base(name) {
				continue
			}
			ke, ok := entry.Info.(*kubeEntry)
			if !ok || !kube.CanRestart(ke.object) {
				break
			}
			target := *ke.object
			jobs = append(jobs, contextJob{entry.Name, func(o *fsops.Operation) ([]byte, error) {
				if err := client.Restart(o.Context(), target); err != nil {
					return nil, err
				}
				return []byte("Rollout restart requested; the controller will apply its configured update strategy.\n"), nil
			}})
			break
		}
	}
	if len(jobs) == 0 {
		return nil
	}
	label := "Restart rollout…"
	if len(selected) > 1 {
		label = fmt.Sprintf("Restart rollout · %d/%d…", len(jobs), len(selected))
	}
	return []menuItem{{label, "", func() { a.contextBatch(p, "Restart workloads", jobs, true) }}}
}
