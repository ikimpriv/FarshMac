package aispaces

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
)

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type prop struct {
	name, kind, description string
}

func object(required []string, props ...prop) map[string]any {
	properties := map[string]any{}
	for _, p := range props {
		properties[p.name] = map[string]any{"type": p.kind, "description": p.description}
	}
	if required == nil {
		required = []string{} // JSON Schema rejects "required": null
	}
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

const pathHelp = "Virtual path, or a reference @id/path"

func stringList(description string) map[string]any {
	return map[string]any{"type": "array", "description": description, "items": map[string]any{"type": "string"}}
}

const attachHelp = "Files or parts of files to hand over with the message instead of pasting their text: \"/docs/spec.md\", \"/docs/spec.md#L10-80\", \"/docs/spec.md#Section title\", or \"#7\" to forward message 7. The server copies them; the recipient reads only what it needs"

var tools = func() []tool {
	edit := object(nil,
		prop{"path", "string", "File for a single replacement, and the default file of edits. " + pathHelp},
		prop{"old_text", "string", "Single replacement: text to find; must occur exactly once"},
		prop{"new_text", "string", "Single replacement: its replacement"})
	edit["properties"].(map[string]any)["edits"] = map[string]any{
		"type":        "array",
		"description": "Several replacements in one call, applied in order. Prefer this to repeated calls.",
		"items": object([]string{"old_text", "new_text"},
			prop{"path", "string", "File of this replacement; defaults to the top-level path"},
			prop{"old_text", "string", "Text to find; must occur exactly once when its turn comes"},
			prop{"new_text", "string", "Replacement"}),
	}
	send := object([]string{"to", "message"},
		prop{"to", "string", "Address from agent_peers: Env/agent, or just Env when it has one agent"},
		prop{"message", "string", "Self-contained but short: the recipient does not see your conversation. Never paste file contents: attach them"})
	send["properties"].(map[string]any)["attach"] = stringList(attachHelp)
	send["properties"].(map[string]any)["artifacts"] = stringList("Immutable artifact IDs to share; recipient gets only these versions")
	send["properties"].(map[string]any)["complexity"] = map[string]any{"type": "string", "enum": Levels,
		"description": "Requires directing rights. Configured task profile: simple (mechanical), moderate, complex (design/deep analysis). Omit to keep settings. agent_peers shows choices"}
	send["properties"].(map[string]any)["model"] = map[string]any{"type": "string",
		"description": "Only for an agent you direct: the model it uses from this message on. agent_peers lists the names its CLI takes. Overrides complexity. Omit to leave it"}
	send["properties"].(map[string]any)["effort"] = map[string]any{"type": "string",
		"description": "Only for an agent you direct: its reasoning effort from this message on, for example low, medium, high (agent_peers lists what its CLI takes). Overrides complexity. Omit to leave it"}
	send["properties"].(map[string]any)["method"] = map[string]any{"type": "string", "enum": Methods,
		"description": "Requires directing rights. direct is cheapest; workflow delegates and verifies; research uses a web research workflow. Check agent_peers for support"}
	send["properties"].(map[string]any)["new_task"] = map[string]any{"type": "boolean",
		"description": "Only for an agent you direct. true when the task does not need the recipient's earlier conversation: its context is cleared first, which makes every step of the task cheaper. The message must then be self-contained. Default false"}
	reply := object([]string{"request_id", "message"},
		prop{"request_id", "string", "Number N from the header [FarshMac message #N ...] or [FarshMac reply to #N ...]"},
		prop{"message", "string", "Your answer, short. Never paste file contents: attach them"})
	reply["properties"].(map[string]any)["attach"] = stringList(attachHelp)
	reply["properties"].(map[string]any)["artifacts"] = stringList("Immutable artifact IDs to share with this reply")
	return []tool{
		{"vfs_list", "List a folder as a tree. Set depth to see a subtree in one call; large folders are shown as a count, list them separately. A reference root shows its @id and whether it is read-only.", object([]string{"path"},
			prop{"path", "string", "Folder, for example /. " + pathHelp},
			prop{"depth", "integer", "Levels to descend, 1 to 6. Default 1"})},
		{"vfs_outline", "Show the structure of a document or source file (headings or declarations with line numbers), or of every such file below a folder. Use it before reading: then read only the section you need.", object([]string{"path"}, prop{"path", "string", "File or folder. " + pathHelp})},
		{"vfs_stat", "Describe one file or folder: size, whether you may change it, and its portable reference (@id/path) to quote to other agents.", object([]string{"path"}, prop{"path", "string", pathHelp})},
		{"vfs_read", "Read a text file, or only a part: one section by its heading or declaration, or a line range. A file you already read in this conversation is not repeated: you get \"unchanged\", or only what changed.", object([]string{"path"},
			prop{"path", "string", pathHelp},
			prop{"section", "string", "Return only this section: a heading or declaration from vfs_outline (a distinctive part of it is enough)"},
			prop{"offset", "integer", "First line to return, starting at 1. Default 1"},
			prop{"limit", "integer", "Number of lines to return. Default: to the end of the file"},
			prop{"numbered", "boolean", "Prefix every line with its number. Default false"},
			prop{"full", "boolean", "Return the text even if you read this version before. Default false"})},
		{"vfs_write", "Create or replace a whole file; missing folders are created. In a referenced project folder it is saved to the real folder at once; a read-only folder refuses it.", object([]string{"path", "content"},
			prop{"path", "string", pathHelp},
			prop{"content", "string", "Complete new content"})},
		{"vfs_edit", "Replace exact text in files. Pass all replacements of a task in one call with edits, across as many files as needed. Nothing is written unless every replacement applies.", edit},
		{"vfs_search", "Search file contents and file names below a folder, or in one file. Results are grouped by file, long lines are cut around the match.", object([]string{"path", "text"},
			prop{"path", "string", "Folder or file to search. " + pathHelp},
			prop{"text", "string", "Text to find; a regular expression when regex is true"},
			prop{"regex", "boolean", "Treat text as a regular expression (RE2 syntax). Default false"},
			prop{"glob", "string", "Only files whose name matches, for example *.md"},
			prop{"context", "integer", "Lines of context around each match, 0 to 5. Default 0"},
			prop{"names_only", "boolean", "Match file and folder names only, not contents. Default false"})},
		{"vfs_copy", "Copy a file or a folder inside this Env. The server copies the bytes: use it instead of reading a file and writing it again.", object([]string{"from", "to"}, prop{"from", "string", "Existing file or folder. " + pathHelp}, prop{"to", "string", "New path. " + pathHelp})},
		{"workspace_pull", "Copy a project folder into your scratch folder so that your own tools can edit, build and test it. Returns the local path. You have no other access to project files on disk.", object([]string{"path"}, prop{"path", "string", "Project folder or file. " + pathHelp})},
		{"workspace_run", "Run build/tests in a pulled folder, inside FarshMac isolation. Bounded output; full log in scratch (16 MiB cap, last 10 retained). Stops on timeout or agent stop.", object([]string{"path", "command"}, prop{"path", "string", pathHelp}, prop{"command", "string", "Shell command"}, prop{"seconds", "integer", "Timeout: 1–300; default 60"})},
		{"workspace_push", "Copy your scratch copy of a project folder back into the project. The server copies the files, so never retype them with vfs_write. Refuses files that changed in the project since you pulled them.", object([]string{"path"}, prop{"path", "string", "The same path you gave to workspace_pull"})},
		{"vfs_mkdir", "Create a folder.", object([]string{"path"}, prop{"path", "string", pathHelp})},
		{"vfs_remove", "Remove a file or folder from this Env. In a referenced project folder the entry is only hidden from this Env; the real file is not deleted.", object([]string{"path"}, prop{"path", "string", pathHelp})},
		{"vfs_rename", "Rename or move within this Env.", object([]string{"from", "to"}, prop{"from", "string", "Existing path"}, prop{"to", "string", "New path"})},
		{"agent_peers", "Show your own address, the agents you may contact with their state, and the requests that wait for your answer.", object(nil)},
		{"agent_send", "Send a request or a message to another agent. Returns at once. The answer is delivered into this conversation automatically as a new message: never wait or poll for it, just finish your turn.", send},
		{"agent_reply", "Answer a message or a reply you received from another agent, in the same thread. Do not reply only to thank or acknowledge: reply when you have information, a decision or a question.", reply},
		{"msg_read", "Read an attachment of a message, or a whole message thread again (messages are kept; use it when one is no longer in your context).", object([]string{"request_id"},
			prop{"request_id", "string", "Message number N"},
			prop{"attachment", "integer", "Attachment number from the message. Omit to read the thread itself"},
			prop{"section", "string", "Return only this section of the attachment"},
			prop{"offset", "integer", "First line to return. Default 1"},
			prop{"limit", "integer", "Number of lines to return. Default: all"})},
	}
}()

const serverInstructions = "This server is the project filesystem of a FarshMac AI Env. Start with env_info; request only needed schemas. Use env_read for batched reads and env_call for operations. " +
	"All project files live in a virtual filesystem rooted at /; use the vfs_* tools for every project read and write. " +
	"Your working directory is private scratch space and does not contain the project. " +
	"Every byte you read or type is paid for again on each later step, so read narrowly: vfs_list with depth for a tree, vfs_outline for the structure of documents and code, then vfs_read with section (or offset and limit) for the part you need; vfs_search to locate text. " +
	"Change files with vfs_edit, passing all replacements at once. To build, run or test code, call workspace_pull, edit the scratch copy, use workspace_run for builds/tests, then workspace_push; never type the same text twice. " +
	"Other agents of the Space are reached with agent_send and answered with agent_reply; their replies arrive on their own, so never wait or poll. " +
	"Keep messages short and never paste file contents into them: attach files or sections. After sending a message or a reply, do not repeat or summarize it in your own output; end the turn with one short line. " +
	"Virtual paths differ between Envs: quote shared files to other agents by reference (@id/path from vfs_stat or vfs_list). " +
	"Referenced project folders are live: what you write there is saved to the real folder at once, unless the folder is read-only. Paths outside them are private to this Env."

func number(a map[string]string, key string, fallback, low, high int) int {
	n, err := strconv.Atoi(strings.TrimSpace(a[key]))
	if err != nil {
		n = fallback
	}
	return max(low, min(high, n))
}

func call(s *Store, session *mcpSession, fs *FS, name string, a map[string]string, raw map[string]any) (string, error) {
	if strings.HasPrefix(name, "env_") {
		return workflowCall(s, session, fs, name, raw)
	}
	if name == "workflow_status" {
		return workflowStatus(s, session)
	}
	if strings.HasPrefix(name, "artifact_") {
		return artifactCall(s, session, fs, name, raw)
	}
	if name == "runtime_check" {
		return runtimeCheck(sessionContext(session), a["browser"] == "true")
	}
	if name == "browser_check" {
		return browserCheck(s, session, fs, raw)
	}
	// A reference names the same real file in every Env; map it to this Env's path.
	local := func(p string) (string, error) {
		if strings.HasPrefix(p, "@") {
			return s.resolveRef(session.env, p)
		}
		return p, nil
	}
	for _, key := range []string{"path", "from", "to"} {
		if name != "agent_send" && name != "agent_reply" && strings.HasPrefix(a[key], "@") {
			p, err := local(a[key])
			if err != nil {
				return "", err
			}
			a[key] = p
		}
	}
	switch name {
	case "agent_peers":
		return s.peerList(session.env, session.agent), nil
	case "agent_send", "agent_reply":
		attached, err := s.collect(session, fs, raw["attach"])
		if err != nil {
			return "", err
		}
		out := outgoing{attachments: attached, talk: &session.conversation}
		if err := decodeArgs(raw["artifacts"], &out.artifacts); err != nil {
			return "", err
		}
		if name == "agent_send" {
			out.direct = directive{level: strings.TrimSpace(a["complexity"]), model: strings.TrimSpace(a["model"]), effort: strings.TrimSpace(a["effort"]), fresh: a["new_task"] == "true", method: strings.TrimSpace(a["method"])}
			return s.sendWith(session.env, session.agent, a["to"], a["message"], out)
		}
		return s.replyWith(session.env, session.agent, a["request_id"], a["message"], out)
	case "msg_read":
		return s.readMessage(session.env, a)
	case "vfs_remove", "vfs_rename":
		if target := a["path"] + a["from"]; fs.Reference(target) {
			return "", fmt.Errorf("permission_denied: %s is a reference attached by the user; only the user attaches, renames or detaches references", clean(target))
		}
	}
	switch name {
	case "vfs_list":
		return list(fs, a["path"], number(a, "depth", 1, 1, 6))
	case "vfs_outline":
		return outline(fs, a["path"])
	case "vfs_stat":
		info, err := fs.Lstat(a["path"])
		if err != nil {
			return "", err
		}
		kind := "file"
		if info.IsDir() {
			kind = "folder"
		}
		access, ref := "read/write", fs.Ref(a["path"])
		if fs.writable(a["path"]) != nil {
			access = "read-only"
		}
		if ref == "" {
			ref = "none (private to this Env, other agents cannot see it)"
		}
		return fmt.Sprintf("%s %s\nsize: %d bytes\nmodified: %s\naccess: %s\nreference: %s", kind, clean(a["path"]), info.Size(), info.ModTime().Format("2006-01-02 15:04:05"), access, ref), nil
	case "vfs_read":
		return read(session, fs, a)
	case "vfs_write":
		if err := write(session, fs, a["path"], []byte(a["content"]), true); err != nil {
			return "", err
		}
		return "written " + saved(fs, a["path"], len(a["content"])), nil
	case "vfs_copy":
		info, err := fs.Lstat(a["from"])
		if err != nil {
			return "", err
		}
		if under(clean(a["to"]), clean(a["from"])) {
			return "", fmt.Errorf("cannot copy a folder into itself")
		}
		if err = fs.copyTree(clean(a["from"]), clean(a["to"]), info); err != nil {
			return "", err
		}
		session.forget(clean(a["to"]))
		return "copied to " + saved(fs, a["to"], int(info.Size())), nil
	case "workspace_pull":
		return workspacePull(s, session, fs, a["path"])
	case "workspace_run":
		return workspaceRun(s, session, a["path"], a["command"], number(a, "seconds", 60, 1, 300))
	case "workspace_push":
		return workspacePush(s, session, fs, a["path"])
	case "vfs_edit":
		return edit(session, fs, a, raw, local)
	case "vfs_mkdir":
		if err := fs.Mkdir(a["path"]); err != nil {
			return "", err
		}
		return "created " + clean(a["path"]), nil
	case "vfs_remove":
		if err := fs.Remove(a["path"], false); err != nil {
			return "", err
		}
		session.forget(clean(a["path"]))
		return "removed " + clean(a["path"]), nil
	case "vfs_rename":
		if err := fs.Rename(a["from"], a["to"]); err != nil {
			return "", err
		}
		session.forget(clean(a["from"]))
		session.forget(clean(a["to"]))
		return "renamed to " + clean(a["to"]), nil
	case "vfs_search":
		return search(fs, a)
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

// write saves a file for the agent, creating missing folders, and notes the
// change: for the agent's own later reads and for its next message.
func write(session *mcpSession, fs *FS, p string, content []byte, typedAll bool) error {
	p = clean(p)
	if err := fs.writable(p); err != nil {
		return err
	}
	old, _, err := fs.ReadFile(p, 8<<20)
	existed := err == nil
	if !existed {
		if err := mkdirAll(fs, path.Dir(p)); err != nil {
			return err
		}
	}
	if err := fs.WriteFile(p, content); err != nil {
		return err
	}
	session.wrote(p, old, existed, content, typedAll)
	if real, shared := source(p, fs.bindings()); shared {
		session.touched(real, old, existed, content)
	}
	return nil
}

// saved describes a file that was just written: its size and the reference
// to quote to other agents, so no vfs_stat is needed afterwards.
func saved(fs *FS, p string, size int) string {
	if ref := fs.Ref(p); ref != "" {
		return fmt.Sprintf("%s (%d bytes, reference %s)", clean(p), size, ref)
	}
	return fmt.Sprintf("%s (%d bytes, private to this Env)", clean(p), size)
}

const listBudget = 150 // lines of a tree; deeper folders are folded to a count

// list prints a folder as an indented tree. Each level is opened only while
// the result stays within the budget, so one large folder cannot flood the
// agent's context; a folded folder shows how many entries it has.
func list(fs *FS, dir string, depth int) (string, error) {
	dir = clean(dir)
	roots := map[string]Binding{}
	for _, b := range fs.bindings() {
		roots[b.Virtual] = b
	}
	type node struct {
		info     os.FileInfo
		full     string
		children []*node
		open     bool
		count    int // entries of a folder that is not open
	}
	root := &node{full: dir, open: true}
	entries, err := fs.ReadDir(dir)
	if err != nil {
		return "", err
	}
	lines, hidden := 0, 0
	for i, info := range entries {
		if i == 400 {
			hidden = len(entries) - i
			break
		}
		root.children = append(root.children, &node{info: info, full: path.Join(dir, info.Name())})
		lines++
	}
	level := root.children
	for d := 1; len(level) > 0; d++ {
		var next []*node
		for _, n := range level {
			if !n.info.IsDir() {
				continue
			}
			if n.info.Name() == ".git" || n.info.Name() == "node_modules" {
				n.count = -1
				continue
			}
			inside, err := fs.ReadDir(n.full)
			if err != nil {
				continue
			}
			if d >= depth || lines+len(inside) > listBudget {
				n.count = len(inside)
				continue
			}
			n.open = true
			for _, info := range inside {
				n.children = append(n.children, &node{info: info, full: path.Join(n.full, info.Name())})
			}
			lines += len(inside)
			next = append(next, n.children...)
		}
		level = next
	}
	var out strings.Builder
	if ref := fs.Ref(dir); ref != "" {
		access := ""
		if fs.writable(dir) != nil {
			access = ", read-only"
		}
		fmt.Fprintf(&out, "[%s is %s%s]\n", dir, ref, access)
	}
	folded := false
	var print func(*node, string)
	print = func(n *node, indent string) {
		for _, c := range n.children {
			note := ""
			if b, ok := roots[c.full]; ok {
				note = "  [@" + b.ID
				if b.ReadOnly {
					note += ", read-only"
				}
				note += "]"
			}
			switch {
			case !c.info.IsDir():
				fmt.Fprintf(&out, "%s%s  %s%s\n", indent, c.info.Name(), short(c.info.Size()), note)
			case c.open:
				fmt.Fprintf(&out, "%s%s/%s\n", indent, c.info.Name(), note)
				print(c, indent+"  ")
			case c.count < 0:
				fmt.Fprintf(&out, "%s%s/  (not listed)%s\n", indent, c.info.Name(), note)
			default:
				folded = folded || c.count > 0
				fmt.Fprintf(&out, "%s%s/  (%d entries)%s\n", indent, c.info.Name(), c.count, note)
			}
		}
	}
	print(root, "")
	if hidden > 0 {
		fmt.Fprintf(&out, "… %d more entries\n", hidden)
	}
	if len(entries) == 0 {
		out.WriteString("(empty folder)")
	}
	return out.String(), nil
}

// read returns a file, a section or a line range of it. What the agent's
// conversation already holds is not sent again.
func read(session *mcpSession, fs *FS, a map[string]string) (string, error) {
	p := clean(a["path"])
	data, truncated, err := fs.ReadFile(p, 8<<20)
	if err != nil {
		return "", err
	}
	if truncated {
		return "", fmt.Errorf("file is larger than 8 MiB; it cannot be read through this tool")
	}
	lines := splitLines(string(data))
	numbered := a["numbered"] == "true"
	first, last, header := 1, len(lines), ""
	switch {
	case a["section"] != "":
		var title string
		if first, last, title, err = sectionRange(outlineOf(p, lines), len(lines), a["section"]); err != nil {
			return "", err
		}
		header = fmt.Sprintf("[section %q: lines %d-%d of %d]\n", title, first, last, len(lines))
	case a["offset"] != "" || a["limit"] != "":
		first = number(a, "offset", 1, 1, max(1, len(lines)))
		last = min(len(lines), first-1+number(a, "limit", len(lines), 1, len(lines)+1))
		header = fmt.Sprintf("[lines %d-%d of %d]\n", first, last, len(lines))
	}
	whole := first == 1 && last == len(lines)
	part := fmt.Sprintf("%d-%d", first, last)
	if numbered {
		part += "n"
	}
	version := versionOf(data)
	if a["full"] != "true" {
		wanted := 0
		for _, line := range lines[min(first-1, len(lines)):last] {
			wanted += len(line) + 1
		}
		if note := session.known(p, version, whole, numbered, part, lines, wanted); note != "" {
			return note, nil
		}
	}
	var out strings.Builder
	if whole && !numbered {
		if len(data) > 512<<10 {
			return string(data[:512<<10]) + "\n[truncated at 512 KiB; read the rest with offset and limit]", nil
		}
		out.Write(data)
	} else {
		out.WriteString(header)
		for i := first; i <= last; i++ {
			if numbered {
				fmt.Fprintf(&out, "%d\t", i)
			}
			out.WriteString(lines[i-1])
			out.WriteByte('\n')
			if out.Len() > 512<<10 {
				fmt.Fprintf(&out, "[stopped at line %d: 512 KiB; continue with offset %d]\n", i, i+1)
				whole = false
				break
			}
		}
	}
	session.remember(p, version, whole, numbered, part, string(data))
	return out.String(), nil
}

// edit applies every requested replacement, or none of them.
func edit(session *mcpSession, fs *FS, a map[string]string, raw map[string]any, local func(string) (string, error)) (string, error) {
	type replacement struct{ path, old, new string }
	var todo []replacement
	if a["old_text"] != "" {
		todo = append(todo, replacement{a["path"], a["old_text"], a["new_text"]})
	}
	if items, ok := raw["edits"].([]any); ok {
		for _, item := range items {
			fields, _ := item.(map[string]any)
			r := replacement{path: a["path"]}
			if p, _ := fields["path"].(string); p != "" {
				var err error
				if r.path, err = local(p); err != nil {
					return "", err
				}
			}
			r.old, _ = fields["old_text"].(string)
			r.new, _ = fields["new_text"].(string)
			todo = append(todo, r)
		}
	}
	if len(todo) == 0 {
		return "", fmt.Errorf("nothing to do: pass old_text and new_text, or edits")
	}
	texts, order, counts := map[string]string{}, []string{}, map[string]int{}
	var failures []string
	for i, r := range todo {
		if r.path == "" {
			failures = append(failures, fmt.Sprintf("edit %d: no path", i+1))
			continue
		}
		file := clean(r.path)
		text, loaded := texts[file]
		if !loaded {
			if err := fs.writable(file); err != nil {
				failures = append(failures, fmt.Sprintf("edit %d: %v", i+1, err))
				continue
			}
			data, truncated, err := fs.ReadFile(file, 8<<20)
			if err != nil || truncated {
				failures = append(failures, fmt.Sprintf("edit %d: cannot read %s", i+1, file))
				continue
			}
			text = string(data)
			order = append(order, file)
		}
		if n := strings.Count(text, r.old); r.old == "" || n != 1 {
			failures = append(failures, fmt.Sprintf("edit %d (%s): old_text occurs %d times, expected exactly once", i+1, file, n))
			texts[file] = text
			continue
		}
		texts[file] = strings.Replace(text, r.old, r.new, 1)
		counts[file]++
	}
	if len(failures) > 0 {
		return "", fmt.Errorf("conflict: nothing was changed.\n%s", strings.Join(failures, "\n"))
	}
	var done []string
	for _, file := range order {
		if err := write(session, fs, file, []byte(texts[file]), false); err != nil {
			return "", fmt.Errorf("%v (already saved: %s)", err, strings.Join(done, ", "))
		}
		done = append(done, fmt.Sprintf("%s ×%d", saved(fs, file, len(texts[file])), counts[file]))
	}
	return fmt.Sprintf("applied %d replacement(s): %s", len(todo), strings.Join(done, ", ")), nil
}

const searchBudget = 5000 // bytes of matches returned; the rest is counted

// snippet shortens a long line to the text around its match.
func snippet(line string, from, to int) string {
	if len(line) <= 200 {
		return line
	}
	start, end := max(0, from-70), min(len(line), max(to, from)+110)
	for start > 0 && line[start]&0xC0 == 0x80 {
		start--
	}
	for end < len(line) && line[end]&0xC0 == 0x80 {
		end++
	}
	text := line[start:end]
	if start > 0 {
		text = "…" + text
	}
	if end < len(line) {
		text += "…"
	}
	return text
}

// search looks through names and contents. Matches are grouped by file and
// stop at a size budget; what did not fit is counted per file.
func search(fs *FS, a map[string]string) (string, error) {
	if a["text"] == "" {
		return "", fmt.Errorf("text is empty")
	}
	locate := func(line string) (int, int) {
		i := strings.Index(line, a["text"])
		return i, i + len(a["text"])
	}
	if a["regex"] == "true" {
		re, err := regexp.Compile(a["text"])
		if err != nil {
			return "", fmt.Errorf("invalid regular expression: %v", err)
		}
		locate = func(line string) (int, int) {
			if m := re.FindStringIndex(line); m != nil {
				return m[0], m[1]
			}
			return -1, -1
		}
	}
	match := func(line string) bool { i, _ := locate(line); return i >= 0 }
	if a["glob"] != "" {
		if _, err := path.Match(a["glob"], "x"); err != nil {
			return "", fmt.Errorf("invalid glob: %v", err)
		}
	}
	context, namesOnly := number(a, "context", 0, 0, 5), a["names_only"] == "true"
	const limit = 2000 // matches counted before the search stops
	var out []string
	found, visited, size := 0, 0, 0
	type overflow struct {
		path  string
		count int
	}
	var rest []overflow
	emit := func(line string) {
		out = append(out, line)
		size += len(line) + 1
	}
	file := func(p, name string, bytes int64) {
		if ok, _ := path.Match(a["glob"], name); a["glob"] != "" && !ok {
			return
		}
		headed := false
		head := func() {
			if !headed {
				emit(p)
				headed = true
			}
		}
		if match(name) && size < searchBudget {
			head()
			found++
		}
		if namesOnly || bytes > 2<<20 {
			return
		}
		r, err := fs.Open(p)
		if err != nil {
			return
		}
		defer r.Close()
		var lines []string
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 64*1024), 2<<20)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.ContainsRune(line, 0) {
				return // binary
			}
			lines = append(lines, line)
		}
		shown, missed := -1, 0
		for i, line := range lines {
			from, to := locate(line)
			if from < 0 || found >= limit {
				continue
			}
			found++
			if size >= searchBudget {
				missed++
				continue
			}
			head()
			if context > 0 && shown >= 0 && i-context > shown+1 {
				emit("  --")
			}
			for j := max(i-context, shown+1); j <= min(i+context, len(lines)-1); j++ {
				text, mark := lines[j], "-"
				if j == i {
					text, mark = snippet(text, from, to), ":"
				} else if f, t := locate(text); f >= 0 {
					text, mark = snippet(text, f, t), ":"
				} else {
					text = snippet(text, 0, 0)
				}
				emit(fmt.Sprintf("  %d%s %s", j+1, mark, text))
				shown = j
			}
		}
		if missed > 0 {
			rest = append(rest, overflow{p, missed})
		}
	}
	var walk func(string) error
	walk = func(d string) error {
		entries, err := fs.ReadDir(d)
		if err != nil {
			return err
		}
		for _, info := range entries {
			if found >= limit || visited >= 20000 {
				return nil
			}
			visited++
			p := path.Join(d, info.Name())
			if !info.IsDir() {
				if info.Mode().IsRegular() {
					file(p, info.Name(), info.Size())
				}
				continue
			}
			if namesOnly && a["glob"] == "" && match(info.Name()) && size < searchBudget {
				emit(p + "/")
				found++
			}
			if info.Name() != ".git" && info.Name() != "node_modules" {
				if err := walk(p); err != nil {
					return err
				}
			}
		}
		return nil
	}
	root := clean(a["path"])
	info, err := fs.Lstat(root)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		err = walk(root)
	} else {
		file(root, info.Name(), info.Size())
	}
	if err != nil {
		return "", err
	}
	if found == 0 {
		return "no matches", nil
	}
	if len(rest) > 0 {
		missed := 0
		var where []string
		for i, o := range rest {
			missed += o.count
			if i < 8 {
				where = append(where, fmt.Sprintf("%s (%d)", o.path, o.count))
			}
		}
		if len(rest) > 8 {
			where = append(where, "…")
		}
		more := ""
		if found >= limit {
			more = " or more"
		}
		emit(fmt.Sprintf("[output limit: %d%s further matches not shown, in %s. Narrow the search with path, glob or a more specific text]", missed, more, strings.Join(where, ", ")))
	}
	return strings.Join(out, "\n"), nil
}
