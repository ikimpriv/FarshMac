package ui

import (
	"context"
	"farshmac/internal/docker"
	"farshmac/internal/metrics"
	"farshmac/internal/podtoast"
	"time"
)

func (a *App) dockerMetrics(d *dockerPanel, containers []docker.Container, text bool) {
	targets := make([]metrics.Target, 0, len(containers))
	agents := make([]*podtoast.Client, len(containers))
	for index, container := range containers {
		c := container
		targets = append(targets, metrics.Target{ID: "docker:" + d.client.Context + ":" + c.ID, Name: c.Names, Labels: map[string]string{"farshmac_container": c.Names}, Fetch: func(ctx context.Context) ([]byte, error) { return d.client.Metrics(ctx, c.ID) }})
		if helper := d.helper(c.ID); helper != "" {
			targets = append(targets, metrics.Target{ID: "docker-podtoast:" + c.ID, Name: c.Names + " · PodToast", PodToast: true, Labels: map[string]string{"farshmac_container": c.Names}, Fetch: func(ctx context.Context) ([]byte, error) {
				if agents[index] == nil || !agents[index].Healthy() {
					if agents[index] != nil {
						agents[index].Close()
					}
					agent, err := d.client.Agent(ctx, helper, c.ID)
					if err != nil {
						return nil, err
					}
					agents[index] = agent
				}
				return agents[index].Metrics(ctx)
			}, History: func(ctx context.Context) ([]metrics.HistoryFrame, error) {
				if agents[index] == nil {
					return nil, nil
				}
				h, err := agents[index].MetricsHistory(ctx)
				if err != nil {
					return nil, err
				}
				var frames []metrics.HistoryFrame
				for _, f := range h.Frames {
					frames = append(frames, metrics.HistoryFrame{At: time.UnixMilli(f.At), Body: []byte(f.Body)})
				}
				return frames, nil
			}})
		}

	}
	title := "Docker · " + d.client.Context
	if len(containers) == 1 {
		title += " / " + containers[0].Names
	}
	session := metrics.NewSession(title, targets, []string{"Docker Engine statistics. Filesystem usage is not inferred from block I/O counters."}, func() {
		for _, agent := range agents {
			if agent != nil {
				agent.Close()
			}
		}
	})
	session.SetReferenceVisibility(len(containers) == 1)
	if text {
		a.startCompactMetrics(session, title)
		return
	}
	if a.Metrics != nil {
		a.Metrics.Close()
	}
	a.Metrics = session
	a.metricsLaunch = &metricsLaunch{Title: title}
	a.detachMetrics = true
	a.NewWindow = true
	a.dirty = true
}
