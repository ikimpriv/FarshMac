package aispaces

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func versionOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// lineDiff returns unified hunks that turn a into b, with the numbers of
// added and removed lines. A difference too large to align is reported as
// one replaced block.
func lineDiff(a, b []string, context int) (string, int, int) {
	head := 0
	for head < len(a) && head < len(b) && a[head] == b[head] {
		head++
	}
	tail := 0
	for tail < len(a)-head && tail < len(b)-head && a[len(a)-1-tail] == b[len(b)-1-tail] {
		tail++
	}
	ma, mb := a[head:len(a)-tail], b[head:len(b)-tail]
	type op struct {
		kind byte // ' ', '-', '+'
		text string
	}
	var ops []op
	for _, line := range a[:head] {
		ops = append(ops, op{' ', line})
	}
	if len(ma)*len(mb) <= 4_000_000 {
		// Longest common subsequence of the differing middle.
		width := len(mb) + 1
		table := make([]int32, (len(ma)+1)*width)
		for i := len(ma) - 1; i >= 0; i-- {
			for j := len(mb) - 1; j >= 0; j-- {
				if ma[i] == mb[j] {
					table[i*width+j] = table[(i+1)*width+j+1] + 1
				} else {
					table[i*width+j] = max(table[(i+1)*width+j], table[i*width+j+1])
				}
			}
		}
		i, j := 0, 0
		for i < len(ma) && j < len(mb) {
			switch {
			case ma[i] == mb[j]:
				ops = append(ops, op{' ', ma[i]})
				i, j = i+1, j+1
			case table[(i+1)*width+j] >= table[i*width+j+1]:
				ops = append(ops, op{'-', ma[i]})
				i++
			default:
				ops = append(ops, op{'+', mb[j]})
				j++
			}
		}
		for ; i < len(ma); i++ {
			ops = append(ops, op{'-', ma[i]})
		}
		for ; j < len(mb); j++ {
			ops = append(ops, op{'+', mb[j]})
		}
	} else {
		for _, line := range ma {
			ops = append(ops, op{'-', line})
		}
		for _, line := range mb {
			ops = append(ops, op{'+', line})
		}
	}
	for _, line := range a[len(a)-tail:] {
		ops = append(ops, op{' ', line})
	}
	added, removed := 0, 0
	var out strings.Builder
	for i := 0; i < len(ops); {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		// A hunk runs until more than 2*context unchanged lines follow.
		start := max(0, i-context)
		end, quiet := i, 0
		for j := i; j < len(ops) && quiet <= 2*context; j++ {
			if ops[j].kind == ' ' {
				quiet++
			} else {
				quiet, end = 0, j
			}
		}
		end = min(len(ops), end+1+context)
		lineA, lineB := 1, 1
		for _, o := range ops[:start] {
			if o.kind != '+' {
				lineA++
			}
			if o.kind != '-' {
				lineB++
			}
		}
		countA, countB := 0, 0
		var body strings.Builder
		for _, o := range ops[start:end] {
			switch o.kind {
			case ' ':
				countA, countB = countA+1, countB+1
			case '-':
				countA, removed = countA+1, removed+1
			case '+':
				countB, added = countB+1, added+1
			}
			body.WriteByte(o.kind)
			body.WriteString(o.text)
			body.WriteByte('\n')
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n%s", lineA, countA, lineB, countB, body.String())
		i = end
	}
	return out.String(), added, removed
}

// What one agent conversation has already been given. The server uses it to
// answer a repeated read with "unchanged" or with only the changes, and to
// tell other agents which files this one touched.

type seenFile struct {
	version  string
	whole    bool
	numbered bool
	content  string          // kept for a whole file, to show later changes as a diff
	parts    map[string]bool // ranges of this version already delivered
}

type fileChange struct {
	added, removed int
	created        bool
}

const (
	keepFile  = 256 << 10 // largest file whose content is kept for diffs
	keepTotal = 6 << 20   // per conversation
)

type conversation struct {
	mu      sync.Mutex
	seen    map[string]*seenFile
	held    int
	changed map[string]*fileChange // real path → what this agent did since its last message
	order   []string
}

// known answers a read the conversation does not need in full: the file is
// unchanged, or it changed little since the agent read all of it.
func (c *conversation) known(p, version string, whole, numbered bool, part string, lines []string, wanted int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.seen[p]
	if st == nil {
		return ""
	}
	if st.version == version {
		if (st.whole && (st.numbered || !numbered)) || st.parts[part] {
			what := "lines " + strings.TrimSuffix(part, "n")
			if st.whole {
				what = fmt.Sprintf("the whole file, %d lines", len(lines))
			}
			return fmt.Sprintf("[unchanged] %s is the same as when you read it in this conversation (%s). Nothing is repeated here: use what you already have. If it is no longer in your context, call vfs_read again with full=true.", p, what)
		}
		return ""
	}
	if !st.whole || st.content == "" {
		return ""
	}
	old := splitLines(st.content)
	diff, added, removed := lineDiff(old, lines, 2)
	if len(diff) == 0 || len(diff) > wanted*6/10 {
		return ""
	}
	c.keep(p, st, version, strings.Join(lines, "\n")+"\n")
	return fmt.Sprintf("[changed] %s was modified since you read it in this conversation (%d → %d lines, +%d −%d). Only the changes are shown; everything else is as you read it. full=true returns the whole file.\n%s", p, len(old), len(lines), added, removed, diff)
}

// keep must be called with c.mu held.
func (c *conversation) keep(p string, st *seenFile, version, content string) {
	c.held -= len(st.content)
	st.version, st.whole, st.parts, st.content = version, true, nil, ""
	if len(content) <= keepFile && c.held+len(content) <= keepTotal {
		st.content = content
		c.held += len(content)
	}
}

// remember records what a read returned.
func (c *conversation) remember(p, version string, whole, numbered bool, part, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen == nil {
		c.seen = map[string]*seenFile{}
	}
	st := c.seen[p]
	if st == nil {
		st = &seenFile{}
		c.seen[p] = st
	}
	if whole {
		c.keep(p, st, version, content)
		st.numbered = numbered
		return
	}
	if st.version != version {
		c.held -= len(st.content)
		*st = seenFile{version: version}
	}
	if st.parts == nil {
		st.parts = map[string]bool{}
	}
	st.parts[part] = true
}

// wrote records a file the agent replaced or edited itself. It knows the new
// text when it typed all of it, or knew all of the old text.
func (c *conversation) wrote(p string, old []byte, existed bool, content []byte, typedAll bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen == nil {
		c.seen = map[string]*seenFile{}
	}
	st := c.seen[p]
	if typedAll || (st != nil && st.whole && st.version == versionOf(old)) {
		if st == nil {
			st = &seenFile{}
			c.seen[p] = st
		}
		c.keep(p, st, versionOf(content), string(content))
	} else if st != nil {
		c.held -= len(st.content)
		delete(c.seen, p)
	}
}

// forget drops what was read below a path; "" drops everything, which is
// what a compacted or cleared conversation needs.
func (c *conversation) forget(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for p, st := range c.seen {
		if prefix == "" || under(p, prefix) {
			c.held -= len(st.content)
			delete(c.seen, p)
		}
	}
}

// touched notes a change to a real file, for the next message of the agent.
func (c *conversation) touched(source string, old []byte, existed bool, content []byte) {
	_, added, removed := lineDiff(splitLines(string(old)), splitLines(string(content)), 0)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.changed == nil {
		c.changed = map[string]*fileChange{}
	}
	ch := c.changed[source]
	if ch == nil {
		ch = &fileChange{created: !existed}
		c.changed[source] = ch
		c.order = append(c.order, source)
	}
	ch.added, ch.removed = ch.added+added, ch.removed+removed
}

// takeChanges returns and clears the files touched since the last message.
func (c *conversation) takeChanges() ([]string, map[string]*fileChange) {
	c.mu.Lock()
	defer c.mu.Unlock()
	order, changed := c.order, c.changed
	c.order, c.changed = nil, nil
	return order, changed
}
