package ui

import (
	"context"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"farshmac/internal/aispaces"
	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"farshmac/internal/remotefs"
	"farshmac/internal/shellsession"
)

// AI Spaces proof of concept (docs/AI_SPACES_ARCHITECTURE_DRAFT.md). The
// panel path is /Space/Env/virtual-path: the first two levels are the
// catalog, everything below is the Env's filesystem: referenced host folders
// (live and writable) plus files private to the Env.
type aiPanel struct{ store *aispaces.Store }

func aiLocation(p string) (space, env, prefix, rel string, level int) {
	parts := strings.Split(strings.Trim(path.Clean("/"+p), "/"), "/")
	if parts[0] == "" {
		return "", "", "", "", 0
	}
	if len(parts) == 1 {
		return parts[0], "", "", "", 1
	}
	return parts[0], parts[1], "/" + parts[0] + "/" + parts[1], "/" + strings.Join(parts[2:], "/"), 2
}

type aiOpener struct {
	store              *aispaces.Store
	space, env, prefix string
}

func (o *aiOpener) Identity() string { return "ai:" + o.store.EnvID(o.space, o.env) }
func (o *aiOpener) Open(context.Context) (remotefs.FS, error) {
	f, err := o.store.FS(o.space, o.env)
	if err != nil {
		return nil, err
	}
	return prefixFS{FS: f, prefix: o.prefix, identity: o.Identity()}, nil
}

// aiAgentInfo is the virtual executable of a configured agent in an Env root.
type aiAgentInfo struct {
	name    string
	running bool
}

func (i aiAgentInfo) Name() string       { return i.name }
func (i aiAgentInfo) Size() int64        { return 0 }
func (i aiAgentInfo) Mode() os.FileMode  { return 0755 }
func (i aiAgentInfo) ModTime() time.Time { return time.Time{} }
func (i aiAgentInfo) IsDir() bool        { return false }
func (i aiAgentInfo) Sys() any           { return nil }

func (p *Panel) aiAgent(name string) bool {
	for _, e := range p.Entries {
		if _, ok := e.Info.(aiAgentInfo); ok && e.Name == name {
			return true
		}
	}
	return false
}

// aiSelection splits the selection into agent executables and other entries.
func (p *Panel) aiSelection() (agents, others []string) {
	for _, full := range p.Selected() {
		if p.aiAgent(path.Base(full)) {
			agents = append(agents, path.Base(full))
		} else {
			others = append(others, full)
		}
	}
	return
}

type aiBackend struct{ remoteBackend }

func (aiBackend) Kind() backendKind                        { return backendAI }
func (aiBackend) Label(*Panel) string                      { return "ai:" }
func (aiBackend) SizeSource(p *Panel) string               { return "ai:" + p.Path }
func (aiBackend) HistoryEntry(p *Panel, abs string) string { return "ai:" + abs }
func (aiBackend) InfoPlugin(p *Panel) string               { return "AI Spaces: " + p.Path }
func (aiBackend) CommandPrompt(p *Panel) string            { return "ai:" + p.Path }
func (aiBackend) Caps(p *Panel) panelCaps {
	if _, _, _, _, level := aiLocation(p.Path); level == 2 {
		return remoteBackend{}.Caps(p)
	}
	return capMkdir | capDelete
}
func (aiBackend) Endpoint(p *Panel) remotefs.Endpoint {
	space, env, prefix, _, level := aiLocation(p.Path)
	if level < 2 {
		return remotefs.Endpoint{}
	}
	return remotefs.Endpoint{Opener: &aiOpener{p.AI.store, space, env, prefix}}
}
func (aiBackend) Browse(p *Panel, o *fsops.Operation) (browseStrategy, error) {
	if p.endpoint().Opener == nil {
		return nil, fmt.Errorf("enter an AI Env first")
	}
	return remoteBackend{}.Browse(p, o)
}
func (aiBackend) DocumentSource(p *Panel, name string) documentSource {
	endpoint := p.endpoint()
	if endpoint.Opener == nil {
		return nil
	}
	return &remoteSource{Opener: endpoint.Opener, Label: p.label(), Path: name}
}
func (aiBackend) Enter(a *App, p *Panel) bool {
	if len(p.Entries) == 0 {
		return true
	}
	e := p.Entries[p.Cursor]
	switch {
	case p.aiAgent(e.Name):
		a.openAIAgent(p, e.Name)
	case e.Dir:
		a.error(p.Load(p.Current()))
	case p.can(capEdit):
		a.openRemoteDocument(false)
	}
	return true
}
func (aiBackend) Open(a *App, p *Panel, edit bool) bool {
	if len(p.Entries) > 0 && p.aiAgent(p.Entries[p.Cursor].Name) {
		a.aiAgentDetails(p, p.Entries[p.Cursor].Name)
	} else if p.can(capEdit) {
		a.openRemoteDocument(edit)
	}
	return true
}
func (aiBackend) Copy(a *App, p *Panel) bool {
	if !p.can(capEdit) {
		a.Status = "Enter an AI Env before copying files"
		return true
	}
	if agents, _ := p.aiSelection(); len(agents) > 0 {
		a.Status = "Agent executables cannot be copied"
		return true
	}
	return false
}
func (aiBackend) Execute(a *App, _ *Panel, _ string) bool {
	a.Status = "Commands run in an agent console: press Enter on an agent executable"
	return true
}

// Mkdir is F7: a Space at the catalog root, an Env inside a Space, a folder
// inside an Env.
func (aiBackend) Mkdir(a *App, p *Panel) bool {
	space, _, _, _, level := aiLocation(p.Path)
	index, store := a.Active, p.AI.store
	switch level {
	case 0:
		a.prompt("Create AI Space", "Space name", "", func(name string) {
			if name = strings.TrimSpace(name); name == "" {
				return
			}
			if err := store.CreateSpace(name); err != nil {
				a.error(err)
				return
			}
			a.refresh()
			a.Panels[index].pendingPosition(name, "")
		})
	case 1:
		a.aiAgentForm("Create AI Env in "+space, true, nil, func(name string, agents []aispaces.Agent) error {
			if err := store.CreateEnv(space, name, agents); err != nil {
				return err
			}
			a.refresh()
			a.Panels[index].pendingPosition(name, "")
			return nil
		})
	default:
		a.remoteMkdir()
	}
	return true
}

// aiAgentForm discovers the installed agents and offers them as checkboxes.
func (a *App) aiAgentForm(title string, named bool, configured []aispaces.Agent, submit func(string, []aispaces.Agent) error) {
	kube.EnsureLoginPath("")
	found := map[string]aispaces.Agent{}
	for _, agent := range aispaces.Discover() {
		found[agent.Name] = agent
	}
	var fields []Field
	if named {
		fields = append(fields, Field{Label: "Env name"})
	}
	first := len(fields)
	var offered []string
	for _, name := range aispaces.Adapters {
		known := false
		for _, c := range configured {
			known = known || c.Name == name
		}
		if known {
			continue
		}
		label := name + " — not installed"
		if agent, ok := found[name]; ok {
			label = name + " — " + agent.Path
		}
		_, installed := found[name]
		fields = append(fields, Field{Label: label, Toggle: true, Checked: installed})
		offered = append(offered, name)
	}
	if len(offered) == 0 {
		a.Status = "All supported agents are already in this Env"
		return
	}
	a.form(title, fields, func(f []Field) {
		name := ""
		if named {
			if name = strings.TrimSpace(f[0].Value); name == "" {
				a.reopenForm(f, 0, "Enter an Env name")
				return
			}
		}
		var agents []aispaces.Agent
		for i, adapter := range offered {
			if !f[first+i].Checked {
				continue
			}
			agent, ok := found[adapter]
			if !ok {
				a.reopenForm(f, first+i, adapter+" is not installed on PATH")
				return
			}
			agents = append(agents, agent)
		}
		if err := submit(name, agents); err != nil {
			a.reopenForm(f, 0, err.Error())
		}
	})
	a.Form.Message = "Select one or more agents. Nothing is started until you press Enter on an agent executable."
}

// Delete is F8. Catalog objects and agents are removed as such, a reference
// is detached, and an entry inside a reference is hidden from this Env only.
func (aiBackend) Delete(a *App, p *Panel, _ bool) bool {
	space, env, _, _, level := aiLocation(p.Path)
	store := p.AI.store
	if level < 2 {
		var names []string
		for _, full := range p.Selected() {
			names = append(names, path.Base(full))
		}
		if len(names) == 0 {
			return true
		}
		what := "AI Space(s) with all their Envs"
		if level == 1 {
			what = "AI Env(s)"
		}
		a.confirm("Delete "+what, fmt.Sprintf("Delete %s: %s?\nRunning agents are stopped. The Env's private files and scratch are discarded. Referenced folders and files stay as they are.", what, strings.Join(names, ", ")), []string{"Delete", "Cancel"}, func(i int) {
			if i != 0 {
				return
			}
			for _, name := range names {
				if level == 0 {
					a.error(store.Delete(name, ""))
				} else {
					a.error(store.Delete(space, name))
				}
			}
			a.refresh()
		})
		return true
	}
	agents, others := p.aiSelection()
	if len(agents) > 0 {
		a.confirm("Remove agents", fmt.Sprintf("Remove %s from this Env?\nRunning sessions are stopped. The Env, its files and other agents stay.", strings.Join(agents, ", ")), []string{"Remove", "Cancel"}, func(i int) {
			if i != 0 {
				return
			}
			for _, name := range agents {
				a.error(store.RemoveAgent(space, env, name))
			}
			a.refresh()
		})
		return true
	}
	if len(others) == 0 {
		return true
	}
	endpoint := p.endpoint()
	a.confirm("Remove from AI Env", fmt.Sprintf("Remove %d selected item(s) from this Env?\nA reference is detached. A file or folder inside a reference is only hidden from this Env and its agents: the real file is not deleted.", len(others)), []string{"Remove", "Cancel"}, func(i int) {
		if i != 0 {
			return
		}
		a.workProgress("Removing from AI Env", func(o *fsops.Operation) error {
			f, err := endpoint.Open(o.Context())
			if err != nil {
				return err
			}
			defer f.Close()
			for _, target := range others {
				if err = f.Remove(target, true); err != nil {
					return err
				}
			}
			return nil
		})
	})
	return true
}

func (p *Panel) loadAI(target string) error {
	space, env, _, rel, level := aiLocation(target)
	store := p.AI.store
	var entries []Entry
	switch level {
	case 0:
		for _, name := range store.SpaceNames() {
			entries = append(entries, dockerCatalogEntry(name, true))
		}
	case 1:
		names, err := store.EnvNames(space)
		if err != nil {
			return err
		}
		for _, name := range names {
			entries = append(entries, dockerCatalogEntry(name, true))
		}
	default:
		copy := *p
		copy.Path = target
		f, err := copy.endpoint().Open(context.Background())
		if err != nil {
			return err
		}
		defer f.Close()
		op := p.readOperation
		if op == nil {
			op = fsops.NewOperation("Reading AI Env")
			defer op.Cancel()
		}
		list, size, err := p.listFS(f, target, op)
		if err != nil {
			return err
		}
		entries = list
		p.TotalBytes = size
		if rel == "/" {
			for _, agent := range store.Agents(space, env) {
				entries = append(entries, Entry{Name: agent.Name, Info: aiAgentInfo{agent.Name, store.Running(space, env, agent.Name) != nil}})
			}
		}
	}
	state := p.beginLoad(target)
	p.sortEntries(entries)
	p.installEntries(entries, state)
	p.ItemCount = len(entries)
	p.InfoText = "AI Spaces"
	return nil
}

// openAISpaces shows the catalog of AI Spaces in a panel (Alt+F1 / Alt+F2).
func (a *App) openAISpaces(index int) {
	store, err := aispaces.Shared()
	if err != nil {
		a.error(err)
		return
	}
	p := &a.Panels[index]
	local := p.filesystemPath()
	p.cancelPanelLoad()
	p.cancelTree()
	if p.Kube != nil && p.Kube != a.Panels[1-index].Kube {
		p.Kube.close()
	}
	p.Kube, p.Remote, p.Archive, p.Docker = nil, nil, nil, nil
	p.AI = &aiPanel{store}
	p.RemoteLocalPath = local
	p.Async = true
	p.Hidden = false
	p.View = "Full"
	p.states = nil
	p.Marked = map[string]bool{}
	a.Active = index
	a.error(p.Load("/"))
}

// aiTransfer handles the transfers whose meaning differs for AI Spaces:
// F5 from a local panel attaches references, catalog objects are renamed.
func (a *App) aiTransfer(p, other *Panel, move, local bool) bool {
	if p.AI == nil && (other.AI == nil || local) {
		return false
	}
	if p.AI != nil {
		space, _, _, _, level := aiLocation(p.Path)
		if level == 2 {
			if agents, _ := p.aiSelection(); len(agents) > 0 {
				a.Status = "Agent executables cannot be copied or moved"
				return true
			}
			if other.AI != nil && !other.can(capCopyIn) {
				a.Status = "Enter an AI Env in the destination panel first"
				return true
			}
			return false // export, copy and rename use the ordinary transfer code
		}
		if !move || !local || len(p.Entries) == 0 || p.Entries[p.Cursor].Name == ".." {
			a.Status = "Shift+F6 renames an AI Space or Env; enter an Env to work with files"
			return true
		}
		old, store := p.Entries[p.Cursor].Name, p.AI.store
		a.prompt("Rename", "New name", old, func(name string) {
			if name = strings.TrimSpace(name); name == "" || name == old {
				return
			}
			var err error
			if level == 0 {
				err = store.Rename(old, "", name)
			} else {
				err = store.Rename(space, old, name)
			}
			a.error(err)
			a.refresh()
		})
		return true
	}
	space, env, _, rel, level := aiLocation(other.Path)
	switch {
	case move:
		a.Status = "Moving into an AI Env is disabled: F5 attaches a reference and leaves the source in place"
	case level < 2:
		a.Status = "Enter an AI Env in the destination panel before attaching sources"
	case p.kind() != backendLocal:
		a.Status = "Only local files and folders can be attached to an AI Env"
	default:
		paths, store := p.Selected(), other.AI.store
		if len(paths) == 0 {
			return true
		}
		receipt := p.selectionReceipt()
		a.confirm("Attach to AI Env", fmt.Sprintf("Attach %d item(s) to %s%s as references? No data is copied.\nRead/write: what an agent or you write there is saved to the real files.\nRead-only: the Env can read them but not change them.", len(paths), env, rel), []string{"Read/write", "Read-only", "Cancel"}, func(i int) {
			if i > 1 {
				return
			}
			if err := store.Attach(space, env, rel, paths, i == 1); err != nil {
				a.error(err)
				return
			}
			receipt.complete()
			a.Status = fmt.Sprintf("Attached %d reference(s) to %s (%s)", len(paths), env, []string{"read/write", "read-only"}[i])
			a.refresh()
		})
	}
	return true
}

func aiConsoleKey(envID, agent string) string { return "ai:" + envID + ":" + agent }

// openAIAgent is Enter on an agent executable: reveal its retained console,
// starting (or resuming) the agent only when it is not running.
func (a *App) openAIAgent(p *Panel, name string) {
	space, env, _, _, _ := aiLocation(p.Path)
	store := p.AI.store
	envID := store.EnvID(space, env)
	key := aiConsoleKey(envID, name)
	a.activateTerminal(key)
	if a.aiConsoles == nil {
		a.aiConsoles = map[string]string{}
	}
	a.aiConsoles[envID] = key
	a.TermVisible, a.dirty = true, true
	if a.Foreign != nil {
		return
	}
	a.foreignEnded = false
	kube.EnsureLoginPath("")
	// An agent started to receive a message is already running without a console.
	process := store.Running(space, env, name)
	if process == nil {
		var err error
		if process, err = store.Launch(space, env, name, a.Cols, max(1, a.Rows-1)); err != nil {
			a.pending = append(a.pending, "\r\n"+err.Error()+"\r\n"...)
			a.error(err)
			return
		}
	} else {
		process.Resize(a.Cols-1, max(1, a.Rows-1)) // a size change makes the CLI redraw its screen
	}
	if !process.Attach() {
		a.Status = name + " is open in another window"
		a.pending = append(a.pending, "\r\n"+a.Status+"\r\n"...)
		return
	}
	a.installForeign(process, name+" · "+env, a.refresh)
	a.Status = name + " · " + env + " · ⌘O panels, the agent keeps running"
}

// showAITerminal is Cmd+O on an AI panel: the last console of this Env, never
// the local shell and never another Env's console.
func (a *App) showAITerminal() bool {
	p := &a.Panels[a.Active]
	if p.AI == nil {
		return false
	}
	space, env, _, _, level := aiLocation(p.Path)
	key := ""
	if level == 2 {
		key = a.aiConsoles[p.AI.store.EnvID(space, env)]
	}
	if key == "" {
		a.Status = "No agent console here yet: press Enter on an agent executable in an Env"
		return true
	}
	a.activateTerminal(key)
	a.TermVisible, a.dirty = true, true
	return true
}

func (a *App) aiShellContext(p *Panel) shellsession.Context {
	space, env, _, _, level := aiLocation(p.Path)
	if level < 2 {
		return shellsession.Context{Kind: "ai", ID: "ai:catalog"}
	}
	return shellsession.Context{Kind: "ai", ID: "ai:" + p.AI.store.EnvID(space, env)}
}

func (a *App) aiAgentDetails(p *Panel, name string) {
	space, env, _, _, _ := aiLocation(p.Path)
	for _, agent := range p.AI.store.Agents(space, env) {
		if agent.Name != name {
			continue
		}
		state := "stopped"
		if p.AI.store.Running(space, env, name) != nil {
			state = "running"
		} else if agent.Started {
			state = "stopped; Enter resumes its session"
		}
		a.showText("Agent "+name, fmt.Sprintf("Agent:      %s\nExecutable: %s\nEnv:        %s / %s\nState:      %s\nSession:    %s\nIsolation:  none (PoC host process; MCP scopes the project files only)\n", name, agent.Path, space, env, state, agent.SessionID))
	}
}

func (a *App) aiContextItems(p *Panel) []menuItem {
	space, env, _, _, level := aiLocation(p.Path)
	store := p.AI.store
	// Agents the user runs outside any Env reach the Spaces through this server.
	host := menuItem{"MCP for your own agents…", "", func() {
		command := aispaces.OperatorCommand()
		quoted := strconv.Quote(command[0]) + " " + command[1]
		text := "An agent you run on this computer, outside any AI Env (Codex, Claude Code), can list AI Spaces,\n" +
			"watch their agents, learn when one has finished, and write to it on your behalf.\n\n" +
			"Codex:\n  codex mcp add farshmac-spaces -- " + quoted + "\n\n" +
			"Claude Code:\n  claude mcp add --scope user farshmac-spaces -- " + quoted + "\n\n" +
			"Other agents: a stdio MCP server, command " + quoted + "\n\n" +
			"Tools: spaces_list, agent_status, agent_send, agent_wait. FarshMac must be running for them to answer.\n" +
			"Its calls are logged in " + store.OperatorLogPath() + "\n"
		if data, err := os.ReadFile(store.OperatorLogPath()); err == nil {
			if len(data) > 256<<10 {
				data = data[len(data)-256<<10:]
			}
			text += "\n" + string(data)
		}
		a.showText("MCP for your own agents", text)
	}}
	if level == 0 {
		return []menuItem{host}
	}
	if level == 1 {
		if len(p.Entries) == 0 || p.Entries[p.Cursor].Name == ".." {
			return nil
		}
		env = p.Entries[p.Cursor].Name
	}
	addAgents := menuItem{"Add agents…", "", func() {
		a.aiAgentForm("Add agents to "+env, false, store.Agents(space, env), func(_ string, agents []aispaces.Agent) error {
			err := store.AddAgents(space, env, agents)
			a.refresh()
			return err
		})
	}}
	communication := menuItem{"Communication…", "", func() {
		others, allowed := store.Peers(space, env)
		if len(others) == 0 {
			a.Status = "This Space has no other Envs to talk to"
			return
		}
		var fields []Field
		for _, other := range others {
			fields = append(fields, Field{Label: other, Toggle: true, Checked: allowed[other]})
		}
		a.form("Agents of "+env+" may contact", fields, func(f []Field) {
			var names []string
			for i, other := range others {
				if f[i].Checked {
					names = append(names, other)
				}
			}
			a.error(store.SetPeers(space, env, names))
			a.Status = fmt.Sprintf("%s may contact: %s", env, strings.Join(append(names, "nobody")[:max(1, len(names))], ", "))
		})
		a.Form.Message = "Checked Envs can be sent requests by the agents of " + env + ". An agent can always answer a request it received, and ask its sender back while the request is open. The other direction is set on the other Env."
	}}
	messages := menuItem{"Space messages", "", func() {
		data, err := os.ReadFile(store.MessagesPath(space))
		if err != nil {
			a.Status = "No messages between agents of this Space yet"
			return
		}
		a.showText("Messages in "+space, string(data))
	}}
	mcpLog := menuItem{"MCP debug log", "", func() {
		file := store.LogPath(space, env)
		data, err := os.ReadFile(file)
		if err != nil {
			a.Status = "No MCP log yet: it is written when an agent of this Env calls its tools"
			return
		}
		if len(data) > 512<<10 {
			data = data[len(data)-512<<10:]
		}
		a.showText("MCP debug log of "+env+" · "+file, string(data))
	}}
	// Where the agents of the Space spent their tokens, from their own transcripts.
	usage := menuItem{"Efficiency report", "", func() {
		a.showText("Efficiency of "+space, store.UsageReport(space))
	}}
	if level == 1 {
		return []menuItem{addAgents, communication, messages, mcpLog, usage, host}
	}
	items := []menuItem{addAgents, communication, messages, mcpLog, usage, host}
	if len(p.Entries) > 0 && p.aiAgent(p.Entries[p.Cursor].Name) {
		name := p.Entries[p.Cursor].Name
		items = append(items, menuItem{"Agent settings…", "", func() {
			model, effort, tools := store.Settings(space, env, name)
			a.form("Settings of "+env+"/"+name, []Field{{Label: "Model", Value: model}, {Label: "Effort", Value: effort}, {Label: "Built-in tools", Value: tools}}, func(f []Field) {
				a.error(store.SetSettings(space, env, name, f[0].Value, f[1].Value, f[2].Value))
				a.Status = "Settings of " + name + " apply the next time it is started"
			})
			a.Form.Message = "Leave a field empty for the default. Model and effort are passed to the agent CLI as they are (for example sonnet, low). " +
				"Built-in tools are the CLI's own tools the agent is offered besides the Env tools: empty means " + aispaces.DefaultTools + "; write default for all of them, or your own list. Every tool offered is paid for on each step."
		}})
		if process := store.Running(space, env, name); process != nil {
			items = append(items, menuItem{"Stop agent " + name, "", func() { process.Close(); a.Status = name + " stopped; Enter resumes its session" }})
		}
		return items
	}
	view := func(f func(*aispaces.FS) string, title string) func() {
		return func() {
			fs, err := store.FS(space, env)
			if err != nil {
				a.error(err)
				return
			}
			a.showText(title, f(fs))
		}
	}
	items = append(items,
		menuItem{"Env sources", "", view(func(fs *aispaces.FS) string {
			var b strings.Builder
			for _, binding := range fs.Sources() {
				mode := "read/write"
				if binding.ReadOnly {
					mode = "read-only"
				}
				fmt.Fprintf(&b, "@%s  %s  ←  %s  (%s)\n", binding.ID, binding.Virtual, binding.Source, mode)
			}
			if b.Len() == 0 {
				return "No sources attached. Copy (F5) local files or folders into this panel to attach them.\n"
			}
			return b.String()
		}, "Sources of "+env)},
	)
	if fs, err := store.FS(space, env); err == nil {
		_, _, _, rel, _ := aiLocation(p.Path)
		if hidden := fs.HiddenBelow(rel); len(hidden) > 0 {
			items = append(items, menuItem{fmt.Sprintf("Show removed entries again (%d)…", len(hidden)), "", func() {
				a.confirm("Show removed entries", "Show these entries in this Env again?\n"+strings.Join(hidden[:min(len(hidden), 12)], "\n"), []string{"Show", "Cancel"}, func(i int) {
					if i == 0 {
						a.error(fs.Restore(rel))
						a.refresh()
					}
				})
			}})
		}
	}
	return items
}

// closeAIConsoles stops the agents whose consoles this window owns.
func (a *App) closeAIConsoles() {
	if strings.HasPrefix(a.currentTerminalKey(), "ai:") && a.Foreign != nil {
		a.Foreign.Close()
	}
	for key, parked := range a.parkedTerminals {
		if strings.HasPrefix(key, "ai:") && parked.stream != nil {
			parked.stream.Close()
		}
	}
}

// pollAINotices shows what the AI Spaces store wants the user to know, for
// example that an agent was sent a reference its Env cannot open.
func (a *App) pollAINotices() {
	if len(a.aiConsoles) == 0 {
		return // no agent was opened from this window
	}
	if a.aiNoticePoll++; a.aiNoticePoll%15 != 0 {
		return // twice a second is enough
	}
	store, err := aispaces.Shared()
	if err != nil {
		return
	}
	for _, notice := range store.Notices() {
		a.Status = notice
		a.dirty = true
	}
}
