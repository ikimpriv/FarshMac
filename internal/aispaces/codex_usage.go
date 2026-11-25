package aispaces

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CodexTokens struct {
	Input     int64 `json:"input_tokens"`
	Cached    int64 `json:"cached_input_tokens"`
	Output    int64 `json:"output_tokens"`
	Reasoning int64 `json:"reasoning_output_tokens"`
}

func (a *CodexTokens) add(b CodexTokens) {
	a.Input += b.Input
	a.Cached += b.Cached
	a.Output += b.Output
	a.Reasoning += b.Reasoning
}

type CodexTurn struct {
	ID, Model, Effort        string
	Calls                    int
	FirstContext, MaxContext int64
	Tokens                   CodexTokens
}
type CodexUsage struct {
	Session, CWD, Model, Effort              string
	Tokens, Approvals                        CodexTokens
	Turns                                    []CodexTurn
	ToolCalls, ToolOutputBytes, LargeOutputs int
	Legacy                                   bool
}

type codexRecord struct {
	Type    string
	Payload struct {
		Type, ID, CWD, Model, Effort, Name string
		TurnID                             string `json:"turn_id"`
		ResponseID                         string `json:"response_id"`
		Usage                              CodexTokens
		Info                               *struct {
			Total CodexTokens `json:"total_token_usage"`
		} `json:"info"`
		Output json.RawMessage
	}
}

func AnalyzeCodexTranscript(ctx context.Context, file string) (CodexUsage, error) {
	f, err := os.Open(file)
	if err != nil {
		return CodexUsage{}, err
	}
	defer f.Close()
	return analyzeCodex(ctx, f)
}
func analyzeCodex(ctx context.Context, r io.Reader) (CodexUsage, error) {
	var u CodexUsage
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 64<<10), 8<<20)
	seen := map[string]bool{}
	turns := map[string]int{}
	turnID := ""
	records := 0
	var legacy CodexTokens
	for scan.Scan() {
		if err := ctx.Err(); err != nil {
			return u, err
		}
		var rec codexRecord
		if json.Unmarshal(scan.Bytes(), &rec) != nil {
			continue
		}
		p := rec.Payload
		switch rec.Type {
		case "session_meta":
			u.Session, u.CWD = p.ID, p.CWD
		case "turn_context":
			u.Model, u.Effort, turnID = p.Model, p.Effort, p.TurnID
		case "token_usage_record":
			if p.ResponseID == "" || seen[p.ResponseID] {
				continue
			}
			seen[p.ResponseID] = true
			records++
			id := p.TurnID
			if id == "" {
				id = turnID
			}
			model, effort := u.Model, u.Effort
			if p.Model != "" {
				model = p.Model
			}
			if p.Effort != "" {
				effort = p.Effort
			}
			key := id + "\x00" + model + "\x00" + effort
			i, ok := turns[key]
			if !ok {
				i = len(u.Turns)
				turns[key] = i
				u.Turns = append(u.Turns, CodexTurn{ID: id, Model: model, Effort: effort, FirstContext: p.Usage.Input})
			}
			t := &u.Turns[i]
			t.Calls++
			t.MaxContext = max(t.MaxContext, p.Usage.Input)
			t.Tokens.add(p.Usage)
			if strings.Contains(model, "auto-review") {
				u.Approvals.add(p.Usage)
			} else {
				u.Tokens.add(p.Usage)
			}
		case "event_msg":
			if p.Type == "token_count" && p.Info != nil {
				legacy = p.Info.Total
			}
		case "response_item":
			if p.Type == "function_call" || p.Type == "custom_tool_call" {
				u.ToolCalls++
			}
			if p.Type == "function_call_output" || p.Type == "custom_tool_call_output" {
				n := len(p.Output)
				var text string
				if json.Unmarshal(p.Output, &text) == nil {
					n = len(text)
				}
				u.ToolOutputBytes += n
				if n > 4096 {
					u.LargeOutputs++
				}
			}
		}
	}
	if records == 0 {
		u.Tokens = legacy
		u.Legacy = true
	}
	return u, scan.Err()
}
func codexHome() string {
	if root := os.Getenv("CODEX_HOME"); root != "" {
		return root
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

// Read only each session's first record to find the exact scratch directory.
// A matching role name or parent directory is insufficient: Envs are isolated.
func codexTranscripts(ctx context.Context, scratch string) ([]string, error) {
	var files []string
	root := filepath.Join(codexHome(), "sessions")
	err := filepath.WalkDir(root, func(file string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if file == root && os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		f, err := os.Open(file)
		if err != nil {
			return nil
		}
		line, _ := bufio.NewReader(io.LimitReader(f, 64<<10)).ReadBytes('\n')
		f.Close()
		var rec codexRecord
		if json.Unmarshal(line, &rec) == nil && rec.Type == "session_meta" && filepath.Clean(rec.Payload.CWD) == filepath.Clean(scratch) {
			files = append(files, file)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}
func (u CodexUsage) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  Session %s · observed %s / %s\n", u.Session, u.Model, u.Effort)
	fmt.Fprintf(&b, "  Tokens: input %d (cached %d), output %d (reasoning %d)\n", u.Tokens.Input, u.Tokens.Cached, u.Tokens.Output, u.Tokens.Reasoning)
	if u.Approvals.Input+u.Approvals.Output > 0 {
		fmt.Fprintf(&b, "  Approval review separately: input %d (cached %d), output %d\n", u.Approvals.Input, u.Approvals.Cached, u.Approvals.Output)
	}
	fmt.Fprintf(&b, "  Tool calls %d · tool output %d bytes · results above 4 KiB: %d\n", u.ToolCalls, u.ToolOutputBytes, u.LargeOutputs)
	if u.Legacy {
		b.WriteString("  Legacy cumulative counters; per-turn/model breakdown unavailable.\n")
	}
	for _, t := range u.Turns {
		fmt.Fprintf(&b, "    Turn %s · %s / %s · %d calls · input context %d → peak %d · input %d (cached %d), output %d\n", t.ID, t.Model, t.Effort, t.Calls, t.FirstContext, t.MaxContext, t.Tokens.Input, t.Tokens.Cached, t.Tokens.Output)
	}
	return b.String()
}

// codexObserved reads a bounded tail for status. A turn_context states the
// CLI's effective configuration; it is distinct from a request sent to it.
func codexObserved(file string) (model, effort string) {
	f, err := os.Open(file)
	if err != nil {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return
	}
	start := max(int64(0), info.Size()-(1<<20))
	_, _ = f.Seek(start, io.SeekStart)
	r := bufio.NewReader(f)
	if start > 0 {
		_, _ = r.ReadBytes('\n')
	}
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 64<<10), 1<<20)
	for scan.Scan() {
		var rec codexRecord
		if json.Unmarshal(scan.Bytes(), &rec) == nil && rec.Type == "turn_context" {
			model, effort = rec.Payload.Model, rec.Payload.Effort
		}
	}
	return
}

func codexConversation(file string, chars int) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	start := max(int64(0), info.Size()-(1<<20))
	_, _ = f.Seek(start, io.SeekStart)
	r := bufio.NewReader(f)
	if start > 0 {
		_, _ = r.ReadBytes('\n')
	}
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 64<<10), 1<<20)
	out := ""
	for scan.Scan() {
		var rec struct {
			Type    string
			Payload struct {
				Type, Role, Name string
				Content          []struct{ Text string }
			}
		}
		if json.Unmarshal(scan.Bytes(), &rec) != nil || rec.Type != "response_item" {
			continue
		}
		p := rec.Payload
		if p.Type == "message" && (p.Role == "assistant" || p.Role == "user") {
			for _, c := range p.Content {
				out += p.Role + ": " + c.Text + "\n"
			}
		}
		if p.Type == "function_call" || p.Type == "custom_tool_call" {
			out += "tool: " + p.Name + "\n"
		}
		if len(out) > chars {
			out = out[len(out)-chars:]
		}
	}
	return strings.ToValidUTF8(out, "")
}
func latestCodexTranscript(scratch string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	files, _ := codexTranscripts(ctx, scratch)
	for i := len(files) - 1; i >= 0; i-- {
		model, _ := codexObserved(files[i])
		if model != "" && !strings.Contains(model, "auto-review") {
			return files[i]
		}
	}
	return ""
}
