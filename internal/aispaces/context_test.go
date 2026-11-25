package aispaces

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// useTool runs one MCP tool call the way an agent's gateway does.
func useTool(t *testing.T, s *Store, session *mcpSession, name string, args any) (string, bool) {
	t.Helper()
	fs, err := s.FS(s.spaceOf(session.env).Name, session.env.Name)
	if err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
	result, rpcErr := handle(s, session, fs, rpcRequest{ID: json.RawMessage("1"), Method: "tools/call", Params: params})
	if rpcErr != nil {
		t.Fatalf("%s: %v", name, rpcErr)
	}
	m := result.(map[string]any)
	return m["content"].([]any)[0].(map[string]any)["text"].(string), m["isError"].(bool)
}

func agentOf(s *Store, env string) *mcpSession {
	return &mcpSession{env: s.Spaces[0].env(env), agent: "claude"}
}

// A file the agent's conversation already holds is not sent to it again: it
// is told that nothing changed, or shown only the difference.
func TestReadsAreNotRepeated(t *testing.T) {
	s, _, source := testEnv(t)
	var text strings.Builder
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&text, "line %d of the specification\n", i)
	}
	spec := filepath.Join(source, "spec.md")
	os.WriteFile(spec, []byte(text.String()), 0644)
	dev := agentOf(s, "Dev")
	read := func(args map[string]any) string {
		args["path"] = "/project/spec.md"
		got, isErr := useTool(t, s, dev, "vfs_read", args)
		if isErr {
			t.Fatalf("read: %s", got)
		}
		return got
	}
	if got := read(map[string]any{}); got != text.String() {
		t.Fatalf("first read = %q", got)
	}
	again := read(map[string]any{})
	if !strings.HasPrefix(again, "[unchanged]") || !strings.Contains(again, "full=true") || len(again) > 400 {
		t.Fatalf("second read = %q", again)
	}
	// Any part of a file read in full is known as well; full=true overrides.
	if got := read(map[string]any{"offset": 5, "limit": 3}); !strings.HasPrefix(got, "[unchanged]") {
		t.Fatalf("range of a known file = %q", got)
	}
	if got := read(map[string]any{"full": true}); got != text.String() {
		t.Fatalf("full read = %q", got)
	}
	// Somebody else changes two lines: only the difference comes back.
	changed := strings.Replace(text.String(), "line 30 of the specification\n", "line 30 was rewritten\nand a line was added\n", 1)
	os.WriteFile(spec, []byte(changed), 0644)
	diff := read(map[string]any{})
	for _, want := range []string{"[changed]", "60 → 61 lines, +2 −1", "@@ -28,5 +28,6 @@", "-line 30 of the specification", "+line 30 was rewritten", "+and a line was added", " line 29 of the specification"} {
		if !strings.Contains(diff, want) {
			t.Fatalf("diff lacks %q:\n%s", want, diff)
		}
	}
	if len(diff) > len(changed)/3 {
		t.Fatalf("the difference is %d bytes of a %d byte file", len(diff), len(changed))
	}
	if got := read(map[string]any{}); !strings.HasPrefix(got, "[unchanged]") {
		t.Fatalf("after a diff the agent knows the new version: %q", got)
	}
	// Its own edit keeps the agent up to date; a rewritten file is sent whole.
	if got, isErr := useTool(t, s, dev, "vfs_edit", map[string]any{"path": "/project/spec.md", "old_text": "line 1 of", "new_text": "LINE ONE of"}); isErr {
		t.Fatal(got)
	}
	if got := read(map[string]any{}); !strings.HasPrefix(got, "[unchanged]") {
		t.Fatalf("after its own edit = %q", got)
	}
	os.WriteFile(spec, []byte("entirely\ndifferent\n"), 0644)
	if got := read(map[string]any{}); got != "entirely\ndifferent\n" {
		t.Fatalf("a rewritten file = %q", got)
	}
	// Every conversation is tracked on its own.
	other := agentOf(s, "Dev")
	if got, _ := useTool(t, s, other, "vfs_read", map[string]any{"path": "/project/a.txt", "limit": 1}); got != "alpha\n" {
		t.Fatalf("another conversation starts empty: %q", got)
	}
	// A compacted or cleared conversation no longer holds what it read.
	dev.forget("")
	if got := read(map[string]any{}); got != "entirely\ndifferent\n" {
		t.Fatalf("after forgetting = %q", got)
	}
}

// The agent CLI's hook tells the server that the conversation was compacted.
func TestCompactionHookResetsReadTracking(t *testing.T) {
	s, _, _ := testEnv(t)
	s.mu.Lock()
	server, err := s.startMCP()
	env := s.Spaces[0].Envs[0]
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(server.socket)
	token := server.register(env, "claude", "session")
	server.mu.Lock()
	session := server.tokens[token]
	server.mu.Unlock()
	useTool(t, s, session, "vfs_read", map[string]any{"path": "/project/a.txt"})
	if got, _ := useTool(t, s, session, "vfs_read", map[string]any{"path": "/project/a.txt"}); !strings.HasPrefix(got, "[unchanged]") {
		t.Fatalf("second read = %q", got)
	}
	t.Setenv(socketEnv, server.socket)
	t.Setenv(tokenEnv, token)
	if EventMain("forget") != 0 {
		t.Fatal("event was not sent")
	}
	for i := 0; i < 200; i++ {
		if got, _ := useTool(t, s, session, "vfs_read", map[string]any{"path": "/project/a.txt"}); got == "alpha\n" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the conversation was not reset")
}

func TestOutlineAndSections(t *testing.T) {
	s, _, source := testEnv(t)
	doc := "# Product\nintro\n\n## Goals\ngoal one\ngoal two\n\n### Non-goals\nnone\n\n## Storage\n```\n# not a heading\n```\nstored locally\n"
	os.MkdirAll(filepath.Join(source, "docs"), 0755)
	os.WriteFile(filepath.Join(source, "docs", "prd.md"), []byte(doc), 0644)
	os.WriteFile(filepath.Join(source, "main.go"), []byte("package main\n\nfunc main() {\n\trun()\n}\n\nfunc run() {}\n"), 0644)
	dev := agentOf(s, "Dev")
	file, _ := useTool(t, s, dev, "vfs_outline", map[string]any{"path": "/project/docs/prd.md"})
	for _, want := range []string{"/project/docs/prd.md — 15 lines", "1: # Product", "4:   ## Goals", "8:     ### Non-goals", "11:   ## Storage"} {
		if !strings.Contains(file, want) {
			t.Fatalf("outline lacks %q:\n%s", want, file)
		}
	}
	if strings.Contains(file, "not a heading") {
		t.Fatalf("a fenced line was taken for a heading:\n%s", file)
	}
	folder, _ := useTool(t, s, dev, "vfs_outline", map[string]any{"path": "/project"})
	for _, want := range []string{"docs/prd.md (15 lines)", "  4:   ## Goals", "main.go (7 lines)", "  3: func main()", "  7: func run()"} {
		if !strings.Contains(folder, want) {
			t.Fatalf("folder outline lacks %q:\n%s", want, folder)
		}
	}
	// A section ends where the next heading of its level begins.
	if got, _ := useTool(t, s, dev, "vfs_read", map[string]any{"path": "/project/docs/prd.md", "section": "goals"}); got != "[section \"Goals\": lines 4-10 of 15]\n## Goals\ngoal one\ngoal two\n\n### Non-goals\nnone\n\n" {
		t.Fatalf("section = %q", got)
	}
	if got, _ := useTool(t, s, dev, "vfs_read", map[string]any{"path": "/project/main.go", "section": "func run"}); got != "[section \"func run() {}\": lines 7-7 of 7]\nfunc run() {}\n" {
		t.Fatalf("declaration = %q", got)
	}
	missing, isErr := useTool(t, s, dev, "vfs_read", map[string]any{"path": "/project/docs/prd.md", "section": "Pricing"})
	if !isErr || !strings.Contains(missing, "11: Storage") {
		t.Fatalf("an unknown section must list the known ones: %q", missing)
	}
}

// One large folder or a file of long lines cannot flood the context.
func TestListAndSearchStayWithinBudget(t *testing.T) {
	s, _, source := testEnv(t)
	for d := 0; d < 12; d++ {
		dir := filepath.Join(source, fmt.Sprintf("module%02d", d), "internal")
		os.MkdirAll(dir, 0755)
		for f := 0; f < 40; f++ {
			os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%02d.go", f)), []byte("package x\n"), 0644)
		}
	}
	long := strings.Repeat("lorem ipsum dolor sit amet ", 60)
	var doc strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&doc, "%s token-%d %s\n", long, i, long)
	}
	os.WriteFile(filepath.Join(source, "long.md"), []byte(doc.String()), 0644)
	dev := agentOf(s, "Dev")
	tree, _ := useTool(t, s, dev, "vfs_list", map[string]any{"path": "/", "depth": 4})
	if lines := strings.Count(tree, "\n"); lines > listBudget+20 || !strings.Contains(tree, "internal/  (40 entries)") || !strings.Contains(tree, "module00/") {
		t.Fatalf("tree has %d lines:\n%s", lines, tree)
	}
	// A folded folder opens when it is listed on its own.
	if got, _ := useTool(t, s, dev, "vfs_list", map[string]any{"path": "/project/module03/internal"}); !strings.Contains(got, "file39.go  10B") {
		t.Fatalf("subfolder = %s", got)
	}
	found, _ := useTool(t, s, dev, "vfs_search", map[string]any{"path": "/project", "text": "token-"})
	if len(found) > searchBudget+2000 || !strings.Contains(found, "further matches not shown") || !strings.Contains(found, "/project/long.md (") {
		t.Fatalf("search returned %d bytes: %s", len(found), found[:min(len(found), 600)])
	}
	first := strings.SplitN(found, "\n", 3)[1]
	if len(first) > 300 || !strings.Contains(first, "token-0") || !strings.HasPrefix(first, "  1: …") {
		t.Fatalf("a long line must be cut around its match: %q", first)
	}
}

func TestWriteCreatesMissingFolders(t *testing.T) {
	s, _, source := testEnv(t)
	dev := agentOf(s, "Dev")
	got, isErr := useTool(t, s, dev, "vfs_write", map[string]any{"path": "/project/docs/diagrams/one.md", "content": "graph\n"})
	if data, _ := os.ReadFile(filepath.Join(source, "docs", "diagrams", "one.md")); isErr || string(data) != "graph\n" {
		t.Fatalf("write = %q, file %q", got, data)
	}
	s.CreateEnv("Space", "Reader", nil)
	s.Attach("Space", "Reader", "/", []string{source}, true)
	if got, isErr := useTool(t, s, agentOf(s, "Reader"), "vfs_write", map[string]any{"path": "/project/new/x.md", "content": "x"}); !isErr || !strings.Contains(got, "read_only") {
		t.Fatalf("a read-only folder accepted a write: %q", got)
	}
	if _, err := os.Stat(filepath.Join(source, "new")); err == nil {
		t.Fatal("a folder was created in a read-only reference")
	}
}

// Files travel with a message as attachments: the sender does not retype
// them, the recipient reads only the part it needs, and nobody gains access
// to anything but the copy that was handed over.
func TestAttachmentsTravelWithMessages(t *testing.T) {
	script := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(script, []byte("#!/bin/sh\nwhile read line; do :; done\n"), 0755)
	project := filepath.Join(t.TempDir(), "project")
	os.MkdirAll(filepath.Join(project, "specs"), 0755)
	os.MkdirAll(filepath.Join(project, "src"), 0755)
	var spec strings.Builder
	spec.WriteString("# Spec\n\n## Storage\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&spec, "storage rule %d: keep the data local and versioned\n", i)
	}
	spec.WriteString("\n## Export\nexport as JSON\n")
	os.WriteFile(filepath.Join(project, "specs", "spec.md"), []byte(spec.String()), 0644)
	os.WriteFile(filepath.Join(project, "src", "app.js"), []byte("let a = 1\n"), 0644)
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	AutoStart = false
	defer func() { AutoStart = true }()
	s.CreateSpace("Space")
	for _, env := range []string{"Architect", "Coder", "Product"} {
		s.CreateEnv("Space", env, []Agent{{Name: "claude", Path: script}})
	}
	s.Attach("Space", "Architect", "/", []string{filepath.Join(project, "specs")}, false)
	s.Attach("Space", "Coder", "/", []string{filepath.Join(project, "src")}, false)
	s.SetPeers("Space", "Architect", []string{"Coder"})
	architect, coder := agentOf(s, "Architect"), agentOf(s, "Coder")

	// The request quotes nothing: a large section and a short one travel with it.
	sent, isErr := useTool(t, s, architect, "agent_send", map[string]any{"to": "Coder", "message": "Implement the storage rules.", "attach": []string{"/specs/spec.md#Storage", "/specs/spec.md#Export"}})
	if isErr || !strings.Contains(sent, "with 2 attachment(s)") || !strings.Contains(sent, "do not repeat the message") {
		t.Fatalf("send = %q", sent)
	}
	inbox := s.Spaces[0].env("Coder").agent("claude").Inbox
	if len(inbox) != 1 {
		t.Fatalf("inbox = %q", inbox)
	}
	for _, want := range []string{"[FarshMac message #1 from Architect/claude]", "Implement the storage rules.", "[Attachment 2: spec.md section \"Export\" (lines 85-86)]\n## Export\nexport as JSON", "1. spec.md section \"Storage\" (lines 3-84) — 82 lines", "msg_read, request_id 1"} {
		if !strings.Contains(inbox[0], want) {
			t.Fatalf("delivered text lacks %q:\n%s", want, inbox[0])
		}
	}
	if strings.Contains(inbox[0], "storage rule 5") || len(inbox[0]) > 900 {
		t.Fatalf("a large attachment was pasted into the prompt (%d bytes)", len(inbox[0]))
	}
	// The recipient reads a part of it, although the folder is not attached to its Env.
	if got, isErr := useTool(t, s, coder, "vfs_read", map[string]any{"path": "/specs/spec.md"}); !isErr {
		t.Fatalf("the Coder must not see the specs folder: %q", got)
	}
	part, isErr := useTool(t, s, coder, "msg_read", map[string]any{"request_id": "1", "attachment": 1, "offset": 2, "limit": 2})
	if isErr || part != "[attachment 1 of #1: spec.md section \"Storage\" (lines 3-84) — lines 2-3 of 82]\nstorage rule 0: keep the data local and versioned\nstorage rule 1: keep the data local and versioned" {
		t.Fatalf("attachment = %q", part)
	}
	// The thread is kept and can be read again; an Env outside it cannot.
	if got, _ := useTool(t, s, coder, "msg_read", map[string]any{"request_id": "#1"}); !strings.Contains(got, "Implement the storage rules.") || !strings.Contains(got, "attachment 1: spec.md") {
		t.Fatalf("thread = %q", got)
	}
	if got, isErr := useTool(t, s, agentOf(s, "Product"), "msg_read", map[string]any{"request_id": "1", "attachment": 1}); !isErr || !strings.Contains(got, "not_found") {
		t.Fatalf("a third Env read the thread: %q", got)
	}
	if got, _ := useTool(t, s, coder, "agent_peers", map[string]any{}); !strings.Contains(got, "#1 from Architect/claude: Implement the storage rules.") {
		t.Fatalf("open requests = %q", got)
	}
	// The answer lists what the Coder changed, and attaches a file.
	useTool(t, s, coder, "vfs_write", map[string]any{"path": "/src/store.js", "content": "export const store = {}\n"})
	useTool(t, s, coder, "vfs_edit", map[string]any{"path": "/src/app.js", "old_text": "let a = 1", "new_text": "let a = 2\nlet b = 3"})
	replied, isErr := useTool(t, s, coder, "agent_reply", map[string]any{"request_id": "1", "message": "Done.", "attach": []string{"/src/store.js"}})
	if isErr || !strings.Contains(replied, "with 1 attachment(s)") {
		t.Fatalf("reply = %q", replied)
	}
	answer := s.Spaces[0].env("Architect").agent("claude").Inbox[0]
	for _, want := range []string{"[FarshMac reply to #1 from Coder/claude]\nDone.", "[Attachment 3: store.js]\nexport const store = {}", "[Files changed by Coder/claude since its previous message:", "and 2 file(s) in folders not attached to your Env"} {
		if !strings.Contains(answer, want) {
			t.Fatalf("reply lacks %q:\n%s", want, answer)
		}
	}
	// With the folder attached, the changes are named by reference.
	s.Attach("Space", "Architect", "/", []string{filepath.Join(project, "src")}, true)
	useTool(t, s, coder, "vfs_edit", map[string]any{"path": "/src/app.js", "old_text": "let b = 3", "new_text": "let b = 4"})
	useTool(t, s, architect, "agent_reply", map[string]any{"request_id": "1", "message": "One more change please."})
	useTool(t, s, coder, "agent_reply", map[string]any{"request_id": "1", "message": "Changed."})
	inbox = s.Spaces[0].env("Architect").agent("claude").Inbox
	if last := inbox[len(inbox)-1]; !strings.Contains(last, "/app.js (+1 −1)") || !strings.Contains(last, "  @") {
		t.Fatalf("changes by reference: %s", last)
	}
	// A forwarded message is an attachment too.
	s.SetPeers("Space", "Coder", []string{"Architect"})
	if got, isErr := useTool(t, s, coder, "agent_send", map[string]any{"to": "Architect", "message": "See the earlier thread.", "attach": []string{"#1"}}); isErr || !strings.Contains(got, "with 1 attachment(s)") {
		t.Fatalf("forward = %q", got)
	}
	if got, isErr := useTool(t, s, coder, "agent_send", map[string]any{"to": "Architect", "message": "x", "attach": []string{"/src/missing.js"}}); !isErr {
		t.Fatalf("a missing attachment was accepted: %q", got)
	}
}

// The user's own agent reads an attachment instead of asking for a file to
// be pasted, and sees real paths of what an agent changed.
func TestOperatorReadsAttachments(t *testing.T) {
	script := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(script, []byte("#!/bin/sh\nwhile read line; do :; done\n"), 0755)
	docs := filepath.Join(t.TempDir(), "docs")
	os.MkdirAll(docs, 0755)
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	AutoStart = false
	defer func() { AutoStart = true }()
	s.CreateSpace("Space")
	s.CreateEnv("Space", "Architect", []Agent{{Name: "claude", Path: script}})
	s.Attach("Space", "Architect", "/", []string{docs}, false)
	if got, err := s.operatorCall("codex", "agent_send", map[string]string{"agent": "Architect", "message": "Draw the diagram"}); err != nil || !strings.Contains(got, "queued") {
		t.Fatalf("send = %q, %v", got, err)
	}
	if text := s.Spaces[0].env("Architect").agent("claude").Inbox[0]; !strings.Contains(text, "Attach files instead of pasting") {
		t.Fatalf("the agent is not told to attach: %s", text)
	}
	architect := agentOf(s, "Architect")
	diagram := "```mermaid\nflowchart LR\n" + strings.Repeat("  A --> B\n", 200) + "```\n"
	useTool(t, s, architect, "vfs_write", map[string]any{"path": "/docs/diagram.md", "content": diagram})
	if got, isErr := useTool(t, s, architect, "agent_reply", map[string]any{"request_id": "1", "message": "The diagram is attached.", "attach": []string{"/docs/diagram.md"}}); isErr || !strings.Contains(got, "user's assistant") {
		t.Fatalf("reply = %q", got)
	}
	waited, err := s.operatorCall("codex", "agent_wait", map[string]string{"agent": "Architect", "seconds": "1"})
	file := filepath.Join(docs, "diagram.md")
	for _, want := range []string{"The diagram is attached.", "message_read, id 1", "1. diagram.md — 203 lines", "copied from " + file, "[Files changed by Architect/claude since its previous message:\n  " + file + " (new, 203 lines)"} {
		if err != nil || !strings.Contains(waited, want) {
			t.Fatalf("agent_wait lacks %q: %v\n%s", want, err, waited)
		}
	}
	if strings.Contains(waited, "A --> B") {
		t.Fatalf("the attachment was pasted into the reply:\n%s", waited)
	}
	got, err := s.operatorCall("codex", "message_read", map[string]string{"id": "1", "attachment": "1", "limit": "3"})
	if err != nil || got != "[attachment 1 of #1: diagram.md — lines 1-3 of 203]\n```mermaid\nflowchart LR\n  A --> B" {
		t.Fatalf("message_read = %q, %v", got, err)
	}
}

func TestClaudeIsStartedWithANarrowToolSet(t *testing.T) {
	dir := t.TempDir()
	args, files, err := claudeArgs(&Agent{Name: "claude", Model: "sonnet", Effort: "low"}, "/Apps/Farsh Mac/farshmac", dir, map[string]string{socketEnv: "/tmp/s", tokenEnv: "t"})
	if err != nil || len(files) != 2 {
		t.Fatal(err, files)
	}
	line := strings.Join(args, " ")
	for _, want := range []string{"--strict-mcp-config", "--allowedTools mcp__farshmac", "--tools " + DefaultTools, "--model sonnet", "--effort low", "--settings " + filepath.Join(dir, "settings.json")} {
		if !strings.Contains(line, want) {
			t.Fatalf("arguments lack %q: %s", want, line)
		}
	}
	mcp, _ := os.ReadFile(filepath.Join(dir, "mcp.json"))
	settings, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	if !strings.Contains(string(mcp), `"alwaysLoad":true`) || !strings.Contains(string(settings), `'/Apps/Farsh Mac/farshmac' --ai-mcp-event forget`) || !strings.Contains(string(settings), `"matcher":"compact"`) {
		t.Fatalf("mcp = %s\nsettings = %s", mcp, settings)
	}
	// "default" leaves the CLI's whole tool set; a list replaces FarshMac's choice.
	if args, _, _ = claudeArgs(&Agent{Name: "claude", Tools: "default"}, "/f", dir, nil); strings.Contains(strings.Join(args, " "), "--tools") {
		t.Fatalf("default tools: %q", args)
	}
	if args, _, _ = claudeArgs(&Agent{Name: "claude", Tools: "Bash"}, "/f", dir, nil); !strings.Contains(strings.Join(args, " "), "--tools Bash") {
		t.Fatalf("chosen tools: %q", args)
	}
}
