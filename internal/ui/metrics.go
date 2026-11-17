package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"farshmac/internal/metrics"
	"k8s.io/client-go/rest"
)

type metricsLaunch struct {
	Title   string
	Config  *rest.Config
	Profile kube.Profile
	Ref     kube.ContainerRef
	Refs    []kube.ContainerRef
	Text    bool
}

func metricSelection(p *Panel) []kube.ContainerRef {
	r := p.kubeLevel()
	refs := []kube.ContainerRef{}
	if r.Level == kubeTypeFolder {
		for _, name := range p.Selected() {
			for _, obj := range r.Objects {
				if obj.Name == path.Base(name) && isPodObject(obj) {
					refs = append(refs, kube.ContainerRef{Namespace: obj.Namespace, Pod: obj.Name})
				}
			}
		}
	}
	if r.Level == kubeContainers && r.Object != nil {
		for _, name := range p.Selected() {
			if path.Base(name) != ".." {
				refs = append(refs, kube.ContainerRef{Namespace: r.Object.Namespace, Pod: r.Object.Name, Container: containerName(path.Base(name))})
			}
		}
	}
	if len(refs) == 0 {
		if ref, ok := podToastTarget(p); ok {
			refs = append(refs, ref)
		}
	}
	return refs
}
func (a *App) openPodMetrics(p *Panel) {
	refs := metricSelection(p)
	if len(refs) == 0 {
		a.Status = "Select pods or a container"
		return
	}
	if len(refs) > 32 {
		a.Status = "Select at most 32 pods per dashboard"
		return
	}
	launch := metricsLaunch{Config: p.Kube.client.Config(), Profile: p.Kube.profile, Ref: refs[0], Refs: refs}
	if len(refs) != 1 || refs[0].Container == "" {
		a.openMetrics(launch)
		return
	}
	a.choose("Live metrics", []string{"Live charts — selected container", "Live charts — whole pod"}, func(i int) {
		if i == 1 {
			launch.Ref.Container = ""
			launch.Refs = []kube.ContainerRef{launch.Ref}
		}
		a.openMetrics(launch)
	})
}

func (a *App) openMetrics(launch metricsLaunch) {
	a.backgroundTask("Discovering metrics", func(o *fsops.Operation) (func(), func(), error) {
		refs := launch.Refs
		if len(refs) == 0 {
			refs = []kube.ContainerRef{launch.Ref}
		}
		refs, filters := quickMetricRefs(refs)
		readers := []*kube.MetricsReader{}
		cleanup := func() {
			for _, r := range readers {
				r.Close()
			}
		}
		targets := []metrics.Target{}
		warnings := []string{}
		singleContainer := false
		for _, ref := range refs {
			targetStart := len(targets)
			if err := o.Check(); err != nil {
				cleanup()
				return nil, nil, err
			}
			c, err := kube.NewFromConfig(o.Context(), launch.Config, launch.Profile)
			if err != nil {
				cleanup()
				return nil, nil, err
			}
			discovery, err := c.DiscoverMetrics(o.Context(), ref)
			if err != nil {
				c.Close()
				warnings = append(warnings, ref.Pod+": "+err.Error())
				continue
			}
			if len(refs) == 1 {
				selected := filters[ref.Namespace+"/"+ref.Pod]
				singleContainer = len(selected) == 1 || len(selected) == 0 && discovery.ContainerCount == 1
			}
			reader := &kube.MetricsReader{Client: c, Ref: ref, UID: discovery.PodUID}
			readers = append(readers, reader)
			for _, warning := range discovery.Warnings {
				warnings = append(warnings, ref.Pod+": "+warning)
			}
			prefix := ref.Namespace + "/" + ref.Pod
			for _, ep := range discovery.Endpoints {
				endpoint := ep
				if ref.Container != "" && ep.Owner != "" && ep.Owner != ref.Container {
					continue
				}
				if len(targets) >= 128 {
					warnings = append(warnings, "Endpoint limit (128) reached")
					break
				}
				targets = append(targets, metrics.Target{PodToast: ep.Container != "", SharedPID: ep.Shared, ID: prefix + "/" + ep.ID, Name: prefix + " · " + ep.Source, Labels: map[string]string{"farshmac_namespace": ref.Namespace, "farshmac_pod": ref.Pod, "farshmac_container": ep.Owner}, ContainerFilter: ref.Container, Fetch: func(ctx context.Context) ([]byte, error) { return reader.Scrape(ctx, endpoint) }})
				if ep.Container != "" {
					targets[len(targets)-1].History = func(ctx context.Context) ([]metrics.HistoryFrame, error) { return reader.History(ctx, endpoint) }
				}
			}
			targets = append(targets, metrics.Target{ID: prefix + "/resource-bounds", Name: prefix + " · Resource requests / limits", Labels: map[string]string{"farshmac_namespace": ref.Namespace, "farshmac_pod": ref.Pod}, ContainerFilter: ref.Container, Fetch: func(ctx context.Context) ([]byte, error) { return c.ResourceBounds(ctx, ref, discovery.PodUID) }})
			targets = append(targets, metrics.Target{ID: prefix + "/resource-metrics", Name: prefix + " · Kubernetes resource metrics", Labels: map[string]string{"farshmac_namespace": ref.Namespace, "farshmac_pod": ref.Pod}, ContainerFilter: ref.Container, Fetch: func(ctx context.Context) ([]byte, error) { return c.ResourceMetrics(ctx, ref, discovery.PodUID) }})
			targets = append(targets, metrics.Target{ID: prefix + "/storage-metrics", Name: prefix + " · Kubelet storage (nodes/proxy)", Labels: map[string]string{"farshmac_namespace": ref.Namespace, "farshmac_pod": ref.Pod}, ContainerFilter: ref.Container, Fetch: func(ctx context.Context) ([]byte, error) { return c.StorageMetrics(ctx, ref, discovery.PodUID) }})
			for i := targetStart; i < len(targets); i++ {
				targets[i].ContainerFilters = filters[prefix]
			}
		}
		title := launch.Ref.Namespace + " / " + launch.Ref.Pod
		if len(refs) > 1 {
			names := []string{}
			for _, r := range refs {
				names = append(names, r.Pod)
			}
			title = fmt.Sprintf("%d pods · %s", len(refs), strings.Join(names, ", "))
		} else if len(filters[launch.Ref.Namespace+"/"+launch.Ref.Pod]) > 1 {
			title += fmt.Sprintf(" / %d containers", len(filters[launch.Ref.Namespace+"/"+launch.Ref.Pod]))
		} else if launch.Ref.Container != "" {
			title += " / " + launch.Ref.Container
		}
		session := metrics.NewSession(title, targets, warnings, cleanup)
		session.SetReferenceVisibility(singleContainer)
		adopted := false
		return func() {
				adopted = true
				a.Status = ""
				if launch.Text {
					a.startCompactMetrics(session, title)
					return
				}
				if a.Metrics != nil {
					a.Metrics.Close()
				}
				a.Metrics = session
				a.metricsLaunch = &launch
				if !a.MetricsOnly {
					a.detachMetrics = true
					a.NewWindow = true
				}
				a.dirty = true
			}, func() {
				if !adopted {
					session.Close()
				}
			}, nil
	})
}
func (a *App) handleMetrics(m Message) bool {
	if a.Metrics == nil {
		return false
	}
	if m.Type == "resize" || m.Type == "historyAppend" || m.Type == "historyRead" || m.Type == "terminalError" || m.Type == "inputBinary" {
		return false
	}
	if m.Type == "key" {
		if m.Meta && (m.Key == "w" || m.Key == "W") {
			a.RequestClose()
			return true
		}
		if m.Alt && m.Key == "F9" {
			a.Zoom = true
			return true
		}
		if m.Key == "Escape" {
			if a.Operation != nil {
				a.Operation.Cancel()
			}
			a.Metrics.Pause(true)
			return true
		}
		return true
	}
	if m.Type != "metrics" {
		return true
	}
	switch m.Key {
	case "configure":
		var cfg struct {
			Keys   []string `json:"keys"`
			Layout string   `json:"layout"`
		}
		if len(m.Text) <= 128<<10 && json.Unmarshal([]byte(m.Text), &cfg) == nil {
			a.Metrics.Configure(cfg.Keys, cfg.Layout)
		}
	case "pause":
		a.Metrics.Pause(m.Button != 0)
	case "interval":
		a.Metrics.Interval(m.Count)
	case "endpoint":
		a.Metrics.Enable(m.Text, m.Button != 0)
	case "detach":
		if !a.MetricsOnly && !a.Busy {
			a.detachMetrics = true
			a.NewWindow = true
		}
	case "rediscover":
		if a.metricsLaunch != nil && a.metricsLaunch.Config != nil && !a.Busy {
			a.openMetrics(*a.metricsLaunch)
		}
	case "close":
		if a.Operation != nil {
			a.Operation.Cancel()
		}
		if a.MetricsOnly {
			a.Quit = true
		} else {
			a.Metrics.Close()
			a.Metrics = nil
			a.dirty = true
		}
	}
	return true
}

func (l metricsLaunch) title() string {
	if l.Title != "" {
		return l.Title
	}
	if len(l.Refs) > 1 {
		return fmt.Sprintf("Metrics · %d pods", len(l.Refs))
	}
	title := "Metrics · " + l.Ref.Namespace + " / " + l.Ref.Pod
	if l.Ref.Container != "" {
		title += " / " + l.Ref.Container
	}
	return title
}
