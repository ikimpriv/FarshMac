package aispaces

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The operator is an agent the user runs on the computer itself, outside any
// Env (Codex, Claude Code). It speaks for the user: it sees every Space,
// watches the agents and writes to them. It has no file tools; it already has
// the user's disk.
//
// OperatorFlag makes the executable its stdio MCP server. The protocol is
// answered by the gateway itself, so the server is listed even while FarshMac
// is not running; only tool calls go to the running application, found anew
// after every FarshMac restart through the operator file.

const OperatorFlag = "--ai-mcp-operator"

// operatorEnv is the FromEnv of a message sent by the operator.
const operatorEnv = "operator"

const operatorFile = "operator.json"

type operatorEndpoint struct{ Socket, Token string }

// operatorNote is an agent's reply waiting to be read by the operator.
type operatorNote struct{ ID, From, Time, Text string }

// Serve starts the MCP socket and publishes the operator endpoint. The file
// is readable by the user only, and the sandbox hides it from Env agents.
func (s *Store) Serve() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.startMCP()
	if err != nil {
		return err
	}
	data, _ := json.Marshal(operatorEndpoint{m.socket, m.operator})
	return os.WriteFile(filepath.Join(s.root, operatorFile), data, 0600)
}

// OperatorCommand is what a host agent is configured to start.
func OperatorCommand() []string {
	self, _ := os.Executable()
	return []string{self, OperatorFlag}
}

func OperatorMain() int {
	root, err := os.UserConfigDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	operatorGateway(filepath.Join(root, "FarshMac", "ai-spaces"), os.Stdin, os.Stdout)
	return 0
}

const operatorInstructions = "This server lets you work, on the user's behalf, with the agents of FarshMac AI Spaces. " +
	"An AI Space holds Envs (roles such as Product, Architect, Coder), each with its own agents and its own view of the project. " +
	"spaces_list shows them and their state. agent_status shows what an agent is doing and what it said last. " +
	"agent_send writes to an agent (a stopped agent is started); agent_wait blocks until it answers or stops working, so do not poll agent_status in a loop. " +
	"Agents answer you with their own reply tool; anything else they print is visible only through agent_status. " +
	"A reply may list attachments and changed files instead of quoting them: read an attachment with message_read, or open the named file yourself if you have disk access. Do not ask an agent to paste a file. " +
	"Respect the roles: ask the agent whose job it is instead of doing its work yourself."

const agentHelp = "Env/agent, single-agent Env, or unique agent name"
const spaceHelp = "Space name; optional if only one exists"

var operatorTools = []tool{
	{"artifact_read", "Read a delivered artifact: summary, file or run.", object([]string{"id"}, prop{"space", "string", spaceHelp}, prop{"id", "string", "Artifact ID"}, prop{"file", "string", "Relative file path"}, prop{"run", "string", "Recorded run ID for details"}, prop{"part", "string", "With run: command or output for paged detail; omit for compact result"}, prop{"offset", "integer", "First file line or run-part byte, default 1"}, prop{"limit", "integer", "Up to 300 file lines or 8192 run-part bytes"})},
	{"spaces_list", "List AI Spaces with their Envs, agents, agent states, attached folders and who may contact whom.", object(nil,
		prop{"space", "string", "Only this AI Space. Default: all"})},
	{"agent_status", "Agent state, unread replies and bounded recent conversation.", object([]string{"agent"},
		prop{"space", "string", spaceHelp},
		prop{"agent", "string", agentHelp},
		prop{"chars", "integer", "How much of the recent conversation to return, 500 to 20000 characters. Default 3000"},
		prop{"screen", "boolean", "Also return the last lines of the agent's console, for example to see a question it is stuck on. Default false"})},
	{"agent_send", "Send a message to an agent as the user. A stopped agent is started. The agent answers with its reply tool; read the answer with wait or agent_wait.", object([]string{"agent", "message"},
		prop{"space", "string", spaceHelp},
		prop{"agent", "string", agentHelp},
		prop{"message", "string", "Self-contained: the agent does not see your conversation"},
		prop{"wait", "integer", "Seconds to wait for the answer in this same call, 0 to 600. Default 0: return at once"},
		prop{"complexity", "string", "How hard the task is: simple, moderate or complex. The agent's model and effort are set to match. Omit to leave them as they are"},
		prop{"model", "string", "Model from the CLI catalog or user profiles (agent_status lists choices). Overrides complexity"},
		prop{"effort", "string", "Its effort from this message on: low, medium, high (Claude also xhigh, max). Overrides complexity"},
		prop{"method", "string", "direct (default), workflow (delegation and review; much higher cost), research (cited web research)"},
		prop{"new_task", "boolean", "Clear context for an independent task only; default false."})},
	{"message_read", "Read a message thread or a bounded section of an attachment.", object([]string{"id"},
		prop{"space", "string", spaceHelp},
		prop{"id", "string", "Message number N from a reply header"},
		prop{"attachment", "integer", "Attachment number from the reply. Omit to read the thread itself"},
		prop{"section", "string", "Return only this section (a heading) of the attachment"},
		prop{"offset", "integer", "First line to return. Default 1"},
		prop{"limit", "integer", "Number of lines to return. Default: all"})},
	{"agent_wait", "Wait for a reply or idle state; returns replies and recent output.", object([]string{"agent"},
		prop{"space", "string", spaceHelp},
		prop{"agent", "string", agentHelp},
		prop{"seconds", "integer", "Longest time to wait, 1 to 600. Default 45. Keep it below your own tool timeout"})},
}

// operatorGateway serves MCP on in/out and relays tool calls to FarshMac.
func operatorGateway(root string, in io.Reader, out io.Writer) {
	r := bufio.NewReaderSize(in, 1<<20)
	enc := json.NewEncoder(out)
	client := "an assistant"
	var conn net.Conn
	var replies *bufio.Reader
	forward := func(raw []byte) ([]byte, error) {
		var err error
		for attempt := 0; attempt < 2; attempt++ {
			if conn == nil {
				var data []byte
				var ep operatorEndpoint
				if data, err = os.ReadFile(filepath.Join(root, operatorFile)); err != nil {
					continue
				}
				json.Unmarshal(data, &ep)
				if conn, err = net.Dial("unix", ep.Socket); err != nil {
					conn = nil
					continue
				}
				fmt.Fprintf(conn, "%s\n%s\n", ep.Token, client)
				replies = bufio.NewReaderSize(conn, 1<<20)
			}
			var line []byte
			if _, err = conn.Write(raw); err == nil {
				if line, err = replies.ReadBytes('\n'); err == nil {
					return line, nil
				}
			}
			conn.Close() // FarshMac was restarted: look it up again
			conn = nil
		}
		return nil, err
	}
	for {
		raw, readErr := r.ReadBytes('\n')
		var req rpcRequest
		if json.Unmarshal(raw, &req) == nil && req.Method != "" && len(req.ID) > 0 {
			reply := map[string]any{"jsonrpc": "2.0", "id": req.ID}
			switch req.Method {
			case "initialize":
				var p struct {
					ProtocolVersion string `json:"protocolVersion"`
					ClientInfo      struct {
						Name string `json:"name"`
					} `json:"clientInfo"`
				}
				json.Unmarshal(req.Params, &p)
				if p.ProtocolVersion == "" {
					p.ProtocolVersion = "2025-06-18"
				}
				if name := strings.Join(strings.Fields(p.ClientInfo.Name), " "); name != "" {
					client = name
				}
				reply["result"] = map[string]any{
					"protocolVersion": p.ProtocolVersion,
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "farshmac-ai-spaces", "version": "0.1-poc"},
					"instructions":    operatorInstructions,
				}
			case "ping":
				reply["result"] = map[string]any{}
			case "tools/list":
				reply["result"] = map[string]any{"tools": operatorTools}
			case "tools/call":
				if !strings.HasSuffix(string(raw), "\n") {
					raw = append(raw, '\n')
				}
				if line, err := forward(raw); err == nil {
					out.Write(line)
					continue
				}
				reply["result"] = toolResult("FarshMac is not running, so its AI Spaces cannot be reached. Ask the user to start FarshMac, then try again.", true)
			default:
				reply["error"] = map[string]any{"code": -32601, "message": "method not found: " + req.Method}
			}
			enc.Encode(reply)
		}
		if readErr != nil {
			return
		}
	}
}

func toolResult(text string, failed bool) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": failed}
}

// serveOperator answers the tool calls of one operator gateway.
func (m *mcpServer) serveOperator(r *bufio.Reader, conn net.Conn) {
	line, err := r.ReadString('\n')
	if err != nil {
		return
	}
	client := strings.TrimSpace(line)
	out := json.NewEncoder(conn)
	id, seq, last := newID()[:8], 0, time.Now()
	for {
		raw, err := r.ReadBytes('\n')
		var req rpcRequest
		if json.Unmarshal(raw, &req) == nil && req.Method == "tools/call" {
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			json.Unmarshal(req.Params, &p)
			args := map[string]string{}
			for name, value := range p.Arguments {
				if text, ok := value.(string); ok {
					args[name] = text
				} else {
					args[name] = fmt.Sprint(value)
				}
			}
			seq++
			entry := logEntry{Agent: client, Conn: id, Seq: seq, ArgBytes: len(raw), GapMS: time.Since(last).Milliseconds()}
			started := time.Now()
			text, callErr := m.s.operatorCall(client, p.Name, args)
			if callErr != nil {
				text = callErr.Error()
			}
			result := toolResult(text, callErr != nil)
			describe(&entry, req, result)
			entry.MS = float64(time.Since(started).Microseconds()) / 1000
			last = time.Now()
			m.logTo(m.s.root, OperatorLogName, entry)
			if out.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}) != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// OperatorLogName is the log of operator tool calls, in the store root.
const OperatorLogName = "operator-log.jsonl"

func (s *Store) OperatorLogPath() string { return filepath.Join(s.root, OperatorLogName) }

func (s *Store) operatorCall(client, name string, a map[string]string) (string, error) {
	if name == "spaces_list" {
		return s.overview(a["space"])
	}
	if name == "artifact_read" {
		s.mu.Lock()
		sp, err := s.operatorSpace(a["space"])
		s.mu.Unlock()
		if err != nil {
			return "", err
		}
		raw := map[string]any{"id": a["id"], "file": a["file"], "run": a["run"], "offset": number(a, "offset", 1, 1, 100000), "limit": number(a, "limit", 160, 1, 300)}
		return readOperatorArtifact(s, sp, raw)
	}
	if name == "message_read" {
		s.mu.Lock()
		sp, err := s.operatorSpace(a["space"])
		s.mu.Unlock()
		if err != nil {
			return "", err
		}
		return s.readThread(sp, nil, a)
	}
	s.mu.Lock()
	sp, err := s.operatorSpace(a["space"])
	var e *Env
	var agent string
	if err == nil {
		e, agent, err = operatorAgent(sp, a["agent"])
	}
	s.mu.Unlock()
	if err != nil {
		return "", err
	}
	switch name {
	case "agent_status":
		return s.status(sp, e, agent, number(a, "chars", 3000, 500, 20000), a["screen"] == "true"), nil
	case "agent_send":
		sent, err := s.operatorSend(sp, e, agent, client, a["message"], directive{level: strings.TrimSpace(a["complexity"]), model: strings.TrimSpace(a["model"]), effort: strings.TrimSpace(a["effort"]), fresh: a["new_task"] == "true", method: strings.TrimSpace(a["method"])})
		if err != nil {
			return "", err
		}
		if wait := number(a, "wait", 0, 0, 600); wait > 0 {
			return sent + "\n\n" + s.wait(sp, e, agent, wait), nil
		}
		return sent + "\nCall agent_wait to get the answer.", nil
	case "agent_wait":
		return s.wait(sp, e, agent, number(a, "seconds", 45, 1, 600)), nil
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

// operatorSpace must be called with s.mu held.
func (s *Store) operatorSpace(name string) (*Space, error) {
	name = strings.TrimSpace(name)
	if name == "" && len(s.Spaces) == 1 {
		return s.Spaces[0], nil
	}
	var names []string
	for _, sp := range s.Spaces {
		if strings.EqualFold(sp.Name, name) {
			return sp, nil
		}
		names = append(names, strconv.Quote(sp.Name))
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("not_found: there are no AI Spaces yet; the user creates them in FarshMac")
	}
	return nil, fmt.Errorf("not_found: no AI Space %q; there are: %s", name, strings.Join(names, ", "))
}

// operatorAgent accepts what resolve does, and a bare agent name that only
// one Env of the Space has.
func operatorAgent(sp *Space, ref string) (*Env, string, error) {
	e, agent, err := resolve(sp, ref)
	if err == nil {
		return e, agent, nil
	}
	if strings.Contains(ref, "/") {
		return nil, "", err
	}
	var matches []string
	for _, x := range sp.Envs {
		if a := x.agent(strings.TrimSpace(ref)); a != nil && !a.Disabled {
			e, agent = x, a.Name
			matches = append(matches, address(x, a.Name))
		}
	}
	switch len(matches) {
	case 0:
		return nil, "", fmt.Errorf("%v; call spaces_list for the Envs and agents of this Space", err)
	case 1:
		return e, agent, nil
	}
	return nil, "", fmt.Errorf("ambiguous: several Envs have an agent %q: %s", ref, strings.Join(matches, ", "))
}

// state must be called with s.mu held.
func (s *Store) state(e *Env, agent string) string {
	p := s.running[e.ID+":"+agent]
	if p == nil {
		return "not running (a message starts it)"
	}
	if busy, quiet := p.busy(); !busy {
		return "idle for " + quiet.Round(time.Second).String()
	}
	return "working"
}

func (s *Store) overview(space string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	spaces := s.Spaces
	if strings.TrimSpace(space) != "" {
		sp, err := s.operatorSpace(space)
		if err != nil {
			return "", err
		}
		spaces = []*Space{sp}
	}
	if len(spaces) == 0 {
		return "There are no AI Spaces yet; the user creates them in FarshMac.", nil
	}
	var b strings.Builder
	for _, sp := range spaces {
		fmt.Fprintf(&b, "AI Space %q\n", sp.Name)
		for _, e := range sp.Envs {
			var peers []string
			for _, id := range e.Peers {
				if other := sp.envByID(id); other != nil {
					peers = append(peers, other.Name)
				}
			}
			sort.Strings(peers)
			if len(peers) == 0 {
				peers = []string{"nobody"}
			}
			fmt.Fprintf(&b, "  Env %s (its agents may contact: %s)\n", e.Name, strings.Join(peers, ", "))
			for _, a := range e.Agents {
				if a.Disabled {
					fmt.Fprintf(&b, "    agent %s: disabled by the user\n", address(e, a.Name))
					continue
				}
				fmt.Fprintf(&b, "    agent %s: %s\n", address(e, a.Name), s.state(e, a.Name))
			}
			for _, bind := range e.Bindings {
				mode := "read/write"
				if bind.ReadOnly {
					mode = "read-only"
				}
				fmt.Fprintf(&b, "    folder %s = %s (%s)\n", bind.Virtual, bind.Source, mode)
			}
		}
	}
	return b.String(), nil
}

func (s *Store) operatorSend(sp *Space, e *Env, agent, client, text string, d directive) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("the message is empty")
	}
	if err := d.check(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sp.NextMessage++
	id := strconv.Itoa(sp.NextMessage)
	if sp.Requests == nil {
		sp.Requests = map[string]request{}
	}
	sp.Requests[id] = request{operatorEnv, client, e.ID, agent}
	recipient := address(e, agent)
	// The user's own agent speaks for the user: it may direct anyone.
	allowed, prefix, note, wrap, err := s.directed(sp, nil, e, agent, id, d)
	if err != nil {
		delete(sp.Requests, id)
		sp.NextMessage--
		return "", err
	}
	delivered := s.direct(e, agent, wrap(fmt.Sprintf(prefix+"[FarshMac message #%s from the user, sent through %s]\n%s\n\n[When you have finished what it asks, report with the agent_reply tool, request_id %s: only that reaches the user. Attach files instead of pasting their text. You may reply more than once, for example with a question.]", id, client, text, id)), d, allowed)
	s.store(sp, &message{Kind: "message", ID: id, From: "user/" + client, To: recipient, FromEnv: operatorEnv, ToEnv: e.ID, Delivered: delivered, Text: text})
	if err := s.save(); err != nil {
		return "", err
	}
	if !delivered {
		return fmt.Sprintf("Message #%s to %s is queued: the agent could not be started, it gets the message when the user starts it in FarshMac.", id, recipient), nil
	}
	return fmt.Sprintf("Message #%s delivered to %s.%s", id, recipient, note), nil
}

// takeNotes returns and removes the replies of one agent to the operator. It
// must be called with s.mu held.
func (s *Store) takeNotes(sp *Space, from string) string {
	var b strings.Builder
	kept := sp.Operator[:0:0]
	for _, n := range sp.Operator {
		if n.From != from {
			kept = append(kept, n)
			continue
		}
		fmt.Fprintf(&b, "[Reply to #%s from %s at %s]\n%s\n\n", n.ID, n.From, n.Time, n.Text)
	}
	if b.Len() > 0 {
		sp.Operator = kept
		s.save()
	}
	return b.String()
}

// wait returns when the agent has replied to the operator, has stopped
// working, or the time is up.
func (s *Store) wait(sp *Space, e *Env, agent string, seconds int) string {
	who := address(e, agent)
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	for {
		s.mu.Lock()
		notes := s.takeNotes(sp, who)
		p := s.running[e.ID+":"+agent]
		s.mu.Unlock()
		switch {
		case notes != "":
			return notes + who + " is " + s.stateOf(e, agent) + "."
		case p == nil:
			return who + " is not running and left no reply.\n" + s.recent(e, agent, 2000)
		}
		if busy, _ := p.busy(); !busy {
			return who + " stopped working without replying to you. It may have finished, or it waits for the user at its console (use agent_status with screen). What it said last:\n" + s.recent(e, agent, 2000)
		}
		if time.Now().After(deadline) {
			return fmt.Sprintf("%s is still working after %d s. Call agent_wait again.", who, seconds)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (s *Store) stateOf(e *Env, agent string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state(e, agent)
}

func (s *Store) recent(e *Env, agent string, chars int) string {
	if agent == "codex" {
		if file := latestCodexTranscript(filepath.Join(s.agentDir(e, agent), "scratch")); file != "" {
			if text := codexConversation(file, chars); text != "" {
				return text
			}
		}
	}
	s.mu.Lock()
	session := ""
	if a := e.agent(agent); a != nil {
		session = a.SessionID
	}
	s.mu.Unlock()
	if text := transcriptTail(session, chars); text != "" {
		return text
	}
	return "(no conversation is recorded for this agent yet)"
}

func (s *Store) status(sp *Space, e *Env, agent string, chars int, screen bool) string {
	who := address(e, agent)
	s.mu.Lock()
	state := s.state(e, agent)
	choices := ""
	if agentConfig := e.agent(agent); agentConfig != nil {
		choices = agentConfig.choices()
	}
	notes := s.takeNotes(sp, who)
	p := s.running[e.ID+":"+agent]
	var open []string
	for id, r := range sp.Requests {
		if r.ToEnv == e.ID && r.ToAgent == agent && r.FromEnv != operatorEnv {
			if asker := sp.envByID(r.FromEnv); asker != nil {
				open = append(open, fmt.Sprintf("#%s from %s", id, address(asker, r.FromAgent)))
			}
		}
	}
	s.mu.Unlock()
	var b strings.Builder
	fmt.Fprintf(&b, "%s in AI Space %q: %s\n", who, sp.Name, state)
	fmt.Fprintf(&b, "Settings: %s\n", choices)
	if agent == "codex" {
		file := latestCodexTranscript(filepath.Join(s.agentDir(e, agent), "scratch"))
		model, effort := codexObserved(file)
		if model != "" {
			fmt.Fprintf(&b, "Last observed CLI turn: %s / %s (may precede a pending request)\n", model, effort)
		} else {
			b.WriteString("Observed model/effort: unavailable until the CLI records a turn\n")
		}
	}
	if len(open) > 0 {
		sort.Strings(open)
		fmt.Fprintf(&b, "Threads with other agents where it is expected to answer: %s\n", strings.Join(open, ", "))
	}
	if notes != "" {
		b.WriteString("\nUnread replies to you:\n" + notes)
	}
	b.WriteString("\nRecent conversation:\n" + s.recent(e, agent, chars) + "\n")
	if screen {
		if p == nil {
			b.WriteString("\nThe agent is not running, so it has no console.\n")
		} else {
			b.WriteString("\nConsole, last lines:\n" + p.screen(40) + "\n")
		}
	}
	return b.String()
}

// transcriptTail renders the end of a Claude Code session transcript:
// prompts, answers and tool calls, without tool output.
func transcriptTail(session string, chars int) string {
	if session == "" {
		return ""
	}
	home, _ := os.UserHomeDir()
	files, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", session+".jsonl"))
	if len(files) == 0 {
		return ""
	}
	f, err := os.Open(files[0])
	if err != nil {
		return ""
	}
	defer f.Close()
	const window = 1 << 20
	if info, err := f.Stat(); err == nil && info.Size() > window {
		f.Seek(-window, io.SeekEnd)
	}
	data, _ := io.ReadAll(f)
	var lines []string
	for _, raw := range strings.Split(string(data), "\n") {
		var rec struct {
			Type    string `json:"type"`
			Time    string `json:"timestamp"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(raw), &rec) != nil || (rec.Type != "user" && rec.Type != "assistant") {
			continue
		}
		who := "PROMPT"
		if rec.Type == "assistant" {
			who = "AGENT"
		}
		if len(rec.Time) >= 19 {
			who = rec.Time[11:19] + "Z " + who
		}
		var text string
		if json.Unmarshal(rec.Message.Content, &text) == nil {
			if strings.TrimSpace(text) != "" {
				lines = append(lines, who+": "+shorten(strings.TrimSpace(text), 1500))
			}
			continue
		}
		var parts []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}
		json.Unmarshal(rec.Message.Content, &parts)
		for _, part := range parts {
			switch part.Type {
			case "text":
				if strings.TrimSpace(part.Text) != "" {
					lines = append(lines, who+": "+shorten(strings.TrimSpace(part.Text), 1500))
				}
			case "tool_use":
				lines = append(lines, "  tool "+part.Name+" "+shorten(string(part.Input), 160))
			}
		}
	}
	// Keep whole entries from the end, as many as fit.
	size, first := 0, len(lines)
	for first > 0 && size+len(lines[first-1]) < chars {
		first--
		size += len(lines[first]) + 1
	}
	if first == len(lines) && first > 0 {
		first-- // one entry longer than the limit: show its end
		last := lines[first]
		for cut := len(last) - chars; cut < len(last); cut++ {
			if cut >= 0 && last[cut]&0xC0 != 0x80 {
				lines[first] = "…" + last[cut:]
				break
			}
		}
	}
	return strings.Join(lines[first:], "\n")
}
