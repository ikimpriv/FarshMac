package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"farshmac/internal/podtoast"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/version"
)

func toastKey(ref kube.ContainerRef) string {
	return ref.Namespace + "/" + ref.Pod + "/" + ref.Container
}
func (k *kubePanel) toast(ref kube.ContainerRef) *podtoast.Client {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.toasts[toastKey(ref)]
}
func podToastTarget(p *Panel) (kube.ContainerRef, bool) {
	r := p.kubeLevel()
	switch r.Level {
	case kubeContainerFS:
		return kube.ContainerRef{Namespace: r.Namespace, Pod: r.Object.Name, Container: r.Container}, true
	case kubeContainers:
		if len(p.Entries) > 0 && p.Entries[p.Cursor].Name != ".." {
			return kube.ContainerRef{Namespace: r.Namespace, Pod: r.Object.Name, Container: containerName(p.Entries[p.Cursor].Name)}, true
		}
		return kube.ContainerRef{Namespace: r.Namespace, Pod: r.Object.Name}, true
	}
	// The selected pod can be distroless, so it may never have loaded as a
	// filesystem. A single-container pod can be selected from its parent.
	if obj := p.kubeCurrent(); isPodObject(obj) {
		return kube.ContainerRef{Namespace: obj.Namespace, Pod: obj.Name}, true
	}
	return kube.ContainerRef{}, false
}

const defaultPodToastImage = "ghcr.io/ikimpriv/podtoast:0.8.2"

func preferredPodToastImage(image string) string {
	if image == "" {
		return defaultPodToastImage
	}
	const official = "ghcr.io/ikimpriv/podtoast:"
	tag, ok := strings.CutPrefix(image, official)
	if !ok {
		return image
	}
	currentTag := strings.TrimPrefix(defaultPodToastImage, official)
	saved, err := version.ParseSemantic(tag)
	current, currentErr := version.ParseSemantic(currentTag)
	if err == nil && currentErr == nil && saved.LessThan(current) {
		return defaultPodToastImage
	}
	return image
}

func (a *App) startPodToast(p *Panel) {
	image := preferredPodToastImage(a.Settings.PodToastImage)
	a.startPodToastPreset(p, EphemeralPreset{PodToast: true, Options: kube.EphemeralOptions{Image: image, PullPolicy: "IfNotPresent", TimeoutSeconds: 90}})
}
func (a *App) startPodToastPreset(p *Panel, preset EphemeralPreset) {
	preset.Options.Image = preferredPodToastImage(preset.Options.Image)
	if targets := kubeContextTargets(p); len(targets) > 1 {
		a.batchEphemeralForm(p, preset, targets)
		return
	}
	ref, ok := podToastTarget(p)
	if !ok {
		a.Status = "PodToast: select a pod or enter a container"
		return
	}
	k := p.Kube
	a.backgroundTask("Reading pod containers", func(o *fsops.Operation) (func(), func(), error) {
		pod, err := k.client.Pod(o.Context(), ref.Namespace, ref.Pod)
		return func() {
			if p.Kube == k {
				a.podToastVersionPrompt(p, preset, ref, pod)
			}
		}, nil, err
	})
}
func (a *App) podToastForm(p *Panel, preset EphemeralPreset, ref kube.ContainerRef, pod *corev1.Pod) {
	names := runningPodContainers(pod)
	if len(names) == 0 {
		a.Status = "PodToast: no running application containers"
		return
	}
	shared := pod.Spec.ShareProcessNamespace != nil && *pod.Spec.ShareProcessNamespace
	found := false
	for _, name := range names {
		if name == ref.Container {
			found = true
		}
	}
	if !found {
		ref.Container = names[0]
	}
	k := p.Kube
	index := a.Active
	fields := []Field{
		{Label: "PodToast image (registry/repository:tag or @sha256:digest)", Value: preset.Options.Image},
		{Label: "Pull image", Toggle: true, Options: []string{"IfNotPresent", "Always", "Never"}, Value: preset.Options.PullPolicy},
		{Label: "Startup timeout, seconds (5–600)", Value: strconv.Itoa(preset.Options.TimeoutSeconds)},
	}
	if len(names) > 1 {
		for _, name := range names {
			fields = append(fields, Field{Label: "Target: " + name, Value: name, Toggle: true, Checked: true})
		}
	}
	a.form("Start PodToast", fields, func(fields []Field) {
		image := strings.TrimSpace(fields[0].Value)
		selected := []string{ref.Container}
		if len(fields) > 3 {
			selected = nil
			for _, f := range fields[3:] {
				if f.Checked {
					selected = append(selected, f.Value)
				}
			}
		}
		if len(selected) == 0 {
			a.reopenForm(fields, 3, "Select at least one target container")
			return
		}
		ref.Container = selected[0]
		if image == "" {
			a.reopenForm(fields, 0, "Enter a PodToast image reference")
			return
		}
		timeout, _ := strconv.Atoi(fields[2].Value)
		if timeout < 5 || timeout > 600 {
			a.reopenForm(fields, 2, "Startup timeout must be 5–600 seconds")
			return
		}
		pullPolicy := fields[1].Value
		a.Settings.PodToastImage = image
		oldAgents := map[string]bool{}
		for _, name := range selected {
			if ec, ok := kube.RunningPodToast(pod, name); ok {
				oldAgents[ec.Name] = true
			}
		}
		a.backgroundTask("Starting PodToast", func(o *fsops.Operation) (func(), func(), error) {
			client, err := k.client.StartPodToastOptions(o.Context(), ref, image, pullPolicy, timeout)
			if err != nil {
				return nil, nil, err
			}
			clients := map[string]*podtoast.Client{toastKey(ref): client}
			warnings := []string{}
			{
				for _, name := range selected {
					if name == ref.Container {
						continue
					}
					other := ref
					other.Container = name
					connected, e := k.client.StartPodToastOptions(o.Context(), other, image, pullPolicy, timeout)
					if e != nil {
						warnings = append(warnings, name+": "+e.Error())
						continue
					}
					clients[toastKey(other)] = connected
				}
			}
			debugShells := []kube.DebugShell{}
			for _, name := range selected {
				target := ref
				target.Container = name
				if clients[toastKey(target)] == nil {
					continue
				}
				d, e := k.client.ResolveDebugShell(o.Context(), target, "")
				if e != nil {
					warnings = append(warnings, name+" shell: "+e.Error())
				} else {
					debugShells = append(debugShells, d)
				}
			}
			adopted := false
			return func() {
					if a.Panels[index].Kube != k {
						for _, c := range clients {
							c.Close()
						}
						return
					}
					k.mu.Lock()
					if k.toasts == nil {
						k.toasts = map[string]*podtoast.Client{}
					}
					oldClients := []*podtoast.Client{}
					for key, c := range clients {
						if old := k.toasts[key]; old != nil {
							oldClients = append(oldClients, old)
						}
						k.toasts[key] = c
					}
					adopted = true
					k.mu.Unlock()
					for _, old := range oldClients {
						old.Drain()
					}
					for _, d := range debugShells {
						k.setDebugShell(d)
					}
					a.Status = "PodToast connected to " + toastKey(ref)
					if shared {
						a.Status = fmt.Sprintf("PodToast: one agent, %d container sessions", len(clients))
					}
					if len(warnings) > 0 {
						a.Status += " · " + strings.Join(warnings, "; ")
					}
					a.rememberEphemeral(EphemeralPreset{PodToast: true, Options: kube.EphemeralOptions{Image: image, PullPolicy: pullPolicy, TimeoutSeconds: timeout}})
					a.refreshEphemeralPod(k, ref, true)
					retiring := []string{}
					for name := range oldAgents {
						if name != client.AgentName {
							retiring = append(retiring, name)
						}
					}
					if len(retiring) > 0 {
						a.Status += " · waiting for old PodToast operations to finish"
						replacement := client.AgentName
						go func() {
							ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
							defer cancel()
							err := k.client.RetireSupersededPodToast(ctx, ref, retiring, replacement)
							a.jobs <- func() {
								if a.closed {
									return
								}
								if err != nil {
									a.Status = "New PodToast active; old agent kept: " + err.Error()
								} else {
									a.Status = "New PodToast active; superseded idle agents stopped"
								}
								a.dirty = true
							}
						}()
					}

				}, func() {
					if !adopted {
						for _, c := range clients {
							c.Close()
						}
					}
				}, nil
		})
		if a.task != nil {
			a.task.reportError = true
		}
	})
	a.Form.Primary = "Connect / reuse"
	var versions []string
	upgrade := false
	for _, name := range names {
		if ec, ok := kube.RunningPodToast(pod, name); ok {
			versions = append(versions, name+": "+ec.Image)
			if ec.Image != preset.Options.Image {
				upgrade = true
			}
		}
	}
	existing := ""
	if len(versions) > 0 {
		existing = "Running PodToast: " + strings.Join(versions, "; ") + ". "
	}
	if upgrade {
		a.Form.Title = "Update PodToast"
		a.Form.Primary = "Attach new agent"
		existing += "Connect the selected image and switch to the new agent. "
	}

	a.Form.Message = existing + "Root debug toolbox with filesystem/ptrace capabilities. Selected targets share one agent only in a shared PID namespace. Created containers remain until the pod is deleted."
}
func (a *App) podToastClient(p *Panel) (*podtoast.Client, bool) {
	ref, ok := podToastTarget(p)
	if !ok {
		a.Status = "PodToast: enter the target container"
		return nil, false
	}
	c := p.Kube.toast(ref)
	if c == nil {
		a.Status = "Start PodToast for this container first (Alt+Enter / F11 > Attach ephemeral container)"
		return nil, false
	}
	return c, true
}
func (a *App) podToastSnapshot(p *Panel) {
	c, ok := a.podToastClient(p)
	if !ok {
		return
	}
	a.backgroundTask("Reading PodToast diagnostics", func(o *fsops.Operation) (func(), func(), error) {
		var snapshot podtoast.Snapshot
		err := c.JSON(o.Context(), "GET", "/v1/snapshot", nil, &snapshot)
		if err != nil {
			return nil, nil, err
		}
		b, err := json.MarshalIndent(snapshot, "", "  ")
		return func() { a.showText("PodToast snapshot.json", string(b)) }, nil, err
	})
}

// Escape terminates the monitor, while ordinary log streams keep their usual
// input behavior. Each sample replaces the display rather than accumulating.
type toastMonitorStream struct{ *logStream }

func (s *toastMonitorStream) Write(b []byte) error {
	if string(b) == "\x1b" {
		s.Close()
		return nil
	}
	return s.logStream.Write(b)
}
func (a *App) podToastMonitor(p *Panel) {
	c, ok := a.podToastClient(p)
	if !ok {
		return
	}
	stream := newLogStream(context.Background(), func(ctx context.Context) (io.ReadCloser, error) {
		r, w := io.Pipe()
		go func() {
			formatter := podtoast.MonitorFormatter{}
			err := c.Monitor(ctx, func(s podtoast.Snapshot) error {
				_, e := io.WriteString(w, "\x1b[H\x1b[2J"+formatter.Format(s))
				return e
			})
			w.CloseWithError(err)
		}()
		return r, nil
	})
	a.startForeign(&toastMonitorStream{stream}, "PodToast monitor · Esc stops", nil)
}
func (a *App) podToastProbe(p *Panel) {
	ref, ok := podToastTarget(p)
	if !ok {
		return
	}
	c := p.Kube.podNetworkToast(ref)
	if c == nil {
		a.Status = "Attach PodToast to this pod first"
		return
	}
	a.form("PodToast network diagnostics", []Field{
		{Label: "Probe", Toggle: true, Options: []string{"dns", "tcp", "http", "load"}, Value: "http"},
		{Label: "Target: DNS name, host:port or HTTP(S) URL", Value: "http://127.0.0.1:8080/"},
		{Label: "Load only: total requests (1–10000)", Value: "100"},
		{Label: "Load only: concurrent requests (1–32)", Value: "4"},
		{Label: "Load only: requests/second (1–1000; max 60s)", Value: "10"},
	}, func(fields []Field) {
		request := podtoast.Probe{Kind: fields[0].Value, Target: strings.TrimSpace(fields[1].Value)}
		request.Requests, _ = strconv.Atoi(fields[2].Value)
		request.Concurrency, _ = strconv.Atoi(fields[3].Value)
		request.Rate, _ = strconv.Atoi(fields[4].Value)
		a.backgroundTask("PodToast "+request.Kind+" · "+request.Target, func(o *fsops.Operation) (func(), func(), error) {
			var result podtoast.ProbeResult
			err := c.JSON(o.Context(), "POST", "/v1/probe", request, &result)
			if err != nil {
				return nil, nil, err
			}
			b, err := json.MarshalIndent(result, "", "  ")
			return func() { a.showText("PodToast network.json", string(b)) }, nil, err
		})
		if a.task != nil {
			a.task.reportError = true
		}
	})
	a.Form.Primary = "Run"
}

func runningPodContainers(pod *corev1.Pod) []string {
	names := []string{}
	statuses := append(append([]corev1.ContainerStatus{}, pod.Status.ContainerStatuses...), pod.Status.InitContainerStatuses...)
	for _, st := range statuses {
		if st.State.Running != nil {
			names = append(names, st.Name)
		}
	}
	return names
}
func (k *kubePanel) podNetworkToast(ref kube.ContainerRef) *podtoast.Client {
	k.mu.Lock()
	defer k.mu.Unlock()
	prefix := ref.Namespace + "/" + ref.Pod + "/"
	for key, c := range k.toasts {
		if strings.HasPrefix(key, prefix) && c.Healthy() {
			return c
		}
	}
	return nil
}

func (k *kubePanel) existingToast(ctx context.Context, ref kube.ContainerRef) (*podtoast.Client, error) {
	k.mu.Lock()
	if k.closed {
		k.mu.Unlock()
		return nil, context.Canceled
	}
	epoch := k.epoch
	cached := k.toasts[toastKey(ref)]
	k.mu.Unlock()
	if cached != nil && cached.Healthy() {
		if cached.AgentName == "" {
			return cached, nil
		}
		pod, e := k.client.Pod(ctx, ref.Namespace, ref.Pod)
		if e != nil {
			return nil, e
		}
		if preferred, ok := kube.RunningPodToast(pod, ref.Container); ok && preferred.Name == cached.AgentName {
			return cached, nil
		}
	}
	c, err := k.client.ConnectExistingPodToast(ctx, ref)
	if err != nil {
		if cached != nil && errors.Is(err, kube.ErrNoPodToast) {
			return nil, fmt.Errorf("PodToast disconnected; reconnect via Attach ephemeral container")
		}
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed || epoch != k.epoch {
		c.Close()
		return nil, context.Canceled
	}
	if old := k.toasts[toastKey(ref)]; old != nil && old.Healthy() && old.AgentName == c.AgentName {
		c.Close()
		return old, nil
	}
	if k.toasts == nil {
		k.toasts = map[string]*podtoast.Client{}
	}
	if old := k.toasts[toastKey(ref)]; old != nil {
		old.Drain()
	}
	k.toasts[toastKey(ref)] = c
	return c, nil
}

func (a *App) podToastVersionPrompt(p *Panel, preset EphemeralPreset, ref kube.ContainerRef, pod *corev1.Pod) {
	var lines []string
	different := false
	for _, name := range runningPodContainers(pod) {
		if ref.Container != "" && ref.Container != name {
			continue
		}
		if ec, ok := kube.RunningPodToast(pod, name); ok {
			lines = append(lines, name+": "+ec.Image)
			if ec.Image != preset.Options.Image {
				different = true
			}
		}
	}
	if len(lines) == 0 {
		a.podToastForm(p, preset, ref, pod)
		return
	}
	title, button := "PodToast version", "Connect / reuse"
	message := "Running agent:\n" + strings.Join(lines, "\n") + "\n\nSelected image: " + preset.Options.Image
	if different {
		title, button = "Update PodToast", "Attach new agent"
		message += "\n\nAttach the selected version and switch to it after connection succeeds? The existing agent stays in the pod."
	}
	a.confirm(title, message, []string{button, "Cancel"}, func(i int) {
		if i == 0 {
			a.podToastForm(p, preset, ref, pod)
		}
	})
}
