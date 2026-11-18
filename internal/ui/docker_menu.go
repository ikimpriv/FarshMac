package ui

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"farshmac/internal/docker"
	"farshmac/internal/fsops"
)

func dockerStateActions(state string) []string {
	switch state {
	case "running":
		return []string{"stop", "restart", "pause"}
	case "paused":
		return []string{"unpause", "stop"}
	case "created", "exited":
		return []string{"start"}
	case "restarting":
		return []string{"stop"}
	}
	return nil
}
func dockerActionLabel(s string) string { return strings.ToUpper(s[:1]) + s[1:] }
func (a *App) dockerConfirm(p *Panel, title string, args []string) {
	client := p.Docker.client
	a.confirm("Docker", title+"?", []string{"Continue", "Cancel"}, func(i int) {
		if i == 0 {
			a.workProgress(title, func(o *fsops.Operation) error { _, err := client.Run(o.Context(), args...); return err })
		}
	})
}
func (a *App) dockerSignal(p *Panel, c docker.Container) {
	a.choose("Send signal to "+c.Names, []string{"TERM — request termination", "HUP — hangup / reload if supported", "INT — interrupt", "USR1 — application-defined", "USR2 — application-defined", "KILL — immediate termination"}, func(i int) {
		signal := []string{"TERM", "HUP", "INT", "USR1", "USR2", "KILL"}[i]
		a.dockerConfirm(p, "Send "+signal+" to "+c.Names, []string{"kill", "--signal", signal, c.ID})
	})
}
func dockerUpdateArgs(id, cpu, memory, restart string) ([]string, error) {
	args := []string{"update"}
	if cpu = strings.TrimSpace(cpu); cpu != "" {
		n, err := strconv.ParseFloat(cpu, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return nil, fmt.Errorf("CPU must be a nonnegative number of cores")
		}
		args = append(args, "--cpus", cpu)
	}
	if memory = strings.TrimSpace(memory); memory != "" {
		args = append(args, "--memory", memory)
	}
	if restart != "Keep unchanged" {
		switch restart {
		case "no", "always", "unless-stopped", "on-failure":
			args = append(args, "--restart", restart)
		default:
			return nil, fmt.Errorf("invalid restart policy")
		}
	}
	if len(args) == 1 {
		return nil, fmt.Errorf("choose at least one setting to change")
	}
	return append(args, id), nil
}
func (a *App) dockerUpdateContainer(p *Panel, c docker.Container) {
	a.form("Update "+c.Names, []Field{{Label: "CPU cores (blank: unchanged; 0: unlimited)"}, {Label: "Memory limit (e.g. 512m; blank: unchanged)"}, {Label: "Restart policy", Value: "Keep unchanged", Toggle: true, Options: []string{"Keep unchanged", "no", "always", "unless-stopped", "on-failure"}}}, func(f []Field) {
		args, err := dockerUpdateArgs(c.ID, f[0].Value, f[1].Value, f[2].Value)
		if err != nil {
			a.error(err)
			return
		}
		a.dockerConfirm(p, "Update limits/policy for "+c.Names, args)
	})
}
func (a *App) dockerObjectItems(p *Panel) ([]menuItem, bool) {
	if len(p.Entries) == 0 || p.Entries[p.Cursor].Name == ".." {
		return nil, true
	}
	name := path.Base(p.Current())
	switch p.Path {
	case "/Images":
		return []menuItem{
			{"Layer history", "", func() { a.dockerOutput(p, "Image layer history", "image", "history", "--no-trunc", name) }},
			{"Tag image…", "", func() {
				a.prompt("Tag image", "Repository:tag", "", func(tag string) {
					if strings.TrimSpace(tag) != "" {
						a.dockerConfirm(p, "Tag image as "+tag, []string{"image", "tag", name, tag})
					}
				})
			}},
			{"Save image to TAR…", "", func() { a.dockerExport(p, name+".tar", []string{"image", "save", name}, "") }},
			{"Run container…", "", func() { a.dockerRunImage(p, name) }},
		}, true
	case "/Networks":
		if name == "host" || name == "none" {
			return []menuItem{{"Connected containers", "", func() { a.dockerNetworkMembers(p, name, false) }}}, true
		}
		return []menuItem{
			{"Connected containers", "", func() { a.dockerNetworkMembers(p, name, false) }},
			{"Connect container…", "", func() { a.dockerNetworkConnect(p, name) }},
			{"Disconnect container…", "", func() { a.dockerNetworkMembers(p, name, true) }},
		}, true
	case "/Volumes":
		return []menuItem{
			{"Containers using this volume", "", func() {
				a.dockerOutput(p, "Volume consumers", "ps", "--all", "--filter", "volume="+name, "--format", "table {{.Names}}\t{{.State}}\t{{.Image}}")
			}},
			{"Back up volume…", "", func() { a.dockerExport(p, name+".tar.gz", nil, name) }},
			{"Attach volume toolbox…", "", func() { a.attachDockerVolume(p, name) }},
		}, true
	case "/Compose":
		return a.dockerComposeItems(p, name), true
	}
	return nil, false
}
func (a *App) dockerRunImage(p *Panel, image string) {
	a.form("Run image "+image, []Field{{Label: "Container name (optional)"}, {Label: "Published ports (comma-separated, e.g. 127.0.0.1:8080:80)"}, {Label: "Command as JSON array (blank: image default)"}, {Label: "Restart policy", Value: "no", Toggle: true, Options: []string{"no", "always", "unless-stopped", "on-failure"}}}, func(f []Field) {
		args, err := dockerRunArgs(image, f)
		if err != nil {
			a.error(err)
			return
		}
		a.dockerConfirm(p, "Create and start container from "+image, args)
	})
}
func dockerRunArgs(image string, f []Field) ([]string, error) {
	args := []string{"run", "--detach", "--restart", f[3].Value}
	if name := strings.TrimSpace(f[0].Value); name != "" {
		args = append(args, "--name", name)
	}
	for _, port := range strings.Split(f[1].Value, ",") {
		if port = strings.TrimSpace(port); port != "" {
			args = append(args, "--publish", port)
		}
	}
	args = append(args, image)
	if text := strings.TrimSpace(f[2].Value); text != "" {
		var command []string
		if err := json.Unmarshal([]byte(text), &command); err != nil {
			return nil, fmt.Errorf("command must be a JSON array of strings")
		}
		args = append(args, command...)
	}
	return args, nil
}
func (a *App) dockerNetworkConnect(p *Panel, network string) {
	client := p.Docker.client
	a.backgroundTask("Available containers", func(o *fsops.Operation) (func(), func(), error) {
		members, err := dockerNetworkMembers(o.Context(), client, network)
		if err != nil {
			return nil, nil, err
		}
		all, err := client.Containers(o.Context())
		if err != nil {
			return nil, nil, err
		}
		var ids, names []string
		for _, c := range all {
			if _, ok := members[c.ID]; !ok && c.State != "dead" && c.State != "removing" {
				ids = append(ids, c.ID)
				names = append(names, c.Names+" · "+c.State)
			}
		}
		return func() {
			if len(ids) == 0 {
				a.Status = "No containers available to connect"
				return
			}
			a.choose("Connect to "+network, names, func(i int) {
				a.dockerConfirm(p, "Connect "+names[i]+" to "+network, []string{"network", "connect", network, ids[i]})
			})
		}, nil, nil
	})
}
func dockerNetworkMembers(ctx context.Context, client docker.Client, network string) (map[string]struct{ Name, IPv4Address, IPv6Address string }, error) {
	b, err := client.Run(ctx, "network", "inspect", "--format", "{{json .Containers}}", network)
	if err != nil {
		return nil, err
	}
	var members map[string]struct{ Name, IPv4Address, IPv6Address string }
	err = json.Unmarshal(b, &members)
	return members, err
}
func (a *App) dockerNetworkMembers(p *Panel, network string, disconnect bool) {
	client := p.Docker.client
	a.backgroundTask("Network containers", func(o *fsops.Operation) (func(), func(), error) {
		members, err := dockerNetworkMembers(o.Context(), client, network)
		if err != nil {
			return nil, nil, err
		}
		var ids []string
		for id := range members {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		var names []string
		for _, id := range ids {
			m := members[id]
			names = append(names, strings.TrimSpace(m.Name+"  "+m.IPv4Address+"  "+m.IPv6Address))
		}
		return func() {
			if !disconnect {
				a.showText("Containers on "+network, strings.Join(names, "\n"))
				return
			}
			if len(ids) == 0 {
				a.Status = "No connected containers"
				return
			}
			a.choose("Disconnect from "+network, names, func(i int) {
				a.dockerConfirm(p, "Disconnect "+names[i]+" from "+network, []string{"network", "disconnect", network, ids[i]})
			})
		}, nil, nil
	})
}

// Archives stream to a private sibling file; a successful hard link publishes
// them without ever overwriting an existing destination or leaving a partial file.
func (a *App) dockerExport(p *Panel, name string, args []string, volume string) {
	fields := []Field{{Label: "Local output path (must not exist)", Value: filepath.Join(os.TempDir(), name)}}
	if volume != "" {
		fields = append(fields, Field{Label: "Toolbox image (requires tar; mounts volume read-only)", Value: defaultPodToastImage})
	}
	client := p.Docker.client
	a.form("Export Docker archive", fields, func(f []Field) {
		dest := expand(f[0].Value, os.TempDir())
		image := ""
		if volume != "" {
			image = strings.TrimSpace(f[1].Value)
			if image == "" {
				a.error(fmt.Errorf("toolbox image is required"))
				return
			}
		}
		a.workProgress("Exporting Docker archive", func(o *fsops.Operation) error { return dockerExportArchive(client, o, dest, args, volume, image) })
	})
}

func (a *App) dockerComposeItems(p *Panel, project string) []menuItem {
	d := p.Docker
	d.mu.Lock()
	var containers []docker.Container
	for _, c := range d.containers {
		if c.Label("com.docker.compose.project") == project {
			containers = append(containers, c)
		}
	}
	d.mu.Unlock()
	items := []menuItem{{"Service logs", "", func() {
		var names []string
		for _, c := range containers {
			names = append(names, c.Names)
		}
		if len(names) > 0 {
			a.choose("Service logs", names, func(i int) { a.dockerOutput(p, "Logs · "+names[i], "logs", "--tail", "2000", containers[i].ID) })
		}
	}}}
	for _, verb := range []string{"start", "stop", "restart", "pause", "unpause"} {
		var ids []string
		for _, c := range containers {
			for _, allowed := range dockerStateActions(c.State) {
				if verb == allowed {
					ids = append(ids, c.ID)
					break
				}
			}
		}
		if len(ids) > 0 {
			args := append([]string{verb}, ids...)
			items = append(items, menuItem{dockerActionLabel(verb) + " services…", "", func() {
				a.dockerConfirm(p, fmt.Sprintf("%s %d services in %s", dockerActionLabel(verb), len(ids), project), args)
			}})
		}
	}
	if len(containers) > 0 {
		c := containers[0]
		if c.Label("com.docker.compose.project.config_files") != "" {
			items = append(items, menuItem{"Recreate from Compose configuration…", "", func() { a.dockerComposeRecreate(p, project, c) }})
		}
	}
	return items
}
func (a *App) dockerComposeRecreate(p *Panel, project string, c docker.Container) {
	client := p.Docker.client
	a.backgroundTask("Checking Compose configuration", func(o *fsops.Operation) (func(), func(), error) {
		args, configPaths, err := dockerComposeRecreateArgs(o, client, project, c)
		if err != nil {
			return nil, nil, err
		}
		return func() {
			a.dockerConfirm(p, "Recreate project "+project+" (services will be replaced)\nConfiguration:\n"+strings.Join(configPaths, "\n"), args)
		}, nil, nil
	})
}

func dockerExportArchive(client docker.Client, o *fsops.Operation, dest string, args []string, volume, image string) (result error) {
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("destination already exists: %s", dest)
	} else if !os.IsNotExist(err) {
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(dest), ".farshmac-export-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	command := append([]string(nil), args...)
	helper := ""
	created := false
	if volume != "" {
		if _, err = client.Run(o.Context(), "volume", "inspect", volume); err != nil {
			return err
		}
		helper = "farshmac-backup-" + strings.ToLower(rand.Text())
		// Register cleanup before create so cancellation cannot orphan a helper.
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_, cleanupErr := client.Run(ctx, "rm", "--force", helper)
			if created && cleanupErr != nil {
				result = errors.Join(result, fmt.Errorf("remove temporary backup container %s: %w", helper, cleanupErr))
			}
		}()
		_, err = client.Run(o.Context(), "create", "--name", helper, "--label", "io.farshmac.backup=true", "--network=none", "--entrypoint", "tar", "--mount", "type=volume,source="+volume+",target=/volume,readonly", image, "-C", "/volume", "-czf", "-", ".")
		if err != nil {
			return err
		}
		created = true
		command = []string{"start", "--attach", helper}
	}
	o.Ready()
	o.Begin(dest, 0)
	if err = client.RunTo(o.Context(), dockerArchiveWriter{out, o}, command...); err != nil {
		return err
	}
	if helper != "" {
		b, e := client.Run(o.Context(), "inspect", "--format", "{{.State.ExitCode}}", helper)
		if e != nil {
			return e
		}
		if strings.TrimSpace(string(b)) != "0" {
			return fmt.Errorf("volume backup failed (exit %s)", strings.TrimSpace(string(b)))
		}
	}
	if err = out.Close(); err != nil {
		return err
	}
	if err = o.Check(); err != nil {
		return err
	}
	return os.Link(out.Name(), dest)
}

type dockerArchiveWriter struct {
	w io.Writer
	o *fsops.Operation
}

func (w dockerArchiveWriter) Write(b []byte) (int, error) {
	if err := w.o.Check(); err != nil {
		return 0, err
	}
	n, err := w.w.Write(b)
	w.o.Advance(int64(n))
	return n, err
}

func dockerComposeRecreateArgs(o *fsops.Operation, client docker.Client, project string, c docker.Container) ([]string, []string, error) {
	// These labels are paths on the creator's machine. Never read matching local
	// paths for a remote daemon without an explicit configuration selection.
	contextName := client.Context
	if contextName == "" {
		contextName = "default"
	}
	b, err := client.Run(o.Context(), "context", "inspect", contextName, "--format", "{{.Endpoints.docker.Host}}")
	if err != nil {
		return nil, nil, err
	}
	if !strings.HasPrefix(strings.TrimSpace(string(b)), "unix://") {
		return nil, nil, fmt.Errorf("Compose recreation requires a locally accessible configuration; this Docker context is remote")
	}
	labelData, err := client.Run(o.Context(), "container", "inspect", "--format", "{{json .Config.Labels}}", c.ID)
	if err != nil {
		return nil, nil, err
	}
	var labels map[string]string
	if err = json.Unmarshal(labelData, &labels); err != nil {
		return nil, nil, err
	}
	if labels["com.docker.compose.project.config_files"] == "" {
		return nil, nil, fmt.Errorf("Compose configuration location is unavailable")
	}
	args := []string{"compose", "--project-name", project}
	dir := labels["com.docker.compose.project.working_dir"]
	if dir != "" {
		args = append(args, "--project-directory", dir)
	}
	var configPaths []string
	for _, file := range strings.Split(labels["com.docker.compose.project.config_files"], ",") {
		if !filepath.IsAbs(file) {
			file = filepath.Join(dir, file)
		}
		st, e := os.Stat(file)
		if e != nil {
			return nil, nil, e
		}
		if !st.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("not a Compose file: %s", file)
		}
		configPaths = append(configPaths, file)
		args = append(args, "--file", file)
	}
	if _, err = client.Run(o.Context(), append(append([]string{}, args...), "config", "--quiet")...); err != nil {
		return nil, nil, err
	}
	args = append(args, "up", "--detach", "--force-recreate", "--no-build")
	return args, configPaths, nil
}
