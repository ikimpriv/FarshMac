package ui

import (
	"farshmac/internal/aispaces"
	"farshmac/internal/kube"
	"os"
	"path/filepath"
	"strings"
)

func (a *App) createAIEnv(store *aispaces.Store, space string, index int) {
	kube.EnsureLoginPath("")
	agents := aispaces.Discover()
	if len(agents) == 0 {
		a.showText("Create AI Env", "Install Claude Code or Codex CLI, then create an Env. No supported CLI was found on PATH.")
		return
	}
	roles, err := store.Roles()
	if err != nil {
		a.error(err)
		return
	}
	names := []string{}
	for _, agent := range agents {
		names = append(names, agent.Name)
	}
	roleNames := []string{"No role"}
	for _, role := range roles {
		roleNames = append(roleNames, role.Name)
	}
	roleNames = append(roleNames, "New role…")
	selectedRole := ""
	a.form("Create AI Env in "+space, []Field{
		{Label: "Env name"},
		{Label: "Agent", Combo: true, Toggle: true, Options: names, Value: names[0]},
		{Label: "Role", Combo: true, Toggle: true, Options: roleNames, Value: roleNames[0]},
		{Label: "Connect team roles (Architect directs Coders and Reviewers)", Toggle: true},
	}, func(f []Field) {
		var agent aispaces.Agent
		for _, v := range agents {
			if v.Name == f[1].Value {
				agent = v
			}
		}
		name := strings.TrimSpace(f[0].Value)
		if err := store.CreateRoleEnv(space, name, agent, selectedRole, f[3].Checked); err != nil {
			a.reopenForm(f, 0, err.Error())
			return
		}
		a.refresh()
		a.Panels[index].pendingPosition(name, "")
	})
	parent := a.Form
	parent.Primary = "Create"
	parent.Message = "Choose a role from the list, or New role… to write your own prompt. Instructions are copied into CLAUDE.md or AGENTS.md."
	parent.Fields[2].OnSelect = func(i int) {
		if i == len(roles)+1 {
			previous := "No role"
			for _, role := range roles {
				if role.ID == selectedRole {
					previous = role.Name
				}
			}
			parent.Fields[2].Value = previous
			a.createAIRole(store, func(role *aispaces.Role) {
				if role != nil {
					roles = append(roles, *role)
					options := append([]string(nil), parent.Fields[2].Options[:len(parent.Fields[2].Options)-1]...)
					parent.Fields[2].Options = append(options, role.Name, "New role…")
					parent.Fields[2].Value, selectedRole = role.Name, role.ID
					a.Status = "Role template saved: " + role.Name
				} else {
					a.Status = "Role creation cancelled"
				}
				a.Form = parent
			})
			return
		}
		selectedRole = ""
		if i > 0 {
			selectedRole = roles[i-1].ID
		}
	}
}

// Custom prompts use the normal multiline editor and its save/discard flow.
// The draft is private and temporary; cancelling cannot create an empty role.
func (a *App) createAIRole(store *aispaces.Store, done func(*aispaces.Role)) {
	var askName func(string, string, string)
	askName = func(name, prompt, message string) {
		a.form("New role", []Field{{Label: "Role name", Value: name}}, func(fields []Field) {
			name = strings.TrimSpace(fields[0].Value)
			if name == "" || len([]rune(name)) > 80 || strings.ContainsAny(name, "\r\n\x00") {
				a.reopenForm(fields, 0, "Enter a role name of 1 to 80 characters")
				return
			}
			roles, err := store.Roles()
			if err != nil {
				a.reopenForm(fields, 0, err.Error())
				return
			}
			for _, role := range roles {
				if strings.EqualFold(role.Name, name) {
					a.reopenForm(fields, 0, "Role already exists")
					return
				}
			}
			dir, err := os.MkdirTemp("", "farshmac-role-")
			if err != nil {
				a.reopenForm(fields, 0, err.Error())
				return
			}
			d := newDocument(filepath.Join(dir, "Role prompt.md"), prompt)
			d.Edit, d.TempDir = true, dir
			d.markSaved()
			d.onClose = func() {
				data, err := os.ReadFile(d.Path)
				if os.IsNotExist(err) {
					done(nil)
					return
				}
				var role aispaces.Role
				if err == nil {
					role, err = store.CreateRole(name, string(data))
				}
				if err != nil {
					askName(name, string(data), err.Error())
					return
				}
				done(&role)
			}
			a.showDocument(d)
			a.Status = "Role: " + name + " · Write instructions · F2 Save · Esc return to setup"
		})
		a.Form.Primary = "Edit prompt"
		a.Form.Message = message
		if message == "" {
			a.Form.Message = "Write the prompt in the editor, save with F2, then press Esc. The role will be available in future Envs."
		}
		a.Status = a.Form.Message
		a.Form.Cancel = func() { done(nil) }
	}
	askName("", "", "")
}

func (a *App) aiSettings() {
	store, err := aispaces.Shared()
	if err != nil {
		a.error(err)
		return
	}
	a.choose("AI Spaces settings", []string{"Role templates…", "Agent defaults…"}, func(i int) {
		if i == 0 {
			a.aiRoleSettings(store)
			return
		}
		a.choose("Agent defaults", aispaces.Adapters, func(i int) {
			name := aispaces.Adapters[i]
			settings, err := store.AgentDefaults(name)
			if err != nil {
				a.error(err)
				return
			}
			a.aiAgentSettingsForm("Defaults for "+name, name, settings, func(next aispaces.AgentSettings) error { return store.SaveAgentDefaults(name, next) })
		})
	})
}
func (a *App) aiRoleSettings(store *aispaces.Store) {
	roles, err := store.Roles()
	if err != nil {
		a.error(err)
		return
	}
	labels := []string{"Add role…"}
	for _, role := range roles {
		labels = append(labels, role.Name)
	}
	a.choose("Role templates", labels, func(i int) {
		if i == 0 {
			a.createAIRole(store, func(*aispaces.Role) { a.aiRoleSettings(store) })
			return
		}
		role := roles[i-1]
		a.choose(role.Name, []string{"Edit instructions…", "Rename…", "Delete template…"}, func(i int) {
			switch i {
			case 0:
				file, err := store.RolePath(role.ID)
				if err != nil {
					a.error(err)
					return
				}
				a.openDocumentAsync(file, true, nil)
			case 1:
				a.prompt("Rename role", "Role name", role.Name, func(name string) {
					if err := store.SaveRole(role.ID, strings.TrimSpace(name)); err != nil {
						a.error(err)
						return
					}
					a.aiRoleSettings(store)
				})
			case 2:
				a.confirm("Delete role template", "Existing Envs keep their instructions.", []string{"Delete", "Cancel"}, func(i int) {
					if i == 0 {
						a.error(store.DeleteRole(role.ID))
						a.aiRoleSettings(store)
					}
				})
			}
		})
	})
}
func (a *App) aiAgentSettingsForm(title, name string, current aispaces.AgentSettings, save func(aispaces.AgentSettings) error) {
	models := []string{""}
	for _, m := range aispaces.ModelCatalog(name) {
		models = append(models, m.ID)
	}
	efforts := append([]string{""}, aispaces.EffortChoices(name)...)
	fields := []Field{{Label: "Model (blank: CLI default)", Value: current.Model, Combo: true, Options: models}, {Label: "Effort", Value: current.Effort, Combo: true, Options: efforts}}
	toolsIndex := -1
	if name == "claude" {
		toolsIndex = len(fields)
		fields = append(fields, Field{Label: "Built-in tools (blank: recommended)", Value: current.Tools})
	} else if name == "codex" {
		toolsIndex = len(fields)
		fields = append(fields, Field{Label: "Host integrations (blank: off, default: inherit)", Value: current.Tools, Combo: true, Options: []string{"", "default"}})
	}
	first := len(fields)
	for _, level := range aispaces.Levels {
		profile, ok := current.Levels[level]
		if !ok {
			profile = aispaces.DefaultProfile(name, level)
		}
		fields = append(fields, Field{Label: level + " task: model", Value: profile.Model, Combo: true, Options: models}, Field{Label: level + " task: effort", Value: profile.Effort, Combo: true, Options: efforts})
	}
	a.form(title, fields, func(f []Field) {
		next := aispaces.AgentSettings{Model: strings.TrimSpace(f[0].Value), Effort: strings.TrimSpace(f[1].Value), Tools: current.Tools, Levels: map[string]aispaces.Profile{}}
		if toolsIndex >= 0 {
			next.Tools = strings.TrimSpace(f[toolsIndex].Value)
		}
		for i, level := range aispaces.Levels {
			next.Levels[level] = aispaces.Profile{Model: strings.TrimSpace(f[first+2*i].Value), Effort: strings.TrimSpace(f[first+2*i+1].Value)}
		}
		if err := save(next); err != nil {
			a.reopenForm(f, 0, err.Error())
			return
		}
		a.Status = "Agent settings saved; running sessions keep their current settings"
	})
	a.Form.Message = "Model suggestions come from the CLI catalog; you may type a custom ID. Profiles apply to directed tasks. Global defaults are copied into new Envs; existing Envs keep their settings."
}
