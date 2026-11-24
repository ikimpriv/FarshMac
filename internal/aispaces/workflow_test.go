package aispaces

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompactWorkflowPermissionsAndBatch(t *testing.T) {
	s, fs, _ := testEnv(t)
	session := agentOf(s, "Dev")
	data, _ := json.Marshal(compactTools)
	if len(data) > 2000 {
		t.Fatalf("tool surface %d", len(data))
	}
	request := map[string]any{"requests": []any{map[string]any{"name": "vfs_read", "arguments": map[string]any{"path": "/project/a.txt"}}, map[string]any{"name": "vfs_read", "arguments": map[string]any{"path": "/absent"}}}}
	out, err := workflowCall(s, session, fs, "env_read", request)
	if err != nil || !strings.Contains(out, "alpha") || !strings.Contains(out, "error") {
		t.Fatalf("partial batch: %s %v", out, err)
	}
	_, err = workflowCall(s, session, fs, "env_read", map[string]any{"requests": []any{map[string]any{"name": "vfs_write", "arguments": map[string]any{"path": "/project/a.txt", "text": "changed"}}}})
	if err == nil {
		t.Fatal("batch write accepted")
	}
	data, _, _ = fs.ReadFile("/project/a.txt", 100)
	if string(data) != "alpha\n" {
		t.Fatal("batch mutated source")
	}
	_, err = workflowCall(s, session, fs, "env_call", map[string]any{"name": "env_call"})
	if err == nil {
		t.Fatal("recursive dispatch")
	}
	out, err = workflowCall(s, session, fs, "env_info", map[string]any{"names": []string{"browser_check", "artifact_publish"}})
	if err != nil || !strings.Contains(out, "cases") || !strings.Contains(out, "runs") {
		t.Fatalf("schema discovery %s %v", out, err)
	}
}

func TestImmutableArtifactEvidenceAndCheckout(t *testing.T) {
	s, fs, source := testEnv(t)
	dev := agentOf(s, "Dev")
	snapshot := workspaceSnapshot(context.Background(), source)
	good, err := s.recordRun(dev, RunEvidence{Before: snapshot, After: snapshot, Exit: 0, Command: "test", Output: "passed"})
	if err != nil {
		t.Fatal(err)
	}
	bad, _ := s.recordRun(dev, RunEvidence{Before: snapshot, After: snapshot, Exit: 7, Output: "failed"})
	out, err := artifactCall(s, dev, fs, "artifact_publish", map[string]any{"path": "/project", "name": "candidate", "runs": []string{good.ID, bad.ID}})
	if err != nil {
		t.Fatal(err)
	}
	var a Artifact
	json.Unmarshal([]byte(out), &a)
	if a.ID == "" {
		t.Fatal(out)
	}
	if err = s.CreateEnv("Space", "Tester", nil); err != nil {
		t.Fatal(err)
	}
	tester := agentOf(s, "Tester")
	other, _ := s.FS("Space", "Tester")
	if _, err = artifactCall(s, tester, other, "artifact_read", map[string]any{"id": a.ID}); err == nil {
		t.Fatal("unshared artifact read")
	}
	s.mu.Lock()
	_, err = s.shareArtifacts(s.Spaces[0], dev.env, tester.env.ID, []string{a.ID})
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(source, "a.txt"), []byte("new version"), 0644)
	out, err = artifactCall(s, tester, other, "artifact_read", map[string]any{"id": a.ID, "file": "a.txt"})
	if err != nil || out != "alpha\n" {
		t.Fatalf("immutable file %q %v", out, err)
	}
	if _, err = other.Open("/project/a.txt"); err == nil {
		t.Fatal("source binding leaked")
	}
	for _, file := range []string{"../artifact.json", "/etc/passwd"} {
		if _, err = artifactCall(s, tester, other, "artifact_read", map[string]any{"id": a.ID, "file": file}); err == nil {
			t.Fatal("traversal accepted")
		}
	}
	out, err = artifactCall(s, tester, other, "artifact_read", map[string]any{"id": a.ID, "run": bad.ID})
	if err != nil || !strings.Contains(out, `"Exit":7`) {
		t.Fatalf("failure lost: %s %v", out, err)
	}
	out, err = artifactCall(s, tester, other, "artifact_checkout", map[string]any{"id": a.ID})
	if err != nil {
		t.Fatal(err)
	}
	var checkout struct{ Path, Snapshot string }
	json.Unmarshal([]byte(out), &checkout)
	if checkout.Snapshot != snapshot || workspaceSnapshot(context.Background(), s.workspace(tester.env, tester.agent, checkout.Path)) != snapshot {
		t.Fatal("checkout not exact")
	}
	verified, _ := s.recordRun(tester, RunEvidence{Before: snapshot, After: snapshot, Exit: 0})
	if _, err = artifactCall(s, tester, other, "artifact_verify", map[string]any{"id": a.ID, "runs": []string{verified.ID}}); err != nil {
		t.Fatal(err)
	}
	changed, _ := s.recordRun(tester, RunEvidence{Before: snapshot, After: "changed", Exit: 0})
	if _, err = artifactCall(s, tester, other, "artifact_verify", map[string]any{"id": a.ID, "runs": []string{changed.ID}}); err == nil {
		t.Fatal("stale evidence accepted")
	}
	if _, err = artifactCall(s, dev, fs, "artifact_publish", map[string]any{"path": "/project", "name": "changed", "runs": []string{good.ID}}); err == nil {
		t.Fatal("old run accepted for new files")
	}
}

func TestArtifactRunResultsSurviveBatchReadBudget(t *testing.T) {
	s, fs, source := testEnv(t)
	dev := agentOf(s, "Dev")
	snapshot := workspaceSnapshot(context.Background(), source)
	command := jsonText(map[string]any{"cases": strings.Repeat("expanded fixture ", 20000)})
	run, err := s.recordRun(dev, RunEvidence{Before: snapshot, After: snapshot, Exit: 1, Command: command, Output: `{"cases":[{"Name":"export","Passed":false,"Error":"wrong payload"}]}`})
	if err != nil {
		t.Fatal(err)
	}
	out, err := artifactCall(s, dev, fs, "artifact_publish", map[string]any{"path": "/project", "name": "review", "runs": []string{run.ID}})
	var a Artifact
	json.Unmarshal([]byte(out), &a)
	if err != nil || a.ID == "" {
		t.Fatalf("publish %s %v", out, err)
	}
	out, err = workflowCall(s, dev, fs, "env_read", map[string]any{"requests": []any{map[string]any{"name": "artifact_read", "arguments": map[string]any{"id": a.ID, "run": run.ID}}}})
	if err != nil || !strings.Contains(out, "wrong payload") || strings.Contains(out, "response capped") || strings.Contains(out, "expanded fixture") || len(out) > 2000 {
		t.Fatalf("result displaced by scenario: %s %v", out, err)
	}
	out, err = artifactCall(s, dev, fs, "artifact_read", map[string]any{"id": a.ID, "run": run.ID, "part": "output"})
	if err != nil || !strings.Contains(out, "wrong payload") {
		t.Fatalf("raw output unavailable %s %v", out, err)
	}
	var joined strings.Builder
	for offset := 1; offset != 0; {
		out, err = artifactCall(s, dev, fs, "artifact_read", map[string]any{"id": a.ID, "run": run.ID, "part": "command", "offset": offset, "limit": 8192})
		var chunk struct {
			Text string
			Next int `json:"next_offset"`
		}
		if err != nil || json.Unmarshal([]byte(out), &chunk) != nil || len(out) > 8000 || chunk.Next != 0 && chunk.Next <= offset {
			t.Fatalf("invalid command page: %s %v", out, err)
		}
		joined.WriteString(chunk.Text)
		offset = chunk.Next
	}
	var decoded map[string]any
	if json.Unmarshal([]byte(joined.String()), &decoded) != nil || jsonText(decoded) != command {
		t.Fatal("paged command lost data")
	}
	large := run
	large.Output = strings.Repeat("<\"line\">\n", 5000)
	out, err = runEvidenceView(large, "", 0, 0)
	if err != nil || len(out) > 8000 || !strings.Contains(out, `"output_truncated":true`) || !strings.Contains(out, `"Exit":1`) {
		t.Fatal("large output must preserve status and advertise pagination")
	}
}

func TestArtifactRespectsOverlayAndScratchBoundary(t *testing.T) {
	s, fs, source := testEnv(t)
	session := agentOf(s, "Dev")
	fs.Remove("/project/sub", true)
	os.Symlink("/etc/passwd", filepath.Join(source, "secret"))
	out, err := artifactCall(s, session, fs, "artifact_publish", map[string]any{"path": "/project", "name": "visible"})
	if err != nil {
		t.Fatal(err)
	}
	var a Artifact
	json.Unmarshal([]byte(out), &a)
	for _, file := range []string{"sub/b.txt", "secret"} {
		if _, err = artifactCall(s, session, fs, "artifact_read", map[string]any{"id": a.ID, "file": file}); err == nil {
			t.Fatal("hidden/symlink included")
		}
	}
	scratch := filepath.Join(s.agentDir(session.env, session.agent), "scratch")
	os.MkdirAll(scratch, 0700)
	os.Symlink(t.TempDir(), filepath.Join(scratch, "work"))
	if _, err = artifactCall(s, session, fs, "artifact_checkout", map[string]any{"id": a.ID}); err == nil {
		t.Fatal("checkout escaped scratch")
	}
}

func TestContextOverridesStayInvocationLocal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	skill := filepath.Join(home, ".codex", "skills", "demo", "SKILL.md")
	os.MkdirAll(filepath.Dir(skill), 0700)
	os.WriteFile(skill, []byte("unchanged"), 0600)
	args := strings.Join(codexContextArgs(""), " ")
	for _, want := range []string{"features.apps=false", "features.plugins=false", "skills.bundled.enabled=false", skill, "enabled=false"} {
		if !strings.Contains(args, want) {
			t.Fatal(args)
		}
	}
	if strings.Contains(args, "approval") || strings.Contains(args, "sandbox") || len(codexContextArgs("default")) > 0 {
		t.Fatal("policy override or explicit default ignored")
	}
	data, _ := os.ReadFile(skill)
	if string(data) != "unchanged" {
		t.Fatal("host skill changed")
	}
}

func TestCompactDispatchKeepsReadOnlyAndContactRights(t *testing.T) {
	s, _, source := testEnv(t)
	s.CreateEnv("Space", "Reader", nil)
	s.Attach("Space", "Reader", "/", []string{source}, true)
	session := agentOf(s, "Reader")
	fs, _ := s.FS("Space", "Reader")
	for _, args := range []map[string]any{
		{"name": "vfs_write", "arguments": map[string]any{"path": "/project/a.txt", "content": "overwrite"}},
		{"name": "agent_send", "arguments": map[string]any{"to": "Dev", "message": "unauthorized"}},
	} {
		if _, err := workflowCall(s, session, fs, "env_call", args); err == nil {
			t.Fatal("wrapper bypassed authority")
		}
	}
	data, _ := os.ReadFile(filepath.Join(source, "a.txt"))
	if string(data) != "alpha\n" {
		t.Fatal("read-only source changed")
	}
}

func TestWorkflowStatusSeparatesRequestedObservedAndPrivateContent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	s, fs, _ := testEnv(t)
	session := agentOf(s, "Dev")
	session.env.Agents = []Agent{{Name: "codex", Model: "requested", Effort: "high"}}
	scratch := filepath.Join(s.agentDir(session.env, "codex"), "scratch")
	os.MkdirAll(filepath.Join(root, "sessions"), 0700)
	lines := []any{
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "native", "cwd": scratch}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"model": "observed", "effort": "medium", "turn_id": "t"}},
		map[string]any{"type": "token_usage_record", "payload": map[string]any{"response_id": "r", "turn_id": "t", "usage": map[string]any{"input_tokens": 100, "cached_input_tokens": 80, "output_tokens": 5}}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"text": "private peer content"}}}},
	}
	lines = append(lines, map[string]any{"type": "token_usage_record", "payload": map[string]any{"response_id": "approval", "turn_id": "t", "model": "auto-review", "usage": map[string]any{"input_tokens": 20, "output_tokens": 1}}})
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(jsonText(line) + "\n")
	}
	os.WriteFile(filepath.Join(root, "sessions", "test.jsonl"), []byte(b.String()), 0600)
	out, err := workflowStatus(s, session)
	for _, want := range []string{"requested", "observed", "medium", `"uncached_input":20`, `"responses":1`, `"approval_responses":1`} {
		if err != nil || !strings.Contains(out, want) {
			t.Fatalf("status %s %v", out, err)
		}
	}
	if strings.Contains(out, "private peer content") {
		t.Fatal("peer transcript exposed")
	}
	batch, err := workflowCall(s, session, fs, "env_read", map[string]any{"requests": []any{
		map[string]any{"name": "workflow_status", "arguments": map[string]any{}},
		map[string]any{"name": "vfs_read", "arguments": map[string]any{"path": "/absent"}},
		map[string]any{"name": "vfs_read", "arguments": map[string]any{"path": "/project/a.txt"}},
	}})
	var items []struct{ Name, Text, Error string }
	if err != nil || json.Unmarshal([]byte(batch), &items) != nil || len(items) != 3 {
		t.Fatalf("status batch: %s %v", batch, err)
	}
	if items[0].Name != "workflow_status" || items[0].Text != out || items[0].Error != "" || items[1].Error == "" || !strings.Contains(items[2].Text, "alpha") {
		t.Fatalf("status metadata or partial read semantics changed: %s", batch)
	}
}
