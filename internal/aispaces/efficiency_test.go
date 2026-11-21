package aispaces

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// Efficiency is tested the way an agent is billed. A scenario is a sequence
// of model steps; each step types its tool calls (output), receives their
// results (new context), and re-reads everything that came before (cached
// context). The meter runs the real tools and prices the bytes that would
// pass through the model, so a change to a tool's output or to the number of
// calls a task needs shows up as a number.
//
// Every scenario is taken from a real session log and has two forms: what an
// agent had to do before, and what the tools allow now. The test fails when
// the new form stops being cheaper, or when it becomes dearer than the
// recorded budget. Budgets go down when a tool improves; raising one needs a
// reason.

const (
	meterBase  = 40_000 // tokens of the CLI's own prompt and tools, re-read on every step
	meterAfter = 20     // further steps of the session during which the scenario's text stays in context
	meterRead  = 0.05   // price ratios of the model the logs were recorded with
)

type meter struct {
	t       *testing.T
	s       *Store
	steps   int
	context float64 // tokens accumulated by the scenario
	cost    float64
	typed   int // bytes the model wrote
	read    int // bytes the model received
}

type invocation struct {
	session *mcpSession
	name    string
	args    map[string]any
}

// step is one model call that makes these tool calls together.
func (m *meter) step(calls ...invocation) []string {
	m.t.Helper()
	var out []string
	typed, read := 0, 0
	for _, c := range calls {
		args, _ := json.Marshal(c.args)
		text, isErr := useTool(m.t, m.s, c.session, c.name, c.args)
		if isErr {
			m.t.Fatalf("%s failed: %s", c.name, text)
		}
		typed += len(c.name) + len(args)
		read += len(text)
		out = append(out, text)
	}
	m.account(typed, read)
	return out
}

// account prices a step from its sizes alone, for behaviour that the current
// tools no longer show.
func (m *meter) account(typed, read int) {
	m.steps++
	m.typed, m.read = m.typed+typed, m.read+read
	fresh := float64(typed+read) / 4
	m.cost += float64(typed)/4*weightOutput + fresh*weightWrite1h + (meterBase+m.context)*meterRead
	m.context += fresh
}

// total adds what the scenario leaves behind in the context.
func (m *meter) total() float64 { return m.cost + m.context*meterRead*meterAfter }

func invoke(session *mcpSession, name string, args map[string]any) invocation {
	return invocation{session, name, args}
}

// efficiencyFixture is a project of the size seen in the logs: some thirty
// top-level entries, a few hundred files, long documents.
func efficiencyFixture(t *testing.T) (*Store, string) {
	t.Helper()
	project := filepath.Join(t.TempDir(), "project")
	for m := 0; m < 14; m++ {
		for _, sub := range []string{"cmd", "internal/handlers", "internal/storage", "web/src"} {
			dir := filepath.Join(project, fmt.Sprintf("service%02d", m), sub)
			os.MkdirAll(dir, 0755)
			for f := 0; f < 8; f++ {
				body := fmt.Sprintf("package x\n\nfunc Handle%d() error {\n\treturn nil\n}\n\nfunc helper%d() {}\n", f, f)
				os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%d.go", f)), []byte(body), 0644)
			}
		}
	}
	os.MkdirAll(filepath.Join(project, "docs"), 0755)
	sentence := "The service keeps its data in one place and every change is recorded with its author and time, so that an operator can explain later what happened and why. "
	for d := 0; d < 6; d++ {
		var doc strings.Builder
		fmt.Fprintf(&doc, "# Document %d\n\n%s\n\n", d, sentence)
		for sec := 0; sec < 12; sec++ {
			fmt.Fprintf(&doc, "## Section %d.%d\n\n", d, sec)
			for p := 0; p < 6; p++ {
				doc.WriteString(strings.Repeat(sentence, 3) + "\n\n")
			}
		}
		doc.WriteString("## Retention\n\nExported files are kept for 45 days and then removed. " + sentence + "\n")
		os.WriteFile(filepath.Join(project, "docs", fmt.Sprintf("doc%d.md", d)), []byte(doc.String()), 0644)
	}
	os.MkdirAll(filepath.Join(project, "src"), 0755)
	for f := 0; f < 4; f++ {
		var code strings.Builder
		for fn := 0; fn < 40; fn++ {
			fmt.Fprintf(&code, "export function step%d_%d(input) {\n  const value = input * %d\n  return value + %d\n}\n\n", f, fn, fn+2, fn)
		}
		os.WriteFile(filepath.Join(project, "src", fmt.Sprintf("module%d.js", f)), []byte(code.String()), 0644)
	}
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(script, []byte("#!/bin/sh\nwhile read line; do :; done\n"), 0755)
	s.CreateSpace("Space")
	for _, env := range []string{"Architect", "Coder"} {
		s.CreateEnv("Space", env, []Agent{{Name: "claude", Path: script}})
		if err := s.Attach("Space", env, "/", []string{project}, false); err != nil {
			t.Fatal(err)
		}
	}
	s.SetPeers("Space", "Architect", []string{"Coder"})
	s.SetPeers("Space", "Coder", []string{"Architect"})
	return s, project
}

// The listing and search output as they were before budgets, to price the
// old behaviour on the same project.
func flatTree(root string, depth int) string {
	var out strings.Builder
	var walk func(dir, prefix string, level int)
	walk = func(dir, prefix string, level int) {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() {
				fmt.Fprintf(&out, "%s%s/\n", prefix, e.Name())
				if level < depth {
					walk(filepath.Join(dir, e.Name()), prefix+e.Name()+"/", level+1)
				}
				continue
			}
			info, _ := e.Info()
			fmt.Fprintf(&out, "%s%s\t%d bytes\n", prefix, e.Name(), info.Size())
		}
	}
	walk(root, "", 1)
	return out.String()
}

func flatSearch(root, virtual string, re *regexp.Regexp) string {
	var out []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || len(out) >= 200 {
			return nil
		}
		data, _ := os.ReadFile(p)
		for i, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) && len(out) < 200 {
				if len(line) > 300 {
					line = line[:300] + "…"
				}
				out = append(out, fmt.Sprintf("%s%s:%d: %s", virtual, strings.TrimPrefix(p, root), i+1, line))
			}
		}
		return nil
	})
	return strings.Join(out, "\n")
}

func TestEfficiencyScenarios(t *testing.T) {
	AutoStart = false
	defer func() { AutoStart = true }()
	type scenario struct {
		name     string
		maxRatio float64 // the new form may cost at most this share of the old one
		budget   float64 // and at most this much, in token units
		run      func(t *testing.T, before, after *meter, s *Store, project string)
	}
	scenarios := []scenario{
		{"orient in a large project", 0.6, 3500, func(t *testing.T, before, after *meter, s *Store, project string) {
			// Log: an agent's first call returned 502 lines, 24 KB, and they stayed for the whole session.
			before.account(60, len(flatTree(project, 3)))
			after.step(invoke(agentOf(s, "Coder"), "vfs_list", map[string]any{"path": "/", "depth": 3}))
		}},
		{"find one rule in the documents", 0.3, 6000, func(t *testing.T, before, after *meter, s *Store, project string) {
			// Log: 17 of 21 reads of an architect took whole documents of 10–18 KB.
			old, architect := agentOf(s, "Architect"), agentOf(s, "Architect")
			before.step(invoke(old, "vfs_list", map[string]any{"path": "/project/docs"}))
			before.step(invoke(old, "vfs_read", map[string]any{"path": "/project/docs/doc3.md"}))
			after.step(invoke(architect, "vfs_search", map[string]any{"path": "/project/docs", "text": "kept for", "glob": "*.md"}))
			got := after.step(invoke(architect, "vfs_read", map[string]any{"path": "/project/docs/doc3.md", "section": "Retention"}))
			if !strings.Contains(got[0], "45 days") {
				t.Fatalf("the section was not found: %q", got[0])
			}
		}},
		{"learn the structure of the documents", 0.1, 4200, func(t *testing.T, before, after *meter, s *Store, project string) {
			// Log: an architect read six documents in full to get oriented, then searched them for headings.
			old, architect := agentOf(s, "Architect"), agentOf(s, "Architect")
			var all []invocation
			for d := 0; d < 6; d++ {
				all = append(all, invoke(old, "vfs_read", map[string]any{"path": fmt.Sprintf("/project/docs/doc%d.md", d)}))
			}
			before.step(all...)
			after.step(invoke(architect, "vfs_outline", map[string]any{"path": "/project/docs"}))
		}},
		{"review a colleague's change", 0.35, 3600, func(t *testing.T, before, after *meter, s *Store, project string) {
			// Log: an architect read the same four source files three times while a coder worked on them.
			old, architect, coder := agentOf(s, "Architect"), agentOf(s, "Architect"), agentOf(s, "Coder")
			var files []invocation
			for f := 0; f < 4; f++ {
				files = append(files, invoke(architect, "vfs_read", map[string]any{"path": fmt.Sprintf("/project/src/module%d.js", f)}))
			}
			for _, c := range files { // both reviewers know the files; this part is not priced
				useTool(t, s, old, c.name, c.args)
				useTool(t, s, architect, c.name, c.args)
			}
			useTool(t, s, coder, "vfs_edit", map[string]any{"edits": []map[string]any{
				{"path": "/project/src/module1.js", "old_text": "return value + 7\n", "new_text": "return value + 70\n"},
				{"path": "/project/src/module2.js", "old_text": "const value = input * 9\n", "new_text": "const value = input * 9\n  audit(value)\n"},
			}})
			var again []invocation
			for _, c := range files {
				args := map[string]any{"path": c.args["path"], "full": true}
				again = append(again, invoke(old, c.name, args))
			}
			before.step(again...)
			got := after.step(files...)
			if !strings.HasPrefix(got[0], "[unchanged]") || !strings.Contains(got[1], "+  return value + 70") || !strings.Contains(got[2], "+  audit(value)") {
				t.Fatalf("review = %q", got)
			}
		}},
		{"search documents with long lines", 0.3, 6300, func(t *testing.T, before, after *meter, s *Store, project string) {
			// Log: one search in one document returned 16.8 KB, 95 lines of prose.
			before.account(90, len(flatSearch(filepath.Join(project, "docs"), "/project/docs", regexp.MustCompile("operator"))))
			after.step(invoke(agentOf(s, "Architect"), "vfs_search", map[string]any{"path": "/project/docs", "text": "operator"}))
		}},
		{"hand over a long answer", 0.5, 7700, func(t *testing.T, before, after *meter, s *Store, project string) {
			// Log: an agent was asked for a file "verbatim" and typed 4.7 KB into its reply;
			// the recipient needed one part of it.
			architect, coder := agentOf(s, "Architect"), agentOf(s, "Coder")
			useTool(t, s, architect, "agent_send", map[string]any{"to": "Coder", "message": "Which rules apply to storage?"})
			useTool(t, s, architect, "agent_send", map[string]any{"to": "Coder", "message": "And to retention?"})
			data, _ := os.ReadFile(filepath.Join(project, "docs", "doc2.md"))
			pasted := string(data[:4700])
			before.step(invoke(coder, "agent_reply", map[string]any{"request_id": "1", "message": "Here are the rules:\n" + pasted}))
			before.account(0, len(pasted)+120) // the recipient's prompt
			after.step(invoke(coder, "agent_reply", map[string]any{"request_id": "2", "message": "The rules are attached.", "attach": []string{"/project/docs/doc2.md"}}))
			after.account(0, 400) // the recipient's prompt: the note and the list
			after.step(invoke(architect, "msg_read", map[string]any{"request_id": "2", "attachment": 1, "section": "Retention"}))
		}},
		{"write a file into a new folder", 0.5, 16000, func(t *testing.T, before, after *meter, s *Store, project string) {
			// Log: the write failed, the agent made the folder and typed 5.5 KB a second time.
			content := strings.Repeat("flowchart LR\n  A[Client] --> B[Server]\n", 140)
			typed, _ := json.Marshal(content)
			before.account(len(typed)+80, 60) // refused
			before.account(70, 30)            // vfs_mkdir
			before.account(len(typed)+80, 90) // typed again
			after.step(invoke(agentOf(s, "Architect"), "vfs_write", map[string]any{"path": "/project/docs/diagrams/components.md", "content": content}))
		}},
	}
	t.Logf("%-40s %14s %14s %7s   %s", "scenario", "before", "now", "ratio", "steps, bytes typed/read")
	for _, sc := range scenarios {
		s, project := efficiencyFixture(t)
		before, after := &meter{t: t, s: s}, &meter{t: t, s: s}
		sc.run(t, before, after, s, project)
		ratio := after.total() / before.total()
		t.Logf("%-40s %14.0f %14.0f %6.0f%%   %d→%d steps, %d/%d → %d/%d", sc.name, before.total(), after.total(), 100*ratio,
			before.steps, after.steps, before.typed, before.read, after.typed, after.read)
		if ratio > sc.maxRatio {
			t.Errorf("%s: now costs %.0f%% of the old way, allowed %.0f%%", sc.name, 100*ratio, 100*sc.maxRatio)
		}
		if after.total() > sc.budget {
			t.Errorf("%s: costs %.0f token units, budget %.0f", sc.name, after.total(), sc.budget)
		}
	}
}

// What every agent pays on every step, whether it uses it or not.
func TestToolSurfaceBudget(t *testing.T) {
	schema, _ := json.Marshal(tools)
	operator, _ := json.Marshal(operatorTools)
	for _, item := range []struct {
		name   string
		size   int
		budget int
	}{
		{"Env tool definitions", len(schema), 11000},
		{"Env server instructions", len(serverInstructions), 1700},
		{"agent notes", len(agentNotes), 2300},
		{"operator tool definitions", len(operator), 4200},
		{"operator instructions", len(operatorInstructions), 1100},
	} {
		t.Logf("%-28s %6d bytes (budget %d)", item.name, item.size, item.budget)
		if item.size > item.budget {
			t.Errorf("%s grew to %d bytes, budget %d: it is paid for on every step of every agent", item.name, item.size, item.budget)
		}
	}
}

func TestUsageIsReadFromATranscript(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "projects", "scratch")
	os.MkdirAll(dir, 0700)
	usage := func(read, write, out int) string {
		return fmt.Sprintf(`"usage":{"input_tokens":2,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d,"output_tokens":%d,"output_tokens_details":{"thinking_tokens":5},"cache_creation":{"ephemeral_1h_input_tokens":%d,"ephemeral_5m_input_tokens":0}}`, read, write, out, write)
	}
	file := strings.Repeat("a line of the file that is long enough\n", 10)
	quoted, _ := json.Marshal(file)
	lines := []string{
		`{"type":"user","message":{"content":"Read the file twice and report."}}`,
		`{"type":"assistant","message":{"id":"m1","model":"claude-opus-5-5","content":[{"type":"tool_use","id":"t1","name":"mcp__farshmac__vfs_read","input":{"path":"/a.md"}}],` + usage(0, 40000, 50) + `}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":` + string(quoted) + `}]}]}}`,
		`{"type":"assistant","message":{"id":"m2","model":"claude-opus-5-5","content":[{"type":"tool_use","id":"t2","name":"mcp__farshmac__vfs_read","input":{"path":"/a.md"}}],` + usage(40000, 200, 40) + `}}`,
		// One model call is stored as several records; it is counted once.
		`{"type":"assistant","message":{"id":"m2","model":"claude-opus-5-5","content":[{"type":"tool_use","id":"t3","name":"mcp__farshmac__agent_reply","input":{"request_id":"1","message":"done"}}],` + usage(40000, 200, 40) + `}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":` + string(quoted) + `},{"type":"tool_result","tool_use_id":"t3","is_error":true,"content":"conflict"}]}}`,
		`{"type":"assistant","message":{"id":"m3","model":"claude-opus-5-5","content":[{"type":"text","text":"I replied that the work is done."}],` + usage(40200, 300, 30) + `}}`,
		`{"type":"system","subtype":"compact_boundary"}`,
		`{"type":"assistant","message":{"id":"m4","model":"claude-opus-5-5","content":[{"type":"text","text":"ok"}],` + usage(30000, 100, 10) + `}}`,
	}
	os.WriteFile(filepath.Join(dir, "session-1.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0600)
	u, err := AnalyzeTranscript(transcriptFile("session-1"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Steps != 4 || u.Compactions != 1 || u.FirstContext != 40002 || u.MaxContext != 40502 || u.CacheRead != 110200 || u.Write1h != 40600 || u.Output != 130 || u.Thinking != 20 {
		t.Fatalf("usage = %+v", u)
	}
	if u.ToolErrors != 1 || u.ReadChars != 2*len(file) || u.RereadChars == 0 || u.RereadChars > len(file) || u.ConsoleAfterMessage != len("I replied that the work is done.") {
		t.Fatalf("signals = %+v", u)
	}
	// 110200 cached tokens at 0.05, 40600 written at 2, 130 output at 5, 8 uncached.
	if cost := u.Cost(); cost != 8+110200*0.05+40600*2+130*5 {
		t.Fatalf("cost = %v", cost)
	}
	if u.Content["vfs_read result"] <= u.Content["agent_reply input"] {
		t.Fatalf("content = %v", u.Content)
	}
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.CreateSpace("Space")
	s.CreateEnv("Space", "Architect", []Agent{{Name: "claude", Path: "/bin/true", SessionID: "session-1"}})
	s.CreateEnv("Space", "Coder", []Agent{{Name: "claude", Path: "/bin/true"}})
	report := s.UsageReport("Space")
	for _, want := range []string{"Architect/claude", "4 steps", "compacted 1 time(s)", "file text read again in the same context", "tool errors 1", "Coder/claude\n  no transcript", "Space total:"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report lacks %q:\n%s", want, report)
		}
	}
}

// Personal-environment measurement: a real Claude Code works on the fixture
// through the Env tools, started exactly as FarshMac starts it, and reports
// what it spent. It costs real tokens, so it runs only on request:
//
//	FARSHMAC_AI_CLAUDE_PROBE=<built farshmac> go test ./internal/aispaces -run TestClaudeEfficiency -v
//
// FARSHMAC_AI_BENCH selects the variants to compare (default "narrow"):
// "narrow" is FarshMac's tool set, "all" is every built-in tool of the CLI.
// FARSHMAC_AI_BENCH_MAX_UNITS fails the test when a task costs more.
func TestClaudeEfficiency(t *testing.T) {
	gateway := os.Getenv("FARSHMAC_AI_CLAUDE_PROBE")
	if gateway == "" {
		t.Skip("set FARSHMAC_AI_CLAUDE_PROBE to a built farshmac executable")
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude is not installed")
	}
	variants := strings.Split(os.Getenv("FARSHMAC_AI_BENCH"), ",")
	if variants[0] == "" {
		variants = []string{"narrow"}
	}
	tasks := []struct{ name, prompt, want string }{
		{"lookup", "Using the project documents, find out for how many days exported files are kept. Answer with the number only.", "45"},
		{"structure", "Which level-2 sections of /project/docs/doc4.md have a title that ends with the digit 7? Answer with the section titles only.", "section 4.7"},
		{"edit", "In /project/docs/doc1.md the retention rule says exported files are kept for 45 days. Change it to 60 days in that document only, then answer with the single word done.", "done"},
	}
	if only := os.Getenv("FARSHMAC_AI_BENCH_TASKS"); only != "" {
		kept := tasks[:0]
		for _, task := range tasks {
			if strings.Contains(","+only+",", ","+task.name+",") {
				kept = append(kept, task)
			}
		}
		tasks = kept
	}
	t.Logf("%-8s %-8s %5s %9s %9s %9s %8s %9s %7s  %s", "variant", "task", "steps", "first ctx", "cache rd", "cache wr", "output", "units", "seconds", "tools")
	for _, variant := range variants {
		for _, task := range tasks {
			s, project := efficiencyFixture(t)
			s.mu.Lock()
			server, err := s.startMCP()
			env := s.Spaces[0].env("Architect")
			s.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			agent := &Agent{Name: "claude", Path: claude, SessionID: uuid()}
			if variant == "all" {
				agent.Tools = "default"
			}
			dir := s.agentDir(env, "claude")
			scratch := filepath.Join(dir, "scratch")
			os.MkdirAll(filepath.Join(scratch, ".tmp"), 0700)
			os.WriteFile(filepath.Join(scratch, "CLAUDE.md"), []byte(fmt.Sprintf(agentNotes, env.Name, env.Name, "Space", address(env, "claude"), "")), 0600)
			token := server.register(env, "claude", agent.SessionID)
			args, files, err := claudeArgs(agent, gateway, dir, map[string]string{socketEnv: server.socket, tokenEnv: token})
			if err != nil {
				t.Fatal(err)
			}
			profile, err := sandboxProfile(scratch, append([]string{gateway, filepath.Dir(realPath(claude))}, files...)...)
			if err != nil {
				t.Fatal(err)
			}
			args = append(args, "--session-id", agent.SessionID, "--output-format", "json", "-p", task.prompt)
			cmd := exec.Command(sandboxExec, append([]string{"-p", profile, claude}, args...)...)
			cmd.Dir = scratch
			cmd.Env = append(agentEnviron(os.Environ()), "TMPDIR="+filepath.Join(scratch, ".tmp"), socketEnv+"="+server.socket, tokenEnv+"="+token)
			out, runErr := cmd.Output()
			os.Remove(server.socket)
			var result struct {
				Result   string  `json:"result"`
				Turns    int     `json:"num_turns"`
				Duration float64 `json:"duration_ms"`
				Cost     float64 `json:"total_cost_usd"`
				IsError  bool    `json:"is_error"`
			}
			if json.Unmarshal(out, &result) != nil || runErr != nil || result.IsError {
				t.Fatalf("%s/%s: claude failed: %v\n%s", variant, task.name, runErr, out)
			}
			// The tools it chose, from the Env's own log.
			logged, _ := os.ReadFile(filepath.Join(s.envDir(env), LogName))
			var used []string
			for _, line := range strings.Split(string(logged), "\n") {
				var entry logEntry
				if json.Unmarshal([]byte(line), &entry) == nil && entry.Tool != "" {
					used = append(used, strings.TrimPrefix(entry.Tool, "vfs_"))
				}
			}
			u, err := AnalyzeTranscript(transcriptFile(agent.SessionID))
			if err != nil || u.Steps == 0 {
				t.Fatalf("%s/%s: no transcript for session %s: %v", variant, task.name, agent.SessionID, err)
			}
			t.Logf("%-8s %-8s %5d %9d %9d %9d %8d %9.0f %7.1f  %s", variant, task.name, u.Steps, u.FirstContext, u.CacheRead, u.Write5m+u.Write1h, u.Output, u.Cost(), result.Duration/1000, strings.Join(used, " "))
			if !strings.Contains(strings.ToLower(result.Result), task.want) {
				t.Errorf("%s/%s: answered %q, want %q", variant, task.name, result.Result, task.want)
			}
			if task.name == "edit" {
				if data, _ := os.ReadFile(filepath.Join(project, "docs", "doc1.md")); !strings.Contains(string(data), "kept for 60 days") {
					t.Errorf("%s/edit: the document was not changed", variant)
				}
			}
			var limit float64
			if fmt.Sscan(os.Getenv("FARSHMAC_AI_BENCH_MAX_UNITS"), &limit); limit > 0 && u.Cost() > limit {
				t.Errorf("%s/%s: cost %.0f units, limit %.0f", variant, task.name, u.Cost(), limit)
			}
		}
	}
}

// Personal-environment check of the hook wiring: a real Claude Code on a PTY,
// started by Launch, reads a file, compacts its conversation, and the Env
// server learns of it, so the next read returns the text again.
func TestClaudeReportsCompaction(t *testing.T) {
	gatewayExecutable = os.Getenv("FARSHMAC_AI_CLAUDE_PROBE")
	if gatewayExecutable == "" {
		t.Skip("set FARSHMAC_AI_CLAUDE_PROBE to a built farshmac executable")
	}
	defer func() { gatewayExecutable = "" }()
	claude, err := exec.LookPath("claude")
	if err != nil {
		t.Skip(err)
	}
	s, _, _ := testEnv(t)
	s.AddAgents("Space", "Dev", []Agent{{Name: "claude", Path: claude}})
	p, err := s.Launch("Space", "Dev", "claude", 120, 40)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	defer os.Remove(s.mcp.socket)
	var screen strings.Builder
	var mu sync.Mutex
	go func() {
		for chunk := range p.Output() {
			mu.Lock()
			screen.Write(chunk)
			mu.Unlock()
		}
	}()
	env := s.Spaces[0].env("Dev")
	logged := func() string {
		data, _ := os.ReadFile(filepath.Join(s.envDir(env), LogName))
		return string(data)
	}
	until := func(what string, seconds int, done func() bool) {
		t.Helper()
		for i := 0; i < seconds*2; i++ {
			if done() {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		mu.Lock()
		text := screen.String()
		mu.Unlock()
		t.Logf("MCP log:\n%s", logged())
		t.Fatalf("%s did not happen; screen tail: %q", what, text[max(0, len(text)-1200):])
	}
	p.idle()
	p.Deliver("Read /project/a.txt with the vfs_read tool and answer with its content only.")
	until("the first read", 90, func() bool { return strings.Count(logged(), `"tool":"vfs_read"`) == 1 })
	p.idle()
	p.Deliver("/compact")
	until("the compaction report", 180, func() bool { return strings.Contains(logged(), forgetMethod) })
	p.idle()
	p.Deliver("Read /project/a.txt with the vfs_read tool once more and answer with its content only.")
	until("the second read", 90, func() bool { return strings.Count(logged(), `"tool":"vfs_read"`) == 2 })
	entries := strings.Split(strings.TrimSpace(logged()), "\n")
	var last logEntry
	for _, line := range entries {
		var entry logEntry
		if json.Unmarshal([]byte(line), &entry) == nil && entry.Tool == "vfs_read" {
			last = entry
		}
	}
	if !strings.HasPrefix(last.Out, "alpha") {
		t.Fatalf("after compaction the file must be sent again: %q", last.Out)
	}
	p.idle()
	t.Logf("\n%s", s.UsageReport("Space"))
}
