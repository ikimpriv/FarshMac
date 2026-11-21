package aispaces

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
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

// Process is a running agent with its PTY. It satisfies shellsession.Stream.
type Process struct {
	cmd    *exec.Cmd
	pty    *os.File
	out    chan []byte
	done   chan struct{}
	err    error
	once   sync.Once
	typing sync.Mutex
	closed chan struct{}

	// Output is buffered here, so an agent nobody is watching keeps running
	// and its console can be opened later.
	mu       sync.Mutex
	backlog  [][]byte
	held     int
	more     chan struct{}
	eof      bool
	last     time.Time // last output, to tell when the CLI is idle at its prompt
	head     []byte    // first output, searched for the folder trust question
	trusted  bool
	attached bool

	// For whoever asks whether the agent has finished its turn.
	pending   int       // messages accepted but not typed in yet
	delivered time.Time // when the last message was typed in
	tail      []byte    // recent output, for a look at the console
}

// busy reports whether the agent is working, and for how long its console
// has been quiet. A CLI in a turn keeps redrawing; at its prompt it is silent.
func (p *Process) busy() (bool, time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	quiet := time.Since(p.last)
	if p.last.IsZero() {
		return true, 0 // still starting
	}
	return p.pending > 0 || quiet < 3*time.Second || time.Since(p.delivered) < 4*time.Second, quiet
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

func (p *Process) Write(data []byte) error { _, err := p.pty.Write(data); return err }
func (p *Process) Resize(cols, rows int) error {
	return pty.Setsize(p.pty, &pty.Winsize{Cols: uint16(max(cols, 20)), Rows: uint16(max(rows, 5))})
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

// Close stops the agent and everything it started.
func (p *Process) Close() {
	p.once.Do(func() {
		close(p.closed)
		if p.cmd.Process != nil {
			syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
			syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		}
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
		if strings.HasPrefix(name, "CLAUDE_CODE_") || name == "CLAUDECODE" || name == "CLAUDE_PID" || name == "CLAUDE_EFFORT" {
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
  workspace_pull, work on the copy here, then workspace_push.

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
	conn, err := net.Dial("unix", os.Getenv(socketEnv))
	if err != nil {
		return 0 // FarshMac is gone; nothing to tell
	}
	defer conn.Close()
	fmt.Fprintf(conn, "%s\n{\"jsonrpc\":\"2.0\",\"method\":\"farshmac/%s\"}\n", os.Getenv(tokenEnv), event)
	return 0
}

// claudeArgs binds a Claude Code process to its Env: the MCP server and
// nothing else, its tools loaded at once rather than searched for, and hooks
// that report a compacted or cleared conversation.
func claudeArgs(agent *Agent, self, dir string, gatewayEnv map[string]string) ([]string, []string, error) {
	config, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"farshmac": map[string]any{
		"command": self, "args": []string{GatewayFlag}, "env": gatewayEnv, "alwaysLoad": true}}})
	mcpFile := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(mcpFile, config, 0600); err != nil {
		return nil, nil, err
	}
	forget := map[string]any{"type": "command", "command": "'" + strings.ReplaceAll(self, "'", `'\''`) + "' " + EventFlag + " forget"}
	settings, _ := json.Marshal(map[string]any{"hooks": map[string]any{
		"SessionStart": []any{
			map[string]any{"matcher": "compact", "hooks": []any{forget}},
			map[string]any{"matcher": "clear", "hooks": []any{forget}},
		},
		"PostCompact": []any{map[string]any{"hooks": []any{forget}}},
	}})
	settingsFile := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsFile, settings, 0600); err != nil {
		return nil, nil, err
	}
	args := []string{"--mcp-config", mcpFile, "--strict-mcp-config", "--allowedTools", "mcp__farshmac", "--settings", settingsFile}
	switch agent.Tools {
	case "":
		args = append(args, "--tools", DefaultTools)
	case "default":
	default:
		args = append(args, "--tools", agent.Tools)
	}
	if agent.Model != "" {
		args = append(args, "--model", agent.Model)
	}
	if agent.Effort != "" {
		args = append(args, "--effort", agent.Effort)
	}
	return args, []string{mcpFile, settingsFile}, nil
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
	var err error
	var agent *Agent
	for i := range e.Agents {
		if e.Agents[i].Name == name {
			agent = &e.Agents[i]
		}
	}
	if agent == nil {
		return nil, fmt.Errorf("agent %s is not configured in this Env", name)
	}
	key := e.ID + ":" + name
	if s.running[key] != nil {
		return nil, fmt.Errorf("%s is already running in another window", name)
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
	resume := false
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if gatewayExecutable != "" {
		self = gatewayExecutable
	}
	if name == "claude" {
		home, _ := os.UserHomeDir()
		stored, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", agent.SessionID+".jsonl"))
		resume = agent.SessionID != "" && len(stored) > 0
		if !resume {
			agent.SessionID = uuid()
		}
	}
	token := server.register(e, name, agent.SessionID)
	gatewayEnv := map[string]string{socketEnv: server.socket, tokenEnv: token}
	var args, files []string
	switch name {
	case "claude":
		if args, files, err = claudeArgs(agent, self, dir, gatewayEnv); err != nil {
			server.revoke(token)
			return nil, err
		}
		if resume {
			args = append(args, "--resume", agent.SessionID)
		} else {
			args = append(args, "--session-id", agent.SessionID)
		}
	case "codex":
		// Untested in the PoC: configuration overrides for a stdio MCP server.
		if agent.Started {
			args = []string{"resume", "--last"}
		}
		args = append(args,
			"-c", "mcp_servers.farshmac.command="+strconv.Quote(self),
			"-c", "mcp_servers.farshmac.args=["+strconv.Quote(GatewayFlag)+"]",
			"-c", "mcp_servers.farshmac.env={"+socketEnv+"="+strconv.Quote(server.socket)+","+tokenEnv+"="+strconv.Quote(token)+"}")
	default:
		return nil, fmt.Errorf("no adapter for %s", name)
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
	agent.Started = true
	inbox := agent.Inbox
	agent.Inbox = nil
	s.save()
	p := &Process{cmd: cmd, pty: file, out: make(chan []byte, 64), done: make(chan struct{}), closed: make(chan struct{}), more: make(chan struct{}, 1)}
	s.running[key] = p
	p.pending = len(inbox)
	if len(inbox) > 0 {
		go func() {
			p.idle() // let the CLI reach its prompt
			for _, text := range inbox {
				p.Deliver(text)
			}
		}()
	}
	go p.pump()
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := file.Read(buf)
			if n > 0 {
				p.collect(append([]byte(nil), buf[:n]...))
			}
			if err != nil {
				break
			}
		}
		err := cmd.Wait()
		file.Close()
		server.revoke(token)
		s.mu.Lock()
		if s.running[key] == p {
			delete(s.running, key)
		}
		s.mu.Unlock()
		select {
		case <-p.closed:
		default:
			p.err = err
		}
		p.mu.Lock()
		p.eof = true
		p.mu.Unlock()
		select {
		case p.more <- struct{}{}:
		default:
		}
	}()
	return p, nil
}
