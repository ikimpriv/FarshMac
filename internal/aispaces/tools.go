package aispaces

import (
	"bufio"
	"fmt"
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
	return []tool{
		{"vfs_list", "List a folder. Set depth to see a whole subtree in one call instead of listing folder by folder. Folders end with /; a reference root shows its @id and whether it is read-only.", object([]string{"path"},
			prop{"path", "string", "Folder, for example /. " + pathHelp},
			prop{"depth", "integer", "Levels to descend, 1 to 6. Default 1"})},
		{"vfs_stat", "Describe one file or folder: size, whether you may change it, and its portable reference (@id/path) to quote to other agents.", object([]string{"path"}, prop{"path", "string", pathHelp})},
		{"vfs_read", "Read a text file. Read only the lines you need with offset and limit, for example after vfs_search gave you a line number.", object([]string{"path"},
			prop{"path", "string", pathHelp},
			prop{"offset", "integer", "First line to return, starting at 1. Default 1"},
			prop{"limit", "integer", "Number of lines to return. Default: to the end of the file"},
			prop{"numbered", "boolean", "Prefix every line with its number. Default false"})},
		{"vfs_write", "Create or replace a whole file. In a referenced project folder it is saved to the real folder at once; a read-only folder refuses it.", object([]string{"path", "content"},
			prop{"path", "string", pathHelp},
			prop{"content", "string", "Complete new content"})},
		{"vfs_edit", "Replace exact text in files. Pass all replacements of a task in one call with edits, across as many files as needed. Nothing is written unless every replacement applies.", edit},
		{"vfs_search", "Search file contents and file names below a folder, or in one file. Returns path:line: text.", object([]string{"path", "text"},
			prop{"path", "string", "Folder or file to search. " + pathHelp},
			prop{"text", "string", "Text to find; a regular expression when regex is true"},
			prop{"regex", "boolean", "Treat text as a regular expression (RE2 syntax). Default false"},
			prop{"glob", "string", "Only files whose name matches, for example *.md"},
			prop{"context", "integer", "Lines of context around each match, 0 to 5. Default 0"},
			prop{"names_only", "boolean", "Match file and folder names only, not contents. Default false"})},
		{"vfs_copy", "Copy a file or a folder inside this Env. The server copies the bytes: use it instead of reading a file and writing it again, for example to place a draft into the project.", object([]string{"from", "to"}, prop{"from", "string", "Existing file or folder. " + pathHelp}, prop{"to", "string", "New path. " + pathHelp})},
		{"workspace_pull", "Copy a project folder into your scratch folder so that your own tools can edit, build and test it. Returns the local path. You have no other access to project files on disk.", object([]string{"path"}, prop{"path", "string", "Project folder or file. " + pathHelp})},
		{"workspace_push", "Copy your scratch copy of a project folder back into the project. The server copies the files, so never retype them with vfs_write. Refuses files that changed in the project since you pulled them.", object([]string{"path"}, prop{"path", "string", "The same path you gave to workspace_pull"})},
		{"vfs_mkdir", "Create a folder.", object([]string{"path"}, prop{"path", "string", pathHelp})},
		{"vfs_remove", "Remove a file or folder from this Env. In a referenced project folder the entry is only hidden from this Env; the real file is not deleted.", object([]string{"path"}, prop{"path", "string", pathHelp})},
		{"vfs_rename", "Rename or move within this Env.", object([]string{"from", "to"}, prop{"from", "string", "Existing path"}, prop{"to", "string", "New path"})},
		{"agent_peers", "Show your own address and the agents you may contact, with their state.", object(nil)},
		{"agent_send", "Send a request or a message to another agent. Returns at once. The answer is delivered into this conversation automatically as a new message: never wait or poll for it, just finish your turn. Name shared files by reference (@id/path), because virtual paths differ between Envs.", object([]string{"to", "message"},
			prop{"to", "string", "Address from agent_peers: Env/agent, or just Env when it has one agent"},
			prop{"message", "string", "Self-contained but short: the recipient does not see your conversation. Put long text (specs, instructions, reports) into a file and give its reference instead of pasting it"})},
		{"agent_reply", "Answer a message or a reply you received from another agent, in the same thread. Use the number from its header. Do not reply only to thank or acknowledge: reply when you have information, a decision or a question.", object([]string{"request_id", "message"},
			prop{"request_id", "string", "Number N from the header [FarshMac message #N ...] or [FarshMac reply to #N ...]"},
			prop{"message", "string", "Your complete answer"})},
	}
}()

const serverInstructions = "This server is the project filesystem of a FarshMac AI Env. " +
	"All project files live in a virtual filesystem rooted at /; use the vfs_* tools for every project read and write. " +
	"Your working directory is private scratch space and does not contain the project. " +
	"Work in few calls: vfs_list with depth for a tree, vfs_search then vfs_read with offset and limit for a fragment, vfs_edit with edits for all replacements at once. " +
	"To build, run or test code, call workspace_pull, work on the copy in your scratch folder with your own tools, then workspace_push; never type the same file twice. " +
	"Keep messages short: put long text into a file and send its reference, and do not send a message only to thank or acknowledge. " +
	"Other agents of the Space are reached with agent_send and answered with agent_reply; their replies arrive on their own, so never wait or poll. " +
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
	// A reference names the same real file in every Env; map it to this Env's path.
	local := func(p string) (string, error) {
		if strings.HasPrefix(p, "@") {
			return s.resolveRef(session.env, p)
		}
		return p, nil
	}
	for _, key := range []string{"path", "from", "to"} {
		if name != "agent_send" && strings.HasPrefix(a[key], "@") {
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
	case "agent_send":
		return s.send(session.env, session.agent, a["to"], a["message"])
	case "agent_reply":
		return s.reply(session.env, session.agent, a["request_id"], a["message"])
	case "vfs_remove", "vfs_rename":
		if target := a["path"] + a["from"]; fs.Reference(target) {
			return "", fmt.Errorf("permission_denied: %s is a reference attached by the user; only the user attaches, renames or detaches references", clean(target))
		}
	}
	switch name {
	case "vfs_list":
		return list(fs, a["path"], number(a, "depth", 1, 1, 6))
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
		return read(fs, a)
	case "vfs_write":
		if err := fs.WriteFile(a["path"], []byte(a["content"])); err != nil {
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
		return "copied to " + saved(fs, a["to"], int(info.Size())), nil
	case "workspace_pull":
		return workspacePull(s, session, fs, a["path"])
	case "workspace_push":
		return workspacePush(s, session, fs, a["path"])
	case "vfs_edit":
		return edit(fs, a, raw, local)
	case "vfs_mkdir":
		if err := fs.Mkdir(a["path"]); err != nil {
			return "", err
		}
		return "created " + clean(a["path"]), nil
	case "vfs_remove":
		if err := fs.Remove(a["path"], false); err != nil {
			return "", err
		}
		return "removed " + clean(a["path"]), nil
	case "vfs_rename":
		if err := fs.Rename(a["from"], a["to"]); err != nil {
			return "", err
		}
		return "renamed to " + clean(a["to"]), nil
	case "vfs_search":
		return search(fs, a)
	}
	return "", fmt.Errorf("unknown tool %s", name)
}

// saved describes a file that was just written: its size and the reference
// to quote to other agents, so no vfs_stat is needed afterwards.
func saved(fs *FS, p string, size int) string {
	if ref := fs.Ref(p); ref != "" {
		return fmt.Sprintf("%s (%d bytes, reference %s)", clean(p), size, ref)
	}
	return fmt.Sprintf("%s (%d bytes, private to this Env)", clean(p), size)
}

// list prints a folder, optionally with its subtree, one relative path per line.
func list(fs *FS, dir string, depth int) (string, error) {
	dir = clean(dir)
	roots := map[string]Binding{}
	for _, b := range fs.bindings() {
		roots[b.Virtual] = b
	}
	var out strings.Builder
	if ref := fs.Ref(dir); ref != "" {
		access := ""
		if fs.writable(dir) != nil {
			access = ", read-only"
		}
		fmt.Fprintf(&out, "[%s is %s%s]\n", dir, ref, access)
	}
	count := 0
	var walk func(string, string, int) error
	walk = func(d, prefix string, level int) error {
		entries, err := fs.ReadDir(d)
		if err != nil {
			return err
		}
		for _, info := range entries {
			if count++; count > 500 {
				out.WriteString("… more entries not shown; list a subfolder\n")
				return nil
			}
			full := path.Join(d, info.Name())
			note := ""
			if b, ok := roots[full]; ok {
				note = "  [@" + b.ID
				if b.ReadOnly {
					note += ", read-only"
				}
				note += "]"
			}
			if !info.IsDir() {
				fmt.Fprintf(&out, "%s%s\t%d bytes%s\n", prefix, info.Name(), info.Size(), note)
				continue
			}
			fmt.Fprintf(&out, "%s%s/%s\n", prefix, info.Name(), note)
			if level < depth && info.Name() != ".git" && info.Name() != "node_modules" {
				if err := walk(full, prefix+info.Name()+"/", level+1); err != nil {
					return err
				}
			}
			if count > 500 {
				return nil
			}
		}
		return nil
	}
	if err := walk(dir, "", 1); err != nil {
		return "", err
	}
	if count == 0 {
		out.WriteString("(empty folder)")
	}
	return out.String(), nil
}

// read returns a file, or the requested lines of it.
func read(fs *FS, a map[string]string) (string, error) {
	data, truncated, err := fs.ReadFile(a["path"], 8<<20)
	if err != nil {
		return "", err
	}
	if truncated {
		return "", fmt.Errorf("file is larger than 8 MiB; it cannot be read through this tool")
	}
	ranged := a["offset"] != "" || a["limit"] != ""
	numbered := a["numbered"] == "true"
	if !ranged && !numbered {
		if len(data) > 512<<10 {
			return string(data[:512<<10]) + "\n[truncated at 512 KiB; read the rest with offset and limit]", nil
		}
		return string(data), nil
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	first := number(a, "offset", 1, 1, max(1, len(lines)))
	last := min(len(lines), first-1+number(a, "limit", len(lines), 1, len(lines)+1))
	var out strings.Builder
	if ranged {
		fmt.Fprintf(&out, "[lines %d-%d of %d]\n", first, last, len(lines))
	}
	for i := first; i <= last; i++ {
		if numbered {
			fmt.Fprintf(&out, "%d\t", i)
		}
		out.WriteString(lines[i-1])
		out.WriteByte('\n')
		if out.Len() > 512<<10 {
			fmt.Fprintf(&out, "[stopped at line %d: 512 KiB; continue with offset %d]\n", i, i+1)
			break
		}
	}
	return out.String(), nil
}

// edit applies every requested replacement, or none of them.
func edit(fs *FS, a map[string]string, raw map[string]any, local func(string) (string, error)) (string, error) {
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
		if err := fs.WriteFile(file, []byte(texts[file])); err != nil {
			return "", fmt.Errorf("%v (already saved: %s)", err, strings.Join(done, ", "))
		}
		done = append(done, fmt.Sprintf("%s ×%d", saved(fs, file, len(texts[file])), counts[file]))
	}
	return fmt.Sprintf("applied %d replacement(s): %s", len(todo), strings.Join(done, ", ")), nil
}

// search looks through names and contents with fixed PoC budgets.
func search(fs *FS, a map[string]string) (string, error) {
	if a["text"] == "" {
		return "", fmt.Errorf("text is empty")
	}
	match := func(line string) bool { return strings.Contains(line, a["text"]) }
	if a["regex"] == "true" {
		re, err := regexp.Compile(a["text"])
		if err != nil {
			return "", fmt.Errorf("invalid regular expression: %v", err)
		}
		match = re.MatchString
	}
	if a["glob"] != "" {
		if _, err := path.Match(a["glob"], "x"); err != nil {
			return "", fmt.Errorf("invalid glob: %v", err)
		}
	}
	context, namesOnly := number(a, "context", 0, 0, 5), a["names_only"] == "true"
	const limit = 200
	var out []string
	found, visited := 0, 0
	file := func(p, name string, size int64) {
		if ok, _ := path.Match(a["glob"], name); a["glob"] != "" && !ok {
			return
		}
		if match(name) {
			out = append(out, p)
			found++
		}
		if namesOnly || size > 2<<20 {
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
		shown := -1
		for i, line := range lines {
			if found >= limit || !match(line) {
				continue
			}
			found++
			if context > 0 && shown >= 0 && i-context > shown+1 {
				out = append(out, "--")
			}
			for j := max(i-context, shown+1); j <= min(i+context, len(lines)-1); j++ {
				text, mark := lines[j], "-"
				if j == i || match(text) {
					mark = ":"
				}
				if len(text) > 300 {
					text = text[:300] + "…"
				}
				out = append(out, fmt.Sprintf("%s%s%d%s %s", p, mark, j+1, mark, text))
				shown = j
			}
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
			if namesOnly && a["glob"] == "" && match(info.Name()) {
				out = append(out, p+"/")
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
	if found >= limit {
		out = append(out, fmt.Sprintf("[stopped at %d matches; narrow the search]", limit))
	}
	return strings.Join(out, "\n"), nil
}
