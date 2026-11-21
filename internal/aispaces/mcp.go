package aispaces

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// GatewayFlag makes the executable a stdio MCP gateway: the agent CLI starts
// it, and it forwards the JSON-RPC stream to the Env-scoped server in the
// running application. The gateway itself has no access to sources.
const GatewayFlag = "--ai-mcp-gateway"

const (
	socketEnv = "FARSHMAC_AI_SOCKET"
	tokenEnv  = "FARSHMAC_AI_TOKEN"
)

func GatewayMain() int {
	conn, err := net.Dial("unix", os.Getenv(socketEnv))
	if err != nil {
		fmt.Fprintln(os.Stderr, "FarshMac AI Spaces is not reachable:", err)
		return 1
	}
	defer conn.Close()
	fmt.Fprintln(conn, os.Getenv(tokenEnv))
	go func() {
		io.Copy(conn, os.Stdin)
		conn.Close()
	}()
	io.Copy(os.Stdout, conn)
	return 0
}

// mcpServer serves every Env of the store on one private socket. A
// connection is bound to an Env by the launch token, never by an argument.
type mcpServer struct {
	s      *Store
	socket string
	mu     sync.Mutex
	tokens map[string]*mcpSession
	logMu  sync.Mutex
	// operator is the token of the user's own agents outside any Env.
	operator string
}

// startMCP must be called with s.mu held.
func (s *Store) startMCP() (*mcpServer, error) {
	if s.mcp != nil {
		return s.mcp, nil
	}
	socket := filepath.Join(os.TempDir(), fmt.Sprintf("farshmac-ai-%d.sock", os.Getpid()))
	os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	os.Chmod(socket, 0600)
	m := &mcpServer{s: s, socket: socket, tokens: map[string]*mcpSession{}, operator: newID() + newID() + newID()}
	s.mcp = m
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go m.serve(conn)
		}
	}()
	return m, nil
}

// mcpSession is what a launch token stands for.
type mcpSession struct {
	env            *Env
	agent, session string // agent entry and its native session reference
}

func (m *mcpServer) register(e *Env, agent, session string) string {
	token := newID() + newID()
	m.mu.Lock()
	m.tokens[token] = &mcpSession{e, agent, session}
	m.mu.Unlock()
	return token
}
func (m *mcpServer) revoke(token string) {
	m.mu.Lock()
	delete(m.tokens, token)
	m.mu.Unlock()
}

// LogName is the per-Env MCP interaction log, one JSON object per line.
const LogName = "mcp-log.jsonl"

// LogPath is where the MCP interaction log of an Env is kept.
func (s *Store) LogPath(space, env string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, e, err := s.find(space, env); err == nil && e != nil {
		return filepath.Join(s.envDir(e), LogName)
	}
	return ""
}

// logEntry describes one MCP message. It is meant for judging how well the
// tool API serves an agent: call order, think time between calls, payload
// sizes, errors and retries. Large values are stored as a head plus a size.
type logEntry struct {
	Time     string         `json:"time"`
	Agent    string         `json:"agent"`
	Session  string         `json:"session,omitempty"`   // agent-native session reference
	Conn     string         `json:"conn"`                // one gateway connection
	Seq      int            `json:"seq"`                 // message number on the connection
	Event    string         `json:"event,omitempty"`     // connect / disconnect
	Method   string         `json:"method,omitempty"`    // JSON-RPC method
	Tool     string         `json:"tool,omitempty"`      // tools/call name
	Args     map[string]any `json:"args,omitempty"`      // tool arguments, long strings shortened
	ArgBytes int            `json:"arg_bytes,omitempty"` // full size of the request line
	GapMS    int64          `json:"gap_ms"`              // since the previous message: agent think time
	MS       float64        `json:"ms"`                  // server time for this request
	IsError  bool           `json:"is_error,omitempty"`  // tool reported an error
	RPCError string         `json:"rpc_error,omitempty"` // protocol-level error
	OutBytes int            `json:"out_bytes,omitempty"` // size of the returned text
	OutLines int            `json:"out_lines,omitempty"` // lines in the returned text
	Out      string         `json:"out,omitempty"`       // head of the returned text
	Client   any            `json:"client,omitempty"`    // initialize: client name and version
	Protocol string         `json:"protocol,omitempty"`  // initialize: requested protocol version
}

func shorten(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && s[cut]&0xC0 == 0x80 { // do not split a UTF-8 sequence
		cut--
	}
	return fmt.Sprintf("%s…[%d bytes total]", s[:cut], len(s))
}

func (m *mcpServer) log(e *Env, entry logEntry) { m.logTo(m.s.envDir(e), LogName, entry) }

func (m *mcpServer) logTo(dir, name string, entry logEntry) {
	entry.Time = time.Now().Format("2006-01-02T15:04:05.000Z07:00")
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	m.logMu.Lock()
	defer m.logMu.Unlock()
	if os.MkdirAll(dir, 0700) != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	f.Write(append(line, '\n'))
	f.Close()
}

type rpcRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (m *mcpServer) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReaderSize(conn, 1<<20)
	line, err := r.ReadString('\n')
	if err != nil {
		return
	}
	token := strings.TrimSpace(line)
	if token == m.operator {
		m.serveOperator(r, conn)
		return
	}
	m.mu.Lock()
	session := m.tokens[token]
	m.mu.Unlock()
	if session == nil {
		return
	}
	env := session.env
	fs := &FS{s: m.s, env: env}
	out := json.NewEncoder(conn)
	base := logEntry{Agent: session.agent, Session: session.session, Conn: newID()[:8]}
	seq, last := 0, time.Now()
	record := func(e logEntry) {
		seq++
		e.Agent, e.Session, e.Conn, e.Seq = base.Agent, base.Session, base.Conn, seq
		e.GapMS = time.Since(last).Milliseconds()
		last = time.Now()
		m.log(env, e)
	}
	record(logEntry{Event: "connect"})
	defer func() { record(logEntry{Event: "disconnect"}) }()
	for {
		raw, err := r.ReadBytes('\n')
		if len(raw) > 0 {
			var req rpcRequest
			if json.Unmarshal(raw, &req) == nil && req.Method != "" {
				entry := logEntry{Method: req.Method, ArgBytes: len(raw)}
				started := time.Now()
				if len(req.ID) == 0 {
					record(entry) // a notification: nothing to answer
				} else {
					// Authority is checked on every call: a stopped agent loses access.
					m.mu.Lock()
					live := m.tokens[token] != nil
					m.mu.Unlock()
					reply := map[string]any{"jsonrpc": "2.0", "id": req.ID}
					if !live {
						reply["error"] = map[string]any{"code": -32000, "message": "permission_denied: session revoked"}
						entry.RPCError = "permission_denied: session revoked"
					} else if result, rpcErr := handle(m.s, session, fs, req); rpcErr != nil {
						reply["error"] = rpcErr
						entry.RPCError = fmt.Sprint(rpcErr["message"])
					} else {
						reply["result"] = result
						describe(&entry, req, result)
					}
					entry.MS = float64(time.Since(started).Microseconds()) / 1000
					record(entry)
					if out.Encode(reply) != nil {
						return
					}
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// describe fills the log entry with what the request asked and returned.
func describe(entry *logEntry, req rpcRequest, result any) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
			ClientInfo      any    `json:"clientInfo"`
		}
		json.Unmarshal(req.Params, &p)
		entry.Protocol, entry.Client = p.ProtocolVersion, p.ClientInfo
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		json.Unmarshal(req.Params, &p)
		entry.Tool, entry.Args = p.Name, map[string]any{}
		for name, value := range p.Arguments {
			if s, ok := value.(string); ok {
				value = shorten(s, 400)
			}
			entry.Args[name] = value
		}
		if r, ok := result.(map[string]any); ok {
			entry.IsError, _ = r["isError"].(bool)
			if content, ok := r["content"].([]any); ok && len(content) > 0 {
				text, _ := content[0].(map[string]any)["text"].(string)
				entry.OutBytes, entry.OutLines = len(text), strings.Count(text, "\n")+1
				entry.Out = shorten(text, 600)
			}
		}
	}
}

func handle(s *Store, session *mcpSession, fs *FS, req rpcRequest) (any, map[string]any) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		return map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "farshmac-ai-env", "version": "0.1-poc"},
			"instructions":    serverInstructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, map[string]any{"code": -32602, "message": "arguments must be strings: " + err.Error()}
		}
		args := map[string]string{}
		for name, value := range p.Arguments {
			if text, ok := value.(string); ok {
				args[name] = text
			} else {
				args[name] = fmt.Sprint(value) // a number where a string was expected
			}
		}
		text, err := call(s, session, fs, p.Name, args, p.Arguments)
		if err != nil {
			text = strings.ReplaceAll(err.Error(), fs.private("/"), "")
		}
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": err != nil}, nil
	}
	return nil, map[string]any{"code": -32601, "message": "method not found: " + req.Method}
}
