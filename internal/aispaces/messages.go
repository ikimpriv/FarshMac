package aispaces

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Agents of one Space talk through the store. An agent is addressed as
// "Env/agent" (or just "Env" when the Env has one agent). The sender is
// always the authenticated MCP session, never an argument.
//
// Nothing polls: a message is typed into the recipient's console as a new
// prompt, and the answer is typed into the asker's console the same way. An
// agent that is not running gets its messages when the user starts it.

// request is a message that still waits for its answer. Envs are IDs.
type request struct{ FromEnv, FromAgent, ToEnv, ToAgent string }

func address(e *Env, agent string) string { return e.Name + "/" + agent }

func (s *Store) spaceOf(e *Env) *Space {
	for _, sp := range s.Spaces {
		for _, x := range sp.Envs {
			if x == e {
				return sp
			}
		}
	}
	return nil
}

func (sp *Space) envByID(id string) *Env {
	for _, e := range sp.Envs {
		if e.ID == id {
			return e
		}
	}
	return nil
}

func (e *Env) agent(name string) *Agent {
	for i := range e.Agents {
		if e.Agents[i].Name == name {
			return &e.Agents[i]
		}
	}
	return nil
}

// Peers reports the other Envs of the Space and which of them the agents of
// env may contact on their own initiative.
func (s *Store) Peers(space, env string) (others []string, allowed map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, e, err := s.find(space, env)
	allowed = map[string]bool{}
	if err != nil || e == nil {
		return nil, allowed
	}
	for _, other := range sp.Envs {
		if other == e {
			continue
		}
		others = append(others, other.Name)
		for _, id := range e.Peers {
			if id == other.ID {
				allowed[other.Name] = true
			}
		}
	}
	return others, allowed
}

// SetPeers replaces the list of Envs the agents of env may contact.
func (s *Store) SetPeers(space, env string, names []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, e, err := s.find(space, env)
	if err != nil {
		return err
	}
	// A link is two-way: both Envs may start a conversation with the other.
	linked := map[string]bool{}
	for _, name := range names {
		if other := sp.env(name); other != nil && other != e {
			linked[other.ID] = true
		}
	}
	without := func(list []string, id string) []string {
		kept := list[:0:0]
		for _, x := range list {
			if x != id {
				kept = append(kept, x)
			}
		}
		return kept
	}
	e.Peers = nil
	for _, other := range sp.Envs {
		if other == e {
			continue
		}
		other.Peers = without(other.Peers, e.ID)
		if linked[other.ID] {
			e.Peers = append(e.Peers, other.ID)
			other.Peers = append(other.Peers, e.ID)
		}
	}
	return s.save()
}

// mayContact: a listed peer, or anyone whose question to this Env is still open.
func mayContact(sp *Space, from, to *Env) bool {
	for _, id := range from.Peers {
		if id == to.ID {
			return true
		}
	}
	if mayDirect(from, to) {
		return true
	}
	for _, r := range sp.Requests {
		if r.FromEnv == to.ID && r.ToEnv == from.ID {
			return true
		}
	}
	return false
}

// peerList is what an agent sees: its own address and whom it may contact.
func (s *Store) peerList(e *Env, agent string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp := s.spaceOf(e)
	if sp == nil {
		return "this Env no longer exists"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You are %s in the AI Space %q.\n", address(e, agent), sp.Name)
	var lines []string
	for _, other := range sp.Envs {
		if other == e || !mayContact(sp, e, other) {
			continue
		}
		for _, a := range other.Agents {
			if a.Disabled {
				continue
			}
			state := "not running: it is started automatically when you message it"
			if s.running[other.ID+":"+a.Name] != nil {
				state = "running"
			}
			if mayDirect(e, other) {
				state += "; you direct it: " + a.choices()
			}
			lines = append(lines, fmt.Sprintf("  %s  (%s)", address(other, a.Name), state))
		}
	}
	if len(lines) == 0 {
		b.WriteString("You may not contact any other agent. The user decides who talks to whom.\n")
	} else {
		sort.Strings(lines)
		b.WriteString("You may contact:\n" + strings.Join(lines, "\n") + "\n")
	}
	// An agent asked to choose a colleague's model or effort must learn here
	// whether it can, and what to tell the user if it cannot.
	directs := false
	for _, other := range sp.Envs {
		directs = directs || (other != e && mayDirect(e, other))
	}
	if directs {
		b.WriteString("For an agent you direct, agent_send takes model and effort (or complexity, which sets both as the user defined), new_task to clear its context, and method for its way of working.\n")
	} else if len(lines) > 0 {
		b.WriteString("You direct nobody: you cannot set another agent's model, effort or context, and agent_send ignores model, effort, complexity, new_task and method from you. Only the user can allow it: in FarshMac, F11 on this Env → Communication… → direct.\n")
	}
	// Messages are kept, so an agent whose conversation was cleared can see
	// what it still owes.
	var open []string
	for id, r := range sp.Requests {
		if r.ToEnv != e.ID || r.ToAgent != agent {
			continue
		}
		subject := ""
		if msgs := s.thread(sp, id); len(msgs) > 0 {
			last := msgs[len(msgs)-1]
			subject = fmt.Sprintf(" from %s: %s", last.From, shorten(strings.SplitN(strings.TrimSpace(last.Text), "\n", 2)[0], 100))
		}
		open = append(open, fmt.Sprintf("  #%s%s", id, subject))
	}
	if len(open) > 0 {
		sort.Strings(open)
		b.WriteString("Waiting for your answer (read with msg_read, answer with agent_reply):\n" + strings.Join(open, "\n") + "\n")
	}
	return b.String()
}

// resolve finds the addressed agent among the Envs of the sender's Space.
func resolve(sp *Space, to string) (*Env, string, error) {
	parts := strings.SplitN(strings.TrimSpace(to), "/", 2)
	var target *Env
	for _, e := range sp.Envs {
		if strings.EqualFold(e.Name, strings.TrimSpace(parts[0])) {
			target = e
		}
	}
	if target == nil {
		return nil, "", fmt.Errorf("not_found: no Env %q in this Space; call agent_peers for valid addresses", parts[0])
	}
	if len(parts) == 2 {
		if a := target.agent(strings.TrimSpace(parts[1])); a != nil && !a.Disabled {
			return target, a.Name, nil
		} else if a != nil {
			return nil, "", fmt.Errorf("not_found: %s is disabled by the user; call agent_peers for the agents in use", address(target, a.Name))
		}
		return nil, "", fmt.Errorf("not_found: Env %s has no agent %q", target.Name, parts[1])
	}
	var names []string
	for _, a := range target.Agents {
		if !a.Disabled {
			names = append(names, a.Name)
		}
	}
	switch len(names) {
	case 0:
		return nil, "", fmt.Errorf("not_found: Env %s has no agent in use", target.Name)
	case 1:
		return target, names[0], nil
	}
	for i := range names {
		names[i] = address(target, names[i])
	}
	return nil, "", fmt.Errorf("ambiguous: address one agent of %s: %s", target.Name, strings.Join(names, ", "))
}

// deliver types text into the agent's console. An agent that is not running
// is started for it, without a window: its console can be opened later and
// shows everything it did. If it cannot be started the text waits for its
// next start. It must be called with s.mu held.
func (s *Store) deliver(e *Env, agent, text string) bool {
	return s.direct(e, agent, text, directive{}, false)
}

// direct delivers a message together with what its sender, allowed to direct
// the recipient, asked for: a task level and a clean context. A running
// agent gets them when it is free; a stopped one is started with them.
func (s *Store) direct(e *Env, agent, text string, d directive, directed bool) bool {
	a := e.agent(agent)
	if a == nil {
		return false
	}
	if directed && d.level != "" {
		a.Level, a.Chosen = d.level, Profile{}
	}
	if directed && d.model != "" {
		a.Chosen.Model = d.model
	}
	if directed && d.effort != "" {
		a.Chosen.Effort = d.effort
	}
	if p := s.running[e.ID+":"+agent]; p != nil {
		p.post(delivery{text: text, directed: directed, fresh: directed && d.fresh, profile: a.profile()})
		return true
	}
	a.Inbox = append(a.Inbox, text)
	a.Fresh = a.Fresh || (directed && d.fresh)
	sp := s.spaceOf(e)
	if sp == nil || !AutoStart {
		return false
	}
	if _, err := s.launch(sp, e, agent, 120, 40); err != nil {
		s.notices = append(s.notices, fmt.Sprintf("%s has a message but could not be started: %v", address(e, agent), err))
		return false
	}
	s.notices = append(s.notices, fmt.Sprintf("Started %s to receive a message; press Enter on it to open its console", address(e, agent)))
	return true
}

// directed checks a sender's directive and says how the recipient is told
// and what the sender hears back. from is nil for the user's own agent, which
// may direct anyone. It must be called with s.mu held.
func (s *Store) directed(sp *Space, from, to *Env, agent, id string, d directive) (allowed bool, prefix, note string, wrap func(string) string, err error) {
	wrap = func(text string) string { return text }
	if d.empty() {
		return false, "", "", wrap, nil
	}
	if err = d.check(); err != nil {
		return false, "", "", wrap, err
	}
	if from != nil && !mayDirect(from, to) {
		return false, "", "", wrap, fmt.Errorf("permission_denied: %s may contact but cannot direct %s; message not sent. Remove directing parameters or ask the user to enable directing in Communication", from.Name, to.Name)
	}
	var said []string
	refused := ""
	if a := to.agent(agent); a != nil {
		if err = a.accepts(d); err != nil {
			return false, "", "", wrap, err
		}
	}
	if a := to.agent(agent); a != nil && !d.plain() {
		if how, why := a.way(d.method); how != nil {
			wrap = how
			said = append(said, "works on it by "+d.method)
		} else {
			refused = fmt.Sprintf(" method %s is not available for %s: %s; it works on it directly.", d.method, address(to, agent), why)
		}
	}
	if d.fresh {
		prefix = "[New task: your earlier conversation was cleared."
		var open []string
		for other, r := range sp.Requests {
			if other != id && r.ToEnv == to.ID && r.ToAgent == agent {
				open = append(open, "#"+other)
			}
		}
		if len(open) > 0 {
			sort.Strings(open)
			prefix += " You still owe answers to " + strings.Join(open, ", ") + ": msg_read shows them."
		}
		prefix += "]\n"
		said = append(said, "starts with a clean context")
	}
	if d.level != "" {
		said = append(said, "is set up for a "+d.level+" task")
	}
	if d.model != "" {
		said = append(said, "gets model "+d.model)
	}
	if d.effort != "" {
		said = append(said, "gets "+d.effort+" effort")
	}
	who := address(to, agent)
	if len(said) == 0 {
		return true, prefix, refused, wrap, nil
	}
	s.notices = append(s.notices, fmt.Sprintf("%s %s for message #%s", who, strings.Join(said, " and "), id))
	return true, prefix, fmt.Sprintf(" Requested: %s %s, as soon as it is free. Not yet confirmed by the CLI.%s", who, strings.Join(said, " and "), refused), wrap, nil
}

// AutoStart starts a stopped agent when a message arrives for it.
var AutoStart = true

// MessagesPath is the log of all messages of a Space, one JSON object per line.
func (s *Store) MessagesPath(space string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sp := s.space(space); sp != nil {
		return filepath.Join(s.root, "spaces", sp.ID, "messages.jsonl")
	}
	return ""
}

// send passes a message from one agent to another and remembers who asked,
// so the answer finds its way back.
func (s *Store) send(from *Env, fromAgent, to, text string) (string, error) {
	return s.sendWith(from, fromAgent, to, text, outgoing{})
}

func (s *Store) sendWith(from *Env, fromAgent, to, text string, out outgoing) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("the message is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sp := s.spaceOf(from)
	if sp == nil {
		return "", fmt.Errorf("this Env no longer exists")
	}
	target, agent, err := resolve(sp, to)
	if err != nil {
		return "", err
	}
	if target == from && agent == fromAgent {
		return "", fmt.Errorf("you cannot message yourself")
	}
	if target != from && !mayContact(sp, from, target) {
		return "", fmt.Errorf("permission_denied: %s may not contact %s. The user decides who talks to whom; call agent_peers to see whom you may contact", from.Name, target.Name)
	}
	artifactNotes, err := s.shareArtifacts(sp, from, target.ID, out.artifacts)
	if err != nil {
		return "", err
	}
	text += artifactNotes
	sp.NextMessage++
	id := strconv.Itoa(sp.NextMessage)
	if sp.Requests == nil {
		sp.Requests = map[string]request{}
	}
	sp.Requests[id] = request{from.ID, fromAgent, target.ID, agent}
	sender, recipient := address(from, fromAgent), address(target, agent)
	allowed, prefix, directNote, wrap, err := s.directed(sp, from, target, agent, id, out.direct)
	if err != nil {
		delete(sp.Requests, id)
		sp.NextMessage--
		return "", err
	}
	m := message{Kind: "message", ID: id, From: sender, To: recipient, FromEnv: from.ID, ToEnv: target.ID, Text: text, Attachments: out.attachments, Artifacts: out.artifacts}
	if allowed {
		m.Complexity, m.NewTask = out.direct.level, out.direct.fresh
	}
	changes, changed := changeNotes(sender, out, target)
	m.Changed = changed
	s.number(sp, &m)
	m.Delivered = s.direct(target, agent, wrap(fmt.Sprintf("%s[FarshMac message #%s from %s]\n%s%s%s\n\n[When you have the answer, send it with the agent_reply tool, request_id %s; attach files instead of pasting them. It is delivered to %s automatically.]", prefix, id, sender, text, attachmentNotes(id, m.Attachments, false), changes, id, sender)), out.direct, allowed)
	delivered := m.Delivered
	s.store(sp, &m)
	if err = s.save(); err != nil {
		return "", err
	}
	state := "delivered to console queue"
	if !delivered {
		state = "queued: that agent could not be started, it receives the message when the user starts it"
	}
	return fmt.Sprintf("Message #%s to %s: %s%s.%s\nThe reply arrives in this conversation on its own as a new message. Do not wait or poll, and do not repeat the message in your output: end your turn now.", id, recipient, state, attached(m.Attachments), directNote+s.unreachable(sp, target, text)), nil
}

// reply returns the answer to whoever sent the message.
func (s *Store) reply(from *Env, fromAgent, id, text string) (string, error) {
	return s.replyWith(from, fromAgent, id, text, outgoing{})
}

func (s *Store) replyWith(from *Env, fromAgent, id, text string, out outgoing) (string, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "#")
	s.mu.Lock()
	defer s.mu.Unlock()
	sp := s.spaceOf(from)
	if sp == nil {
		return "", fmt.Errorf("this Env no longer exists")
	}
	r, ok := sp.Requests[id]
	if ok && r.FromEnv == from.ID && r.FromAgent == fromAgent {
		return "", fmt.Errorf("conflict: you spoke last in thread #%s; wait for the answer, it arrives on its own", id)
	}
	if !ok || r.ToEnv != from.ID || r.ToAgent != fromAgent {
		return "", fmt.Errorf("not_found: no message #%s addressed to you; start a new one with agent_send", id)
	}
	artifactNotes, err := s.shareArtifacts(sp, from, r.FromEnv, out.artifacts)
	if err != nil {
		return "", err
	}
	text += artifactNotes
	if r.FromEnv == operatorEnv {
		// The user's own assistant has no console: it collects its replies.
		sender := address(from, fromAgent)
		m := message{Kind: "reply", ID: id, From: sender, To: "user/" + r.FromAgent, FromEnv: from.ID, ToEnv: operatorEnv, Delivered: true, Text: text, Attachments: out.attachments, Artifacts: out.artifacts}
		changes, changed := changeNotes(sender, out, nil)
		m.Changed = changed
		s.number(sp, &m)
		s.store(sp, &m)
		sp.Operator = append(sp.Operator, operatorNote{id, sender, time.Now().Format("15:04:05"), text + attachmentNotes(id, m.Attachments, true) + changes})
		if err := s.save(); err != nil {
			return "", err
		}
		return fmt.Sprintf("Reply to #%s delivered to the user's assistant%s. Do not repeat it in your output: end your turn.", id, attached(m.Attachments)), nil
	}
	asker := sp.envByID(r.FromEnv)
	if asker == nil {
		delete(sp.Requests, id)
		s.save()
		return "", fmt.Errorf("not_found: the sender of #%s no longer exists", id)
	}
	// The thread stays open the other way round, so a reply can be answered.
	sp.Requests[id] = request{r.ToEnv, r.ToAgent, r.FromEnv, r.FromAgent}
	sender, recipient := address(from, fromAgent), address(asker, r.FromAgent)
	m := message{Kind: "reply", ID: id, From: sender, To: recipient, FromEnv: from.ID, ToEnv: asker.ID, Text: text, Attachments: out.attachments, Artifacts: out.artifacts}
	changes, changed := changeNotes(sender, out, asker)
	m.Changed = changed
	s.number(sp, &m)
	m.Delivered = s.deliver(asker, r.FromAgent, fmt.Sprintf("[FarshMac reply to #%s from %s]\n%s%s%s\n\n[To continue this thread use agent_reply with request_id %s. Do not reply only to thank or acknowledge.]", id, sender, text, attachmentNotes(id, m.Attachments, false), changes, id))
	delivered := m.Delivered
	s.store(sp, &m)
	if err := s.save(); err != nil {
		return "", err
	}
	state := "delivered to " + recipient
	if !delivered {
		state = "queued: " + recipient + " could not be started and receives it when the user starts it"
	}
	return fmt.Sprintf("Reply to #%s %s%s.%s Do not repeat it in your output: end your turn.", id, state, attached(m.Attachments), s.unreachable(sp, asker, text)), nil
}

// attached confirms to the sender what went with its message.
func attached(list []attachment) string {
	if len(list) == 0 {
		return ""
	}
	size := 0
	for _, a := range list {
		size += a.Bytes
	}
	return fmt.Sprintf(", with %d attachment(s), %s", len(list), short(int64(size)))
}

var refPattern = regexp.MustCompile(`@([0-9a-f]{6})\b`)

// unreachable informs the sender when its text names a reference the recipient
// cannot open. Prefer bounded delivery over broader source grants.
// Must be called with s.mu held.
func (s *Store) unreachable(sp *Space, recipient *Env, text string) string {
	var missing []string
	seen := map[string]bool{}
	for _, m := range refPattern.FindAllStringSubmatch(text, -1) {
		source, known := sp.Refs[m[1]]
		if !known || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		attached := false
		for _, b := range recipient.Bindings {
			attached = attached || source == b.Source || strings.HasPrefix(source, b.Source+string(filepath.Separator))
		}
		if !attached {
			missing = append(missing, "@"+m[1])
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("\nSource references %s are informational for %s (not attached). Share artifact IDs or message attachments for review; do not copy source trees or request broader access merely to deliver evidence.", strings.Join(missing, ", "), recipient.Name)
}

// Notices returns and clears what the user should be told.
func (s *Store) Notices() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	notices := s.notices
	s.notices = nil
	return notices
}
