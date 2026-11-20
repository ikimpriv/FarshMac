package ui

import (
	"encoding/json"
	"errors"
	"farshmac/internal/fsops"
	"fmt"
	"strconv"
	"strings"

	"farshmac/internal/kube"
	"farshmac/internal/podtoast"
	"k8s.io/apimachinery/pkg/types"
)

type ephemeralBatchTarget struct {
	Ref          kube.ContainerRef
	UID          types.UID
	Agent, Image string
}

func (a *App) batchEphemeralForm(p *Panel, preset EphemeralPreset, refs []kube.ContainerRef) {
	k := p.Kube
	a.backgroundTask("Reading selected pods", func(op *fsops.Operation) (func(), func(), error) {
		var targets []ephemeralBatchTarget
		var problems []string
		seen := map[string]bool{}
		for _, ref := range refs {
			if err := op.Check(); err != nil {
				return nil, nil, err
			}
			pod, err := k.client.Pod(op.Context(), ref.Namespace, ref.Pod)
			if err != nil {
				problems = append(problems, toastKey(ref)+": "+err.Error())
				continue
			}
			before := len(targets)
			for _, name := range runningPodContainers(pod) {
				if ref.Container != "" && ref.Container != name {
					continue
				}
				target := ref
				target.Container = name
				if seen[toastKey(target)] {
					continue
				}
				seen[toastKey(target)] = true
				entry := ephemeralBatchTarget{Ref: target, UID: pod.UID}
				if ec, ok := kube.RunningPodToast(pod, name); ok {
					entry.Agent, entry.Image = ec.Name, ec.Image
				}
				targets = append(targets, entry)
			}
			if len(targets) == before && ref.Container == "" && !preset.PodToast {
				// Network-only agents also work when no target process is running.
				targets = append(targets, ephemeralBatchTarget{Ref: ref, UID: pod.UID})
			} else if len(targets) == before {
				problems = append(problems, toastKey(ref)+": no new running target containers")
			}
		}
		return func() {
			if p.Kube == k {
				a.showBatchEphemeralForm(p, preset, targets, problems)
			}
		}, nil, nil
	})
}
func (a *App) showBatchEphemeralForm(p *Panel, preset EphemeralPreset, targets []ephemeralBatchTarget, problems []string) {
	if len(targets) == 0 {
		a.showText("No running target containers", strings.Join(problems, "\n"))
		return
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
		{Label: "Pod network only (one per selected pod)", Toggle: true},
		{Label: "Command as JSON argv (blank = image entrypoint)", Value: command},
		{Label: "Access", Toggle: true, Options: []string{"Default", "Restricted", "Network", "Processes", "Files and shell", "Privileged"}, Value: o.Profile},
		{Label: "Run as UID (blank = image / pod default)", Value: uid},
		{Label: "Pull image", Toggle: true, Options: []string{"IfNotPresent", "Always", "Never"}, Value: o.PullPolicy},
		{Label: "Startup timeout, seconds (5–600)", Value: strconv.Itoa(o.TimeoutSeconds)},
		{Label: "Interactive TTY", Toggle: true, Checked: o.TTY},
		{Label: "Read-only debug container root", Toggle: true, Checked: o.ReadOnlyRoot},
	}
	if preset.PodToast {
		fields = []Field{{Label: "PodToast image", Value: o.Image}, {Label: "Pull image", Toggle: true, Options: []string{"IfNotPresent", "Always", "Never"}, Value: o.PullPolicy}, {Label: "Startup timeout, seconds (5–600)", Value: strconv.Itoa(o.TimeoutSeconds)}}
	}
	offset := len(fields)
	for _, target := range targets {
		label := toastKey(target.Ref)
		if target.Image != "" {
			label += " · " + target.Image
		}
		fields = append(fields, Field{Label: label, Toggle: true, Checked: true})
	}
	k := p.Kube
	title := "Attach ephemeral containers"
	if preset.PodToast {
		title = "PodToast · selected targets"
	}
	a.form(title, fields, func(f []Field) {
		next := kube.EphemeralOptions{Image: strings.TrimSpace(f[0].Value)}
		network := false
		if preset.PodToast {
			next.PullPolicy = f[1].Value
			next.TimeoutSeconds, _ = strconv.Atoi(f[2].Value)
		} else {
			network = f[1].Checked
			next.Profile = f[3].Value
			next.PullPolicy = f[5].Value
			next.TimeoutSeconds, _ = strconv.Atoi(f[6].Value)
			next.TTY = f[7].Checked
			next.ReadOnlyRoot = f[8].Checked
			if raw := strings.TrimSpace(f[2].Value); raw != "" {
				if err := json.Unmarshal([]byte(raw), &next.Command); err != nil || next.Command == nil {
					a.reopenForm(f, 2, "Use a JSON argv array")
					return
				}
			}
			if raw := strings.TrimSpace(f[4].Value); raw != "" {
				v, err := strconv.ParseInt(raw, 10, 64)
				if err != nil {
					a.reopenForm(f, 4, "Invalid UID")
					return
				}
				next.RunAsUser = &v
			}
		}
		if next.Image == "" || next.TimeoutSeconds < 5 || next.TimeoutSeconds > 600 {
			a.reopenForm(f, 0, "Enter an image and a startup timeout of 5–600 seconds")
			return
		}
		if !preset.PodToast {
			if err := next.Validate(); err != nil {
				a.reopenForm(f, 0, err.Error())
				return
			}
		}
		var selected []ephemeralBatchTarget
		seen := map[string]bool{}
		for i, target := range targets {
			if f[offset+i].Checked {
				if network {
					target.Ref.Container = ""
				}
				key := toastKey(target.Ref)
				if !seen[key] {
					selected = append(selected, target)
					seen[key] = true
				}
			}
		}
		if len(selected) == 0 {
			a.reopenForm(f, offset, "Select at least one target")
			return
		}
		a.runEphemeralBatch(p, k, EphemeralPreset{PodToast: preset.PodToast, Options: next}, selected)
	})
	a.Form.Primary = "Apply to selected"
	a.Form.Message = "Esc stops remaining work; containers already created remain. No shell opens automatically."
	if preset.PodToast {
		a.Form.Message += " Existing matching agents are reused; replacements become active only after connection succeeds. Root filesystem/ptrace access is used."
	}
	if len(problems) > 0 {
		a.Form.Message += "\nUnavailable targets:\n" + strings.Join(problems, "\n")
	}
}
func (a *App) runEphemeralBatch(p *Panel, k *kubePanel, preset EphemeralPreset, targets []ephemeralBatchTarget) {
	receipt := p.selectionReceipt()
	markedTargets := map[string][]string{}
	level := p.kubeLevel()
	for name := range receipt.marks {
		for _, entry := range p.Entries {
			if entry.Name != name {
				continue
			}
			for _, target := range targets {
				ref := target.Ref
				ke, ok := entry.Info.(*kubeEntry)
				if ok && isPodObject(ke.object) && ke.object.Namespace == ref.Namespace && ke.object.Name == ref.Pod || level.Level == kubeContainers && containerName(name) == ref.Container && level.Namespace == ref.Namespace && level.Object.Name == ref.Pod {
					markedTargets[name] = append(markedTargets[name], toastKey(ref))
				}
			}
		}
	}
	a.backgroundTask("Applying to selected targets", func(op *fsops.Operation) (func(), func(), error) {
		a.operationQuestions(op)
		for _, target := range targets {
			op.Add(toastKey(target.Ref), 0)
		}
		op.Ready()
		succeeded := map[string]bool{}
		completed := 0
		changed := map[string]kube.ContainerRef{}
		type retirement struct {
			ref              kube.ContainerRef
			old, replacement string
		}
		retirements := map[string]retirement{}
		for _, target := range targets {
			if op.Check() != nil {
				break
			}
			ref := target.Ref
			key := toastKey(ref)
			op.Begin(key, 0)
			err := op.Attempt("attach ephemeral container", key, "", func() error {
				pod, err := k.client.Pod(op.Context(), ref.Namespace, ref.Pod)
				if err != nil {
					return err
				}
				if pod.UID != target.UID {
					return fmt.Errorf("pod was replaced; select it again")
				}
				changed[ref.Namespace+"/"+ref.Pod] = ref
				if preset.PodToast {
					oldAgent := ""
					if ec, ok := kube.RunningPodToast(pod, ref.Container); ok {
						oldAgent = ec.Name
					}
					client, err := k.client.StartPodToastOptions(op.Context(), ref, preset.Options.Image, preset.Options.PullPolicy, preset.Options.TimeoutSeconds)
					if err != nil {
						return err
					}
					k.mu.Lock()
					if k.closed {
						k.mu.Unlock()
						client.Close()
						return fmt.Errorf("connection closed")
					}
					if k.toasts == nil {
						k.toasts = map[string]*podtoast.Client{}
					}
					old := k.toasts[key]
					k.toasts[key] = client
					k.mu.Unlock()
					if old != nil && old != client {
						old.Drain()
					}
					if shell, e := k.client.ResolveDebugShell(op.Context(), ref, ""); e == nil {
						k.setDebugShell(shell)
					}
					if oldAgent != "" && oldAgent != client.AgentName {
						retirements[ref.Namespace+"/"+ref.Pod+"/"+oldAgent] = retirement{ref, oldAgent, client.AgentName}
					}
				} else {
					started, err := k.client.StartEphemeral(op.Context(), ref, preset.Options)
					if err != nil {
						return err
					}
					if ref.Container != "" {
						if shell, e := k.client.ResolveDebugShell(op.Context(), ref, started.Container); e == nil {
							k.setDebugShell(shell)
						}
					}
				}
				return nil
			})
			if errors.Is(err, fsops.ErrCancelled) {
				op.Cancel()
				break
			}
			if err == nil {
				completed++
				succeeded[key] = true
			}
			op.Complete(key)
		}
		// Switch every selected session before stopping a shared agent. If a
		// skipped/failed session still uses it, retain it rather than break that session.
		for _, r := range retirements {
			if op.Check() != nil {
				break
			}
			inUse := false
			k.mu.Lock()
			for key, c := range k.toasts {
				if strings.HasPrefix(key, r.ref.Namespace+"/"+r.ref.Pod+"/") && c.AgentName == r.old {
					inUse = true
					break
				}
			}
			k.mu.Unlock()
			if inUse {
				continue
			}
			err := op.Attempt("retire superseded PodToast", r.old, "", func() error {
				return k.client.RetireSupersededPodToast(op.Context(), r.ref, []string{r.old}, r.replacement)
			})
			if err != nil {
				for _, target := range targets {
					if target.Ref.Namespace == r.ref.Namespace && target.Ref.Pod == r.ref.Pod {
						delete(succeeded, toastKey(target.Ref))
					}
				}
			}
			if errors.Is(err, fsops.ErrCancelled) {
				op.Cancel()
				break
			}
		}
		return func() {
			if p.Kube == k {
				for _, ref := range changed {
					a.refreshEphemeralPod(k, ref, false)
				}
			}
			if completed > 0 {
				a.rememberEphemeral(preset)
			}
			for name := range receipt.marks {
				keys := markedTargets[name]
				all := len(keys) > 0
				for _, key := range keys {
					if !succeeded[key] {
						all = false
					}
				}
				if !all {
					delete(receipt.marks, name)
				}
			}
			receipt.complete()
		}, nil, nil
	})
	if a.task != nil {
		a.task.applyCompleted = true
	}
}
