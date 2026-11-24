package aispaces

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The efficiency report answers "where did the tokens go" for the agents of
// a Space, from the transcripts their CLI keeps. It is the measure behind
// every change to the MCP tools: the same scenario is run before and after,
// and the two reports are compared.
//
// An agent pays for its context on every step. A piece of text costs once
// when it enters the context (a cache write), again on each later step it is
// still there (a cache read), and most of all when the model types it
// (output). The weights below are price ratios to one fresh input token; they
// turn four token counters into one number that can be compared.

const (
	weightOutput  = 5.0
	weightWrite5m = 1.25
	weightWrite1h = 2.0
)

// weightRead is the price of re-reading one cached token. Checked 2026-11.
func weightRead(model string) float64 {
	switch {
	case strings.Contains(model, "fable-5-1"), strings.Contains(model, "fable-5-2"), strings.Contains(model, "mythos-5-1"), strings.Contains(model, "mythos-5-2"):
		return 0.025
	case strings.Contains(model, "opus-5-5"), strings.Contains(model, "sonnet-5-5"):
		return 0.05
	}
	return 0.1
}

// Usage is what one agent conversation spent.
type Usage struct {
	Model                              string
	Steps, Compactions                 int // model calls; times the context was compacted
	FirstContext, MaxContext           int // tokens sent with the first and the largest call
	Input, CacheRead, Write5m, Write1h int
	Output, Thinking                   int
	ToolSearches, ToolErrors           int
	ReadChars, RereadChars             int                // file text received; of it, lines already received in the same context
	ConsoleChars, ConsoleAfterMessage  int                // text the agent printed; of it, right after sending a message
	Content                            map[string]float64 // weighted cost by what the context was filled with
	Largest                            []string           // the biggest single tool results
}

// Cost is the weighted total, in fresh-input-token units.
func (u *Usage) Cost() float64 {
	return float64(u.Input) + float64(u.CacheRead)*weightRead(u.Model) + float64(u.Write5m)*weightWrite5m + float64(u.Write1h)*weightWrite1h + float64(u.Output)*weightOutput
}

type transcriptRecord struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	Sidechain bool   `json:"isSidechain"`
	Message   struct {
		ID      string          `json:"id"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   struct {
			Input   int `json:"input_tokens"`
			Read    int `json:"cache_read_input_tokens"`
			Write   int `json:"cache_creation_input_tokens"`
			Output  int `json:"output_tokens"`
			Details struct {
				Thinking int `json:"thinking_tokens"`
			} `json:"output_tokens_details"`
			Creation struct {
				H1 int `json:"ephemeral_1h_input_tokens"`
				M5 int `json:"ephemeral_5m_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	} `json:"message"`
}

type transcriptBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

func blockText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Text string `json:"text"`
	}
	json.Unmarshal(raw, &parts)
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(part.Text)
	}
	return b.String()
}

// AnalyzeTranscript reads a Claude Code session transcript.
func AnalyzeTranscript(file string) (*Usage, error) {
	return analyzeClaudeTranscript(context.Background(), file)
}

func analyzeClaudeTranscript(ctx context.Context, file string) (*Usage, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	u := &Usage{Content: map[string]float64{}}
	type block struct {
		window, step int // step that first saw it
		kind         string
		chars        int
		typed        bool
	}
	var blocks []block
	var stepWindow []int
	steps := map[string]int{}
	tools := map[string]string{} // tool_use id → name
	window := 0
	seenLines := map[string]bool{}
	type big struct {
		chars int
		what  string
	}
	var largest []big
	afterMessage := false
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return u, err
		}
		line, readErr := r.ReadBytes('\n')
		var rec transcriptRecord
		if len(line) > 0 && json.Unmarshal(line, &rec) == nil && !rec.Sidechain {
			switch {
			case rec.Type == "system" && rec.Subtype == "compact_boundary":
				window++
				u.Compactions++
				seenLines = map[string]bool{}
			case rec.Type == "assistant":
				m := rec.Message
				step, known := steps[m.ID]
				if !known {
					step = len(stepWindow)
					steps[m.ID] = step
					stepWindow = append(stepWindow, window)
					context := m.Usage.Input + m.Usage.Read + m.Usage.Write
					if u.FirstContext == 0 {
						u.FirstContext = context
					}
					u.MaxContext = max(u.MaxContext, context)
					u.Input += m.Usage.Input
					u.CacheRead += m.Usage.Read
					h1, m5 := m.Usage.Creation.H1, m.Usage.Creation.M5
					if h1+m5 == 0 {
						m5 = m.Usage.Write
					}
					u.Write1h, u.Write5m = u.Write1h+h1, u.Write5m+m5
					u.Output += m.Usage.Output
					u.Thinking += m.Usage.Details.Thinking
					if m.Model != "" && !strings.HasPrefix(m.Model, "<") {
						u.Model = m.Model
					}
				}
				var content []transcriptBlock
				json.Unmarshal(m.Content, &content)
				for _, b := range content {
					switch b.Type {
					case "text":
						blocks = append(blocks, block{window, step, "console text", len(b.Text), true})
						u.ConsoleChars += len(b.Text)
						if afterMessage {
							u.ConsoleAfterMessage += len(b.Text)
							afterMessage = false
						}
					case "tool_use":
						name := strings.TrimPrefix(b.Name, "mcp__farshmac__")
						tools[b.ID] = name
						blocks = append(blocks, block{window, step, name + " input", len(b.Input), true})
						if name == "ToolSearch" {
							u.ToolSearches++
						}
						afterMessage = name == "agent_send" || name == "agent_reply"
					}
				}
			case rec.Type == "user":
				step := len(stepWindow) // seen by the next call
				var text string
				if json.Unmarshal(rec.Message.Content, &text) == nil {
					blocks = append(blocks, block{window, step, "prompts and messages", len(text), false})
					afterMessage = false
					break
				}
				var content []transcriptBlock
				json.Unmarshal(rec.Message.Content, &content)
				for _, b := range content {
					switch b.Type {
					case "text":
						blocks = append(blocks, block{window, step, "prompts and messages", len(b.Text), false})
						afterMessage = false
					case "tool_result":
						name, text := tools[b.ToolUseID], blockText(b.Content)
						blocks = append(blocks, block{window, step, name + " result", len(text), false})
						if b.IsError {
							u.ToolErrors++
						}
						largest = append(largest, big{len(text), name})
						if name == "vfs_read" || name == "Read" {
							u.ReadChars += len(text)
							// Only a line an earlier result already brought counts;
							// a file may repeat its own lines.
							var fresh []string
							for _, l := range strings.Split(text, "\n") {
								if l = strings.TrimSpace(l); len(l) >= 20 {
									if seenLines[l] {
										u.RereadChars += len(l)
									} else {
										fresh = append(fresh, l)
									}
								}
							}
							for _, l := range fresh {
								seenLines[l] = true
							}
						}
					}
				}
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return nil, readErr
			}
			break
		}
	}
	u.Steps = len(stepWindow)
	// A block is paid for when it enters the context and on every later call
	// of the same context window.
	later := map[[2]int]int{} // window, first step → calls that include it
	perWindow := map[int][]int{}
	for step, w := range stepWindow {
		perWindow[w] = append(perWindow[w], step)
	}
	read := weightRead(u.Model)
	for _, b := range blocks {
		key := [2]int{b.window, b.step}
		n, ok := later[key]
		if !ok {
			for _, step := range perWindow[b.window] {
				if step >= b.step {
					n++
				}
			}
			later[key] = n
		}
		weight := weightWrite1h + read*float64(n)
		if b.typed {
			weight += weightOutput
		}
		u.Content[b.kind] += float64(b.chars) * weight
	}
	sort.Slice(largest, func(i, j int) bool { return largest[i].chars > largest[j].chars })
	for i := 0; i < len(largest) && i < 3; i++ {
		u.Largest = append(u.Largest, fmt.Sprintf("%s %s", largest[i].what, short(int64(largest[i].chars))))
	}
	return u, nil
}

func thousands(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.0fK", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

func percent(part, whole float64) string {
	if whole <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%.0f%%", 100*part/whole)
}

// String renders the conversation's spending for reading.
func (u *Usage) String() string {
	var b strings.Builder
	cost := u.Cost()
	read := float64(u.CacheRead) * weightRead(u.Model)
	write := float64(u.Write5m)*weightWrite5m + float64(u.Write1h)*weightWrite1h
	fmt.Fprintf(&b, "  model %s · %d steps · context %s → %s tokens · compacted %d time(s)\n", u.Model, u.Steps, thousands(u.FirstContext), thousands(u.MaxContext), u.Compactions)
	fmt.Fprintf(&b, "  tokens: cache read %s · cache write %s · output %s (thinking %s) · uncached input %s\n", thousands(u.CacheRead), thousands(u.Write5m+u.Write1h), thousands(u.Output), thousands(u.Thinking), thousands(u.Input))
	fmt.Fprintf(&b, "  weighted cost %s units: re-reading context %s, new context %s, output %s\n", thousands(int(cost)), percent(read, cost), percent(write, cost), percent(float64(u.Output)*weightOutput, cost))
	fmt.Fprintf(&b, "    of it the fixed part (first context × steps): %s\n", percent(float64(u.FirstContext*u.Steps)*weightRead(u.Model), cost))
	type share struct {
		kind  string
		value float64
	}
	var shares []share
	total := 0.0
	for kind, value := range u.Content {
		shares = append(shares, share{kind, value})
		total += value
	}
	sort.Slice(shares, func(i, j int) bool { return shares[i].value > shares[j].value })
	var top []string
	for i := 0; i < len(shares) && i < 7; i++ {
		top = append(top, fmt.Sprintf("%s %s", shares[i].kind, percent(shares[i].value, total)))
	}
	fmt.Fprintf(&b, "  what filled the context, by cost: %s\n", strings.Join(top, " · "))
	fmt.Fprintf(&b, "  signals: file text read again in the same context %s · console text right after sending a message %s · tool searches %d · tool errors %d\n",
		percent(float64(u.RereadChars), float64(u.ReadChars)), percent(float64(u.ConsoleAfterMessage), float64(u.ConsoleChars)), u.ToolSearches, u.ToolErrors)
	if len(u.Largest) > 0 {
		fmt.Fprintf(&b, "  largest tool results: %s\n", strings.Join(u.Largest, ", "))
	}
	return b.String()
}

func transcriptFile(session string) string {
	if session == "" {
		return ""
	}
	home, _ := os.UserHomeDir()
	files, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", session+".jsonl"))
	if len(files) == 0 {
		return ""
	}
	return files[0]
}

// UsageReport describes what every agent of a Space spent so far.
func (s *Store) UsageReport(space string) string {
	return s.UsageReportContext(context.Background(), space)
}

func (s *Store) UsageReportContext(ctx context.Context, space string) string {
	type entry struct{ who, session, agent, scratch string }
	s.mu.Lock()
	sp := s.space(space)
	var agents []entry
	if sp != nil {
		for _, e := range sp.Envs {
			for _, a := range e.Agents {
				agents = append(agents, entry{address(e, a.Name), a.SessionID, a.Name, filepath.Join(s.agentDir(e, a.Name), "scratch")})
			}
		}
	}
	s.mu.Unlock()
	if sp == nil {
		return "no such Space"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Efficiency of the agents of %q, from their own transcripts.\n", space)
	b.WriteString("Claude estimates only, in units of one fresh input token: output ×5, new context ×1.25 or ×2 (cache write, 5 minutes or 1 hour), re-read context ×0.025–0.1 by model.\n\n")
	total := 0.0
	var codex, approvals CodexTokens
	b.WriteString("Codex: observed token counters, not a dollar estimate. Cached input is included in input; reasoning is included in output. Turns are CLI turns, not necessarily one FarshMac message.\n\n")
	for _, agent := range agents {
		if ctx.Err() != nil {
			return b.String() + "\nCancelled"
		}
		fmt.Fprintf(&b, "%s\n", agent.who)
		if agent.agent == "codex" {
			files, err := codexTranscripts(ctx, agent.scratch)
			if err != nil {
				fmt.Fprintf(&b, "  Cannot discover transcripts: %v\n", err)
			}
			if len(files) == 0 {
				b.WriteString("  No Codex transcript for this Env's scratch directory\n")
			}
			for _, file := range files {
				u, err := AnalyzeCodexTranscript(ctx, file)
				if err != nil {
					fmt.Fprintf(&b, "  Partial transcript: %v\n", err)
				}
				codex.add(u.Tokens)
				approvals.add(u.Approvals)
				b.WriteString(u.String())
			}
			b.WriteString("\n")
			continue
		}
		file := transcriptFile(agent.session)
		if file == "" {
			b.WriteString("  no transcript (not started yet, or not a Claude Code agent)\n\n")
			continue
		}
		u, err := analyzeClaudeTranscript(ctx, file)
		if err != nil || u.Steps == 0 {
			b.WriteString("  the transcript has no model calls yet\n\n")
			continue
		}
		total += u.Cost()
		b.WriteString(u.String() + "\n")
	}
	fmt.Fprintf(&b, "Claude total: %s estimated units\nCodex total: input %d (cached %d), output %d (reasoning %d)\nAdditional Codex approval review: input %d, output %d\n", thousands(int(total)), codex.Input, codex.Cached, codex.Output, codex.Reasoning, approvals.Input, approvals.Output)
	return b.String()
}
