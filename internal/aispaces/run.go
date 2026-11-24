package aispaces

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const runLogLimit = 16 << 20
const runTailLimit = 4096

// boundedLog drains noisy tools without keeping their output in memory or
// filling the disk. A response always states when the artifact was truncated.
type boundedLog struct {
	mu             sync.Mutex
	file           io.Writer
	written, total int
	tail           []byte
	err            error
}

func (w *boundedLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	w.total += n
	if w.err == nil && w.written < runLogLimit {
		var k int
		k, w.err = w.file.Write(p[:min(n, runLogLimit-w.written)])
		w.written += k
	}
	if len(p) >= runTailLimit {
		w.tail = append(w.tail[:0], p[len(p)-runTailLimit:]...)
	} else {
		w.tail = append(w.tail, p...)
		if len(w.tail) > runTailLimit {
			w.tail = append([]byte(nil), w.tail[len(w.tail)-runTailLimit:]...)
		}
	}
	return n, nil
}

func runEnvironment(scratch string) []string {
	// Build tools get private caches, not the user's credential/config environment.
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Join(scratch, ".runtime-home"), "TMPDIR=" + filepath.Join(scratch, ".tmp"), "XDG_CACHE_HOME=" + filepath.Join(scratch, ".cache"), "npm_config_cache=" + filepath.Join(scratch, ".cache", "npm"), "GOCACHE=" + filepath.Join(scratch, ".cache", "go-build"), "GOPATH=" + filepath.Join(scratch, ".cache", "go"), "PLAYWRIGHT_BROWSERS_PATH=" + filepath.Join(scratch, ".cache", "browsers"), "LANG=en_US.UTF-8", "CI=1"}
}

func workspaceRun(s *Store, session *mcpSession, virtual, command string, seconds int) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command is required")
	}
	scratch := realPath(filepath.Join(s.agentDir(session.env, session.agent), "scratch"))
	local, err := filepath.EvalSymlinks(s.workspace(session.env, session.agent, virtual))
	if err != nil {
		return "", fmt.Errorf("workspace_pull this folder first: %w", err)
	}
	rel, err := filepath.Rel(scratch, local)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("workspace path escapes scratch")
	}
	root, err := os.OpenRoot(scratch)
	if err != nil {
		return "", err
	}
	defer root.Close()
	for _, p := range []string{".runtime-home", ".tmp", ".cache", ".runs"} {
		if err = root.MkdirAll(p, 0700); err != nil {
			return "", err
		}
	}
	// Retain the latest ten logs. No symlinks are followed by os.Root.
	entries, _ := os.ReadDir(filepath.Join(scratch, ".runs"))
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for len(entries) >= 10 {
		if entries[0].Type().IsRegular() && strings.HasSuffix(entries[0].Name(), ".log") {
			_ = root.Remove(filepath.Join(".runs", entries[0].Name()))
		}
		entries = entries[1:]
	}
	logRel := filepath.Join(".runs", time.Now().UTC().Format("20060102T150405.000000000")+"-"+newID()+".log")
	file, err := root.OpenFile(logRel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer file.Close()
	profile, err := sandboxProfile(scratch)
	if err != nil {
		return "", err
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(home, ".codex"), filepath.Join(home, ".claude"), filepath.Join(home, ".claude.json"), filepath.Join(home, "Library", "Keychains")} {
		profile += "\n(deny file-read-data file-write* (subpath " + sbQuote(realPath(p)) + "))\n"
	}
	parent := session.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(max(1, min(300, seconds)))*time.Second)
	defer cancel()
	snapshot := workspaceSnapshot(ctx, local)
	cmd := exec.CommandContext(ctx, sandboxExec, "-p", profile, "/bin/sh", "-c", command)
	cmd.Dir, cmd.Env = local, runEnvironment(scratch)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = time.Second
	log := &boundedLog{file: file}
	cmd.Stdout, cmd.Stderr = log, log
	started := time.Now()
	err = cmd.Run()
	// Also end background descendants, even when the parent exited successfully.
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	status := "exit 0"
	if err != nil {
		status = err.Error()
	}
	if ctx.Err() != nil {
		status = ctx.Err().Error()
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	artifact := filepath.Join(scratch, logRel)
	trunc := ""
	if log.total > log.written {
		trunc = fmt.Sprintf(" (truncated: kept %d of %d bytes)", log.written, log.total)
	}
	if log.err != nil {
		trunc += " (log error: " + log.err.Error() + ")"
	}
	exit := 0
	if err != nil || ctx.Err() != nil {
		exit = -1
		if cmd.ProcessState != nil {
			exit = cmd.ProcessState.ExitCode()
		}
		if exit == 0 {
			exit = -1
		}
	}
	run, recordErr := s.recordRun(session, RunEvidence{Origin: "workspace", Command: command, Before: snapshot, After: workspaceSnapshot(ctx, local), Exit: exit, DurationMS: time.Since(started).Milliseconds(), Output: string(log.tail)})
	if recordErr != nil {
		return "", fmt.Errorf("command finished (%s), but evidence could not be saved: %w", status, recordErr)
	}
	return fmt.Sprintf("%s · %s\nRun ID: %s (server recorded; use with artifact_publish/verify)\nBefore run: %s\nLog: %s%s\nLast %d output bytes:\n%s", status, time.Since(started).Round(time.Millisecond), run.ID, snapshot, artifact, trunc, len(log.tail), string(log.tail)), nil
}
