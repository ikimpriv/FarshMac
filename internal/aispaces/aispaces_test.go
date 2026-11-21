package aispaces

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testEnv(t *testing.T) (*Store, *FS, string) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "project")
	os.MkdirAll(filepath.Join(source, "sub"), 0755)
	os.WriteFile(filepath.Join(source, "a.txt"), []byte("alpha\n"), 0644)
	os.WriteFile(filepath.Join(source, "sub", "b.txt"), []byte("beta\n"), 0644)
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CreateSpace("Space"); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateEnv("Space", "Dev", nil); err != nil {
		t.Fatal(err)
	}
	if err = s.Attach("Space", "Dev", "/", []string{source}, false); err != nil {
		t.Fatal(err)
	}
	fs, err := s.FS("Space", "Dev")
	if err != nil {
		t.Fatal(err)
	}
	return s, fs, source
}

func names(t *testing.T, fs *FS, dir string) string {
	t.Helper()
	list, err := fs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, info := range list {
		out = append(out, info.Name())
	}
	return strings.Join(out, ",")
}

func TestReferencesAreLiveAndShared(t *testing.T) {
	s, fs, source := testEnv(t)
	if got := names(t, fs, "/"); got != "project" {
		t.Fatalf("root = %q", got)
	}
	// Writes below a reference land in the real folder at once.
	if err := fs.WriteFile("/project/a.txt", []byte("changed\n")); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFile("/project/new.txt", []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if err := fs.Mkdir("/project/docs"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Rename("/project/new.txt", "/project/docs/renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Remove("/project/sub", true); err != nil {
		t.Fatal(err)
	}
	changed, _ := os.ReadFile(filepath.Join(source, "a.txt"))
	renamed, _ := os.ReadFile(filepath.Join(source, "docs", "renamed.txt"))
	if string(changed) != "changed\n" || string(renamed) != "new\n" {
		t.Fatalf("real folder not updated: %q %q", changed, renamed)
	}
	// Removing only hides the entry from this Env; the real folder keeps it.
	if _, err := os.Stat(filepath.Join(source, "sub", "b.txt")); err != nil {
		t.Fatalf("remove deleted a real file: %v", err)
	}
	if _, err := fs.Open("/project/sub/b.txt"); err == nil || names(t, fs, "/project") != "a.txt,docs" {
		t.Fatalf("removed entry still visible: %q", names(t, fs, "/project"))
	}
	// Another Env that references the same folder sees the same files.
	if err := s.CreateEnv("Space", "Other", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Attach("Space", "Other", "/", []string{source}, true); err != nil {
		t.Fatal(err)
	}
	other, _ := s.FS("Space", "Other")
	if got := names(t, other, "/project"); got != "a.txt,docs,sub" {
		t.Fatalf("other Env = %q", got)
	}
	// Attached read-only there, it can be read but not changed.
	if err := other.WriteFile("/project/from-other.txt", nil); err == nil || !strings.Contains(err.Error(), "read_only") {
		t.Fatalf("read-only reference accepted a write: %v", err)
	}
	if err := fs.WriteFile("/project/later.txt", nil); err != nil {
		t.Fatal(err)
	}
	if got := names(t, other, "/project"); got != "a.txt,docs,later.txt,sub" {
		t.Fatalf("other Env = %q", got)
	}
	// Hidden entries can be shown again.
	if got := fs.HiddenBelow("/project"); len(got) != 1 || got[0] != "/project/sub" {
		t.Fatalf("hidden = %q", got)
	}
	if err := fs.Restore("/project"); err != nil || names(t, fs, "/project") != "a.txt,docs,later.txt,sub" {
		t.Fatalf("restore: %q, %v", names(t, fs, "/project"), err)
	}
	// Paths outside the references are private to one Env.
	if err := fs.WriteFile("/notes.txt", []byte("private\n")); err != nil {
		t.Fatal(err)
	}
	if got := names(t, other, "/"); got != "project" {
		t.Fatalf("private file leaked: %q", got)
	}
	// The catalog survives a restart.
	reopened, err := Open(s.root)
	if err != nil || len(reopened.Spaces) != 1 || len(reopened.Spaces[0].Envs[0].Bindings) != 1 {
		t.Fatalf("catalog not persisted: %v", err)
	}
	// Removing the reference itself only detaches it.
	if err := fs.Remove("/project", true); err != nil {
		t.Fatal(err)
	}
	if got := names(t, fs, "/"); got != "notes.txt" {
		t.Fatalf("root after detach = %q", got)
	}
	if _, err := os.Stat(filepath.Join(source, "a.txt")); err != nil {
		t.Fatal("detach deleted the source")
	}
}

func TestMCPTools(t *testing.T) {
	s, fs, _ := testEnv(t)
	session := &mcpSession{env: s.Spaces[0].Envs[0], agent: "claude"}
	rpc := func(method, params string) map[string]any {
		result, rpcErr := handle(s, session, fs, rpcRequest{ID: json.RawMessage("1"), Method: method, Params: json.RawMessage(params)})
		if rpcErr != nil {
			t.Fatalf("%s: %v", method, rpcErr)
		}
		b, _ := json.Marshal(result)
		var m map[string]any
		json.Unmarshal(b, &m)
		return m
	}
	text := func(m map[string]any) string {
		return m["content"].([]any)[0].(map[string]any)["text"].(string)
	}
	if rpc("initialize", `{"protocolVersion":"2025-03-26"}`)["protocolVersion"] != "2025-03-26" {
		t.Fatal("protocol version not negotiated")
	}
	if n := len(rpc("tools/list", `{}`)["tools"].([]any)); n != len(tools) {
		t.Fatalf("tools = %d", n)
	}
	if got := text(rpc("tools/call", `{"name":"vfs_list","arguments":{"path":"/project"}}`)); !strings.Contains(got, "a.txt") || !strings.Contains(got, "sub/") {
		t.Fatalf("list = %q", got)
	}
	rpc("tools/call", `{"name":"vfs_edit","arguments":{"path":"/project/a.txt","old_text":"alpha","new_text":"omega"}}`)
	if got := text(rpc("tools/call", `{"name":"vfs_read","arguments":{"path":"/project/a.txt"}}`)); got != "omega\n" {
		t.Fatalf("read = %q", got)
	}
	if got := text(rpc("tools/call", `{"name":"vfs_search","arguments":{"path":"/","text":"beta"}}`)); got != "/project/sub/b.txt:1: beta" {
		t.Fatalf("search = %q", got)
	}
	if m := rpc("tools/call", `{"name":"vfs_read","arguments":{"path":"/../../etc/passwd"}}`); m["isError"] != true {
		t.Fatalf("escape was not rejected: %v", m)
	}
}

// The gateway speaks newline-delimited JSON-RPC over the private socket and is
// bound to its Env by the launch token alone.
func TestMCPSocketAuthority(t *testing.T) {
	s, _, _ := testEnv(t)
	s.mu.Lock()
	server, err := s.startMCP()
	env := s.Spaces[0].Envs[0]
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(server.socket)
	token := server.register(env, "claude", "test-session")
	ask := func(token, request string) string {
		conn, err := net.Dial("unix", server.socket)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		fmt.Fprintf(conn, "%s\n%s\n", token, request)
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		line, _ := bufio.NewReader(conn).ReadString('\n')
		return line
	}
	list := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"vfs_list","arguments":{"path":"/"}}}`
	if got := ask(token, list); !strings.Contains(got, `"id":7`) || !strings.Contains(got, "project/") {
		t.Fatalf("reply = %q", got)
	}
	if got := ask("wrong-token", list); got != "" {
		t.Fatalf("unknown token was served: %q", got)
	}
	// Every message is logged with enough detail to review the tool API.
	var logged []byte
	for i := 0; i < 100 && strings.Count(string(logged), `"event":"disconnect"`) < 2; i++ {
		time.Sleep(10 * time.Millisecond) // the disconnect records are written after the replies
		logged, _ = os.ReadFile(filepath.Join(s.envDir(env), LogName))
	}
	for _, want := range []string{`"event":"connect"`, `"agent":"claude"`, `"session":"test-session"`, `"tool":"vfs_list"`, `"args":{"path":"/"}`, `"out":"project/  [@`, `"event":"disconnect"`} {
		if !strings.Contains(string(logged), want) {
			t.Fatalf("log lacks %s:\n%s", want, logged)
		}
	}
	server.revoke(token)
	if got := ask(token, list); got != "" {
		t.Fatalf("revoked token was served: %q", got)
	}
}

// Personal-environment diagnostic: a real Claude Code CLI lists the Env through
// the gateway. Needs a built farshmac executable and a logged-in claude.
func TestClaudeUsesEnvMCP(t *testing.T) {
	gateway := os.Getenv("FARSHMAC_AI_CLAUDE_PROBE")
	if gateway == "" {
		t.Skip("set FARSHMAC_AI_CLAUDE_PROBE to a built farshmac executable")
	}
	s, _, _ := testEnv(t)
	s.mu.Lock()
	server, err := s.startMCP()
	env := s.Spaces[0].Envs[0]
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(server.socket)
	config, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"farshmac": map[string]any{"command": gateway, "args": []string{GatewayFlag}, "env": map[string]string{socketEnv: server.socket, tokenEnv: server.register(env, "claude", "test-session")}}}})
	file := filepath.Join(t.TempDir(), "mcp.json")
	os.WriteFile(file, config, 0600)
	cmd := exec.Command("claude", "--mcp-config", file, "--strict-mcp-config", "--allowedTools", "mcp__farshmac", "-p", "Call the vfs_list tool of the farshmac MCP server for path /project and reply with only the names it returned.")
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	t.Logf("claude: %s", out)
	if err != nil || !strings.Contains(string(out), "a.txt") {
		t.Fatalf("claude did not read the Env: %v", err)
	}
}

func TestLaunchRunsAgentOnPTYOnce(t *testing.T) {
	script := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(script, []byte("#!/bin/sh\necho agent-in \"$PWD\" child=\"$CLAUDE_CODE_CHILD_SESSION$CLAUDECODE\"\nsleep 0.3\n"), 0755)
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	t.Setenv("CLAUDECODE", "1")
	s, _, _ := testEnv(t)
	if err := s.AddAgents("Space", "Dev", []Agent{{Name: "claude", Path: script}}); err != nil {
		t.Fatal(err)
	}
	p, err := s.Launch("Space", "Dev", "claude", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(s.mcp.socket)
	if _, err = s.Launch("Space", "Dev", "claude", 80, 24); err == nil {
		t.Fatal("a second process was started for a running agent")
	}
	var output strings.Builder
	for chunk := range p.Output() {
		output.Write(chunk)
	}
	// An agent must not become a child session of the CLI that started FarshMac.
	if err = p.Err(); err != nil || !strings.Contains(output.String(), filepath.Join("agents", "claude", "scratch")+" child=\r") {
		t.Fatalf("output %q, %v", output.String(), err)
	}
	if s.Running("Space", "Dev", "claude") != nil || !s.Agents("Space", "Dev")[0].Started {
		t.Fatal("agent state not updated after exit")
	}
}

func TestAgentMessagingFollowsCommunicationRules(t *testing.T) {
	AutoStart = false
	defer func() { AutoStart = true }()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.CreateSpace("Space")
	for _, name := range []string{"Product", "Architect", "Coder"} {
		if err = s.CreateEnv("Space", name, []Agent{{Name: "claude", Path: "/bin/echo"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.SetPeers("Space", "Product", []string{"Architect"}); err != nil {
		t.Fatal(err)
	}
	env := func(name string) *Env { return s.Spaces[0].env(name) }
	if got := s.peerList(env("Product"), "claude"); !strings.Contains(got, "You are Product/claude") || !strings.Contains(got, "Architect/claude") || strings.Contains(got, "Coder") {
		t.Fatalf("peers = %q", got)
	}
	if _, err = s.send(env("Product"), "claude", "Coder", "do it"); err == nil || !strings.Contains(err.Error(), "permission_denied") {
		t.Fatalf("Product reached Coder: %v", err)
	}
	if _, err = s.send(env("Architect"), "claude", "Product/claude", "hello"); err == nil {
		t.Fatal("Architect started a conversation it was not allowed to start")
	}
	sent, err := s.send(env("Product"), "claude", "architect", "Feature request: dark theme")
	if err != nil || !strings.Contains(sent, "Message #1 to Architect/claude") || !strings.Contains(sent, "Do not wait") {
		t.Fatalf("send = %q, %v", sent, err)
	}
	// Not running: the message waits for the agent's next start.
	inbox := env("Architect").agent("claude").Inbox
	if len(inbox) != 1 || !strings.Contains(inbox[0], "[FarshMac message #1 from Product/claude]") || !strings.Contains(inbox[0], "request_id 1") {
		t.Fatalf("inbox = %q", inbox)
	}
	// While the question is open, the asked agent may ask back.
	if _, err = s.send(env("Architect"), "claude", "Product", "Which platforms?"); err != nil {
		t.Fatalf("clarifying question refused: %v", err)
	}
	if _, err = s.reply(env("Coder"), "claude", "1", "not mine"); err == nil {
		t.Fatal("another agent answered a message that was not addressed to it")
	}
	if _, err = s.reply(env("Architect"), "claude", "#1", "Added to the spec"); err != nil {
		t.Fatal(err)
	}
	back := env("Product").agent("claude").Inbox
	if len(back) != 2 || !strings.Contains(back[1], "[FarshMac reply to #1 from Architect/claude]\nAdded to the spec") {
		t.Fatalf("reply not routed to the asker: %q", back)
	}
	if _, err = s.reply(env("Architect"), "claude", "1", "again"); err == nil {
		t.Fatal("a message was answered twice")
	}
	log, _ := os.ReadFile(s.MessagesPath("Space"))
	if strings.Count(string(log), "\n") != 3 || !strings.Contains(string(log), `"kind":"reply"`) {
		t.Fatalf("message log = %s", log)
	}
}

func TestAgentGetsItsOwnInstructionFile(t *testing.T) {
	script := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(script, []byte("#!/bin/sh\ncat CLAUDE.md\n"), 0755)
	s, fs, _ := testEnv(t)
	if err := s.AddAgents("Space", "Dev", []Agent{{Name: "claude", Path: script}}); err != nil {
		t.Fatal(err)
	}
	fs, _ = s.FS("Space", "Dev")
	if got := names(t, fs, "/"); got != "CLAUDE.md,project" {
		t.Fatalf("Env root = %q", got)
	}
	if err := fs.WriteFile("/CLAUDE.md", []byte("Always answer in haiku.\n")); err != nil {
		t.Fatal(err)
	}
	p, err := s.Launch("Space", "Dev", "claude", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(s.mcp.socket)
	var output strings.Builder
	for chunk := range p.Output() {
		output.Write(chunk)
	}
	if got := output.String(); !strings.Contains(got, "Always answer in haiku.") || !strings.Contains(got, "Your address is Dev/claude") {
		t.Fatalf("agent instructions = %q", got)
	}
}

// Personal-environment diagnostic: two real Claude Code agents exchange a
// request and its answer through console delivery, without polling.
func TestClaudeAgentsTalk(t *testing.T) {
	gatewayExecutable = os.Getenv("FARSHMAC_AI_CLAUDE_PROBE")
	if gatewayExecutable == "" {
		t.Skip("set FARSHMAC_AI_CLAUDE_PROBE to a built farshmac executable")
	}
	defer func() { gatewayExecutable = "" }()
	claude, err := exec.LookPath("claude")
	if err != nil {
		t.Skip(err)
	}
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.CreateSpace("Space")
	s.CreateEnv("Space", "Product", []Agent{{Name: "claude", Path: claude}})
	s.CreateEnv("Space", "Architect", []Agent{{Name: "claude", Path: claude}})
	s.SetPeers("Space", "Product", []string{"Architect"})
	var screenMu sync.Mutex
	screens := map[string]*strings.Builder{}
	// Only Product is started. Architect must be started by the message,
	// pass the folder trust question on its own, and answer.
	product, err := s.Launch("Space", "Product", "claude", 120, 40)
	if err != nil {
		t.Fatal(err)
	}
	defer product.Close()
	watch := func(env string, p *Process) {
		screen := &strings.Builder{}
		screenMu.Lock()
		screens[env] = screen
		screenMu.Unlock()
		go func() {
			for chunk := range p.Output() {
				screenMu.Lock()
				screen.Write(chunk)
				screenMu.Unlock()
			}
		}()
	}
	watch("Product", product)
	defer os.Remove(s.mcp.socket)
	defer func() {
		if p := s.Running("Space", "Architect", "claude"); p != nil {
			p.Close()
		}
	}()
	product.idle()
	procs := []*Process{product}
	procs[0].Deliver("Use the agent_send tool to ask Architect exactly this: What is 17+25? Answer with the number only.")
	var log []byte
	for i := 0; i < 150 && !strings.Contains(string(log), `"kind":"reply"`); i++ {
		time.Sleep(time.Second)
		log, _ = os.ReadFile(s.MessagesPath("Space"))
	}
	time.Sleep(15 * time.Second) // let the asker react to the delivered reply
	if p := s.Running("Space", "Architect", "claude"); p != nil {
		watch("Architect", p)
		time.Sleep(time.Second)
	}
	for _, env := range []string{"Product", "Architect"} {
		calls, _ := os.ReadFile(filepath.Join(s.envDir(s.Spaces[0].env(env)), LogName))
		t.Logf("%s MCP log:\n%s", env, calls)
	}
	t.Logf("messages:\n%s", log)
	if !strings.Contains(string(log), `"kind":"reply"`) || !strings.Contains(string(log), "42") {
		for env, screen := range screens {
			screenMu.Lock()
			text := screen.String()
			screenMu.Unlock()
			t.Logf("%s screen tail: %q", env, text[max(0, len(text)-1500):])
		}
		t.Fatal("the agents did not complete a request and its reply")
	}
}

func TestMCPToolsWorkInFewCalls(t *testing.T) {
	s, fs, source := testEnv(t)
	os.WriteFile(filepath.Join(source, "sub", "notes.md"), []byte("one\ntwo R11\nthree\nfour R12\nfive\n"), 0644)
	// The same real folder is attached to a second Env at another path, read-only.
	s.CreateEnv("Space", "Reader", nil)
	if err := s.Attach("Space", "Reader", "/deep/er", []string{filepath.Join(source, "sub")}, true); err != nil {
		t.Fatal(err)
	}
	reader, _ := s.FS("Space", "Reader")
	sessions := map[*FS]*mcpSession{fs: {env: s.Spaces[0].env("Dev"), agent: "claude"}, reader: {env: s.Spaces[0].env("Reader"), agent: "claude"}}
	run := func(f *FS, name, args string) (string, bool) {
		result, rpcErr := handle(s, sessions[f], f, rpcRequest{ID: json.RawMessage("1"), Method: "tools/call", Params: json.RawMessage(`{"name":"` + name + `","arguments":` + args + `}`)})
		if rpcErr != nil {
			t.Fatalf("%s: %v", name, rpcErr)
		}
		m := result.(map[string]any)
		return m["content"].([]any)[0].(map[string]any)["text"].(string), m["isError"].(bool)
	}
	// One call shows the whole tree, with reference IDs and read-only marks.
	tree, _ := run(fs, "vfs_list", `{"path":"/","depth":3}`)
	for _, want := range []string{"project/  [@", "project/a.txt\t6 bytes", "project/sub/notes.md"} {
		if !strings.Contains(tree, want) {
			t.Fatalf("tree lacks %q:\n%s", want, tree)
		}
	}
	if tree, _ = run(reader, "vfs_list", `{"path":"/","depth":3}`); !strings.Contains(tree, "deep/er/sub/  [@") || !strings.Contains(tree, "read-only]") {
		t.Fatalf("read-only reference not marked:\n%s", tree)
	}
	// A fragment instead of the whole file.
	if got, _ := run(fs, "vfs_read", `{"path":"/project/sub/notes.md","offset":2,"limit":2,"numbered":true}`); got != "[lines 2-3 of 5]\n2\ttwo R11\n3\tthree\n" {
		t.Fatalf("range = %q", got)
	}
	// Regular expressions, a name filter and context lines.
	if got, _ := run(fs, "vfs_search", `{"path":"/","text":"R1[12]$","regex":true,"glob":"*.md","context":1}`); got != "/project/sub/notes.md-1- one\n/project/sub/notes.md:2: two R11\n/project/sub/notes.md-3- three\n/project/sub/notes.md:4: four R12\n/project/sub/notes.md-5- five" {
		t.Fatalf("search = %q", got)
	}
	if got, _ := run(fs, "vfs_search", `{"path":"/","text":"notes","names_only":true}`); got != "/project/sub/notes.md" {
		t.Fatalf("name search = %q", got)
	}
	// Several replacements in several files in one call; all or nothing.
	failed, isErr := run(fs, "vfs_edit", `{"edits":[{"path":"/project/a.txt","old_text":"alpha","new_text":"omega"},{"path":"/project/sub/notes.md","old_text":"missing","new_text":"x"}]}`)
	if original, _ := os.ReadFile(filepath.Join(source, "a.txt")); !isErr || !strings.Contains(failed, "edit 2") || string(original) != "alpha\n" {
		t.Fatalf("partial batch was applied: %q, %q", failed, original)
	}
	done, isErr := run(fs, "vfs_edit", `{"path":"/project/sub/notes.md","edits":[{"old_text":"one","new_text":"1"},{"old_text":"two","new_text":"2"},{"path":"/project/a.txt","old_text":"alpha","new_text":"omega"}]}`)
	notes, _ := os.ReadFile(filepath.Join(source, "sub", "notes.md"))
	if a, _ := os.ReadFile(filepath.Join(source, "a.txt")); isErr || !strings.HasPrefix(string(notes), "1\n2 R11") || string(a) != "omega\n" || !strings.Contains(done, "applied 3 replacement(s)") {
		t.Fatalf("batch = %q, notes %q, a %q", done, notes, a)
	}
	// A reference names the same real file in both Envs, whatever the path.
	stat, _ := run(fs, "vfs_stat", `{"path":"/project/sub/notes.md"}`)
	ref := stat[strings.Index(stat, "reference: ")+len("reference: "):]
	if !strings.HasPrefix(ref, "@") || !strings.HasSuffix(ref, "/sub/notes.md") || !strings.Contains(stat, "access: read/write") {
		t.Fatalf("stat = %q", stat)
	}
	if got, isErr := run(reader, "vfs_read", `{"path":"`+ref+`","limit":1}`); isErr || got != "[lines 1-1 of 5]\n1\n" {
		t.Fatalf("reference did not resolve in the other Env: %q", got)
	}
	if got, _ := run(reader, "vfs_stat", `{"path":"`+ref+`"}`); !strings.Contains(got, "file /deep/er/sub/notes.md") || !strings.Contains(got, "access: read-only") {
		t.Fatalf("stat by reference = %q", got)
	}
	if got, isErr := run(reader, "vfs_edit", `{"path":"`+ref+`","old_text":"1","new_text":"x"}`); !isErr || !strings.Contains(got, "read_only") {
		t.Fatalf("read-only reference was edited: %q", got)
	}
	// A file outside the folder attached to that Env stays out of reach.
	outside, _ := run(fs, "vfs_stat", `{"path":"/project/a.txt"}`)
	if got, isErr := run(reader, "vfs_read", `{"path":"`+outside[strings.Index(outside, "reference: ")+len("reference: "):]+`"}`); !isErr || !strings.Contains(got, "not attached to this Env") {
		t.Fatalf("reference reached outside the Env: %q", got)
	}
}

func TestAgentIsConfinedToItsScratch(t *testing.T) {
	home, _ := os.UserHomeDir()
	marker := filepath.Join(home, ".farshmac-sandbox-test")
	defer os.Remove(marker)
	script := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(script, []byte("#!/bin/sh\nls \"$HOME\" >/dev/null 2>&1 && echo HOME_READ\ntouch \"$HOME/.farshmac-sandbox-test\" 2>/dev/null && echo HOME_WRITE\necho x > /tmp/farshmac-sandbox-test 2>/dev/null && echo TMP_WRITE\necho inside > here.txt && cat here.txt\necho temp > \"$TMPDIR/t\" && echo TEMP_OK\n"), 0755)
	s, _, source := testEnv(t)
	s.AddAgents("Space", "Dev", []Agent{{Name: "claude", Path: script}})
	p, err := s.Launch("Space", "Dev", "claude", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(s.mcp.socket)
	var output strings.Builder
	for chunk := range p.Output() {
		output.Write(chunk)
	}
	got := output.String()
	if strings.Contains(got, "HOME_READ") || strings.Contains(got, "HOME_WRITE") || strings.Contains(got, "TMP_WRITE") {
		t.Fatalf("agent escaped its scratch folder: %q", got)
	}
	if !strings.Contains(got, "inside") || !strings.Contains(got, "TEMP_OK") {
		t.Fatalf("agent cannot use its own scratch folder: %q", got)
	}
	_ = source
}

func TestWorkspaceCopyThreadsAndReferenceWarnings(t *testing.T) {
	AutoStart = false
	defer func() { AutoStart = true }()
	s, fs, source := testEnv(t)
	s.AddAgents("Space", "Dev", []Agent{{Name: "claude", Path: "/bin/echo"}})
	s.CreateEnv("Space", "Reviewer", []Agent{{Name: "claude", Path: "/bin/echo"}})
	s.SetPeers("Space", "Dev", []string{"Reviewer"})
	dev, reviewer := s.Spaces[0].env("Dev"), s.Spaces[0].env("Reviewer")
	session := &mcpSession{env: dev, agent: "claude"}
	run := func(name, args string) (string, bool) {
		result, rpcErr := handle(s, session, fs, rpcRequest{ID: json.RawMessage("1"), Method: "tools/call", Params: json.RawMessage(`{"name":"` + name + `","arguments":` + args + `}`)})
		if rpcErr != nil {
			t.Fatalf("%s: %v", name, rpcErr)
		}
		m := result.(map[string]any)
		return m["content"].([]any)[0].(map[string]any)["text"].(string), m["isError"].(bool)
	}
	// Pull a folder into scratch, change it there with ordinary tools, push it back.
	pulled, isErr := run("workspace_pull", `{"path":"/project"}`)
	local := s.workspace(dev, "claude", "/project")
	if isErr || !strings.Contains(pulled, local) {
		t.Fatalf("pull = %q", pulled)
	}
	os.WriteFile(filepath.Join(local, "a.txt"), []byte("built\n"), 0644)
	os.MkdirAll(filepath.Join(local, "dist"), 0755)
	os.WriteFile(filepath.Join(local, "dist", "out.js"), []byte("js\n"), 0644)
	pushed, isErr := run("workspace_push", `{"path":"/project"}`)
	real, _ := os.ReadFile(filepath.Join(source, "dist", "out.js"))
	if a, _ := os.ReadFile(filepath.Join(source, "a.txt")); isErr || string(a) != "built\n" || string(real) != "js\n" || !strings.Contains(pushed, "Pushed 2 file(s)") {
		t.Fatalf("push = %q, a %q, out %q", pushed, a, real)
	}
	// A file somebody else changed since the pull is not overwritten.
	os.WriteFile(filepath.Join(source, "a.txt"), []byte("theirs\n"), 0644)
	os.WriteFile(filepath.Join(local, "a.txt"), []byte("mine\n"), 0644)
	if got, isErr := run("workspace_push", `{"path":"/project"}`); !isErr || !strings.Contains(got, "conflict") {
		t.Fatalf("stale push was accepted: %q", got)
	}
	if a, _ := os.ReadFile(filepath.Join(source, "a.txt")); string(a) != "theirs\n" {
		t.Fatalf("stale push overwrote %q", a)
	}
	// Copy without passing the bytes through the model; the result names the reference.
	fs.WriteFile("/draft.md", []byte("readme\n"))
	copied, isErr := run("vfs_copy", `{"from":"/draft.md","to":"/project/README.md"}`)
	if readme, _ := os.ReadFile(filepath.Join(source, "README.md")); isErr || string(readme) != "readme\n" || !strings.Contains(copied, "reference @") {
		t.Fatalf("copy = %q, %q", copied, readme)
	}
	// The sender learns at once that the recipient cannot open a reference, and so does the user.
	ref := fs.Ref("/project")
	sent, _ := run("agent_send", `{"to":"Reviewer","message":"Please review `+ref+`/a.txt"}`)
	if !strings.Contains(sent, "Warning: Reviewer cannot open "+ref) {
		t.Fatalf("no warning about an unreachable reference: %q", sent)
	}
	if notices := s.Notices(); len(notices) != 1 || !strings.Contains(notices[0], "Reviewer cannot open "+ref) {
		t.Fatalf("user notices = %q", notices)
	}
	// A reply can be answered in the same thread, by the right side only.
	if _, err := s.reply(reviewer, "claude", "1", "Three remarks"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.reply(reviewer, "claude", "1", "again"); err == nil || !strings.Contains(err.Error(), "you spoke last") {
		t.Fatalf("out-of-turn reply: %v", err)
	}
	if got, isErr := run("agent_reply", `{"request_id":1,"message":"Fixed all three"}`); isErr || !strings.Contains(got, "Reply to #1") {
		t.Fatalf("reply to a reply = %q", got)
	}
	if inbox := reviewer.agent("claude").Inbox; len(inbox) != 2 || !strings.Contains(inbox[1], "[FarshMac reply to #1 from Dev/claude]\nFixed all three") {
		t.Fatalf("thread not continued: %q", inbox)
	}
}

func TestMessageStartsAStoppedAgent(t *testing.T) {
	// The fake agent shows the folder trust question, then echoes what it is sent.
	script := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(script, []byte("#!/bin/sh\nprintf '\\033[2G\\342\\235\\257 No, exit\\r\\n\\033[4GYes, I trust this folder\\r\\nEnter to confirm\\r\\n'\nread first\necho \"answered=[$first]\"\nwhile read line; do echo \"got: $line\"; done\n"), 0755)
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.CreateSpace("Space")
	s.CreateEnv("Space", "Product", []Agent{{Name: "claude", Path: script}})
	s.CreateEnv("Space", "Architect", []Agent{{Name: "claude", Path: script}})
	s.SetPeers("Space", "Product", []string{"Architect"})
	sent, err := s.send(s.Spaces[0].env("Product"), "claude", "Architect", "Wake up and review")
	if err != nil || !strings.Contains(sent, "delivered") {
		t.Fatalf("send = %q, %v", sent, err)
	}
	p := s.Running("Space", "Architect", "claude")
	if p == nil {
		t.Fatal("the stopped agent was not started")
	}
	defer p.Close()
	defer os.Remove(s.mcp.socket)
	if notices := s.Notices(); len(notices) != 1 || !strings.Contains(notices[0], "Started Architect/claude") {
		t.Fatalf("notices = %q", notices)
	}
	// Nobody reads the console yet; the output is kept for whoever opens it.
	if !p.Attach() || p.Attach() {
		t.Fatal("the console must be attachable exactly once")
	}
	var screen strings.Builder
	deadline := time.After(40 * time.Second)
	for !strings.Contains(screen.String(), "Wake up and review") {
		select {
		case chunk := <-p.Output():
			screen.Write(chunk)
		case <-deadline:
			t.Fatalf("message never reached the started agent: %q", screen.String())
		}
	}
	// The trust question was answered with Down, Enter: the first line read is empty.
	if got := screen.String(); !strings.Contains(got, "answered=[") || !strings.Contains(got, "FarshMac message #1 from Product/claude") {
		t.Fatalf("console = %q", got)
	}
}

// The real store lives in the home folder, which the sandbox closes: the
// agent must still be able to read what it is started with.
func TestSandboxedAgentStartsFromAStoreInTheHomeFolder(t *testing.T) {
	home, _ := os.UserHomeDir()
	root, err := os.MkdirTemp(home, ".farshmac-ai-test-")
	if err != nil {
		t.Skip(err)
	}
	defer os.RemoveAll(root)
	script := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(script, []byte("#!/bin/sh\ncat \"$2\" >/dev/null && echo CONFIG_OK\ncat CLAUDE.md >/dev/null && echo NOTES_OK\necho x > work.txt && echo SCRATCH_OK\ncat ../mcp.json >/dev/null 2>&1 && echo SIBLING_OK\nls .. >/dev/null 2>&1 && echo PARENT_LISTED\n"), 0755)
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	s.CreateSpace("Space")
	s.CreateEnv("Space", "Dev", []Agent{{Name: "claude", Path: script}})
	p, err := s.Launch("Space", "Dev", "claude", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(s.mcp.socket)
	var output strings.Builder
	for chunk := range p.Output() {
		output.Write(chunk)
	}
	for _, want := range []string{"CONFIG_OK", "NOTES_OK", "SCRATCH_OK"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("agent in a home-folder store lacks %s: %q", want, output.String())
		}
	}
}

// The user's own agent, outside any Env, sees the Spaces, writes to an agent
// by role, learns when it has answered and reads what it said.
func TestOperatorTalksToEnvAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// The fake agent echoes what it is sent, then falls silent.
	script := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(script, []byte("#!/bin/sh\necho ready\nwhile read line; do echo \"got: $line\"; done\n"), 0755)
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	s.CreateSpace("Shop")
	s.CreateEnv("Shop", "Product", []Agent{{Name: "claude", Path: script}})
	s.CreateEnv("Shop", "Architect", []Agent{{Name: "claude", Path: script}})
	if err = s.Serve(); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(s.mcp.socket)
	if info, err := os.Stat(filepath.Join(root, operatorFile)); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("operator file: %v, %v", info, err)
	}

	// A real gateway on pipes: the protocol is answered locally, calls go to the store.
	toGateway, fromGateway := io.Pipe()
	replies, out := io.Pipe()
	go operatorGateway(root, toGateway, out)
	reader := bufio.NewReader(replies)
	id := 0
	rpc := func(method, params string) string {
		id++
		fmt.Fprintf(fromGateway, `{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`+"\n", id, method, params)
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		return line
	}
	call := func(name, args string) string {
		var reply struct {
			Result struct {
				Content []struct{ Text string }
				IsError bool
			}
		}
		raw := rpc("tools/call", fmt.Sprintf(`{"name":%q,"arguments":%s}`, name, args))
		if err := json.Unmarshal([]byte(raw), &reply); err != nil || len(reply.Result.Content) == 0 {
			t.Fatalf("%s: %q", name, raw)
		}
		if reply.Result.IsError {
			return "ERROR " + reply.Result.Content[0].Text
		}
		return reply.Result.Content[0].Text
	}
	if got := rpc("initialize", `{"protocolVersion":"2025-06-18","clientInfo":{"name":"codex"}}`); !strings.Contains(got, "farshmac-ai-spaces") {
		t.Fatalf("initialize = %q", got)
	}
	if got := rpc("tools/list", `{}`); !strings.Contains(got, "agent_wait") || strings.Contains(got, "vfs_") {
		t.Fatalf("tools = %q", got)
	}
	if got := call("spaces_list", `{}`); !strings.Contains(got, `AI Space "Shop"`) || !strings.Contains(got, "agent Architect/claude: not running") {
		t.Fatalf("spaces_list = %q", got)
	}
	if got := call("agent_status", `{"agent":"claude"}`); !strings.Contains(got, "ambiguous") {
		t.Fatalf("a name two Envs share must be refused: %q", got)
	}
	if got := call("agent_send", `{"space":"shop","agent":"architect","message":"Review the plan"}`); !strings.Contains(got, "Message #1 delivered to Architect/claude") {
		t.Fatalf("agent_send = %q", got)
	}
	p := s.Running("Shop", "Architect", "claude")
	if p == nil {
		t.Fatal("the stopped agent was not started")
	}
	defer p.Close()
	if got := call("agent_wait", `{"agent":"Architect","seconds":1}`); !strings.Contains(got, "still working") {
		t.Fatalf("an agent that just got a message is working: %q", got)
	}
	// The agent answers with its own reply tool, as often as it likes.
	architect := s.Spaces[0].env("Architect")
	if _, err = s.reply(architect, "claude", "1", "The plan is fine"); err != nil {
		t.Fatal(err)
	}
	if got := call("agent_wait", `{"agent":"Architect","seconds":5}`); !strings.Contains(got, "[Reply to #1 from Architect/claude") || !strings.Contains(got, "The plan is fine") {
		t.Fatalf("agent_wait = %q", got)
	}
	if _, err = s.reply(architect, "claude", "1", "One more thing"); err != nil {
		t.Fatal(err)
	}
	// The conversation is read from the agent's own transcript.
	session := "0123"
	s.mu.Lock()
	architect.agent("claude").SessionID = session
	s.mu.Unlock()
	project := filepath.Join(home, ".claude", "projects", "scratch")
	os.MkdirAll(project, 0700)
	os.WriteFile(filepath.Join(project, session+".jsonl"), []byte(`{"type":"user","message":{"content":"Review the plan"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Looking at it"},{"type":"tool_use","name":"vfs_read","input":{"path":"/plan.md"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"secret tool output"}]}}
`), 0600)
	got := call("agent_status", `{"agent":"Architect","screen":true}`)
	for _, want := range []string{"One more thing", "PROMPT: Review the plan", "AGENT: Looking at it", `tool vfs_read {"path":"/plan.md"}`} {
		if !strings.Contains(got, want) {
			t.Fatalf("agent_status lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "secret tool output") {
		t.Fatalf("tool output must be left out:\n%s", got)
	}
	// Left alone, the agent is seen to have stopped working.
	if got := call("agent_wait", `{"agent":"Architect","seconds":20}`); !strings.Contains(got, "stopped working without replying") {
		t.Fatalf("agent_wait = %q", got)
	}
	if got := call("agent_status", `{"agent":"Architect/claude","screen":true}`); !strings.Contains(got, "FarshMac message #1 from the user, sent through codex") || !strings.Contains(got, "idle for") {
		t.Fatalf("console view = %q", got)
	}
	// Operator calls are logged like those of Env agents.
	if logged, _ := os.ReadFile(s.OperatorLogPath()); !strings.Contains(string(logged), `"tool":"agent_send"`) || !strings.Contains(string(logged), `"agent":"codex"`) {
		t.Fatalf("operator log = %s", logged)
	}
}

func TestOperatorGatewayWithoutFarshMac(t *testing.T) {
	var out strings.Builder
	operatorGateway(t.TempDir(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n"+`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"spaces_list","arguments":{}}}`+"\n"), &out)
	if got := out.String(); !strings.Contains(got, "spaces_list") || !strings.Contains(got, "FarshMac is not running") || !strings.Contains(got, `"isError":true`) {
		t.Fatalf("gateway = %q", got)
	}
}
