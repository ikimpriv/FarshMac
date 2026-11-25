package aispaces

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceRunBoundsLogsAndKillsOnCancellation(t *testing.T) {
	s, f, _ := testEnv(t)
	session := agentOf(s, "Dev")
	session.agent = "codex"
	if _, err := workspacePull(s, session, f, "/project"); err != nil {
		t.Fatal(err)
	}
	out, err := workspaceRun(s, session, "/project", "printf 'test passed'; exit 7", 5)
	if err != nil || !strings.Contains(out, "exit status 7") || !strings.Contains(out, "test passed") {
		t.Fatalf("run: %s %v", out, err)
	}
	if strings.Contains(out, "sandbox_init") {
		t.Fatal("sandbox not available")
	}
	session.ctx, session.cancel = context.WithCancel(context.Background())
	go func() { time.Sleep(150 * time.Millisecond); session.cancel() }()
	started := time.Now()
	out, err = workspaceRun(s, session, "/project", "sleep 20 & wait", 30)
	if err != nil || !strings.Contains(out, "context canceled") || time.Since(started) > 3*time.Second {
		t.Fatalf("cancel: %s %v", out, err)
	}
	var buf bytes.Buffer
	log := boundedLog{file: &buf}
	chunk := bytes.Repeat([]byte("x"), 1<<20)
	for i := 0; i < 20; i++ {
		log.Write(chunk)
	}
	if buf.Len() != runLogLimit || len(log.tail) != runTailLimit || log.total != 20<<20 {
		t.Fatalf("unbounded log: %d %d %d", buf.Len(), len(log.tail), log.total)
	}
}
func TestWorkspaceRunRejectsEscapeAndUsesPrivateCache(t *testing.T) {
	s, f, _ := testEnv(t)
	session := agentOf(s, "Dev")
	session.agent = "codex"
	if _, err := workspacePull(s, session, f, "/project"); err != nil {
		t.Fatal(err)
	}
	local := s.workspace(session.env, session.agent, "/escape")
	os.Symlink(t.TempDir(), local)
	if _, err := workspaceRun(s, session, "/escape", "true", 2); err == nil {
		t.Fatal("scratch escape accepted")
	}
	scratch := filepath.Join(s.agentDir(session.env, session.agent), "scratch")
	env := strings.Join(runEnvironment(scratch), "\n")
	if !strings.Contains(env, "npm_config_cache="+scratch) || strings.Contains(env, tokenEnv) || strings.Contains(env, "OPENAI_API_KEY") {
		t.Fatal("runtime environment leaks or cache is shared")
	}
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(scratch, ".runs"))
	if _, err := workspaceRun(s, session, "/project", "true", 2); err == nil {
		t.Fatal("log symlink escape accepted")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) > 0 {
		t.Fatal("wrote outside scratch")
	}
}
