package ui

import (
	"encoding/json"
	"farshmac/internal/podtoast"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"farshmac/internal/docker"
	"farshmac/internal/fsops"
)

type contextJob struct {
	name string
	run  func(*fsops.Operation) ([]byte, error)
}

// Keep partial successes visible on cancellation; an individual failure must not
// prevent independent selected objects from being processed.
func (a *App) contextBatch(p *Panel, title string, jobs []contextJob, mutate bool) {
	if len(jobs) == 0 {
		return
	}
	start := func() {
		receipt := p.selectionReceipt()
		a.backgroundTask(title, func(o *fsops.Operation) (func(), func(), error) {
			var report strings.Builder
			success := 0
			completedNames := map[string]bool{}
			for _, j := range jobs {
				if o.Check() != nil {
					report.WriteString("Cancelled; remaining objects were not processed.\n")
					break
				}
				o.Add(j.name, 0)
				b, err := j.run(o)
				fmt.Fprintf(&report, "\n── %s ──\n", j.name)
				if err != nil {
					fmt.Fprintf(&report, "Error: %v\n", err)
				} else {
					success++
					completedNames[j.name] = true
					report.WriteString("Completed\n")
				}
				// Reports stay bounded even for selections containing many noisy objects.
				budget := max(0, 2*1024*1024-report.Len())
				if len(b) > min(budget, 128*1024) {
					b = b[:min(budget, 128*1024)]
					report.Write(b)
					report.WriteString("\n[output truncated]\n")
				} else {
					report.Write(b)
				}
				o.Complete(j.name)
			}
			return func() {
				if mutate {
					for name := range receipt.marks {
						if !completedNames[name] {
							delete(receipt.marks, name)
						}
					}
					receipt.complete()
					a.refresh()
				}
				a.showText(fmt.Sprintf("%s · %d/%d completed", title, success, len(jobs)), report.String())
			}, nil, nil
		})
		if a.task != nil {
			a.task.applyCompleted = true
		}
	}
	if !mutate {
		start()
		return
	}
	var names []string
	for _, j := range jobs {
		if len(names) < 12 {
			names = append(names, j.name)
		}
	}
	if len(jobs) > len(names) {
		names = append(names, fmt.Sprintf("… and %d more", len(jobs)-len(names)))
	}
	a.confirm(title, fmt.Sprintf("Apply to %d objects?\n%s", len(jobs), strings.Join(names, "\n")), []string{"Apply", "Cancel"}, func(i int) {
		if i == 0 {
			start()
		}
	})
}
func dockerCommandJob(client docker.Client, name string, args ...string) contextJob {
	return contextJob{name, func(o *fsops.Operation) ([]byte, error) { return client.Run(o.Context(), args...) }}
}
func (a *App) dockerBatchItems(p *Panel) []menuItem {
	d := p.Docker
	selected := p.Selected()
	var names []string
	for _, s := range selected {
		names = append(names, path.Base(s))
	}
	var items []menuItem
	report := func(label string, args func(string) []string) {
		items = append(items, menuItem{label, "", func() {
			var jobs []contextJob
			for _, n := range names {
				jobs = append(jobs, dockerCommandJob(d.client, n, args(n)...))
			}
			a.contextBatch(p, label, jobs, false)
		}})
	}
	switch p.Path {
	case "/Images":
		report("Layer history", func(n string) []string { return []string{"image", "history", "--no-trunc", n} })
		items = append(items, menuItem{"Save selected images to TAR…", "", func() { a.dockerExport(p, "images.tar", append([]string{"image", "save"}, names...), "") }})
		return items // Tag/run require a distinct name/configuration for each image.
	case "/Volumes":
		report("Containers using selected volumes", func(n string) []string {
			return []string{"ps", "--all", "--filter", "volume=" + n, "--format", "table {{.Names}}\t{{.State}}\t{{.Image}}"}
		})
		items = append(items, menuItem{"Back up selected volumes…", "", func() {
			a.form("Back up volumes", []Field{{Label: "Local output directory", Value: os.TempDir()}, {Label: "Toolbox image (read-only volume access)", Value: defaultPodToastImage}}, func(f []Field) {
				dir := expand(f[0].Value, os.TempDir())
				image := strings.TrimSpace(f[1].Value)
				if st, e := os.Stat(dir); e != nil || !st.IsDir() {
					a.error(fmt.Errorf("choose an existing output directory"))
					return
				}
				if image == "" {
					a.error(fmt.Errorf("toolbox image is required"))
					return
				}
				var jobs []contextJob
				for _, n := range names {
					jobs = append(jobs, contextJob{n, func(o *fsops.Operation) ([]byte, error) {
						return nil, dockerExportArchive(d.client, o, filepath.Join(dir, n+".tar.gz"), nil, n, image)
					}})
				}
				a.contextBatch(p, "Back up volumes", jobs, true)
			})
		}})
		return items // A volume toolbox opens one filesystem context.
	case "/Networks":
		report("Connected containers", func(n string) []string { return []string{"network", "inspect", "--format", "{{json .Containers}}", n} })
		for _, verb := range []string{"connect", "disconnect"} {
			var eligible []string
			for _, n := range names {
				if n != "host" && n != "none" {
					eligible = append(eligible, n)
				}
			}
			if len(eligible) == 0 {
				continue
			}
			items = append(items, menuItem{fmt.Sprintf("%s container · %d networks…", dockerActionLabel(verb), len(eligible)), "", func() { a.dockerBatchNetwork(p, eligible, verb) }})
		}
		return items
	}
	var containers []docker.Container
	seen := map[string]bool{}
	if p.Path == "/Compose" {
		d.mu.Lock()
		for _, c := range d.containers {
			for _, n := range names {
				if c.Label("com.docker.compose.project") == n && !seen[c.ID] {
					containers = append(containers, c)
					seen[c.ID] = true
				}
			}
		}
		d.mu.Unlock()
	} else {
		for _, s := range selected {
			c, prefix, _, ok := d.location(s)
			if !ok || prefix != s {
				return nil
			} // Files are not a selection of containers.
			if !seen[c.ID] {
				containers = append(containers, c)
				seen[c.ID] = true
			}
		}
	}

	if p.Path == "/Compose" {
		var representatives []docker.Container
		projects := map[string]bool{}
		for _, c := range containers {
			project := c.Label("com.docker.compose.project")
			if !projects[project] && c.Label("com.docker.compose.project.config_files") != "" {
				representatives = append(representatives, c)
				projects[project] = true
			}
		}
		if len(representatives) > 0 {
			items = append(items, menuItem{fmt.Sprintf("Recreate from Compose configuration · %d projects…", len(representatives)), "", func() {
				a.backgroundTask("Checking Compose configurations", func(o *fsops.Operation) (func(), func(), error) {
					var jobs []contextJob
					for _, c := range representatives {
						project := c.Label("com.docker.compose.project")
						args, files, err := dockerComposeRecreateArgs(o, d.client, project, c)
						if err != nil {
							return nil, nil, fmt.Errorf("%s: %w", project, err)
						}
						jobs = append(jobs, dockerCommandJob(d.client, project+" (services replaced): "+strings.Join(files, ", "), args...))
					}
					return func() { a.contextBatch(p, "Recreate Compose projects", jobs, true) }, nil, nil
				})
			}})
		}
	}
	var diagnostics []contextJob
	for _, c := range containers {
		if helper := d.helper(c.ID); helper != "" {
			diagnostics = append(diagnostics, contextJob{c.Names, func(o *fsops.Operation) ([]byte, error) {
				agent, err := d.client.Agent(o.Context(), helper, c.ID)
				if err != nil {
					return nil, err
				}
				defer agent.Close()
				var snapshot podtoast.Snapshot
				if err = agent.JSON(o.Context(), "GET", "/v1/snapshot", nil, &snapshot); err != nil {
					return nil, err
				}
				return json.MarshalIndent(snapshot, "", "  ")
			}})
		}
	}
	if len(diagnostics) > 0 {
		items = append(items, menuItem{fmt.Sprintf("PodToast diagnostics · %d containers", len(diagnostics)), "", func() { a.contextBatch(p, "PodToast diagnostics", diagnostics, false) }})
	}
	if len(containers) == 0 {
		return nil
	}
	items = append(items, menuItem{"Live metrics…", "", func() { a.dockerMetrics(d, containers, false) }})
	for _, kind := range []string{"Processes", "Logs", "Filesystem changes", "Published ports"} {
		var jobs []contextJob
		for _, c := range containers {
			var args []string
			switch kind {
			case "Processes":
				if c.State != "running" {
					continue
				}
				args = []string{"top", c.ID, "-eo", "pid,ppid,user,%cpu,%mem,args"}
			case "Logs":
				args = []string{"logs", "--tail", "2000", c.ID}
			case "Filesystem changes":
				args = []string{"diff", c.ID}
			case "Published ports":
				args = []string{"port", c.ID}
			}
			jobs = append(jobs, dockerCommandJob(d.client, c.Names, args...))
		}
		if len(jobs) > 0 {
			items = append(items, menuItem{fmt.Sprintf("%s · %d containers", kind, len(jobs)), "", func() { a.contextBatch(p, kind, jobs, false) }})
		}
	}
	for _, verb := range []string{"start", "stop", "restart", "pause", "unpause"} {
		var jobs []contextJob
		for _, c := range containers {
			for _, v := range dockerStateActions(c.State) {
				if v == verb {
					jobs = append(jobs, dockerCommandJob(d.client, c.Names, verb, c.ID))
				}
			}
		}
		if len(jobs) > 0 {
			items = append(items, menuItem{fmt.Sprintf("%s containers · %d/%d…", dockerActionLabel(verb), len(jobs), len(containers)), "", func() { a.contextBatch(p, dockerActionLabel(verb), jobs, true) }})
		}
	}
	items = append(items, menuItem{"Resource limits / restart policy…", "", func() {
		a.form("Update selected containers", []Field{{Label: "CPU cores (blank: unchanged; 0: unlimited)"}, {Label: "Memory limit (blank: unchanged)"}, {Label: "Restart policy", Value: "Keep unchanged", Toggle: true, Options: []string{"Keep unchanged", "no", "always", "unless-stopped", "on-failure"}}}, func(f []Field) {
			var jobs []contextJob
			for _, c := range containers {
				args, e := dockerUpdateArgs(c.ID, f[0].Value, f[1].Value, f[2].Value)
				if e != nil {
					a.error(e)
					return
				}
				jobs = append(jobs, dockerCommandJob(d.client, c.Names, args...))
			}
			a.contextBatch(p, "Update containers", jobs, true)
		})
	}})
	var signalTargets, toastTargets []docker.Container
	for _, c := range containers {
		if c.State == "running" || c.State == "paused" {
			signalTargets = append(signalTargets, c)
		}
		if c.State == "running" {
			toastTargets = append(toastTargets, c)
		}
	}
	if len(signalTargets) > 0 {
		items = append(items, menuItem{fmt.Sprintf("Send signal · %d containers…", len(signalTargets)), "", func() {
			signals := []string{"TERM", "HUP", "INT", "USR1", "USR2", "KILL"}
			a.choose("Send signal", signals, func(i int) {
				var jobs []contextJob
				for _, c := range signalTargets {
					jobs = append(jobs, dockerCommandJob(d.client, c.Names, "kill", "--signal", signals[i], c.ID))
				}
				a.contextBatch(p, "Send "+signals[i], jobs, true)
			})
		}})
	}
	if len(toastTargets) > 0 {
		items = append(items, menuItem{fmt.Sprintf("Attach / repair PodToast toolbox · %d containers…", len(toastTargets)), "", func() {
			a.prompt("Attach PodToast toolboxes", "Image (root toolbox; target PID/network and AppArmor profile; SYS_PTRACE)", defaultPodToastImage, func(image string) {
				image = strings.TrimSpace(image)
				if image == "" {
					return
				}
				var jobs []contextJob
				for _, c := range toastTargets {
					jobs = append(jobs, contextJob{c.Names, func(o *fsops.Operation) ([]byte, error) {
						if err := d.refresh(o.Context()); err != nil {
							return nil, err
						}
						id, e := d.client.EnsureToolbox(o.Context(), c.ID, image, d.helper(c.ID))
						if e == nil {
							d.mu.Lock()
							if d.helpers == nil {
								d.helpers = map[string]string{}
							}
							d.helpers[c.ID] = id
							d.mu.Unlock()
						}
						return []byte(id), e
					}})
				}
				a.contextBatch(p, "Attach PodToast toolboxes", jobs, true)
			})
		}})
	}
	return items
}
func (a *App) dockerBatchNetwork(p *Panel, networks []string, verb string) {
	client := p.Docker.client
	a.backgroundTask("Reading network membership", func(o *fsops.Operation) (func(), func(), error) {
		all, err := client.Containers(o.Context())
		if err != nil {
			return nil, nil, err
		}
		byContainer := map[string][]string{}
		for _, n := range networks {
			members, e := dockerNetworkMembers(o.Context(), client, n)
			if e != nil {
				return nil, nil, e
			}
			for _, c := range all {
				_, present := members[c.ID]
				if (verb == "disconnect" && present) || (verb == "connect" && !present && c.State != "dead" && c.State != "removing") {
					byContainer[c.ID] = append(byContainer[c.ID], n)
				}
			}
		}
		return func() {
			var labels []string
			var targets []docker.Container
			for _, c := range all {
				if len(byContainer[c.ID]) > 0 {
					targets = append(targets, c)
					labels = append(labels, fmt.Sprintf("%s · %d networks", c.Names, len(byContainer[c.ID])))
				}
			}
			if len(targets) == 0 {
				a.Status = "No eligible containers"
				return
			}
			a.choose(dockerActionLabel(verb)+" container", labels, func(i int) {
				c := targets[i]
				var jobs []contextJob
				for _, n := range byContainer[c.ID] {
					jobs = append(jobs, dockerCommandJob(client, n+" / "+c.Names, "network", verb, n, c.ID))
				}
				a.contextBatch(p, dockerActionLabel(verb)+" networks", jobs, true)
			})
		}, nil, nil
	})
}
