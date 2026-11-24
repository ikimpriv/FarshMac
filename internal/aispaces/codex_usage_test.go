package aispaces

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexUsageDeduplicatesAndSeparatesApprovals(t *testing.T) {
	record := `{"type":"token_usage_record","payload":{"response_id":"r1","turn_id":"t1","usage":{"input_tokens":1000,"cached_input_tokens":700,"output_tokens":80,"reasoning_output_tokens":20}}}`
	log := strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"s1","cwd":"/scratch"}}`,
		`{"type":"turn_context","payload":{"model":"known-model","effort":"high","turn_id":"t1"}}`, record, record,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":90000,"output_tokens":8000}}}}`,
		`{"type":"token_usage_record","payload":{"response_id":"r2","turn_id":"t1","usage":{"input_tokens":1500,"cached_input_tokens":1000,"output_tokens":100,"reasoning_output_tokens":40}}}`,
		`{"type":"turn_context","payload":{"model":"codex-auto-review","effort":"low","turn_id":"approval"}}`,
		`{"type":"token_usage_record","payload":{"response_id":"a1","turn_id":"approval","usage":{"input_tokens":300,"output_tokens":10}}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call","name":"exec"}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call_output","output":"some result"}}`,
		`{"incomplete`,
	}, "\n")
	u, err := analyzeCodex(context.Background(), strings.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	if u.Legacy || u.Tokens != (CodexTokens{2500, 1700, 180, 60}) || u.Approvals.Input != 300 || u.Approvals.Output != 10 {
		t.Fatalf("wrong counters %+v", u)
	}
	if len(u.Turns) != 2 || u.Turns[0].Calls != 2 || u.Turns[0].FirstContext != 1000 || u.Turns[0].MaxContext != 1500 || u.ToolCalls != 1 || u.ToolOutputBytes != 11 {
		t.Fatalf("wrong breakdown %+v", u)
	}
	legacy, err := analyzeCodex(context.Background(), strings.NewReader(`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":20,"output_tokens":3}}}}`))
	if err != nil || !legacy.Legacy || legacy.Tokens.Input != 20 {
		t.Fatalf("legacy %+v %v", legacy, err)
	}
}
func TestCodexTranscriptIsolationAndCancellation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	os.MkdirAll(filepath.Join(root, "sessions"), 0700)
	os.WriteFile(filepath.Join(root, "sessions", "a.jsonl"), []byte("{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/scratch/role\"}}\n"), 0600)
	os.WriteFile(filepath.Join(root, "sessions", "b.jsonl"), []byte("{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/scratch/role2\"}}\n"), 0600)
	files, err := codexTranscripts(context.Background(), "/scratch/role")
	if err != nil || len(files) != 1 || filepath.Base(files[0]) != "a.jsonl" {
		t.Fatalf("files %v %v", files, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = codexTranscripts(ctx, "/scratch/role"); err == nil {
		t.Fatal("not cancelled")
	}
}
