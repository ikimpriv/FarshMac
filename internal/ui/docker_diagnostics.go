package ui

import (
	"encoding/json"
	"farshmac/internal/docker"
	"farshmac/internal/fsops"
	"farshmac/internal/podtoast"
	"fmt"
	"strings"
)

func (a *App) dockerDiagnostics(p *Panel, c docker.Container) {
	d := p.Docker
	helper := d.helper(c.ID)
	if helper == "" {
		a.Status = "Attach PodToast with F11 to read target diagnostics"
		return
	}
	a.backgroundTask("PodToast diagnostics", func(o *fsops.Operation) (func(), func(), error) {
		agent, err := d.client.Agent(o.Context(), helper, c.ID)
		if err != nil {
			return nil, nil, err
		}
		defer agent.Close()
		var snapshot podtoast.Snapshot
		err = agent.JSON(o.Context(), "GET", "/v1/snapshot", nil, &snapshot)
		b, _ := json.MarshalIndent(snapshot, "", "  ")
		return func() { a.showText("PodToast · "+c.Names, string(b)) }, nil, err
	})
}
func (a *App) dockerTraffic(p *Panel, c docker.Container) {
	d := p.Docker
	a.form("Docker network monitor", []Field{
		{Label: "PodToast image", Value: defaultPodToastImage},
		{Label: "Attach network helper (NET_ADMIN / NET_RAW; shared target network)", Toggle: true, Checked: true},
	}, func(fields []Field) {
		image, attach := strings.TrimSpace(fields[0].Value), fields[1].Checked
		a.backgroundTask("Starting Docker traffic monitor", func(o *fsops.Operation) (func(), func(), error) {
			list, err := d.client.Containers(o.Context())
			if err != nil {
				return nil, nil, err
			}
			helper := ""
			for _, entry := range list {
				if entry.Label("io.farshmac.podtoast.network") == c.ID && entry.State == "running" {
					helper = entry.ID
					break
				}
			}
			if helper == "" {
				if !attach {
					return nil, nil, fmt.Errorf("no network helper attached")
				}
				if image == "" {
					return nil, nil, fmt.Errorf("image is required")
				}
				b, e := d.client.Run(o.Context(), "run", "-d", "--label", "io.farshmac.podtoast.network="+c.ID, "--network=container:"+c.ID, "--pid=container:"+c.ID, "--cap-add=NET_ADMIN", "--cap-add=NET_RAW", image, "idle")
				if e != nil {
					return nil, nil, e
				}
				helper = strings.TrimSpace(string(b))
			}
			agent, err := d.client.AgentCommand(o.Context(), helper, "traffic")
			if err != nil {
				return nil, nil, err
			}
			var state podtoast.TrafficStatus
			if err = agent.JSON(o.Context(), "GET", "/v1/traffic/overview", nil, &state); err != nil {
				agent.Close()
				return nil, nil, err
			}
			adopted := false
			return func() { adopted = true; a.beginTraffic(agent, "Docker · "+c.Names, state) }, func() {
				if !adopted {
					agent.Close()
				}
			}, nil
		})
	})
	a.Form.Primary = "Open monitor"
}
