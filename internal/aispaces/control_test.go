package aispaces

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type watched struct {
	mu   sync.Mutex
	text strings.Builder
}

func watch(p *Process) *watched {
	w := &watched{}
	go func() {
		for chunk := range p.Output() {
			w.mu.Lock()
			w.text.Write(chunk)
			w.mu.Unlock()
		}
	}()
	return w
}

func (w *watched) until(t *testing.T, what string, seconds int) string {
	t.Helper()
	for i := 0; i < seconds*4; i++ {
		w.mu.Lock()
		text := w.text.String()
		w.mu.Unlock()
		if strings.Contains(text, what) {
			return text
		}
		time.Sleep(250 * time.Millisecond)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	t.Fatalf("console never showed %q: %q", what, w.text.String())
	return ""
}

// An agent the user allowed to direct another names a task level and a new
// task; the recipient's CLI is told in its own terms, in place.
func TestAnAgentDirectsAnother(t *testing.T) {
	// The fake CLI acknowledges direct commands without a picker.
	script := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(script, []byte("#!/bin/sh\necho ready\nwhile read line; do echo \"got: $line\"; case \"$line\" in */model*) echo 'Set model to sonnet';; */effort*) echo 'Set effort level to low (this session only)';; esac; done\n"), 0755)
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.CreateSpace("Space")
	for _, env := range []string{"Architect", "Coder", "Product"} {
		s.CreateEnv("Space", env, []Agent{{Name: "claude", Path: script}})
	}
	s.SetDirects("Space", "Architect", []string{"Coder"})
	s.SetPeers("Space", "Product", []string{"Coder"})
	architect, product := agentOf(s, "Architect"), agentOf(s, "Product")
	if got, _ := useTool(t, s, architect, "agent_peers", map[string]any{}); !strings.Contains(got, "Coder/claude") || !strings.Contains(got, "you direct it") {
		t.Fatalf("peers = %q", got)
	}
	if got, _ := useTool(t, s, product, "agent_peers", map[string]any{}); strings.Contains(got, "you direct it") {
		t.Fatalf("Product does not direct anyone: %q", got)
	}
	coder, err := s.Launch("Space", "Coder", "claude", 120, 40)
	if err != nil {
		t.Fatal(err)
	}
	defer stop(coder)
	defer os.Remove(s.mcp.socket)
	screen := watch(coder)
	// Directing without permission must fail, without silently delivering the task.
	got, isErr := useTool(t, s, product, "agent_send", map[string]any{"to": "Coder", "message": "plain question", "complexity": "simple", "new_task": true})
	if !isErr || !strings.Contains(got, "permission_denied") {
		t.Fatalf("undirected send = %q", got)
	}
	got, isErr = useTool(t, s, product, "agent_send", map[string]any{"to": "Coder", "message": "plain question"})
	if isErr {
		t.Fatal(got)
	}
	screen.until(t, "plain question", 30)
	if got, isErr = useTool(t, s, architect, "agent_send", map[string]any{"to": "Coder", "message": "x", "complexity": "enormous"}); !isErr || !strings.Contains(got, "simple, moderate, complex") {
		t.Fatalf("unknown level = %q", got)
	}
	got, isErr = useTool(t, s, architect, "agent_send", map[string]any{"to": "Coder", "message": "rename the function", "complexity": "simple", "new_task": true, "method": "workflow"})
	for _, want := range []string{"starts with a clean context", "is set up for a simple task", "method workflow is not available", "Workflow, Agent"} {
		if isErr || !strings.Contains(got, want) {
			t.Fatalf("directed send lacks %q: %q", want, got)
		}
	}
	text := screen.until(t, "rename the function", 60)
	clear, model, effort, task := strings.Index(text, "got: \x1b[200~/clear"), strings.Index(text, "/model sonnet"), strings.LastIndex(text, "/effort"), strings.Index(text, "rename the function")
	if clear < 0 || !(clear < model && model < effort && effort < task) || strings.Contains(text[clear:], "ultracode") {
		t.Fatalf("order: clear %d, model %d, effort %d, task %d in %q", clear, model, effort, task, text)
	}
	if !strings.Contains(text, "/effort low") || strings.Contains(text, "\x1b[D") || strings.Contains(text, "\x1b[C") {
		t.Fatalf("effort must use an explicit command, not picker keys: %q", text)
	}
	if !strings.Contains(text[task-200:task], "[New task: your earlier conversation was cleared. You still owe answers to #1") {
		t.Fatalf("the recipient is not told about the clearing: %q", text[task-300:task])
	}
	if settings := s.Settings("Space", "Coder", "claude"); settings.Level != "simple" || settings.Levels["simple"] != (Profile{"sonnet", "low"}) {
		t.Fatalf("settings = %+v", settings)
	}
	// The same level again changes nothing in the CLI; another one does.
	useTool(t, s, architect, "agent_send", map[string]any{"to": "Coder", "message": "second task", "complexity": "simple"})
	text = screen.until(t, "second task", 40)
	if strings.Count(text, "got: \x1b[200~/model") != 1 {
		t.Fatalf("an unchanged level typed commands again: %q", text)
	}
	// With the tools, the CLI is asked for a workflow in its own words.
	settings := s.Settings("Space", "Coder", "claude")
	settings.Tools = "default"
	s.SetSettings("Space", "Coder", "claude", settings)
	if got, _ = useTool(t, s, architect, "agent_send", map[string]any{"to": "Coder", "message": "audit everything", "method": "workflow"}); !strings.Contains(got, "works on it by workflow") {
		t.Fatalf("workflow = %q", got)
	}
	screen.until(t, "ultracode", 40)
	if wrap, why := (&Agent{Name: "claude", Tools: "default"}).way("research"); wrap == nil || !strings.HasPrefix(wrap("why?"), "/deep-research why?") {
		t.Fatalf("research: %v", why)
	}
}

// A stopped agent is started with the level and the new conversation; a CLI
// that cannot change in place is restarted behind the same console.
func TestDirectingStartsOrRestartsTheCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	script := filepath.Join(t.TempDir(), "codex")
	os.WriteFile(script, []byte("#!/bin/sh\necho \"started with: $*\"\nwhile read line; do echo \"got: $line\"; case \"$line\" in */model*) echo 'Unknown command: /model';; esac; done\n"), 0755)
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.CreateSpace("Space")
	s.CreateEnv("Space", "Coder", []Agent{{Name: "codex", Path: script}, {Name: "claude", Path: script, SessionID: "0a0a0a0a-0000-4000-8000-000000000001"}})
	// Stopped Claude: the level becomes flags, the new task a new session.
	AutoStart = false
	if got, err := s.operatorCall("codex", "agent_send", map[string]string{"agent": "Coder/claude", "message": "do it", "complexity": "complex", "new_task": "true"}); err != nil || !strings.Contains(got, "queued") {
		t.Fatalf("send = %q, %v", got, err)
	}
	AutoStart = true
	claude := s.Spaces[0].Envs[0].agent("claude")
	if !claude.Fresh || claude.Level != "complex" {
		t.Fatalf("agent = %+v", claude)
	}
	args, _, _ := claudeArgs(claude, "/f", t.TempDir(), nil)
	if line := strings.Join(args, " "); !strings.Contains(line, "--model opus --effort high") {
		t.Fatalf("arguments = %s", line)
	}
	// An older Codex without the argument syntax is resumed with flags.
	p, err := s.Launch("Space", "Coder", "codex", 120, 40)
	if err != nil {
		t.Fatal(err)
	}
	defer stop(p)
	defer os.Remove(s.mcp.socket)
	screen := watch(p)
	screen.until(t, "started with:", 20)
	if got, err := s.operatorCall("codex", "agent_send", map[string]string{"agent": "Coder/codex", "message": "next job", "complexity": "simple"}); err != nil || !strings.Contains(got, "is set up for a simple task") {
		t.Fatalf("send = %q, %v", got, err)
	}
	// Queue a second profile before the first is applied. It must not change
	// the launch parameters of the first queued task.
	if _, err := s.operatorCall("codex", "agent_send", map[string]string{"agent": "Coder/codex", "message": "last job", "complexity": "complex"}); err != nil {
		t.Fatal(err)
	}
	text := screen.until(t, "next job", 60)
	restarted := strings.Index(text, "FarshMac restarted this agent with low effort")
	if restarted < 0 || !strings.Contains(text[restarted:], `model_reasoning_effort="low"`) || !strings.Contains(text[restarted:], "resume --last") {
		t.Fatalf("console = %q", text)
	}
	text = screen.until(t, "last job", 60)
	if high := strings.LastIndex(text, `model_reasoning_effort="high"`); high < restarted {
		t.Fatalf("second queued profile was not applied: %q", text)
	}
	if s.Running("Space", "Coder", "codex") != p {
		t.Fatal("the console must stay the same")
	}
}

func TestCodexProfileCommandsStayInTheSameProcess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	script := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
echo ready
while IFS= read -r line; do
  echo "got: $line"
  case "$line" in
    *'/model test-model --reasoning=high'*) echo 'Model changed to test-model high for this session only';;
    *'/model --reasoning=low'*) echo 'Model changed to test-model low for this session only';;
    *'/model next-model --reasoning=low'*) echo 'Model changed to next-model low for this session only';;
    *'/model '*) echo 'Unknown command: /model';;
  esac
done
`), 0755); err != nil {
		t.Fatal(err)
	}
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.CreateSpace("Space")
	s.CreateEnv("Space", "Coder", []Agent{{Name: "codex", Path: script}})
	p, err := s.Launch("Space", "Coder", "codex", 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer stop(p)
	defer os.Remove(s.mcp.socket)
	screen := watch(p)
	screen.until(t, "ready", 10)
	p.mu.Lock()
	original := p.run
	p.mu.Unlock()
	for _, want := range []Profile{{"test-model", "high"}, {"test-model", "low"}, {"test-model", "low"}} {
		if !p.prepare(delivery{profile: want}) {
			t.Fatalf("prepare failed: %+v", want)
		}
		p.mu.Lock()
		current, run := p.current, p.run
		p.mu.Unlock()
		if current != want || run != original {
			t.Fatalf("profile changed by restarting or not applied: %+v", current)
		}
	}
	text := screen.until(t, "Model changed to test-model low", 10)
	if strings.Count(text, "got: \x1b[200~/model") != 2 || !strings.Contains(text, "/model --reasoning=low") {
		t.Fatalf("expected combined change, effort-only change, then no-op: %q", text)
	}
	// Changing only the model must carry the existing reasoning level.
	if !p.prepare(delivery{profile: Profile{Model: "next-model"}}) {
		t.Fatal("model-only change failed")
	}
	p.mu.Lock()
	current, run := p.current, p.run
	p.mu.Unlock()
	if current != (Profile{"next-model", "low"}) || run != original {
		t.Fatalf("model-only change lost effort or restarted: %+v", current)
	}
	screen.until(t, "/model next-model --reasoning=low", 10)
	// A stale success in scrollback must not acknowledge the next command.
	if codexConfigure(p, Profile{Model: "unsupported"}) {
		t.Fatal("an old acknowledgement accepted an unsupported command")
	}
}

func TestClaudeRefusedEffortSaveResumesWithFlags(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	script := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(script, []byte(`#!/bin/sh
echo "started with: $*"
while IFS= read -r line; do
  echo "got: $line"
  case "$line" in
    *'/effort high'*) echo "Couldn't set effort level: settings.json is read-only";;
  esac
done
`), 0755)
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.CreateSpace("Space")
	s.CreateEnv("Space", "Coder", []Agent{{Name: "claude", Path: script, Model: "sonnet", Effort: "low"}})
	p, err := s.Launch("Space", "Coder", "claude", 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer stop(p)
	defer os.Remove(s.mcp.socket)
	screen := watch(p)
	screen.until(t, "started with:", 10)
	s.mu.Lock()
	session := s.Spaces[0].env("Coder").agent("claude").SessionID
	s.mu.Unlock()
	// A real started Claude has a native transcript and must resume that ID.
	transcripts := filepath.Join(home, ".claude", "projects", "probe")
	if err := os.MkdirAll(transcripts, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(transcripts, session+".jsonl"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if !p.prepare(delivery{profile: Profile{"sonnet", "high"}}) {
		t.Fatal("failed to fall back to launch arguments")
	}
	text := screen.until(t, "FarshMac restarted this agent", 10)
	text = screen.until(t, "--effort high", 10)
	if !strings.Contains(text, "/effort high") || strings.Contains(text, "\x1b[D") || strings.Contains(text, "\x1b[C") || strings.Contains(text, "--disable-slash-commands") {
		t.Fatalf("unexpected control path: %q", text)
	}
	if !strings.Contains(text, "--resume "+session) {
		t.Fatalf("fallback must retain the native conversation: %q", text)
	}
	if s.Running("Space", "Coder", "claude") != p {
		t.Fatal("fallback replaced the console")
	}
}

// No model request is sent. Exercise the real CLI parser/acknowledgement and
// the launch sandbox, while proving that the personal config stays untouched.
func TestNativeCodexProfileCommands(t *testing.T) {
	gateway := os.Getenv("FARSHMAC_AI_CODEX_CONTROL_PROBE")
	if gateway == "" {
		t.Skip("set FARSHMAC_AI_CODEX_CONTROL_PROBE to a built farshmac executable")
	}
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Skip(err)
	}
	var candidates []ModelChoice
	for _, model := range ModelCatalog("codex") {
		if oneOf("medium", model.Efforts) && oneOf("high", model.Efforts) {
			candidates = append(candidates, model)
		}
	}
	if len(candidates) < 2 {
		t.Skip("two catalog models with medium and high effort are needed")
	}
	home, _ := os.UserHomeDir()
	configHome := os.Getenv("CODEX_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".codex")
	}
	config := filepath.Join(configHome, "config.toml")
	before, readErr := os.ReadFile(config)
	defer func() {
		after, err := os.ReadFile(config)
		if (readErr == nil) != (err == nil) || !bytes.Equal(before, after) {
			t.Error("personal Codex config changed")
		}
	}()
	gatewayExecutable = gateway
	defer func() { gatewayExecutable = "" }()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.CreateSpace("Space")
	s.CreateEnv("Space", "Coder", []Agent{{Name: "codex", Path: binary, Model: candidates[0].ID, Effort: "medium"}})
	p, err := s.Launch("Space", "Coder", "codex", 140, 40)
	if err != nil {
		t.Fatal(err)
	}
	defer stop(p)
	defer os.Remove(s.mcp.socket)
	screen := watch(p)
	p.idle()
	p.mu.Lock()
	original := p.run
	p.mu.Unlock()
	for _, want := range []Profile{{candidates[0].ID, "high"}, {candidates[1].ID, "medium"}} {
		if !p.prepare(delivery{profile: want}) {
			t.Fatalf("prepare failed for %+v", want)
		}
		p.mu.Lock()
		current, run := p.current, p.run
		p.mu.Unlock()
		if run != original || current != want {
			screen.mu.Lock()
			text := screen.text.String()
			screen.mu.Unlock()
			t.Fatalf("CLI did not apply %+v in place; tail: %q", want, text[max(0, len(text)-1800):])
		}
	}
}

// stop ends an agent and waits until its console has written its last state,
// so a test's temporary store is quiet before it is removed.
func stop(p *Process) {
	p.Close()
	select {
	case <-p.ended:
	case <-time.After(10 * time.Second):
	}
}

// Personal-environment check with a real Claude Code (needs
// FARSHMAC_AI_CLAUDE_PROBE): a running agent is given a simple new task; its
// conversation is cleared and its model and effort change in place, and the
// user's own settings file stays as it was.
func TestClaudeIsDirectedInPlace(t *testing.T) {
	gatewayExecutable = os.Getenv("FARSHMAC_AI_CLAUDE_PROBE")
	if gatewayExecutable == "" {
		t.Skip("set FARSHMAC_AI_CLAUDE_PROBE to a built farshmac executable")
	}
	defer func() { gatewayExecutable = "" }()
	claude, err := exec.LookPath("claude")
	if err != nil {
		t.Skip(err)
	}
	home, _ := os.UserHomeDir()
	settingsFile := filepath.Join(home, ".claude", "settings.json")
	before, readErr := os.ReadFile(settingsFile)
	defer func() {
		after, err := os.ReadFile(settingsFile)
		if (readErr == nil) != (err == nil) || !bytes.Equal(before, after) {
			if readErr == nil {
				os.WriteFile(settingsFile, before, 0600)
			}
			t.Errorf("the user's settings file was changed by the agent; it has been restored")
		}
	}()
	s, _, _ := testEnv(t)
	s.AddAgents("Space", "Dev", []Agent{{Name: "claude", Path: claude}})
	env := s.Spaces[0].env("Dev")
	p, err := s.Launch("Space", "Dev", "claude", 120, 40)
	if err != nil {
		t.Fatal(err)
	}
	defer stop(p)
	defer os.Remove(s.mcp.socket)
	screen := watch(p)
	answered := func(session string) (model, effort string, steps int) {
		data, _ := os.ReadFile(transcriptFile(session))
		for _, line := range strings.Split(string(data), "\n") {
			var rec struct {
				Type    string `json:"type"`
				Effort  string `json:"effort"`
				Message struct {
					Model string `json:"model"`
				} `json:"message"`
			}
			if json.Unmarshal([]byte(line), &rec) == nil && rec.Type == "assistant" {
				model, effort, steps = rec.Message.Model, rec.Effort, steps+1
			}
		}
		return
	}
	session := func() string {
		s.mu.Lock()
		defer s.mu.Unlock()
		return env.agent("claude").SessionID
	}
	wait := func(what string, done func() bool) {
		t.Helper()
		for i := 0; i < 240; i++ {
			if done() {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		screen.mu.Lock()
		text := screen.text.String()
		screen.mu.Unlock()
		t.Fatalf("%s did not happen; console tail: %q", what, text[max(0, len(text)-900):])
	}
	first := session()
	if _, err := s.operatorCall("test", "agent_send", map[string]string{"agent": "Dev", "message": "Reply to this with agent_reply: the single word one."}); err != nil {
		t.Fatal(err)
	}
	wait("the first answer", func() bool { _, _, n := answered(first); busy, _ := p.busy(); return n > 0 && !busy })
	model, effort, _ := answered(first)
	t.Logf("before: model %s, effort %s", model, effort)
	got, err := s.operatorCall("test", "agent_send", map[string]string{"agent": "Dev", "message": "Reply to this with agent_reply: the single word two.", "complexity": "simple", "new_task": "true"})
	if err != nil || !strings.Contains(got, "starts with a clean context and is set up for a simple task") {
		t.Fatalf("send = %q, %v", got, err)
	}
	wait("the new conversation", func() bool { return session() != first })
	wait("the second answer", func() bool { _, _, n := answered(session()); busy, _ := p.busy(); return n > 0 && !busy })
	model, effort, steps := answered(session())
	t.Logf("after: model %s, effort %s, %d model calls in the new conversation", model, effort, steps)
	if !strings.Contains(model, "sonnet") || effort != "low" {
		t.Errorf("the simple level did not reach the CLI: model %s, effort %s", model, effort)
	}
	if u, err := AnalyzeTranscript(transcriptFile(session())); err == nil {
		t.Logf("first context of the new conversation: %d tokens", u.FirstContext)
	}
	if s.Running("Space", "Dev", "claude") != p {
		t.Error("the agent was replaced instead of changed in place")
	}
}

// Model and effort can be named outright; a contact link works both ways; a
// disabled agent is out of the way.
func TestExplicitChoicesLinksAndDisabledAgents(t *testing.T) {
	AutoStart = false
	defer func() { AutoStart = true }()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.CreateSpace("Space")
	s.CreateEnv("Space", "Architect", []Agent{{Name: "claude", Path: "/bin/echo"}})
	s.CreateEnv("Space", "Coder", []Agent{{Name: "claude", Path: "/bin/echo"}, {Name: "codex", Path: "/bin/echo"}})
	s.CreateEnv("Space", "Product", []Agent{{Name: "claude", Path: "/bin/echo"}})
	s.SetPeers("Space", "Architect", []string{"Product"})
	s.SetDirects("Space", "Architect", []string{"Coder"})
	architect, product := agentOf(s, "Architect"), agentOf(s, "Product")
	// The link set on one Env holds for the other; removing it removes both.
	if got, isErr := useTool(t, s, product, "agent_send", map[string]any{"to": "Architect", "message": "feature request"}); isErr {
		t.Fatalf("the link must be two-way: %q", got)
	}
	s.SetPeers("Space", "Product", nil)
	if _, allowed := s.Peers("Space", "Architect"); allowed["Product"] {
		t.Fatal("removing a link on one side must remove it on the other")
	}
	// Two agents in the Env: the short address is refused, the peers list
	// says what may be named for each.
	peers, _ := useTool(t, s, architect, "agent_peers", map[string]any{})
	for _, want := range []string{"Coder/claude", "catalog/configured models haiku/sonnet/opus", "Coder/codex", "configured complexity simple/moderate/complex", "agent_send takes model and effort"} {
		if !strings.Contains(peers, want) {
			t.Fatalf("peers lack %q:\n%s", want, peers)
		}
	}
	if got, isErr := useTool(t, s, architect, "agent_send", map[string]any{"to": "Coder", "message": "x"}); !isErr || !strings.Contains(got, "ambiguous") {
		t.Fatalf("two agents = %q", got)
	}
	got, isErr := useTool(t, s, architect, "agent_send", map[string]any{"to": "Coder/claude", "message": "refactor storage", "model": "opus", "effort": "xhigh"})
	if isErr || !strings.Contains(got, "gets model opus and gets xhigh effort") {
		t.Fatalf("explicit = %q", got)
	}
	coder := s.Spaces[0].env("Coder")
	if p := coder.agent("claude").profile(); p != (Profile{"opus", "xhigh"}) {
		t.Fatalf("profile = %+v", p)
	}
	// A level named later replaces what was named outright.
	useTool(t, s, architect, "agent_send", map[string]any{"to": "Coder/claude", "message": "rename", "complexity": "simple"})
	if p := coder.agent("claude").profile(); p != (Profile{"sonnet", "low"}) {
		t.Fatalf("after a level: %+v", p)
	}
	for _, bad := range []map[string]any{{"effort": "invalid-effort", "to": "Coder/codex"}, {"model": "opus; rm -rf /", "to": "Coder/claude"}} {
		bad["message"] = "x"
		if got, isErr := useTool(t, s, architect, "agent_send", bad); !isErr {
			t.Fatalf("accepted %v: %q", bad, got)
		}
	}
	// Disabled: not listed, not addressed, not started; the Env answers by its name again.
	if err := s.SetDisabled("Space", "Coder", "codex", true); err != nil {
		t.Fatal(err)
	}
	if peers, _ = useTool(t, s, architect, "agent_peers", map[string]any{}); strings.Contains(peers, "Coder/codex") {
		t.Fatalf("a disabled agent is listed:\n%s", peers)
	}
	if got, isErr := useTool(t, s, architect, "agent_send", map[string]any{"to": "Coder", "message": "now unambiguous"}); isErr || !strings.Contains(got, "to Coder/claude") {
		t.Fatalf("short address = %q", got)
	}
	if got, isErr := useTool(t, s, architect, "agent_send", map[string]any{"to": "Coder/codex", "message": "x"}); !isErr || !strings.Contains(got, "disabled by the user") {
		t.Fatalf("disabled agent = %q", got)
	}
	if _, err := s.Launch("Space", "Coder", "codex", 80, 24); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("a disabled agent was started: %v", err)
	}
	if list, _ := s.overview("Space"); !strings.Contains(list, "Coder/codex: disabled by the user") {
		t.Fatalf("overview = %s", list)
	}
}
