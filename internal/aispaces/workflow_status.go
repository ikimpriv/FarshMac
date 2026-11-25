package aispaces

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Metadata only: no peer prompts, source files, credentials or transcript text.
func workflowStatus(s *Store, session *mcpSession) (string, error) {
	type entry struct {
		address, scratch string
		agent            Agent
	}
	var entries []entry
	s.mu.Lock()
	sp := s.spaceOf(session.env)
	if sp != nil {
		for _, e := range sp.Envs {
			for _, a := range e.Agents {
				entries = append(entries, entry{address(e, a.Name), filepath.Join(s.agentDir(e, a.Name), "scratch"), a})
			}
		}
	}
	s.mu.Unlock()
	if sp == nil {
		return "", fmt.Errorf("Space no longer exists")
	}
	ctx, cancel := context.WithTimeout(sessionContext(session), 8*time.Second)
	defer cancel()
	rows := []map[string]any{}
	for i, e := range entries {
		if i >= 32 || ctx.Err() != nil {
			rows = append(rows, map[string]any{"remaining": len(entries) - i, "status": "budget reached; report is partial"})
			break
		}
		row := map[string]any{"agent": e.address, "requested": e.agent.profile(), "observed": "unavailable"}
		if e.agent.Name == "codex" {
			files, err := codexTranscripts(ctx, e.scratch)
			var tokens, approvals CodexTokens
			responses, approvalResponses, calls := 0, 0, 0
			observed := []map[string]any{}
			for _, file := range files {
				u, parseErr := AnalyzeCodexTranscript(ctx, file)
				if parseErr != nil {
					err = parseErr
					break
				}
				tokens.add(u.Tokens)
				approvals.add(u.Approvals)
				calls += u.ToolCalls
				for _, turn := range u.Turns {
					if strings.Contains(turn.Model, "auto-review") {
						approvalResponses += turn.Calls
					} else {
						responses += turn.Calls
					}
				}
				observed = append(observed, map[string]any{"model": u.Model, "effort": u.Effort, "session": u.Session})
				if len(observed) > 4 {
					observed = observed[len(observed)-4:]
				}
			}
			if len(observed) > 0 {
				row["observed"] = observed
				row["sessions"] = len(files)
				row["tokens"] = tokens
				row["approval_tokens"] = approvals
				row["uncached_input"] = tokens.Input - tokens.Cached
				row["tool_calls"] = calls
				row["responses"] = responses
				row["approval_responses"] = approvalResponses
			}
			if err != nil {
				row["error"] = err.Error()
			}
		} else if file := transcriptFile(e.agent.SessionID); file != "" {
			u, err := analyzeClaudeTranscript(ctx, file)
			if err != nil {
				row["error"] = err.Error()
			} else {
				row["observed"] = map[string]any{"model": u.Model, "effort": "not reported"}
				row["tokens"] = map[string]any{"input": u.Input, "cache_read": u.CacheRead, "cache_write_5m": u.Write5m, "cache_write_1h": u.Write1h, "output": u.Output}
				row["responses"] = u.Steps
			}
		}
		rows = append(rows, row)
	}
	return jsonText(map[string]any{"agents": rows, "note": "Observed counters, not dollar costs. Codex cached input is included in input, reasoning in output; approval counters are separate. Read at handoff, do not poll."}), nil
}
