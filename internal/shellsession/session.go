// Package shellsession manages shell contexts and command lifecycles without
// depending on panels, renderers or connection backends. State methods run on
// the owner's serial queue; streams perform blocking I/O asynchronously.
package shellsession

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"farshmac/internal/terminal"
	"fmt"
	"path"
	"strings"
	"time"
)

type Context struct{ Kind, ID, Directory, ExecutionDirectory string }
type Command struct{ Text, Path string }
type Result struct {
	Output                      []byte
	Completed, Started, Changed bool
	ContextReported             bool
	Context                     Context
	Err                         error
}
type Stream interface {
	Write([]byte) error
	Resize(int, int) error
	Output() <-chan []byte
	Done() <-chan struct{}
	Err() error
	Close()
}
type Session struct {
	context          Context
	prefix           string
	tail             []byte
	ready, running   bool
	queued           *Command
	desiredDirectory string
	connectingSince  time.Time
	stream           Stream
}

func NewRemote(c Context, now time.Time) (*Session, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	return &Session{context: c, prefix: "\x1b]777;farshmac-" + hex.EncodeToString(token[:]) + ";", connectingSince: now}, nil
}
func (s *Session) Context() Context   { return s.context }
func (s *Session) Ready() bool        { return s.ready }
func (s *Session) Running() bool      { return s.running }
func (s *Session) Pending() bool      { return s.running || s.queued != nil }
func (s *Session) HasQueued() bool    { return s.queued != nil }
func (s *Session) Bind(stream Stream) { s.stream = stream }
func (s *Session) Close() {
	if s.stream != nil {
		s.stream.Close()
	}
}
func (s *Session) Queue(c Command) error {
	if s.Pending() {
		return fmt.Errorf("a panel command is already pending")
	}
	if len(c.Text)+len(c.Path) > 16<<10 {
		return fmt.Errorf("shell command exceeds 16 KiB")
	}
	if len(directoryLine(c.Path))+len(terminal.Quote(c.Text))+16 > 64<<10 {
		return fmt.Errorf("encoded shell command exceeds 64 KiB")
	}
	s.queued = &c
	return nil
}
func (s *Session) TakeQueued() *Command { c := s.queued; s.queued = nil; return c }
func (s *Session) Sent()                { s.ready = false; s.running = true }
func (s *Session) Busy()                { s.ready = false }
func (s *Session) Prompt() bool {
	done := s.running
	s.ready = true
	s.running = false
	s.connectingSince = time.Time{}
	return done
}
func (s *Session) End() Result {
	r := Result{Output: s.tail, Completed: s.Pending(), Context: s.context}
	s.tail = nil
	s.ready = false
	s.running = false
	s.queued = nil
	s.desiredDirectory = ""
	s.connectingSince = time.Time{}
	return r
}
func (s *Session) Input(data []byte) error {
	if s.stream == nil {
		return fmt.Errorf("shell is not connected")
	}
	if !s.connectingSince.IsZero() && (string(data) == "\x1b" || string(data) == "\x03") {
		s.Close()
		return nil
	}
	if bytes.ContainsAny(data, "\r\n") {
		s.ready = false
	}
	return s.stream.Write(data)
}
func (s *Session) Expire(now time.Time) bool {
	if s.connectingSince.IsZero() || now.Sub(s.connectingSince) <= 30*time.Second {
		return false
	}
	s.connectingSince = time.Time{}
	s.Close()
	return true
}
func (s *Session) RequestDirectory(dir string) {
	if dir != "" && dir != s.context.Directory {
		s.desiredDirectory = dir
	}
}
func (s *Session) Dispatch() Result {
	r := Result{}
	if !s.ready || s.stream == nil {
		return r
	}
	var line string
	if c := s.queued; c != nil {
		line = directoryLine(c.Path) + " && eval " + terminal.Quote(c.Text) + "\n"
	} else if s.desiredDirectory != "" {
		line = directoryLine(s.desiredDirectory) + "\n"
	} else {
		return r
	}
	if err := s.stream.Write([]byte(line)); err != nil {
		r.Err = err
		return r
	}
	s.ready = false
	if s.queued != nil {
		s.running = true
		s.queued = nil
		r.Started = true
	}
	s.desiredDirectory = ""
	return r
}
func directoryLine(dir string) string {
	return "\x15cd -- \"${TARGET_ROOT:-}\"" + terminal.Quote(path.Clean("/"+dir))
}

// The marker is outside displayed PS1 so readline redraws cannot complete a
// command. Child shells cannot emit their parent's protocol. NUL separates
// logical cwd from execution cwd; it cannot occur in a filesystem path.
func (s *Session) Launch(shell, label string) string {
	signal := "[ \"$$\" = \"$FARSHMAC_PROMPT_PID\" ] && { _fm_dir=$PWD; if [ -n \"${TARGET_ROOT:-}\" ]; then _fm_suffix=${PWD#\"$TARGET_ROOT\"}; if [ \"$_fm_suffix\" = \"$PWD\" ]; then _fm_dir=; elif [ -z \"$_fm_suffix\" ]; then _fm_dir=/; elif [ \"${_fm_suffix#/}\" != \"$_fm_suffix\" ]; then _fm_dir=$_fm_suffix; else _fm_dir=; fi; fi; printf \"$FARSHMAC_PROMPT_FORMAT\" \"$_fm_dir\" \"$PWD\"; } >&2"
	label = strings.NewReplacer("\\", "\\\\", "$", "\\$", string(rune(96)), "\\"+string(rune(96))).Replace(label)
	prompt := "$(eval \"$FARSHMAC_PROMPT_COMMAND\")[" + label + "] $ "
	setup := "unset PROMPT_COMMAND; "
	args := " -i"
	if path.Base(shell) == "bash" {
		args = " --noprofile --norc -i"
		prompt = "[" + label + "] $ "
		setup = "export PROMPT_COMMAND=" + terminal.Quote(signal) + "; "
	}
	return "FARSHMAC_PROMPT_COMMAND=" + terminal.Quote(signal) + "; export FARSHMAC_PROMPT_COMMAND; " + setup + "FARSHMAC_PROMPT_FORMAT=" + terminal.Quote(s.prefix+"%s\\000%s\\007") + "; export FARSHMAC_PROMPT_FORMAT; FARSHMAC_PROMPT_PID=$$; export FARSHMAC_PROMPT_PID; ENV=/dev/null; export ENV; export PS1=" + terminal.Quote(prompt) + "; exec " + terminal.Quote(shell) + args
}
func (s *Session) Feed(data []byte) Result {
	r := Result{}
	data = append(s.tail, data...)
	s.tail = nil
	prefix := []byte(s.prefix)
	if len(prefix) == 0 {
		r.Output = data
		return r
	}
	for len(data) > 0 {
		at := bytes.Index(data, prefix)
		if at < 0 {
			n := min(len(data), len(prefix)-1)
			for n > 0 && !bytes.Equal(data[len(data)-n:], prefix[:n]) {
				n--
			}
			r.Output = append(r.Output, data[:len(data)-n]...)
			s.tail = append([]byte(nil), data[len(data)-n:]...)
			break
		}
		r.Output = append(r.Output, data[:at]...)
		data = data[at:]
		end := bytes.IndexByte(data[len(prefix):], 7)
		if end < 0 {
			if len(data) > 32<<10 {
				r.Output = append(r.Output, data...)
			} else {
				s.tail = append([]byte(nil), data...)
			}
			break
		}
		payload := data[len(prefix) : len(prefix)+end]
		data = data[len(prefix)+end+1:]
		parts := bytes.Split(payload, []byte{0})
		if len(parts) != 2 || !validDirectory(string(parts[1])) {
			continue
		}
		logical, execution := string(parts[0]), string(parts[1])
		if logical != "" && !validDirectory(logical) {
			continue
		}
		r.ContextReported = true
		previous := s.context
		if logical != "" {
			logical = path.Clean(logical)
		}
		s.context.Directory = logical
		s.context.ExecutionDirectory = execution
		r.Changed = r.Changed || s.context != previous
		r.Completed = s.Prompt() || r.Completed
		sent := s.Dispatch()
		r.Started = r.Started || sent.Started
		if sent.Err != nil {
			r.Err = sent.Err
		}
	}
	r.Context = s.context
	return r
}
func validDirectory(dir string) bool {
	if !strings.HasPrefix(dir, "/") {
		return false
	}
	for _, c := range dir {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}
