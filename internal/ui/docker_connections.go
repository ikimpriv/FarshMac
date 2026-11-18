package ui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"farshmac/internal/docker"
	"farshmac/internal/fsops"
	"farshmac/internal/kube"
)

func (a *App) createDockerContext(index int) {
	a.choose("Create Docker context", []string{"SSH host — address, user, port and optional key", "SSH alias — use existing SSH configuration"}, func(i int) { a.dockerContextForm(index, i == 1) })
}
func (a *App) dockerContextForm(index int, alias bool) {
	fields := []Field{{Label: "Context name"}, {Label: "Server address"}, {Label: "SSH user (blank: SSH default)"}, {Label: "SSH port (blank: SSH default)"}, {Label: "Private key (optional; F4 to choose)"}}
	if alias {
		fields = []Field{{Label: "Context name"}, {Label: "SSH alias (F4 to choose)"}}
	}
	a.form("Create Docker context", fields, func(f []Field) {
		o := docker.SSHContextOptions{Name: strings.TrimSpace(f[0].Value), Host: strings.TrimSpace(f[1].Value), Alias: alias}
		if !alias {
			o.User = strings.TrimSpace(f[2].Value)
			o.Port = strings.TrimSpace(f[3].Value)
			o.IdentityFile = strings.TrimSpace(f[4].Value)
		}
		if e := o.Validate(); e != nil {
			a.reopenForm(f, 0, e.Error())
			return
		}
		// Retain the complete form on failure, including selection callbacks.
		original := a.submitting
		a.backgroundTask("Creating Docker context", func(op *fsops.Operation) (func(), func(), error) {
			err := (docker.Client{}).CreateSSHContext(op.Context(), o)
			return func() {
				if err != nil {
					a.Form = original
					a.Form.Message = err.Error()
					return
				}
				a.Status = "Created Docker context " + o.Name
				a.dockerConnectionsSelected(index, o.Name)
			}, nil, nil
		})
		if a.task != nil {
			a.task.applyCompleted = true
		}
	})
	form := a.Form
	form.Primary = "Create"
	form.Message = "Creates a Docker context without changing the default context. SSH keys remain on this Mac. Unlock encrypted keys in ssh-agent before connecting."
	if !alias {
		form.Message += " An explicit key adds a dedicated Host entry to ~/.ssh/config."
	}
	form.OnKey = func(m Message) bool {
		if m.Key != "F4" {
			return false
		}
		if alias && form.Focus == 1 {
			home, _ := os.UserHomeDir()
			aliases := dockerSSHAliases(filepath.Join(home, ".ssh", "config"))
			if len(aliases) == 0 {
				a.Status = "No named SSH aliases found; enter an alias manually"
				return true
			}
			a.choose("SSH alias", aliases, func(i int) {
				form.Fields[1].Value = aliases[i]
				form.Cursor = utf8.RuneCountInString(aliases[i])
				a.Form = form
			})
			a.Form.Cancel = func() { a.Form = form }
			return true
		}
		if !alias && form.Focus == 4 {
			home, _ := os.UserHomeDir()
			dir := filepath.Join(home, ".ssh")
			if value := form.Fields[4].Value; value != "" {
				dir = filepath.Dir(expand(value, home))
			}
			a.dockerKeyPicker(form, dir)
			return true
		}
		return false
	}
}
func (a *App) dockerKeyPicker(form *Form, dir string) {
	a.backgroundTask("SSH key files", func(o *fsops.Operation) (func(), func(), error) {
		f, e := os.Open(dir)
		if e != nil {
			return func() { a.Form = form; a.Status = e.Error() }, nil, nil
		}
		defer f.Close()
		entries, e := f.ReadDir(2001)
		if e != nil && e != io.EOF {
			return func() { a.Form = form; a.Status = e.Error() }, nil, nil
		}
		truncated := len(entries) > 2000
		if truncated {
			entries = entries[:2000]
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].IsDir() != entries[j].IsDir() {
				return entries[i].IsDir()
			}
			return entries[i].Name() < entries[j].Name()
		})
		labels := []string{".."}
		for _, entry := range entries {
			label := entry.Name()
			if entry.IsDir() {
				label += "/"
			}
			labels = append(labels, label)
		}
		return func() {
			a.choose("SSH key · "+dir, labels, func(i int) {
				if i == 0 {
					a.dockerKeyPicker(form, filepath.Dir(dir))
					return
				}
				name := filepath.Join(dir, entries[i-1].Name())
				st, e := os.Stat(name)
				if e != nil {
					a.Form = form
					a.Status = e.Error()
					return
				}
				if st.IsDir() {
					a.dockerKeyPicker(form, name)
					return
				}
				form.Fields[4].Value = name
				form.Cursor = utf8.RuneCountInString(name)
				a.Form = form
			})
			a.Form.Cancel = func() { a.Form = form }
			if truncated {
				a.Form.Message = "First 2000 entries shown. You can enter an exact key path in the form."
			}
		}, nil, nil
	})
}

// Discovery is only a convenience: OpenSSH itself resolves aliases, includes,
// wildcard defaults and ProxyJump at connection time. Never evaluate Match exec.
func dockerSSHAliases(config string) []string {
	seen, aliases := map[string]bool{}, map[string]bool{}
	home, _ := os.UserHomeDir()
	var read func(string, int)
	read = func(name string, depth int) {
		if depth > 8 || len(seen) >= 128 || seen[name] {
			return
		}
		seen[name] = true
		f, e := os.Open(name)
		if e != nil {
			return
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, 1<<20))
		if e != nil {
			return
		}
		for _, line := range strings.Split(string(b), "\n") {
			parts := strings.Fields(strings.SplitN(line, "#", 2)[0])
			if len(parts) < 2 {
				continue
			}
			switch strings.ToLower(parts[0]) {
			case "host":
				for _, host := range parts[1:] {
					if !strings.ContainsAny(host, "*?!\"'") {
						aliases[host] = true
					}
				}
			case "include":
				for _, pattern := range parts[1:] {
					pattern = strings.Trim(pattern, "\"'")
					if strings.HasPrefix(pattern, "~/") {
						pattern = filepath.Join(home, pattern[2:])
					} else if !filepath.IsAbs(pattern) {
						pattern = filepath.Join(home, ".ssh", pattern)
					}
					matches, _ := filepath.Glob(pattern)
					for _, m := range matches {
						read(m, depth+1)
					}
				}
			}
		}
	}
	read(config, 0)
	names := make([]string, 0, len(aliases))
	for name := range aliases {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
func (a *App) dockerConnectionsSelected(index int, selected string) {
	a.backgroundTask("Docker contexts", func(o *fsops.Operation) (func(), func(), error) {
		kube.EnsureLoginPath("")
		names, err := (docker.Client{}).Names(o.Context(), "context")
		return func() {
			choices := append([]string{"Create Context…"}, names...)
			a.choose("Docker context", choices, func(i int) {
				if i == 0 {
					a.createDockerContext(index)
				} else {
					a.connectDocker(index, names[i-1])
				}
			})
			if err != nil {
				a.Form.Message = fmt.Sprintf("Cannot list Docker contexts: %v", err)
			}
			for i, n := range names {
				if n == selected {
					a.Form.Focus = i + 1
				}
			}
		}, nil, nil
	})
}
