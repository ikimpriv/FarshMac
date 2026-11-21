package aispaces

import (
	"encoding/json"
	"fmt"
	"os"
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
	e.Peers = nil
	for _, name := range names {
		if other := sp.env(name); other != nil && other != e {
			e.Peers = append(e.Peers, other.ID)
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
			state := "not running: it is started automatically when you message it"
			if s.running[other.ID+":"+a.Name] != nil {
				state = "running"
			}
			lines = append(lines, fmt.Sprintf("  %s  (%s)", address(other, a.Name), state))
		}
	}
	if len(lines) == 0 {
		b.WriteString("You may not contact any other agent. The user decides who talks to whom.\n")
		return b.String()
	}
	sort.Strings(lines)
	b.WriteString("You may contact:\n" + strings.Join(lines, "\n") + "\n")
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
		if a := target.agent(strings.TrimSpace(parts[1])); a != nil {
			return target, a.Name, nil
		}
		return nil, "", fmt.Errorf("not_found: Env %s has no agent %q", target.Name, parts[1])
	}
	if len(target.Agents) != 1 {
		var names []string
		for _, a := range target.Agents {
			names = append(names, address(target, a.Name))
		}
		return nil, "", fmt.Errorf("ambiguous: address one agent of %s: %s", target.Name, strings.Join(names, ", "))
	}
	return target, target.Agents[0].Name, nil
}

// deliver types text into the agent's console. An agent that is not running
// is started for it, without a window: its console can be opened later and
// shows everything it did. If it cannot be started the text waits for its
// next start. It must be called with s.mu held.
func (s *Store) deliver(e *Env, agent, text string) bool {
	if p := s.running[e.ID+":"+agent]; p != nil {
		p.expect(1)
		go p.Deliver(text)
		return true
	}
	a := e.agent(agent)
	if a == nil {
		return false
	}
	a.Inbox = append(a.Inbox, text)
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

// record must be called with s.mu held.
func (s *Store) record(sp *Space, kind, id, from, to, text string, delivered bool) {
	dir := filepath.Join(s.root, "spaces", sp.ID)
	if os.MkdirAll(dir, 0700) != nil {
		return
	}
	line, _ := json.Marshal(map[string]any{"time": time.Now().Format("2006-01-02T15:04:05Z07:00"), "kind": kind, "id": id, "from": from, "to": to, "delivered": delivered, "text": text})
	if f, err := os.OpenFile(filepath.Join(dir, "messages.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
		f.Write(append(line, '\n'))
		f.Close()
	}
}

// send passes a message from one agent to another and remembers who asked,
// so the answer finds its way back.
func (s *Store) send(from *Env, fromAgent, to, text string) (string, error) {
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
	sp.NextMessage++
	id := strconv.Itoa(sp.NextMessage)
	if sp.Requests == nil {
		sp.Requests = map[string]request{}
	}
	sp.Requests[id] = request{from.ID, fromAgent, target.ID, agent}
	sender, recipient := address(from, fromAgent), address(target, agent)
	delivered := s.deliver(target, agent, fmt.Sprintf("[FarshMac message #%s from %s]\n%s\n\n[When you have the answer, send it with the agent_reply tool, request_id %s. It is delivered to %s automatically.]", id, sender, text, id, sender))
	s.record(sp, "message", id, sender, recipient, text, delivered)
	if err = s.save(); err != nil {
		return "", err
	}
	state := "delivered"
	if !delivered {
		state = "queued: that agent could not be started, it receives the message when the user starts it"
	}
	return fmt.Sprintf("Message #%s to %s: %s.%s\nThe reply will arrive in this conversation automatically as a new message. Do not wait, poll or check for it: finish your turn now.", id, recipient, state, s.unreachable(sp, target, text)), nil
}

// reply returns the answer to whoever sent the message.
func (s *Store) reply(from *Env, fromAgent, id, text string) (string, error) {
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
	if r.FromEnv == operatorEnv {
		// The user's own assistant has no console: it collects its replies.
		sender := address(from, fromAgent)
		sp.Operator = append(sp.Operator, operatorNote{id, sender, time.Now().Format("15:04:05"), text})
		s.record(sp, "reply", id, sender, "user/"+r.FromAgent, text, true)
		if err := s.save(); err != nil {
			return "", err
		}
		return fmt.Sprintf("Reply to #%s delivered to the user's assistant.", id), nil
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
	delivered := s.deliver(asker, r.FromAgent, fmt.Sprintf("[FarshMac reply to #%s from %s]\n%s\n\n[To continue this thread use agent_reply with request_id %s. Do not reply only to thank or acknowledge.]", id, sender, text, id))
	s.record(sp, "reply", id, sender, recipient, text, delivered)
	if err := s.save(); err != nil {
		return "", err
	}
	state := "delivered to " + recipient
	if !delivered {
		state = "queued: " + recipient + " could not be started and receives it when the user starts it"
	}
	return fmt.Sprintf("Reply to #%s %s.%s", id, state, s.unreachable(sp, asker, text)), nil
}

var refPattern = regexp.MustCompile(`@([0-9a-f]{6})\b`)

// unreachable warns the sender when its text names a reference the recipient
// cannot open, and tells the user which folder to attach. It must be called
// with s.mu held.
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
			s.notices = append(s.notices, fmt.Sprintf("AI Env %s cannot open %s (%s): attach that folder to it if its agent should read it", recipient.Name, "@"+m[1], filepath.Base(source)))
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("\nWarning: %s cannot open %s: that folder is not attached to its Env. The user has been told. Do not expect it to read those files until the user attaches the folder.", recipient.Name, strings.Join(missing, ", "))
}

// Notices returns and clears what the user should be told.
func (s *Store) Notices() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	notices := s.notices
	s.notices = nil
	return notices
}
