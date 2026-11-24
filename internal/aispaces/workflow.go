package aispaces

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Three stable entry points keep tool discovery out of every agent's context.
// env_call preserves the original tool's authority checks; it is not a batch
// executor and cannot call itself. Existing clients can still call old names.
var compactTools = []tool{
	{"env_info", "Get bindings, contacts, runtime and requested settings. For operation schemas pass names; do not print the whole tool catalog.", object(nil, prop{"names", "array", "Exact operation names to describe (at most 8)"})},
	{"env_read", "Read up to 12 files, sections, messages, artifacts or workflow_status in one bounded call. Each item returns its own error; no writes.", object([]string{"requests"}, prop{"requests", "array", "Items: {name, arguments}; read operations only"})},
	{"env_call", "Perform one operation using its schema from env_info(names). Every original filesystem/contact permission still applies.", object([]string{"name", "arguments"}, prop{"name", "string", "Operation name"}, prop{"arguments", "object", "Operation arguments"})},
}

func init() {
	compactTools[0].InputSchema["properties"].(map[string]any)["names"] = stringList("Up to 8 exact operation names")
	items := object([]string{"name", "arguments"}, prop{"name", "string", "Read operation"}, prop{"arguments", "object", "Its arguments"})
	compactTools[1].InputSchema["properties"].(map[string]any)["requests"] = map[string]any{"type": "array", "minItems": 1, "maxItems": 12, "items": items}
}

func jsonText(v any) string { data, _ := json.Marshal(v); return string(data) }
func stringArgs(raw map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = s
		} else {
			out[k] = fmt.Sprint(v)
		}
	}
	return out
}
func decodeArgs(raw any, target any) error {
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("arguments exceed 1 MiB")
	}
	return json.Unmarshal(data, target)
}

func workflowCall(s *Store, session *mcpSession, fs *FS, name string, raw map[string]any) (string, error) {
	switch name {
	case "env_info":
		var args struct{ Names []string }
		if err := decodeArgs(raw, &args); err != nil {
			return "", err
		}
		if len(args.Names) > 8 {
			return "", fmt.Errorf("describe at most 8 operations")
		}
		if len(args.Names) > 0 {
			out := []tool{}
			for _, n := range args.Names {
				found := false
				for _, t := range allOperations() {
					if t.Name == n {
						out = append(out, t)
						found = true
						break
					}
				}
				if !found {
					return "", fmt.Errorf("unknown operation %q", n)
				}
			}
			return jsonText(out), nil
		}
		bindings := []map[string]any{}
		for _, b := range fs.bindings() {
			bindings = append(bindings, map[string]any{"path": b.Virtual, "reference": "@" + b.ID, "read_only": b.ReadOnly})
		}
		names := []string{}
		for _, op := range allOperations() {
			names = append(names, op.Name)
		}
		return jsonText(map[string]any{"bindings": bindings, "contacts": s.peerList(session.env, session.agent), "operations": names, "runtime": cachedRuntime(),
			"workflow": "Describe only needed operations. Use env_read for independent reads. Architect calls runtime_check(browser=true) once per workflow if runtime facts are absent. Reuse cached/shared facts. Write reports/tests to the listed writable bindings. Run browser_check(artifact,spec) using a reusable VFS JSON specification; evidence attaches automatically. Share the same artifact ID, never republish it just for review."}), nil
	case "env_call":
		var args struct {
			Name      string
			Arguments map[string]any
		}
		if err := decodeArgs(raw, &args); err != nil {
			return "", err
		}
		if strings.HasPrefix(args.Name, "env_") {
			return "", fmt.Errorf("recursive dispatch is not allowed")
		}
		if args.Arguments == nil {
			args.Arguments = map[string]any{}
		}
		return call(s, session, fs, args.Name, stringArgs(args.Arguments), args.Arguments)
	case "env_read":
		var args struct {
			Requests []struct {
				Name      string
				Arguments map[string]any
			}
		}
		if err := decodeArgs(raw, &args); err != nil {
			return "", err
		}
		if len(args.Requests) < 1 || len(args.Requests) > 12 {
			return "", fmt.Errorf("provide 1–12 read requests")
		}
		// Validate the entire batch before reading: a write hidden later in a
		// batch must not execute, and no read changes filesystem authority.
		for _, r := range args.Requests {
			if !oneOf(r.Name, []string{"vfs_read", "vfs_list", "vfs_outline", "vfs_stat", "vfs_search", "msg_read", "artifact_read", "workflow_status"}) {
				return "", fmt.Errorf("%s is not a batch read", r.Name)
			}
		}
		out := []map[string]any{}
		remaining := 48 << 10
		for i, r := range args.Requests {
			if remaining <= 0 {
				out = append(out, map[string]any{"index": i, "error": "response budget exhausted; request remaining items separately"})
				continue
			}
			a := stringArgs(r.Arguments)
			if r.Name == "vfs_read" || r.Name == "msg_read" {
				a["limit"] = fmt.Sprint(number(a, "limit", 160, 1, 500))
			}
			text, err := call(s, session, fs, r.Name, a, r.Arguments)
			item := map[string]any{"index": i, "name": r.Name}
			if err != nil {
				item["error"] = shorten(err.Error(), 500)
			} else {
				cap := min(remaining, 12<<10)
				if len(text) > cap {
					text = shorten(text, cap) + "\n[response capped; read a smaller range]"
					// Do not claim truncated text was delivered into the context.
					if r.Name == "vfs_read" {
						session.forget("")
					}
				}
				item["text"] = text
				remaining -= len(text)
			}
			out = append(out, item)
		}
		return jsonText(out), nil
	}
	return "", fmt.Errorf("unknown workflow operation")
}

func allOperations() []tool {
	out := append([]tool(nil), tools...)
	out = append(out, artifactTools...)
	out = append(out, browserTools...)
	out = append(out, tool{"workflow_status", "Read requested and observed model/effort and token counters for this Space at a handoff. No private peer content. Bounded, partial on timeout.", object(nil)})
	out = append(out, tool{"runtime_check", "Get runtime facts cached across Envs for 10 minutes, invalidated on app restart or Chrome replacement. Architect calls once if no shared result. browser=true includes real focus/native-select keys. Capabilities are not project-test passes.", object(nil, prop{"browser", "boolean", "Run the native browser preflight"})})
	return out
}

func probeRuntime(ctx context.Context, browser bool) (string, error) {
	versions := map[string]string{}
	for _, name := range []string{"node", "npm", "go", "rg"} {
		p, err := exec.LookPath(name)
		if err != nil {
			versions[name] = "unavailable"
			continue
		}
		c, cancel := context.WithTimeout(ctx, 3*time.Second)
		flag := "--version"
		if name == "go" {
			flag = "version"
		}
		out, err := exec.CommandContext(c, p, flag).CombinedOutput()
		cancel()
		if err != nil {
			versions[name] = "unavailable: " + shorten(err.Error(), 160)
		} else {
			versions[name] = shorten(strings.TrimSpace(string(out)), 160)
		}
	}
	result := map[string]any{"tools": versions, "browser_execution": "Use browser_check(artifact,spec): VFS JSON specs, shared fixtures/blocks, automatic evidence attachment. Reuse these facts; no per-role preflight. Fresh context per case; no supplied JavaScript."}
	if browser {
		result["browser"] = browserPreflight(ctx)
	}
	return jsonText(result), nil
}
