package aispaces

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Messages are kept: every message and reply of a Space is a record in its
// messages.jsonl, and what a sender attaches is stored beside it by content.
// An attachment is a copy the sender chose to hand over, taken at that
// moment. It is the cheap form of pasting text into the message: the sender
// does not retype it, and the recipient reads only the part it needs. It
// gives the recipient nothing beyond that copy; the folder it came from stays
// as closed to the recipient as it was.

type attachment struct {
	N     int    `json:"n"`
	Name  string `json:"name"`
	Path  string `json:"path,omitempty"` // real file it was taken from; shown only to the user's own agent
	Lines int    `json:"lines"`
	Bytes int    `json:"bytes"`
	Blob  string `json:"blob"`
	text  string // content, until it is stored
}

type message struct {
	Time        string       `json:"time"`
	Kind        string       `json:"kind"` // message or reply
	ID          string       `json:"id"`
	From        string       `json:"from"`
	To          string       `json:"to"`
	FromEnv     string       `json:"from_env,omitempty"`
	ToEnv       string       `json:"to_env,omitempty"`
	Delivered   bool         `json:"delivered"`
	Text        string       `json:"text"`
	Attachments []attachment `json:"attachments,omitempty"`
	Changed     []string     `json:"changed,omitempty"` // real files the sender changed before it
	Artifacts   []string     `json:"artifacts,omitempty"`
	Complexity  string       `json:"complexity,omitempty"`
	NewTask     bool         `json:"new_task,omitempty"`
}

// outgoing is what travels with a message besides its text.
type outgoing struct {
	attachments []attachment
	artifacts   []string
	talk        *conversation // its file changes are reported with the message
	direct      directive     // task level and context the sender asks for
}

const (
	attachmentLimit = 256 << 10 // one attachment
	attachmentCount = 8
	inlineLimit     = 900 // attachments this small are delivered with the message
)

var lineRange = regexp.MustCompile(`^[Ll]?(\d+)(?:\s*-\s*[Ll]?(\d+))?$`)
var forwarded = regexp.MustCompile(`^(?:#|msg:|message\s+#?)(\d+)$`)

// collect resolves what the sender wants to attach. It must not be called
// with s.mu held.
func (s *Store) collect(session *mcpSession, fs *FS, raw any) ([]attachment, error) {
	var specs []string
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		// A model sometimes passes the list as its JSON text.
		if json.Unmarshal([]byte(v), &specs) != nil && strings.TrimSpace(v) != "" {
			specs = []string{v}
		}
	case []any:
		for _, item := range v {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				specs = append(specs, text)
			}
		}
	default:
		return nil, fmt.Errorf("attach must be a list of paths")
	}
	if len(specs) > attachmentCount {
		return nil, fmt.Errorf("quota_exceeded: at most %d attachments per message", attachmentCount)
	}
	var out []attachment
	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if m := forwarded.FindStringSubmatch(spec); m != nil {
			text, err := s.threadText(session.env, m[1])
			if err != nil {
				return nil, err
			}
			out = append(out, attachment{Name: "message #" + m[1], text: text})
			continue
		}
		file, part := spec, ""
		resolve := func(p string) (string, error) {
			if strings.HasPrefix(p, "@") {
				return s.resolveRef(session.env, p)
			}
			return clean(p), nil
		}
		p, err := resolve(file)
		if err == nil {
			_, err = fs.Lstat(p)
		}
		if i := strings.LastIndex(spec, "#"); err != nil && i > 0 {
			file, part = spec[:i], strings.TrimSpace(spec[i+1:])
			if p, err = resolve(file); err != nil {
				return nil, err
			}
		}
		info, err := fs.Lstat(p)
		if err != nil {
			return nil, fmt.Errorf("attach %s: %v", spec, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("attach %s: it is a folder; attach files", spec)
		}
		data, truncated, err := fs.ReadFile(p, 2<<20)
		if err != nil || truncated || strings.ContainsRune(string(data[:min(len(data), 4096)]), 0) {
			return nil, fmt.Errorf("attach %s: only text files of up to 2 MiB can be attached", spec)
		}
		lines := splitLines(string(data))
		first, last, name := 1, len(lines), path.Base(p)
		if m := lineRange.FindStringSubmatch(part); m != nil {
			first, _ = strconv.Atoi(m[1])
			last = first
			if m[2] != "" {
				last, _ = strconv.Atoi(m[2])
			}
			first, last = max(1, min(first, len(lines))), max(1, min(last, len(lines)))
			name = fmt.Sprintf("%s lines %d-%d", name, first, last)
		} else if part != "" {
			var title string
			if first, last, title, err = sectionRange(outlineOf(p, lines), len(lines), part); err != nil {
				return nil, fmt.Errorf("attach %s: %v", spec, err)
			}
			name = fmt.Sprintf("%s section %q (lines %d-%d)", name, title, first, last)
		}
		text := ""
		if len(lines) > 0 {
			text = strings.Join(lines[first-1:last], "\n") + "\n"
		}
		if len(text) > attachmentLimit {
			return nil, fmt.Errorf("quota_exceeded: attach %s: more than %d KiB; attach a section or a line range", spec, attachmentLimit>>10)
		}
		real, _ := source(p, fs.bindings())
		out = append(out, attachment{Name: name, Path: real, text: text})
	}
	for i := range out {
		out[i].Bytes, out[i].Lines, out[i].Blob = len(out[i].text), strings.Count(out[i].text, "\n"), versionOf([]byte(out[i].text))
	}
	return out, nil
}

func (s *Store) spaceDir(sp *Space) string { return filepath.Join(s.root, "spaces", sp.ID) }

// thread returns the stored records of one message number. It must be called
// with s.mu held.
func (s *Store) thread(sp *Space, id string) []message {
	f, err := os.Open(filepath.Join(s.spaceDir(sp), "messages.jsonl"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []message
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 8<<20)
	for scanner.Scan() {
		var m message
		if json.Unmarshal(scanner.Bytes(), &m) == nil && m.ID == id {
			out = append(out, m)
		}
	}
	return out
}

// number gives the attachments their numbers, which run through the thread,
// and keeps their contents. It must be called with s.mu held.
func (s *Store) number(sp *Space, m *message) {
	dir := s.spaceDir(sp)
	if os.MkdirAll(filepath.Join(dir, "blobs"), 0700) != nil {
		return
	}
	next := 1
	for _, earlier := range s.thread(sp, m.ID) {
		next += len(earlier.Attachments)
	}
	for i := range m.Attachments {
		a := &m.Attachments[i]
		a.N = next + i
		if file := filepath.Join(dir, "blobs", a.Blob); a.text != "" {
			if _, err := os.Stat(file); err != nil {
				os.WriteFile(file, []byte(a.text), 0600)
			}
		}
	}
}

// store appends the record to the Space's message log. It must be called
// with s.mu held.
func (s *Store) store(sp *Space, m *message) {
	dir := s.spaceDir(sp)
	if os.MkdirAll(dir, 0700) != nil {
		return
	}
	m.Time = time.Now().Format("2006-01-02T15:04:05Z07:00")
	line, _ := json.Marshal(m)
	if f, err := os.OpenFile(filepath.Join(dir, "messages.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
		f.Write(append(line, '\n'))
		f.Close()
	}
}

// took reports whether the Env sent or received a record of the thread.
func took(msgs []message, e *Env) bool {
	for _, m := range msgs {
		if m.FromEnv == e.ID || m.ToEnv == e.ID || (m.FromEnv == "" && (strings.HasPrefix(m.From, e.Name+"/") || strings.HasPrefix(m.To, e.Name+"/"))) {
			return true
		}
	}
	return false
}

func renderThread(msgs []message) string {
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "[%s #%s from %s to %s, %s]\n%s\n", m.Kind, m.ID, m.From, m.To, m.Time, strings.TrimSpace(m.Text))
		for _, a := range m.Attachments {
			fmt.Fprintf(&b, "  attachment %d: %s — %d lines, %s\n", a.N, a.Name, a.Lines, short(int64(a.Bytes)))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// threadText is a thread as an attachment of another message: forwarding.
func (s *Store) threadText(e *Env, id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp := s.spaceOf(e)
	if sp == nil {
		return "", fmt.Errorf("this Env no longer exists")
	}
	msgs := s.thread(sp, id)
	if len(msgs) == 0 || !took(msgs, e) {
		return "", fmt.Errorf("not_found: no message #%s that you sent or received", id)
	}
	return renderThread(msgs), nil
}

// attachmentNotes is what the recipient is told about attachments: small
// ones in full, the rest as a list to read from.
func attachmentNotes(id string, list []attachment, operator bool) string {
	if len(list) == 0 {
		return ""
	}
	var inline, listed strings.Builder
	budget := 2 * inlineLimit
	for _, a := range list {
		if !operator && a.Bytes <= inlineLimit && a.Bytes <= budget && a.text != "" {
			budget -= a.Bytes
			fmt.Fprintf(&inline, "\n\n[Attachment %d: %s]\n%s", a.N, a.Name, strings.TrimRight(a.text, "\n"))
			continue
		}
		fmt.Fprintf(&listed, "\n  %d. %s — %d lines, %s", a.N, a.Name, a.Lines, short(int64(a.Bytes)))
		if operator && a.Path != "" {
			fmt.Fprintf(&listed, ", copied from %s", a.Path)
		}
	}
	out := inline.String()
	if listed.Len() > 0 {
		tool := "msg_read, request_id " + id
		if operator {
			tool = "message_read, id " + id
		}
		out += "\n\n[Attachments, not shown here. Read what you need with " + tool + ":" + listed.String() + "]"
	}
	return out
}

// refFor names a real file for an Env that has it attached.
func refFor(e *Env, real string) string {
	for _, b := range e.Bindings {
		if real == b.Source {
			return "@" + b.ID
		}
		if strings.HasPrefix(real, b.Source+string(filepath.Separator)) {
			return "@" + b.ID + "/" + filepath.ToSlash(strings.TrimPrefix(real, b.Source+string(filepath.Separator)))
		}
	}
	return ""
}

// changeNotes lists the files the sender changed since its previous message,
// named the way the recipient can open them. recipient is nil for the user's
// own agent, which sees real paths.
func changeNotes(sender string, out outgoing, recipient *Env) (string, []string) {
	if out.talk == nil {
		return "", nil
	}
	order, changed := out.talk.takeChanges()
	if len(order) == 0 {
		return "", nil
	}
	var lines []string
	outside := 0
	for _, real := range order {
		name := real
		if recipient != nil {
			if name = refFor(recipient, real); name == "" {
				outside++
				continue
			}
		}
		ch, what := changed[real], ""
		if ch.created {
			what = fmt.Sprintf("new, %d lines", ch.added)
		} else {
			what = fmt.Sprintf("+%d −%d", ch.added, ch.removed)
		}
		lines = append(lines, fmt.Sprintf("  %s (%s)", name, what))
	}
	if outside > 0 {
		lines = append(lines, fmt.Sprintf("  and %d file(s) in folders not attached to your Env", outside))
	}
	return fmt.Sprintf("\n\n[Files changed by %s since its previous message:\n%s]", sender, strings.Join(lines, "\n")), order
}

// readMessage returns a thread or one of its attachments. viewer is nil for
// the user's own agent, which may read every thread of the Space it names.
func (s *Store) readMessage(viewer *Env, a map[string]string) (string, error) {
	s.mu.Lock()
	sp := s.spaceOf(viewer)
	s.mu.Unlock()
	if sp == nil {
		return "", fmt.Errorf("this Env no longer exists")
	}
	return s.readThread(sp, viewer, a)
}

func (s *Store) readThread(sp *Space, viewer *Env, a map[string]string) (string, error) {
	id := strings.TrimPrefix(strings.TrimSpace(a["request_id"]+a["id"]), "#")
	s.mu.Lock()
	msgs := s.thread(sp, id)
	dir := s.spaceDir(sp)
	s.mu.Unlock()
	if len(msgs) == 0 || (viewer != nil && !took(msgs, viewer)) {
		return "", fmt.Errorf("not_found: no message #%s that you sent or received", id)
	}
	text, header := renderThread(msgs), fmt.Sprintf("thread #%s", id)
	name := "thread.md"
	if a["attachment"] != "" {
		n, found := number(a, "attachment", 0, 0, 1<<20), false
		for _, m := range msgs {
			for _, att := range m.Attachments {
				if att.N == n {
					data, err := os.ReadFile(filepath.Join(dir, "blobs", att.Blob))
					if err != nil {
						return "", fmt.Errorf("attachment %d of #%s is no longer stored", n, id)
					}
					text, header, name, found = string(data), fmt.Sprintf("attachment %d of #%s: %s", n, id, att.Name), strings.Fields(att.Name)[0], true
				}
			}
		}
		if !found {
			return "", fmt.Errorf("not_found: message #%s has no attachment %d", id, n)
		}
	}
	lines := splitLines(text)
	first, last := 1, len(lines)
	switch {
	case a["section"] != "":
		var title string
		var err error
		if first, last, title, err = sectionRange(outlineOf(name, lines), len(lines), a["section"]); err != nil {
			return "", err
		}
		header += fmt.Sprintf(", section %q", title)
	case a["offset"] != "" || a["limit"] != "":
		first = number(a, "offset", 1, 1, max(1, len(lines)))
		last = min(len(lines), first-1+number(a, "limit", len(lines), 1, len(lines)+1))
	}
	if len(lines) == 0 {
		return "[" + header + ": empty]", nil
	}
	body := strings.Join(lines[first-1:last], "\n")
	if len(body) > 256<<10 {
		body = cut(body, 256<<10) + "\n[cut at 256 KiB; continue with offset]"
	}
	return fmt.Sprintf("[%s — lines %d-%d of %d]\n%s", header, first, last, len(lines), body), nil
}
