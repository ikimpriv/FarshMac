package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
)

// EphemeralPreset is saved in setup.json. It intentionally has no environment
// or credential fields. Integration is an explicit role, not an image-name guess.
type EphemeralPreset struct {
	PodToast bool
	Options  kube.EphemeralOptions
}

func (a *App) panelContextMenu() {
	p := &a.Panels[a.Active]
	// A single marked item is the target even when the cursor is elsewhere.
	if selected := p.Selected(); len(selected) == 1 {
		for i, e := range p.Entries {
			if e.Name == path.Base(selected[0]) {
				p.Cursor = i
				break
			}
		}
	}
	multiFiles := p.fileContextActions() && len(p.Selected()) > 1
	items := []menuItem{}
	if p.Docker != nil {
		items = append(items, a.dockerContextItems(p)...)
		if !multiFiles && len(dockerMetricSelection(p)) > 0 {
			items = append(items, menuItem{"Quick Live Metrics", "⌃⇧Q", a.toggleQuickMetrics})
		}
	}
	if p.fileContextActions() && len(p.Selected()) > 0 {
		items = append(items, menuItem{"Send by Email…", "", func() { a.nativeSelectedFiles("email") }}, menuItem{"Create archive…", "", a.archiveSelectionMenu}, menuItem{"Open In…", "", func() { a.nativeSelectedFiles("openIn") }})
	}
	if p.Kube != nil && !multiFiles {
		items = append(items, a.kubeRestartItems(p)...)
		if len(metricSelection(p)) > 0 {
			items = append(items, menuItem{"Live metrics…", "", func() { a.openPodMetrics(p) }}, menuItem{"Quick Live Metrics", "⌃⇧Q", a.toggleQuickMetrics})
		}
		targets := kubeContextTargets(p)
		if len(targets) > 1 {
			items = append(items, a.podToastBatchItems(p, targets)...)
		}
		if len(targets) > 0 {
			items = append(items, menuItem{"Attach ephemeral container…", "", func() { a.ephemeralChoices(p) }})
			label := "PodToast version / update…"
			if len(targets) == 1 {
				ref := targets[0]
				if client := p.Kube.toast(ref); client != nil && client.ImageRef != "" {
					label = "PodToast: " + client.ImageRef + " · version / update…"
				}
			}
			items = append(items, menuItem{label, "", func() { a.startPodToast(p) }})
			if len(targets) == 1 && len(p.Selected()) <= 1 {
				items = append(items, menuItem{"Traffic monitoring / faults…", "", func() { a.trafficForm(p) }})
				if ref, _ := podToastTarget(p); p.Kube.toast(ref) != nil {
					items = append(items, menuItem{"PodToast live monitor", "", func() { a.podToastMonitor(p) }}, menuItem{"PodToast snapshot", "", func() { a.podToastSnapshot(p) }})
				}
				if ref, _ := podToastTarget(p); p.Kube.podNetworkToast(ref) != nil {
					items = append(items, menuItem{"Pod network diagnostics…", "", func() { a.podToastProbe(p) }})
				}
				items = append(items, menuItem{"Open shell in container", "", func() { a.kubeShell(p) }})
			}
		}
	}
	if len(items) == 0 {
		a.Status = "No context actions for this selection"
		return
	}
	labels := make([]string, len(items))
	for i, item := range items {
		labels[i] = item.Label
	}
	a.choose("Context menu", labels, func(i int) { items[i].Run() })
}
func (a *App) ephemeralChoices(p *Panel) {
	presets := append([]EphemeralPreset(nil), a.Settings.EphemeralHistory...)
	labels := []string{"PodToast — integrated files and diagnostics…"}
	for _, preset := range presets {
		role := preset.Options.Profile
		if preset.PodToast {
			role = "PodToast"
		}
		labels = append(labels, preset.Options.Image+" · "+role+"…")
	}
	labels = append(labels, "New image…")
	a.choose("Ephemeral container", labels, func(i int) {
		if i == 0 {
			a.startPodToast(p)
			return
		}
		if i == len(labels)-1 {
			a.ephemeralForm(p, EphemeralPreset{Options: kube.EphemeralOptions{Profile: "Default", PullPolicy: "IfNotPresent", TTY: true, TimeoutSeconds: 90}})
			return
		}
		preset := presets[i-1]
		if preset.PodToast {
			a.startPodToastPreset(p, preset)
		} else {
			a.ephemeralForm(p, preset)
		}
	})
}
func (a *App) rememberEphemeral(preset EphemeralPreset) {
	encoded, _ := json.Marshal(preset)
	list := []EphemeralPreset{preset}
	for _, old := range a.Settings.EphemeralHistory {
		b, _ := json.Marshal(old)
		if string(b) != string(encoded) && len(list) < 16 {
			list = append(list, old)
		}
	}
	a.Settings.EphemeralHistory = list
	// Respect concurrent setup changes; do not force overwrite or show a second
	// confirmation dialog after the cluster operation has already succeeded.
	if err := a.writeSetup(false); err != nil {
		a.Status += " · connection history was not saved: " + err.Error()
	}
}
func (a *App) ephemeralForm(p *Panel, preset EphemeralPreset) {
	if targets := kubeContextTargets(p); len(targets) > 1 {
		a.batchEphemeralForm(p, preset, targets)
		return
	}
	ref, ok := podToastTarget(p)
	if !ok {
		a.Status = "Select a pod or container"
		return
	}
	k := p.Kube
	a.backgroundTask("Reading pod containers", func(op *fsops.Operation) (func(), func(), error) {
		pod, err := k.client.Pod(op.Context(), ref.Namespace, ref.Pod)
		return func() {
			if p.Kube == k {
				a.ephemeralFormTargets(p, preset, ref, runningPodContainers(pod))
			}
		}, nil, err
	})
}
func (a *App) ephemeralFormTargets(p *Panel, preset EphemeralPreset, ref kube.ContainerRef, names []string) {
	k := p.Kube
	if ref.Container == "" && len(names) == 1 {
		ref.Container = names[0]
	}

	o := preset.Options
	command := ""
	if len(o.Command) > 0 {
		b, _ := json.Marshal(o.Command)
		command = string(b)
	}
	uid := ""
	if o.RunAsUser != nil {
		uid = strconv.FormatInt(*o.RunAsUser, 10)
	}
	fields := []Field{
		{Label: "Image", Value: o.Image},
		{Label: "Pod network only (no target filesystem)", Toggle: true, Checked: len(names) == 0},
		{Label: "Command as JSON argv (blank = image entrypoint)", Value: command},
		{Label: "Access", Toggle: true, Options: []string{"Default", "Restricted", "Network", "Processes", "Files and shell", "Privileged"}, Value: o.Profile},
		{Label: "Run as UID (blank = image / pod default)", Value: uid},
		{Label: "Pull image", Toggle: true, Options: []string{"IfNotPresent", "Always", "Never"}, Value: o.PullPolicy},
		{Label: "Startup timeout, seconds (5–600)", Value: strconv.Itoa(o.TimeoutSeconds)},
		{Label: "Interactive TTY", Toggle: true, Checked: o.TTY}, {Label: "Read-only debug container root", Toggle: true, Checked: o.ReadOnlyRoot},
	}
	for _, name := range names {
		fields = append(fields, Field{Label: "Target: " + name, Value: name, Toggle: true, Checked: true})
	}
	a.form("Attach ephemeral container", fields, func(fields []Field) {
		next := kube.EphemeralOptions{Image: strings.TrimSpace(fields[0].Value), Profile: fields[3].Value, PullPolicy: fields[5].Value, TTY: fields[7].Checked, ReadOnlyRoot: fields[8].Checked}
		selected := []string{}
		if fields[1].Checked {
			selected = append(selected, "")
		} else {
			for _, f := range fields[9:] {
				if f.Checked {
					selected = append(selected, f.Value)
				}
			}
		}
		if len(selected) == 0 {
			a.reopenForm(fields, 9, "Select at least one target or pod network only")
			return
		}
		ref.Container = selected[0]
		if raw := strings.TrimSpace(fields[2].Value); raw != "" {
			if err := json.Unmarshal([]byte(raw), &next.Command); err != nil || next.Command == nil {
				a.reopenForm(fields, 2, "Use a JSON argv array, e.g. [\"/bin/sh\"]")
				return
			}
		}
		if raw := strings.TrimSpace(fields[4].Value); raw != "" {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				a.reopenForm(fields, 4, "Invalid UID")
				return
			}
			next.RunAsUser = &id
		}
		next.TimeoutSeconds, _ = strconv.Atoi(fields[6].Value)
		if err := next.Validate(); err != nil {
			a.reopenForm(fields, 0, err.Error())
			return
		}
		a.backgroundTask("Starting ephemeral container", func(op *fsops.Operation) (func(), func(), error) {
			var debug kube.ContainerRef
			bindings := []kube.DebugShell{}
			warnings := []string{}
			for i, name := range selected {
				target := ref
				target.Container = name
				started, e := k.client.StartEphemeral(op.Context(), target, next)
				if e != nil {
					if i == 0 {
						return nil, nil, e
					}
					warnings = append(warnings, name+": "+e.Error())
					continue
				}
				if i == 0 {
					debug = started
				}
				if name != "" {
					d, e := k.client.ResolveDebugShell(op.Context(), target, started.Container)
					if e == nil {
						bindings = append(bindings, d)
					} else {
						warnings = append(warnings, name+": "+e.Error())
					}
				}
			}
			pod, err := k.client.Pod(op.Context(), ref.Namespace, ref.Pod)
			if err != nil {
				return nil, nil, err
			}
			ended := false
			exitCode := int32(0)
			for _, st := range pod.Status.EphemeralContainerStatuses {
				if st.Name == debug.Container && st.State.Terminated != nil {
					ended = true
					exitCode = st.State.Terminated.ExitCode
				}
			}
			return func() {
				a.Status = "Ephemeral container " + debug.Container + " started"
				for _, d := range bindings {
					k.setDebugShell(d)
				}
				if len(warnings) > 0 {
					a.Status += " · " + strings.Join(warnings, "; ")
				}
				a.rememberEphemeral(EphemeralPreset{Options: next})
				if len(bindings) > 0 {
					ref = bindings[0].Target
				}
				a.refreshEphemeralPod(k, ref, len(bindings) > 0)
				if len(bindings) > 0 {
					return
				}
				if ended {
					stream := newLogStream(context.Background(), func(ctx context.Context) (io.ReadCloser, error) {
						return k.client.Logs(ctx, ref.Namespace, ref.Pod, kube.LogOptions{Container: debug.Container, TailLines: 1000})
					})
					a.startForeign(stream, fmt.Sprintf("ephemeral %s · exit %d", debug.Container, exitCode), nil)
					return
				}
				stream, err := kube.StartExec(context.Background(), k.client.Attacher(next.TTY), debug, []string{"attach"}, a.Cols, max(1, a.Rows-1))
				if err != nil {
					a.error(err)
					return
				}
				a.startForeign(stream, "ephemeral "+debug.Container, nil)
			}, nil, nil
		})
		if a.task != nil {
			a.task.reportError = true
		}
	})
	a.Form.Primary = "Start / attach"
	a.Form.Message = "Files and shell adds ptrace and filesystem capabilities; use UID 0 for distroless access. Privileged grants full privileges. Esc does not delete a container already created."
}

// Adding a debugger changes a single-container pod into a container list.
// Remap open filesystem panels before invalidating their cached pod summary.
func (a *App) refreshEphemeralPod(k *kubePanel, ref kube.ContainerRef, integrate bool) {
	paths := map[int]string{}
	for i := range a.Panels {
		p := &a.Panels[i]
		if p.Kube != k {
			continue
		}
		r := p.kubeLevel()
		if r.Object != nil && r.Object.Name == ref.Pod && r.Namespace == ref.Namespace && r.Level == kubeContainerFS {
			if base := virtualPodRoot(p.Path, ref.Pod); base != "" {
				paths[i] = path.Join(base, r.Container, r.FSPath)
			}
		}
	}
	if integrate {
		p := &a.Panels[a.Active]
		if p.Kube == k {
			if _, ok := paths[a.Active]; !ok {
				if base := virtualPodRoot(p.Current(), ref.Pod); base != "" {
					paths[a.Active] = path.Join(base, ref.Container)
				}
			}
		}
	}
	k.mu.Lock()
	delete(k.pods, ref.Namespace+"/"+ref.Pod)
	k.lastPath = ""
	k.mu.Unlock()
	for i, destination := range paths {
		a.error(a.Panels[i].Load(destination))
	}
}
func virtualPodRoot(p, pod string) string {
	parts := kubeSegments(p)
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "pods" && parts[i+1] == pod {
			return "/" + path.Join(parts[:i+2]...)
		}
	}
	return ""
}
