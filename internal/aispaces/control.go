package aispaces

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// One agent may direct another through configured task levels or explicit
// validated model/effort choices, never arbitrary console commands. What a
// level means and how a conversation is cleared depends on the recipient's
// CLI and the user's configuration.
//
// A running agent is changed with explicit CLI commands, never by stepping
// through a model or effort picker. Only an acknowledged command counts as
// applied. Unsupported commands (or Claude's refused attempt to save a user
// default) fall back to resume with launch arguments behind the same console.

// Profile is what a task level means for one agent.
type Profile struct{ Model, Effort string }

// Levels are the task complexities a directing agent may name.
var Levels = []string{"simple", "moderate", "complex"}

type adapter struct {
	// levels is the default meaning of each level. An empty field leaves the
	// model or the effort as it is.
	levels map[string]Profile
	// clear is the console command that starts a new conversation in the
	// running CLI. Without one the CLI is restarted with a new session.
	clear string
	// configure applies only the supplied fields in place. false or nil means
	// resume with flags; never send the next task under an unacknowledged profile.
	configure func(p *Process, profile Profile) bool
	flags     func(Profile) []string
	// efforts and models are what a directing agent may name for this CLI;
	// models is a hint, any plain name is passed on.
	efforts, models []string
	// methods are the ways of working a sender may ask for besides the
	// plain one, in this CLI's own terms.
	methods map[string]method
}

// method is how one CLI is told to work in a particular way on one message.
type method struct {
	needs []string            // built-in tools the agent must have been given
	wrap  func(string) string // the message as the CLI has to receive it
}

// Methods are the ways of working a directing agent may name.
var Methods = []string{"direct", "workflow", "research"}

var claudeEfforts = []string{"low", "medium", "high", "xhigh", "max"}

func claudeConfigure(p *Process, profile Profile) bool {
	if profile.Model != "" && !p.controlCommand("/model "+profile.Model, "setmodelto", true) {
		return false
	}
	if profile.Effort != "" {
		return p.controlCommand("/effort "+profile.Effort, "seteffortlevelto"+profile.Effort, true)
	}
	return true
}

func codexConfigure(p *Process, profile Profile) bool {
	command := "/model"
	if profile.Model != "" {
		command += " " + profile.Model
	}
	if profile.Effort != "" {
		command += " --reasoning=" + profile.Effort
	}
	// An effort-only change keeps the CLI's selected model. The actual model
	// may be unknown when this session was launched with the CLI default.
	return p.controlCommand(command, "modelchangedto", false, profile.Model, profile.Effort)
}

// controlCommand examines only new output, not an acknowledgement still in
// scrollback. A CLI that lacks this syntax or rejects a protected settings
// write is resumed with arguments. The only optional key is confirmation of
// Claude's explicit cache-cost prompt, not navigation through a picker.
func (p *Process) controlCommand(command, acknowledgement string, confirm bool, values ...string) bool {
	p.mu.Lock()
	start := p.received
	p.mu.Unlock()
	p.command(command)
	confirmed := false
	for i := 0; i < 40; i++ {
		p.mu.Lock()
		n := min(p.received-start, uint64(len(p.tail)))
		text := string(p.tail[len(p.tail)-int(n):])
		ended := p.eof
		p.mu.Unlock()
		text = strings.ToLower(strings.Join(strings.Fields(escapes.ReplaceAllString(text, " ")), ""))
		for _, failure := range []string{"unknowncommand", "unrecognizedcommand", "unknownoption", "unexpectedargument", "couldn'tseteffortlevel", "couldn'tswitchmodel"} {
			if strings.Contains(text, failure) {
				return false
			}
		}
		if at := strings.LastIndex(text, acknowledgement); at >= 0 {
			matched := true
			for _, value := range values {
				matched = matched && strings.Contains(text[at:], strings.ToLower(value))
			}
			if matched {
				return true
			}
		}
		if ended {
			return false
		}
		if confirm && !confirmed && strings.Contains(text, "entertoconfirm") {
			p.keys("\r")
			confirmed = true
		}
		select {
		case <-p.closed:
			return false
		case <-time.After(150 * time.Millisecond):
		}
	}
	return false
}

var adapters = map[string]adapter{
	"claude": {
		levels:    map[string]Profile{"simple": {"sonnet", "low"}, "moderate": {"opus", "medium"}, "complex": {"opus", "high"}},
		clear:     "/clear",
		configure: claudeConfigure,
		efforts:   claudeEfforts,
		models:    []string{"haiku", "sonnet", "opus", "fable"},
		// Not exercised with a real CLI in the PoC: both run many sub-agents.
		methods: map[string]method{
			// The keyword makes Claude Code plan a multi-agent workflow for this one prompt.
			"workflow": {[]string{"Workflow", "Agent"}, func(text string) string { return text + "\n\n[Do this as a workflow: ultracode]" }},
			// The bundled research workflow must start the message.
			"research": {[]string{"Workflow", "Agent", "WebSearch", "WebFetch"}, func(text string) string { return "/deep-research " + text }},
		},
		flags: func(p Profile) []string {
			var args []string
			if p.Model != "" {
				args = append(args, "--model", p.Model)
			}
			if p.Effort != "" {
				args = append(args, "--effort", p.Effort)
			}
			return args
		},
	},
	"codex": {
		configure: codexConfigure,
		levels:    map[string]Profile{"simple": {"", "low"}, "moderate": {"", "medium"}, "complex": {"", "high"}},
		efforts:   []string{"low", "medium", "high"},
		flags: func(p Profile) []string {
			var args []string
			if p.Model != "" {
				args = append(args, "-m", p.Model)
			}
			if p.Effort != "" {
				args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(p.Effort))
			}
			return args
		},
	},
}

// level returns what a complexity means for this agent: the user's own
// definition, or the adapter's.
func (a *Agent) level(name string) Profile {
	if custom, ok := a.Levels[name]; ok {
		return custom
	}
	return adapters[a.Name].levels[name]
}

// profile is the model and effort the agent runs with now: its own settings,
// overridden by the level a directing agent chose last.
func (a *Agent) profile() Profile {
	p := Profile{a.Model, a.Effort}
	if a.Level != "" {
		l := a.level(a.Level)
		if l.Model != "" {
			p.Model = l.Model
		}
		if l.Effort != "" {
			p.Effort = l.Effort
		}
	}
	if a.Chosen.Model != "" {
		p.Model = a.Chosen.Model
	}
	if a.Chosen.Effort != "" {
		p.Effort = a.Chosen.Effort
	}
	return p
}

// modelName is what may be typed into a CLI or passed to it as a model.
var modelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\[\]-]{0,63}$`)

// accepts checks a model and an effort named for this agent's CLI.
func (a *Agent) accepts(d directive) error {
	if d.model != "" && !oneOf(d.model, a.knownModels()) {
		return fmt.Errorf("model %q is not in the CLI catalog or user-configured profiles; use agent_peers or a configured complexity", d.model)
	}
	candidate := *a
	if d.level != "" {
		candidate.Level = d.level
		candidate.Chosen = Profile{}
	}
	if d.model != "" {
		candidate.Chosen.Model = d.model
	}
	if d.effort != "" {
		candidate.Chosen.Effort = d.effort
	}
	return validateProfile(a.Name, candidate.profile())
}

func (a *Agent) choices() string {
	models := a.knownModels()
	out := "configured complexity simple/moderate/complex"
	if len(models) > 0 {
		out += "; catalog/configured models " + strings.Join(models, "/")
	}
	return out + "; effort " + strings.Join(EffortChoices(a.Name), "/") + "; requested " + a.profile().String() + " (not proof of application)"
}

func (p Profile) String() string {
	parts := []string{}
	if p.Model != "" {
		parts = append(parts, p.Model)
	}
	if p.Effort != "" {
		parts = append(parts, p.Effort+" effort")
	}
	if len(parts) == 0 {
		return "the CLI's defaults"
	}
	return strings.Join(parts, ", ")
}

// directive is what a sender asks for besides delivering its message.
type directive struct {
	level  string // task complexity, or ""
	model  string // a model named outright, or ""
	effort string // an effort named outright, or ""
	fresh  bool   // a new task: the recipient starts it with an empty context
	method string // way of working, "" or "direct" for the plain one
}

func (d directive) empty() bool {
	return d.level == "" && d.model == "" && d.effort == "" && !d.fresh && d.plain()
}
func (d directive) plain() bool { return d.method == "" || d.method == "direct" }

func oneOf(value string, known []string) bool {
	for _, k := range known {
		if value == k {
			return true
		}
	}
	return false
}

func (d directive) check() error {
	if d.level != "" && !oneOf(d.level, Levels) {
		return fmt.Errorf("complexity must be one of: %s", strings.Join(Levels, ", "))
	}
	if d.method != "" && !oneOf(d.method, Methods) {
		return fmt.Errorf("method must be one of: %s", strings.Join(Methods, ", "))
	}
	return nil
}

// way returns how the agent's CLI is told to use a method, or why it cannot.
func (a *Agent) way(name string) (func(string) string, string) {
	m, known := adapters[a.Name].methods[name]
	if !known {
		return nil, "its CLI has no such way of working"
	}
	tools := "," + strings.ReplaceAll(a.Tools, " ", "") + ","
	if a.Tools == "" {
		tools = "," + DefaultTools + ","
	}
	var missing []string
	for _, need := range m.needs {
		if a.Tools != "default" && !strings.Contains(tools, ","+need+",") {
			missing = append(missing, need)
		}
	}
	if len(missing) > 0 {
		return nil, "the user has not given it the tools " + strings.Join(missing, ", ") + " (Agent settings…, Built-in tools)"
	}
	return m.wrap, ""
}

// mayDirect: the user allowed the agents of from to set the task level and
// reset the context of the agents of to.
func mayDirect(from, to *Env) bool {
	for _, id := range from.Directs {
		if id == to.ID {
			return true
		}
	}
	return false
}

// Directs reports which other Envs the agents of env may direct.
func (s *Store) Directs(space, env string) map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	allowed := map[string]bool{}
	sp, e, err := s.find(space, env)
	if err != nil || e == nil {
		return allowed
	}
	for _, other := range sp.Envs {
		if other != e && mayDirect(e, other) {
			allowed[other.Name] = true
		}
	}
	return allowed
}

// SetDisabled takes an agent out of use, or back: a disabled agent is not
// addressed, listed or started, so an Env with one enabled agent is reached
// by its name alone.
func (s *Store) SetDisabled(space, env, agent string, disabled bool) error {
	s.mu.Lock()
	_, e, err := s.find(space, env)
	var a *Agent
	if err == nil {
		if a = e.agent(agent); a == nil {
			err = fmt.Errorf("agent %s is not configured in this Env", agent)
		}
	}
	var p *Process
	if err == nil {
		a.Disabled = disabled
		if disabled {
			p = s.running[e.ID+":"+agent]
		}
		err = s.save()
	}
	s.mu.Unlock()
	if p != nil {
		p.Close()
	}
	return err
}

// Disabled reports whether the user took the agent out of use.
func (s *Store) Disabled(space, env, agent string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, e, err := s.find(space, env); err == nil && e != nil {
		if a := e.agent(agent); a != nil {
			return a.Disabled
		}
	}
	return false
}

// SetDirects replaces the list of Envs the agents of env may direct.
func (s *Store) SetDirects(space, env string, names []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, e, err := s.find(space, env)
	if err != nil {
		return err
	}
	e.Directs = nil
	for _, name := range names {
		if other := sp.env(name); other != nil && other != e {
			e.Directs = append(e.Directs, other.ID)
		}
	}
	return s.save()
}

// AgentSettings are what the user decides about one agent's launch.
type AgentSettings struct {
	Model, Effort, Tools string
	Levels               map[string]Profile // what each level means for this agent, defaults included
	Level                string             // the level a directing agent chose last
}

// Settings returns the launch settings of an agent.
func (s *Store) Settings(space, env, agent string) AgentSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := AgentSettings{Levels: map[string]Profile{}}
	if _, e, err := s.find(space, env); err == nil && e != nil {
		if a := e.agent(agent); a != nil {
			out.Model, out.Effort, out.Tools, out.Level = a.Model, a.Effort, a.Tools, a.Level
			for _, name := range Levels {
				out.Levels[name] = a.level(name)
			}
		}
	}
	return out
}

// SetSettings changes the launch settings; they apply at the next start, or
// when an agent directs this one again. A level equal to the adapter's
// default is not stored, so later defaults still reach it.
func (s *Store) SetSettings(space, env, agent string, settings AgentSettings) error {
	if err := validateSettings(agent, settings); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, e, err := s.find(space, env)
	if err != nil {
		return err
	}
	a := e.agent(agent)
	if a == nil {
		return fmt.Errorf("agent %s is not configured in this Env", agent)
	}
	a.Model, a.Effort, a.Tools = strings.TrimSpace(settings.Model), strings.TrimSpace(settings.Effort), strings.TrimSpace(settings.Tools)
	a.Levels = nil
	a.Chosen, a.Level = Profile{}, ""
	for _, name := range Levels {
		if custom, ok := settings.Levels[name]; ok && custom != adapters[a.Name].levels[name] {
			if a.Levels == nil {
				a.Levels = map[string]Profile{}
			}
			a.Levels[name] = custom
		}
	}
	return s.save()
}
