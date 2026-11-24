package aispaces

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// gatewayExecutable replaces the running executable as the MCP gateway in
// tests, whose own binary does not understand GatewayFlag.
var gatewayExecutable string

// Adapters are the console agents the PoC knows how to configure.
var Adapters = []string{"claude", "codex"}

// Discover finds the supported agent CLIs on PATH.
func Discover() []Agent {
	var found []Agent
	for _, name := range Adapters {
		if p, err := exec.LookPath(name); err == nil {
			found = append(found, Agent{Name: name, Path: p})
		}
	}
	return found
}

// Process is the console of an agent. It satisfies shellsession.Stream. The
// CLI behind it can be replaced (a new model, a new conversation) while the
// console, its window and its queue of messages stay.
type Process struct {
	store *Store
	env   *Env
	name  string

	out    chan []byte
	done   chan struct{}
	err    error
	once   sync.Once
	typing sync.Mutex
	closed chan struct{}

	// Output is buffered here, so an agent nobody is watching keeps running
	// and its console can be opened later.
	mu       sync.Mutex
	run      *incarnation
	swapping bool          // the CLI is being replaced: its exit does not end the console
	ending   bool          // the CLI exited for good; the console is being ended
	ended    chan struct{} // closed when the console has ended
	endOnce  sync.Once
	current  Profile // model and effort the CLI has now, as far as FarshMac set them
	cols     int
	rows     int
	backlog  [][]byte
	held     int
	more     chan struct{}
	eof      bool
	last     time.Time // last output, to tell when the CLI is idle at its prompt
	head     []byte    // first output, searched for the folder trust question
	trusted  bool
	attached bool

	// Messages wait here and are typed in order by one worker.
	todo []delivery
	wake chan struct{}

	// For whoever asks whether the agent has finished its turn.
	pending   int       // messages accepted but not typed in yet
	delivered time.Time // when the last message was typed in
	tail      []byte    // recent output, for a look at the console
	received  uint64    // output cursor; control acknowledgements must be new
	// Reported by the CLI's own hooks, when it has them: far more exact than
	// watching the console.
	hooked bool      // a hook has reported at least once
	turn   bool      // between a submitted prompt and the end of the answer
	asking bool      // the turn waits for the user's permission
	turned time.Time // when turn last changed
}

// incarnation is one run of the CLI behind a console.
type incarnation struct {
	cmd     *exec.Cmd
	pty     *os.File
	token   string
	profile Profile       // what it was started with
	exited  chan struct{} // closed when it is gone and its token revoked
}

func (r *incarnation) kill() {
	if r.cmd.Process != nil {
		syscall.Kill(-r.cmd.Process.Pid, syscall.SIGTERM)
		syscall.Kill(-r.cmd.Process.Pid, syscall.SIGKILL)
	}
}

// delivery is a message waiting to be typed into the console.
type delivery struct {
	text     string
	directed bool    // the sender set the task level or asked for a new task
	fresh    bool    // start it with an empty context
	profile  Profile // settings captured when this message was accepted
}

// working reports whether the CLI is in a turn. It must be called with p.mu held.
func (p *Process) working() bool {
	if p.hooked {
		// A prompt queued while the CLI worked starts right after its turn ends.
		return p.turn || time.Since(p.turned) < 1500*time.Millisecond
	}
	return p.last.IsZero() || time.Since(p.last) < 3*time.Second || time.Since(p.delivered) < 4*time.Second
}

// busy reports whether the agent is working or has messages still to
// receive, and for how long its console has been quiet.
func (p *Process) busy() (bool, time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.last.IsZero() {
		return true, 0 // still starting
	}
	return p.pending > 0 || p.working(), time.Since(p.last)
}

// event records what a hook of the CLI reported.
func (p *Process) event(kind string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hooked, p.turned = true, time.Now()
	switch kind {
	case "busy":
		p.turn, p.asking = true, false
	case "idle":
		p.turn, p.asking = false, false
	case "ask":
		p.asking = true
	}
}

// expect announces messages that Deliver will type in.
func (p *Process) expect(n int) {
	p.mu.Lock()
	p.pending += n
	p.mu.Unlock()
}

// screen is a rough text view of the last console output: escape sequences
// removed, repeated redraws of a line dropped.
func (p *Process) screen(lines int) string {
	p.mu.Lock()
	text := escapes.ReplaceAllString(string(p.tail), "")
	p.mu.Unlock()
	var kept []string
	for _, line := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' }) {
		line = strings.TrimSpace(strings.Map(func(r rune) rune {
			if r < ' ' {
				return -1
			}
			return r
		}, line))
		if line != "" && (len(kept) == 0 || kept[len(kept)-1] != line) {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept[max(0, len(kept)-lines):], "\n")
}

const backlogLimit = 4 << 20

// Attach claims the console for one window; false means another has it.
func (p *Process) Attach() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.attached {
		return false
	}
	p.attached = true
	return true
}

func (p *Process) collect(chunk []byte) {
	p.mu.Lock()
	p.received += uint64(len(chunk))
	p.backlog = append(p.backlog, chunk)
	p.held += len(chunk)
	for p.held > backlogLimit && len(p.backlog) > 1 {
		p.held -= len(p.backlog[0])
		p.backlog = p.backlog[1:]
	}
	p.last = time.Now()
	if p.tail = append(p.tail, chunk...); len(p.tail) > 64<<10 {
		p.tail = append([]byte(nil), p.tail[len(p.tail)-32<<10:]...)
	}
	answer := ""
	if !p.trusted && len(p.head) < 64<<10 {
		p.head = append(p.head, chunk...)
		answer = trustAnswer(p.head)
		p.trusted = answer != ""
	}
	p.mu.Unlock()
	select {
	case p.more <- struct{}{}:
	default:
	}
	if answer != "" {
		go func() {
			time.Sleep(700 * time.Millisecond)
			p.typing.Lock()
			defer p.typing.Unlock()
			for _, key := range strings.Split(answer, "|") {
				p.Write([]byte(key))
				time.Sleep(300 * time.Millisecond)
			}
		}()
	}
}

var escapes = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\\\)`)

// trustAnswer recognises Claude Code's question whether a new folder is
// trusted and returns the keys that accept it. The folder is the agent's own
// scratch, created by FarshMac, and an agent started to receive a message has
// nobody at its console to answer.
func trustAnswer(screen []byte) string {
	text := strings.Join(strings.Fields(escapes.ReplaceAllString(string(screen), " ")), "")
	yes := strings.Index(text, "Yes,Itrustthisfolder")
	if yes < 0 || !strings.Contains(text, "Entertoconfirm") {
		return ""
	}
	if cursor := strings.LastIndex(text[:yes], "❯"); cursor >= 0 && strings.HasPrefix(text[cursor+len("❯"):], "No") {
		return "\x1b[B|\r" // the cursor is on "No, exit": move down first
	}
	return "\r"
}

// idle waits until the CLI has been quiet for a moment: it is at its prompt.
func (p *Process) idle() {
	started := time.Now()
	for time.Since(started) < 60*time.Second {
		time.Sleep(500 * time.Millisecond)
		p.mu.Lock()
		quiet := !p.last.IsZero() && time.Since(p.last) > 2500*time.Millisecond
		p.mu.Unlock()
		if quiet && time.Since(started) > 4*time.Second {
			return
		}
	}
}

// pump hands the buffered output to whoever reads the console.
func (p *Process) pump() {
	for {
		p.mu.Lock()
		var chunk []byte
		if len(p.backlog) > 0 {
			chunk = p.backlog[0]
			p.backlog = p.backlog[1:]
			p.held -= len(chunk)
		}
		eof := p.eof
		p.mu.Unlock()
		if chunk == nil {
			if eof {
				close(p.out)
				close(p.done)
				return
			}
			<-p.more
			continue
		}
		p.out <- chunk
	}
}

func (p *Process) Write(data []byte) error {
	p.mu.Lock()
	file := p.run.pty
	p.mu.Unlock()
	_, err := file.Write(data)
	return err
}
func (p *Process) Resize(cols, rows int) error {
	p.mu.Lock()
	p.cols, p.rows = max(cols, 20), max(rows, 5)
	file := p.run.pty
	p.mu.Unlock()
	return pty.Setsize(file, &pty.Winsize{Cols: uint16(max(cols, 20)), Rows: uint16(max(rows, 5))})
}
func (p *Process) Output() <-chan []byte { return p.out }
func (p *Process) Done() <-chan struct{} { return p.done }
func (p *Process) Err() error            { <-p.done; return p.err }

// Deliver types text into the agent's console as a new prompt. Bracketed
// paste keeps a multi-line text one prompt; Enter follows once the CLI has
// taken it. A busy CLI queues the prompt until its current turn ends.
func (p *Process) Deliver(text string) {
	p.typing.Lock()
	defer p.typing.Unlock()
	p.Write([]byte("\x1b[200~" + strings.TrimSpace(text) + "\x1b[201~"))
	time.Sleep(400 * time.Millisecond)
	p.Write([]byte("\r"))
	time.Sleep(400 * time.Millisecond)
	p.mu.Lock()
	p.pending = max(0, p.pending-1)
	p.delivered = time.Now()
	p.mu.Unlock()
}

// post queues a message for the console; messages are typed in the order
// they were accepted.
func (p *Process) post(d delivery) {
	p.mu.Lock()
	p.todo = append(p.todo, d)
	p.pending++
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// work types the queued messages. A directed one waits until the agent is
// free, and the CLI is then given the level and the context it asks for.
func (p *Process) work() {
	p.idle() // let the CLI reach its prompt
	for {
		p.mu.Lock()
		var d delivery
		have := len(p.todo) > 0
		if have {
			d = p.todo[0]
		}
		over := p.eof
		p.mu.Unlock()
		if over {
			return
		}
		if !have {
			select {
			case <-p.wake:
				continue
			case <-p.ended:
				return
			}
		}
		if d.directed && (!p.settle() || !p.prepare(d)) {
			return // the console ended; what is left goes back to the inbox
		}
		p.Deliver(d.text)
		// Only now is it out of the queue: a console that ended meanwhile
		// has already returned it to the inbox.
		p.mu.Lock()
		if p.eof || len(p.todo) == 0 {
			p.mu.Unlock()
			return
		}
		p.todo = p.todo[1:]
		p.mu.Unlock()
	}
}

// settle waits until the CLI is between turns. false: the console ended.
func (p *Process) settle() bool {
	free := 0
	for free < 3 {
		select {
		case <-p.ended:
			return false
		case <-p.closed:
			return false
		case <-time.After(500 * time.Millisecond):
		}
		p.mu.Lock()
		over := p.eof
		if p.working() {
			free = 0
		} else {
			free++
		}
		p.mu.Unlock()
		if over {
			return false
		}
	}
	return true
}

// command types a console command of the CLI, as one pasted line.
func (p *Process) command(text string) {
	p.typing.Lock()
	defer p.typing.Unlock()
	p.Write([]byte("\x1b[200~" + text + "\x1b[201~"))
	time.Sleep(400 * time.Millisecond)
	p.Write([]byte("\r"))
	time.Sleep(600 * time.Millisecond)
}

func (p *Process) keys(sequence string) {
	p.typing.Lock()
	defer p.typing.Unlock()
	p.Write([]byte(sequence))
	time.Sleep(150 * time.Millisecond)
}

// glance is the end of what the console showed, without escape sequences
// and spaces, to recognise a dialog of the CLI.
func (p *Process) glance(n int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	text := strings.Join(strings.Fields(escapes.ReplaceAllString(string(p.tail), " ")), "")
	return text[max(0, len(text)-n):]
}

// prepare gives the CLI the context, model and effort the next message is to
// be handled with: in place where the CLI can, by replacing it otherwise.
func (p *Process) prepare(d delivery) bool {
	s := p.store
	want := d.profile
	s.mu.Lock()
	exists := p.env.agent(p.name) != nil
	s.mu.Unlock()
	if !exists {
		return false
	}
	p.mu.Lock()
	now := p.current
	p.mu.Unlock()
	// Empty fields mean keep the current choice, including on resume and
	// when a new model would otherwise reset the CLI's reasoning default.
	if want.Model == "" {
		want.Model = now.Model
	}
	if want.Effort == "" {
		want.Effort = now.Effort
	}
	control := adapters[p.name]
	model := want.Model != "" && want.Model != now.Model
	effort := want.Effort != "" && want.Effort != now.Effort
	if (d.fresh && control.clear == "") || ((model || effort) && control.configure == nil) {
		if err := s.restart(p, d.fresh, want); err != nil {
			return false
		}
		p.idle()
		return true
	}
	if d.fresh {
		// First, so that the model is not switched under a cached conversation.
		p.command(control.clear)
		for i := 0; i < 30; i++ {
			time.Sleep(500 * time.Millisecond)
			p.mu.Lock()
			quiet := time.Since(p.last) > 1200*time.Millisecond
			p.mu.Unlock()
			if quiet && i >= 2 {
				break
			}
		}
	}
	if model || effort {
		changed := Profile{}
		if model {
			changed.Model = want.Model
			changed.Effort = want.Effort
		}
		if effort {
			changed.Effort = want.Effort
		}
		if !control.configure(p, changed) {
			if err := s.restart(p, d.fresh, want); err != nil {
				return false
			}
			p.idle()
			return true
		}
		if model {
			now.Model = want.Model
		}
		if effort {
			now.Effort = want.Effort
		}
	}
	p.mu.Lock()
	p.current = now
	p.mu.Unlock()
	return true
}

// Close stops the agent and everything it started.
func (p *Process) Close() {
	p.once.Do(func() {
		// Under the lock that installs a new run, so none escapes the stop.
		p.mu.Lock()
		close(p.closed)
		run := p.run
		p.mu.Unlock()
		run.kill()
	})
}

// Running returns the live process of an agent, or nil.
func (s *Store) Running(space, env, agent string) *Process {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, e, err := s.find(space, env)
	if err != nil || e == nil {
		return nil
	}
	return s.running[e.ID+":"+agent]
}

func uuid() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// agentEnviron drops the variables an agent CLI sets for its own children.
// When FarshMac itself was started from inside such a CLI, they would make
// the agent a child of that session: Claude Code then keeps no transcript of
// its own and cannot be resumed.
func agentEnviron(env []string) []string {
	kept := env[:0:0]
	for _, v := range env {
		name := strings.SplitN(v, "=", 2)[0]
		if strings.HasPrefix(name, "CLAUDE_CODE_") || name == "CLAUDECODE" || name == "CLAUDE_PID" || name == "CLAUDE_EFFORT" || name == "CODEX_THREAD_ID" || name == "CODEX_INTERNAL_ORIGINATOR_OVERRIDE" {
			continue
		}
		kept = append(kept, v)
	}
	return kept
}

const agentNotes = `# FarshMac AI Env: %s

You are an agent of the AI Env "%s" in the AI Space "%s". Your address is %s.

- The project is NOT in this working directory. It lives in a virtual
  filesystem rooted at / and is reachable only through the "farshmac" MCP
  server. Use its tools for every project read and write.
- Discover through env_info and request only needed operation schemas. Use
  env_read for bounded batched reads; env_call dispatches other operations.
  Do not dump tool catalogs or load unrelated host integrations.
- Read narrowly: everything you read stays in your context and is paid for
  again on every later step. vfs_list with depth shows a tree, vfs_outline
  shows the structure of a document, a source file or a whole folder; then
  read the part you need with vfs_read and section (or offset and limit).
  Read a whole file only when you need all of it. A file you already read is
  not sent again unless it changed.
- Referenced project folders are live: what you write there is saved to the
  real folder immediately, unless the folder is attached read-only. Paths
  outside them are private to this Env.
- Other agents: agent_peers shows whom you may contact, agent_send sends a
  request, agent_reply answers one. A message from another agent arrives as
  a prompt that starts with [FarshMac message #N from ...]. Replies to your
  own requests arrive the same way, on their own: never wait, sleep or poll
  for them, just end your turn.
- Keep messages short and never paste file contents into them. Put long text
  into a file and attach it, or attach the section the recipient needs. In
  the message text name shared files by reference (@id/path, shown by
  vfs_stat and vfs_list), because virtual paths differ between Envs.
- After you send a message or a reply, do not repeat or summarize it in your
  own output: finish the turn with one short line.
- This working directory is your scratch space, and the only place on disk
  you can write. You cannot read the user's home folder or the real project
  folders: do not try, it fails. To build, run or test project code, call
  workspace_pull, workspace_run for tests, then workspace_push.

# Your instructions

%s
`

// DefaultTools are the built-in tools a Claude Code agent of an Env gets
// unless the user chooses otherwise. Every tool the CLI offers is described
// to the model on each step; an Env agent works through the MCP server and
// its scratch folder, and has no use for the rest.
const DefaultTools = "Bash,Read,Edit,Write,AskUserQuestion,TodoWrite"

// EventFlag makes the executable tell the Env server what happened to the
// agent's conversation. The agent CLI's hooks run it.
const EventFlag = "--ai-mcp-event"

func EventMain(event string) int {
	// A hook is given a description of the event on its standard input.
	var told struct {
		Session string `json:"session_id"`
		Source  string `json:"source"`
		Kind    string `json:"notification_type"`
	}
	if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice == 0 {
		// One JSON value, without waiting for the CLI to close the pipe.
		read := make(chan json.RawMessage, 1)
		go func() {
			var value json.RawMessage
			json.NewDecoder(io.LimitReader(os.Stdin, 1<<20)).Decode(&value)
			read <- value
		}()
		select {
		case value := <-read:
			json.Unmarshal(value, &told)
		case <-time.After(500 * time.Millisecond):
		}
	}
	conn, err := net.Dial("unix", os.Getenv(socketEnv))
	if err != nil {
		return 0 // FarshMac is gone; nothing to tell
	}
	defer conn.Close()
	params, _ := json.Marshal(map[string]string{"session": told.Session, "source": told.Source, "kind": told.Kind})
	// Nothing may be printed: the CLI adds a hook's output to the conversation.
	fmt.Fprintf(conn, "%s\n{\"jsonrpc\":\"2.0\",\"method\":\"farshmac/%s\",\"params\":%s}\n", os.Getenv(tokenEnv), event, params)
	return 0
}

// claudeArgs binds a Claude Code process to its Env: the MCP server and
// nothing else, its tools loaded at once rather than searched for, and hooks
// that report the state of the conversation: a turn begun or ended, a
// question to the user, a compacted or cleared context.
func claudeArgs(agent *Agent, self, dir string, gatewayEnv map[string]string) ([]string, []string, error) {
	config, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"farshmac": map[string]any{
		"command": self, "args": []string{GatewayFlag}, "env": gatewayEnv, "alwaysLoad": true}}})
	mcpFile := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(mcpFile, config, 0600); err != nil {
		return nil, nil, err
	}
	report := func(event string) []any {
		return []any{map[string]any{"type": "command", "command": "'" + strings.ReplaceAll(self, "'", `'\''`) + "' " + EventFlag + " " + event}}
	}
	settings, _ := json.Marshal(map[string]any{"hooks": map[string]any{
		"SessionStart": []any{
			map[string]any{"matcher": "compact", "hooks": report("forget")},
			map[string]any{"matcher": "clear", "hooks": report("forget")},
		},
		"PostCompact":      []any{map[string]any{"hooks": report("forget")}},
		"UserPromptSubmit": []any{map[string]any{"hooks": report("busy")}},
		"Stop":             []any{map[string]any{"hooks": report("idle")}},
		"StopFailure":      []any{map[string]any{"hooks": report("idle")}},
		"Notification":     []any{map[string]any{"hooks": report("notify")}},
	}})
	settingsFile := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsFile, settings, 0600); err != nil {
		return nil, nil, err
	}
	args := []string{"--mcp-config", mcpFile, "--strict-mcp-config", "--allowedTools", "mcp__farshmac", "--settings", settingsFile}
	switch agent.Tools {
	case "":
		// --disable-slash-commands also disables built-in /model, /effort and
		// /clear in current Claude Code. Keep them; the tool allowlist and
		// empty setting sources still bound the Env's capabilities.
		args = append(args, "--tools", DefaultTools, "--setting-sources", "")
	case "default":
	default:
		args = append(args, "--tools", agent.Tools)
	}
	return append(args, adapters["claude"].flags(agent.profile())...), []string{mcpFile, settingsFile}, nil
}

// Launch starts an agent in its scratch folder with the Env's MCP server
// configured. A known native session is resumed instead of starting anew.
func (s *Store) Launch(space, env, name string, cols, rows int) (*Process, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, e, err := s.find(space, env)
	if err != nil {
		return nil, err
	}
	return s.launch(sp, e, name, cols, rows)
}

// launch must be called with s.mu held.
func (s *Store) launch(sp *Space, e *Env, name string, cols, rows int) (*Process, error) {
	agent := e.agent(name)
	if agent == nil {
		return nil, fmt.Errorf("agent %s is not configured in this Env", name)
	}
	if agent.Disabled {
		return nil, fmt.Errorf("%s is disabled in this Env; enable it from its menu first", name)
	}
	key := e.ID + ":" + name
	if s.running[key] != nil {
		return nil, fmt.Errorf("%s is already running in another window", name)
	}
	inbox := agent.Inbox
	run, err := s.spawn(sp, e, agent, cols, rows, nil)
	if err != nil {
		return nil, err
	}
	p := &Process{store: s, env: e, name: name, run: run, current: run.profile, cols: max(cols, 20), rows: max(rows, 5),
		out: make(chan []byte, 64), done: make(chan struct{}), closed: make(chan struct{}), ended: make(chan struct{}), more: make(chan struct{}, 1), wake: make(chan struct{}, 1)}
	s.running[key] = p
	for _, text := range inbox {
		p.post(delivery{text: text})
	}
	go p.pump()
	go p.read(run)
	go p.work()
	return p, nil
}

// sessionID is the only form of a native session reference that is stored
// or passed to a CLI: the reference comes back from the agent's own process.
var sessionID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// spawn starts the agent's CLI in its scratch folder with the Env's MCP
// server configured and the model and effort of its current task level. A
// known native session is resumed unless a new conversation was asked for.
// It must be called with s.mu held.
func (s *Store) spawn(sp *Space, e *Env, agent *Agent, cols, rows int, requested *Profile) (*incarnation, error) {
	wanted := agent.profile()
	if requested != nil {
		wanted = *requested
	}
	launch := *agent
	launch.Model, launch.Effort, launch.Level, launch.Chosen = wanted.Model, wanted.Effort, "", Profile{}
	name := agent.Name
	control, known := adapters[name]
	if !known {
		return nil, fmt.Errorf("no adapter for %s", name)
	}
	server, err := s.startMCP()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(s.envDir(e), "agents", name)
	scratch := filepath.Join(dir, "scratch")
	if err = os.MkdirAll(scratch, 0700); err != nil {
		return nil, err
	}
	// The agent's own instructions are the file the user edits in the Env root.
	s.ensureInstructions(e)
	own, _ := os.ReadFile(filepath.Join(s.envDir(e), "overlay", InstructionFile(name)))
	notes := []byte(fmt.Sprintf(agentNotes, e.Name, e.Name, sp.Name, address(e, name), own))
	os.Remove(filepath.Join(scratch, "CLAUDE.md"))
	os.Remove(filepath.Join(scratch, "AGENTS.md"))
	os.WriteFile(filepath.Join(scratch, InstructionFile(name)), notes, 0600)
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if gatewayExecutable != "" {
		self = gatewayExecutable
	}
	if agent.Fresh {
		agent.SessionID, agent.Started = "", false
	}
	if !sessionID.MatchString(agent.SessionID) {
		agent.SessionID = "" // never pass anything else to a CLI
	}
	resume := false
	if name == "claude" {
		resume = agent.SessionID != "" && transcriptFile(agent.SessionID) != ""
		if !resume {
			agent.SessionID = uuid()
		}
	}
	token := server.register(e, name, agent.SessionID)
	gatewayEnv := map[string]string{socketEnv: server.socket, tokenEnv: token}
	var args, files []string
	switch name {
	case "claude":
		if args, files, err = claudeArgs(&launch, self, dir, gatewayEnv); err != nil {
			server.revoke(token)
			return nil, err
		}
		if resume {
			args = append(args, "--resume", agent.SessionID)
		} else {
			args = append(args, "--session-id", agent.SessionID)
		}
	case "codex":
		args = append(args, codexContextArgs(agent.Tools)...)
		// Override the Env gateway, and disable the host-only FarshMac operator.
		args = append(args, "-c", "mcp_servers.farshmac-spaces.enabled=false", "-c", `mcp_servers.farshmac-spaces.command="/usr/bin/false"`)
		if agent.Started {
			args = append([]string{"resume", "--last"}, args...)
		}
		args = append(args,
			"-c", "mcp_servers.farshmac.enabled=true",
			"-c", "mcp_servers.farshmac.command="+strconv.Quote(self),
			"-c", "mcp_servers.farshmac.args=["+strconv.Quote(GatewayFlag)+"]",
			"-c", "mcp_servers.farshmac.env={"+socketEnv+"="+strconv.Quote(server.socket)+","+tokenEnv+"="+strconv.Quote(token)+"}")
		args = append(args, control.flags(wanted)...)
	}
	// Fail closed: without the sandbox the agent would have the user's whole disk.
	if _, err = os.Stat(sandboxExec); err != nil {
		server.revoke(token)
		return nil, fmt.Errorf("agent isolation is unavailable (%s is missing); the agent was not started", sandboxExec)
	}
	// The MCP configuration and the hooks sit beside the scratch folder, not inside it.
	profile, err := sandboxProfile(scratch, append([]string{self, filepath.Dir(realPath(agent.Path))}, files...)...)
	if err != nil {
		server.revoke(token)
		return nil, err
	}
	temp := filepath.Join(scratch, ".tmp")
	os.MkdirAll(temp, 0700)
	cmd := exec.Command(sandboxExec, append([]string{"-p", profile, agent.Path}, args...)...)
	cmd.Dir = scratch
	// The hooks of the CLI reach the Env server with the same token as its MCP gateway.
	cmd.Env = append(agentEnviron(os.Environ()), "TERM=xterm-256color", "COLORTERM=truecolor", "TMPDIR="+temp, socketEnv+"="+server.socket, tokenEnv+"="+token)
	file, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(max(cols, 20)), Rows: uint16(max(rows, 5))})
	if err != nil {
		server.revoke(token)
		return nil, err
	}
	agent.Started, agent.Fresh, agent.Inbox = true, false, nil
	s.save()
	return &incarnation{cmd: cmd, pty: file, token: token, profile: wanted, exited: make(chan struct{})}, nil
}

// read passes the output of one run of the CLI to the console, and ends the
// console when that run was its last.
func (p *Process) read(run *incarnation) {
	buf := make([]byte, 32*1024)
	for {
		n, err := run.pty.Read(buf)
		if n > 0 {
			p.collect(append([]byte(nil), buf[:n]...))
		}
		if err != nil {
			break
		}
	}
	err := run.cmd.Wait()
	run.pty.Close()
	p.store.mu.Lock()
	if p.store.mcp != nil {
		p.store.mcp.revoke(run.token)
	}
	p.store.mu.Unlock()
	p.mu.Lock()
	replaced := p.swapping || p.run != run
	p.ending = p.ending || !replaced // decided once, with restart's own check
	p.mu.Unlock()
	close(run.exited)
	if !replaced {
		p.finish(err)
	}
}

// finish ends the console. Messages it did not get to type go back to the
// agent's inbox, for its next start.
func (p *Process) finish(err error) {
	s := p.store
	s.mu.Lock()
	if key := p.env.ID + ":" + p.name; s.running[key] == p {
		delete(s.running, key)
	}
	p.mu.Lock()
	left := p.todo
	select {
	case <-p.closed:
	default:
		p.err = err // before eof: whoever sees the end may read it
	}
	p.todo, p.pending, p.ending = nil, 0, true
	p.mu.Unlock()
	if a := p.env.agent(p.name); a != nil && len(left) > 0 {
		for _, d := range left {
			a.Inbox = append(a.Inbox, d.text)
			a.Fresh = a.Fresh || d.fresh
		}
		s.save()
	}
	s.mu.Unlock()
	// Neither the console's EOF nor the lifecycle signal may precede the
	// final store write: their consumers can dispose of the store immediately.
	p.mu.Lock()
	p.eof = true
	p.mu.Unlock()
	p.endOnce.Do(func() { close(p.ended) })
	select {
	case p.more <- struct{}{}:
	default:
	}
}

// restart replaces the CLI behind a console with a new run that has the
// agent's current model and effort; fresh also gives it a new conversation.
// The console, its window and its queue stay.
func (s *Store) restart(p *Process, fresh bool, want Profile) error {
	p.mu.Lock()
	old, cols, rows := p.run, p.cols, p.rows
	ended := p.eof || p.ending
	p.swapping = !ended
	p.mu.Unlock()
	if ended {
		return fmt.Errorf("the agent has stopped")
	}
	old.kill()
	<-old.exited
	fail := func(err error) error {
		p.mu.Lock()
		p.swapping = false
		p.mu.Unlock()
		p.finish(err)
		return err
	}
	select {
	case <-p.closed:
		return fail(fmt.Errorf("the agent was stopped"))
	default:
	}
	s.mu.Lock()
	sp, agent := s.spaceOf(p.env), p.env.agent(p.name)
	var next *incarnation
	err := fmt.Errorf("the agent no longer exists")
	if sp != nil && agent != nil {
		agent.Fresh = agent.Fresh || fresh
		next, err = s.spawn(sp, p.env, agent, cols, rows, &want)
	}
	s.mu.Unlock()
	if err != nil {
		return fail(err)
	}
	what := "with " + next.profile.String()
	if fresh {
		what += " and a new conversation"
	}
	p.collect([]byte("\r\n\x1b[2m— FarshMac restarted this agent " + what + " —\x1b[0m\r\n"))
	p.mu.Lock()
	stopped := false
	select {
	case <-p.closed:
		stopped = true
	default:
	}
	p.run, p.swapping, p.current = next, false, next.profile
	p.hooked, p.turn, p.asking, p.head, p.trusted = false, false, false, nil, false
	if p.cols != cols || p.rows != rows { // the window was resized meanwhile
		pty.Setsize(next.pty, &pty.Winsize{Cols: uint16(p.cols), Rows: uint16(p.rows)})
	}
	p.mu.Unlock()
	go p.read(next)
	if stopped {
		next.kill() // stopped while it was starting; read ends the console
		return fmt.Errorf("the agent was stopped")
	}
	return nil
}
