// Package aispaces is the proof of concept for docs/AI_SPACES_ARCHITECTURE_DRAFT.md:
// a catalog of Spaces and Envs, live references with per-Env hidden entries,
// an Env-scoped MCP server and sandboxed console-agent launching on macOS.
// Immutable artifacts and server-recorded checks support bounded peer review.
// The catalog remains a local JSON file; this is not a multi-host scheduler.
package aispaces

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

type Agent struct {
	Name      string   // entry name in the Env root, also the adapter ("claude", "codex")
	Path      string   // resolved executable
	SessionID string   // adapter-native session reference used to resume
	Started   bool     // launched at least once
	Inbox     []string `json:",omitempty"` // messages waiting for the next start
	// Optional launch settings; empty means the CLI's own default.
	Model  string `json:",omitempty"`
	Effort string `json:",omitempty"`
	Tools  string `json:",omitempty"` // built-in tools the CLI offers: a list, "default" for all, "" for FarshMac's choice
	// Task levels: the user's own meaning of each, the one chosen last by a
	// directing agent, and whether the next start begins a new conversation.
	Levels map[string]Profile `json:",omitempty"`
	Level  string             `json:",omitempty"`
	Fresh  bool               `json:",omitempty"`
	// Chosen is a model or effort a directing agent named itself; it stands
	// until that agent names a level or another value.
	Chosen Profile `json:",omitzero"`
	// Disabled: kept in the Env but out of use: not addressed, not started.
	Disabled bool `json:",omitempty"`
}

type Binding struct {
	ID              string // same for one real path in every Env of the Space
	Virtual, Source string
	ReadOnly        bool `json:",omitempty"`
}

type Env struct {
	ID, Name string
	Role     string `json:",omitempty"`
	Agents   []Agent
	Bindings []Binding
	Hidden   []string `json:",omitempty"` // paths inside references removed from this Env only
	Peers    []string `json:",omitempty"` // IDs of the Envs this Env's agents may contact
	Directs  []string `json:",omitempty"` // Envs whose agents these may direct: task level and context
}

type Space struct {
	ID, Name    string
	Envs        []*Env
	NextMessage int                `json:",omitempty"`
	Requests    map[string]request `json:",omitempty"` // messages waiting for an answer
	Refs        map[string]string  `json:",omitempty"` // reference ID → real path
	Operator    []operatorNote     `json:",omitempty"` // replies not yet read by the user's own agent
}

type Store struct {
	mu      sync.Mutex
	root    string
	Spaces  []*Space
	mcp     *mcpServer
	running map[string]*Process
	notices []string
}

var shared struct {
	once  sync.Once
	store *Store
	err   error
}

// Shared is the one store of this process; all windows use it.
func Shared() (*Store, error) {
	shared.once.Do(func() {
		root, err := os.UserConfigDir()
		if err != nil {
			shared.err = err
			return
		}
		shared.store, shared.err = Open(filepath.Join(root, "FarshMac", "ai-spaces"))
		if shared.err == nil {
			shared.store.Serve() // for the user's agents outside any Env
		}
	})
	return shared.store, shared.err
}

func Open(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	s := &Store{root: root, running: map[string]*Process{}}
	b, err := os.ReadFile(filepath.Join(root, "catalog.json"))
	if err == nil {
		err = json.Unmarshal(b, &s.Spaces)
	} else if os.IsNotExist(err) {
		err = nil
	}
	if err == nil {
		s.migrate()
	}
	if err == nil {
		s.save()
	}
	return s, err
}

// refID returns the ID of a real path in this Space, assigning one on first use.
func (sp *Space) refID(source string) string {
	for id, known := range sp.Refs {
		if known == source {
			return id
		}
	}
	if sp.Refs == nil {
		sp.Refs = map[string]string{}
	}
	id := newID()[:6]
	sp.Refs[id] = source
	return id
}

// resolveRef turns a portable reference (@id/path) into the virtual path it
// has in this Env. The folder must be attached to the Env, directly or
// through a folder that contains it.
func (s *Store) resolveRef(e *Env, ref string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp := s.spaceOf(e)
	parts := strings.SplitN(strings.TrimPrefix(ref, "@"), "/", 2)
	if sp == nil || sp.Refs[parts[0]] == "" {
		return "", fmt.Errorf("not_found: no reference @%s in this Space", parts[0])
	}
	real := sp.Refs[parts[0]]
	if len(parts) == 2 {
		real = filepath.Join(real, filepath.FromSlash(path.Clean("/"+parts[1])))
	}
	for _, b := range e.Bindings {
		if real == b.Source || strings.HasPrefix(real, b.Source+string(filepath.Separator)) {
			return b.Virtual + filepath.ToSlash(strings.TrimPrefix(real, b.Source)), nil
		}
	}
	return "", fmt.Errorf("not_found: %s is not attached to this Env; ask the user to attach that folder", ref)
}

// migrate moves files that an earlier PoC build kept in a shadow copy of a
// reference into the real folder. Existing real files are never replaced.
func (s *Store) migrate() {
	for _, sp := range s.Spaces {
		for _, e := range sp.Envs {
			for i := range e.Bindings {
				if e.Bindings[i].ID == "" {
					e.Bindings[i].ID = sp.refID(e.Bindings[i].Source)
				}
			}
			for _, b := range e.Bindings {
				shadow := filepath.Join(s.envDir(e), "overlay", filepath.FromSlash(b.Virtual))
				filepath.Walk(shadow, func(full string, info os.FileInfo, err error) error {
					if err != nil || info.IsDir() {
						return nil
					}
					rel, _ := filepath.Rel(shadow, full)
					target := filepath.Join(b.Source, rel)
					if _, err := os.Lstat(target); os.IsNotExist(err) && os.MkdirAll(filepath.Dir(target), 0755) == nil {
						if data, err := os.ReadFile(full); err == nil && os.WriteFile(target, data, info.Mode().Perm()) == nil {
							os.Remove(full)
						}
					}
					return nil
				})
			}
		}
	}
}

// save must be called with s.mu held.
func (s *Store) save() error {
	b, err := json.MarshalIndent(s.Spaces, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.root, ".catalog.tmp")
	if err = os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.root, "catalog.json"))
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func validName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00\r\n") {
		return fmt.Errorf("enter a name without slashes")
	}
	return nil
}

func (s *Store) space(name string) *Space {
	for _, sp := range s.Spaces {
		if sp.Name == name {
			return sp
		}
	}
	return nil
}
func (sp *Space) env(name string) *Env {
	for _, e := range sp.Envs {
		if e.Name == name {
			return e
		}
	}
	return nil
}
func (s *Store) find(space, env string) (*Space, *Env, error) {
	sp := s.space(space)
	if sp == nil {
		return nil, nil, fmt.Errorf("AI Space %q no longer exists", space)
	}
	if env == "" {
		return sp, nil, nil
	}
	e := sp.env(env)
	if e == nil {
		return nil, nil, fmt.Errorf("AI Env %q no longer exists", env)
	}
	return sp, e, nil
}

func (s *Store) SpaceNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := []string{}
	for _, sp := range s.Spaces {
		names = append(names, sp.Name)
	}
	return names
}
func (s *Store) EnvNames(space string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, _, err := s.find(space, "")
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range sp.Envs {
		names = append(names, e.Name)
	}
	return names, nil
}

func (s *Store) CreateSpace(name string) error {
	if err := validName(name); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.space(name) != nil {
		return fmt.Errorf("AI Space %q already exists", name)
	}
	s.Spaces = append(s.Spaces, &Space{ID: newID(), Name: name})
	return s.save()
}

func (s *Store) CreateEnv(space, name string, agents []Agent) error {
	if err := validName(name); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, _, err := s.find(space, "")
	if err != nil {
		return err
	}
	if sp.env(name) != nil {
		return fmt.Errorf("AI Env %q already exists", name)
	}
	sp.Envs = append(sp.Envs, &Env{ID: newID(), Name: name, Agents: agents})
	return s.save()
}

// Rename renames a Space (env == "") or an Env; IDs, sources and sessions stay.
func (s *Store) Rename(space, env, name string) error {
	if err := validName(name); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, e, err := s.find(space, env)
	if err != nil {
		return err
	}
	if e == nil {
		if s.space(name) != nil {
			return fmt.Errorf("AI Space %q already exists", name)
		}
		sp.Name = name
	} else {
		if sp.env(name) != nil {
			return fmt.Errorf("AI Env %q already exists", name)
		}
		e.Name = name
	}
	return s.save()
}

// Delete removes a Space (env == "") or one Env with its private files and scratch.
// Agents are stopped first. Sources are only detached, never touched.
func (s *Store) Delete(space, env string) error {
	s.mu.Lock()
	sp, e, err := s.find(space, env)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	doomed := sp.Envs
	if e != nil {
		doomed = []*Env{e}
	}
	var stop []*Process
	for _, d := range doomed {
		for key, p := range s.running {
			if strings.HasPrefix(key, d.ID+":") {
				stop = append(stop, p)
			}
		}
	}
	if e == nil {
		for i, x := range s.Spaces {
			if x == sp {
				s.Spaces = append(s.Spaces[:i], s.Spaces[i+1:]...)
				break
			}
		}
	} else {
		for i, x := range sp.Envs {
			if x == e {
				sp.Envs = append(sp.Envs[:i], sp.Envs[i+1:]...)
				break
			}
		}
	}
	err = s.save()
	s.mu.Unlock()
	for _, p := range stop {
		p.Close()
	}
	for _, d := range doomed {
		os.RemoveAll(s.envDir(d))
	}
	return err
}

func (s *Store) envDir(e *Env) string { return filepath.Join(s.root, "envs", e.ID) }

// EnvID is the stable identity behind the current names.
func (s *Store) EnvID(space, env string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, e, err := s.find(space, env); err == nil && e != nil {
		return e.ID
	}
	return ""
}

func (s *Store) Agents(space, env string) []Agent {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, e, err := s.find(space, env)
	if err != nil || e == nil {
		return nil
	}
	return append([]Agent(nil), e.Agents...)
}

// AddAgents adds the agents that are not configured yet.
func (s *Store) AddAgents(space, env string, agents []Agent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, e, err := s.find(space, env)
	if err != nil {
		return err
	}
	prefs, err := s.preferences()
	if err != nil {
		return err
	}
	for _, a := range agents {
		defaults := prefs.Agents[a.Name]
		if a.Model == "" {
			a.Model = defaults.Model
		}
		if a.Effort == "" {
			a.Effort = defaults.Effort
		}
		if a.Tools == "" {
			a.Tools = defaults.Tools
		}
		if a.Levels == nil {
			a.Levels = defaults.Levels
		}
		known := false
		for _, old := range e.Agents {
			known = known || old.Name == a.Name
		}
		if !known {
			e.Agents = append(e.Agents, a)
		}
	}
	return s.save()
}

// RemoveAgent stops one agent and removes its entry; the Env, its overlay
// and the other agents stay.
func (s *Store) RemoveAgent(space, env, name string) error {
	s.mu.Lock()
	_, e, err := s.find(space, env)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	p := s.running[e.ID+":"+name]
	for i, a := range e.Agents {
		if a.Name == name {
			e.Agents = append(e.Agents[:i], e.Agents[i+1:]...)
			break
		}
	}
	err = s.save()
	dir := filepath.Join(s.envDir(e), "agents", name)
	s.mu.Unlock()
	if p != nil {
		p.Close()
	}
	os.RemoveAll(dir)
	return err
}

// Attach registers host paths as references below the virtual folder dir.
func (s *Store) Attach(space, env, dir string, sources []string, readOnly bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, e, err := s.find(space, env)
	if err != nil {
		return err
	}
	for _, source := range sources {
		source = filepath.Clean(source)
		if _, err := os.Lstat(source); err != nil {
			return err
		}
		virtual := path.Join("/", dir, filepath.Base(source))
		replaced := false
		for i := range e.Bindings {
			if e.Bindings[i].Virtual == virtual {
				e.Bindings[i].ID, e.Bindings[i].Source, e.Bindings[i].ReadOnly, replaced = sp.refID(source), source, readOnly, true
			}
		}
		if !replaced {
			e.Bindings = append(e.Bindings, Binding{ID: sp.refID(source), Virtual: virtual, Source: source, ReadOnly: readOnly})
		}
	}
	return s.save()
}

// FS is the effective filesystem of one Env.
func (s *Store) FS(space, env string) (*FS, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, e, err := s.find(space, env)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, fmt.Errorf("not an AI Env")
	}
	s.ensureInstructions(e)
	return &FS{s: s, env: e}, nil
}

// InstructionFile is the file an agent CLI reads for its standing
// instructions: the name each CLI looks for in its working folder.
func InstructionFile(agent string) string {
	if agent == "claude" {
		return "CLAUDE.md"
	}
	return "AGENTS.md"
}

// ensureInstructions gives every agent of the Env its own instruction file
// in the Env root, where the user edits it. It must be called with s.mu held.
func (s *Store) ensureInstructions(e *Env) {
	dir := filepath.Join(s.envDir(e), "overlay")
	for _, a := range e.Agents {
		file := filepath.Join(dir, InstructionFile(a.Name))
		if _, err := os.Lstat(file); os.IsNotExist(err) && os.MkdirAll(dir, 0755) == nil {
			os.WriteFile(file, []byte(fmt.Sprintf("# Instructions for %s in %s\n\nDescribe this agent's role and how it should work.\nThe agent reads this file each time it starts.\n", a.Name, e.Name)), 0644)
		}
	}
}
