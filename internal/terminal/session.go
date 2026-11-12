// Package terminal owns a persistent interactive shell attached to a real PTY.
package terminal

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

type Event struct {
	Commands []string
	Data     []byte
	Ready    bool
	Running  bool
	Cwd      string
	Status   int
	Exit     bool
	Err      error
}
type Session struct {
	Shell        string
	Events       chan Event
	file         *os.File
	cmd          *exec.Cmd
	input        chan []byte
	done         chan struct{}
	exited       chan struct{}
	once         sync.Once
	dir          string
	commandKey   string
	directoryKey string
	historyKey   string
}

func ValidShell(shell string) bool { return shell == "bash" || shell == "sh" || shell == "zsh" }

func Start(cwd string, cols, rows int, selected ...string) (*Session, error) {
	shell := "bash"
	if len(selected) > 0 && selected[0] != "" {
		shell = selected[0]
	}
	return StartConfigured(cwd, cols, rows, shell, true)
}

// StartConfigured retains startup files but optionally uses a lightweight prompt.
func StartConfigured(cwd string, cols, rows int, shell string, customPrompt bool) (*Session, error) {
	if !ValidShell(shell) {
		return nil, fmt.Errorf("unsupported shell %q", shell)
	}
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(tokenBytes)
	commandKey := "\x1b[777;" + token + "~"
	directoryKey := "\x1b[778;" + token + "~"
	historyKey := "\x1b[779;" + token + "~"
	dir, err := os.MkdirTemp("", "farshmac-shell-*")
	if err != nil {
		return nil, err
	}
	// A private startup wrapper loads the user's normal zsh configuration first,
	// then reports prompt readiness and cwd over a private OSC sequence.
	rc := `if [[ -r "$FARSHMAC_USER_ZDOTDIR/.zshrc" ]]; then
  source "$FARSHMAC_USER_ZDOTDIR/.zshrc"
fi
function _farshmac_precmd() {
  local farshmac_status=${1:-$?}
  _farshmac_bindkeys
  printf '\033]777;` + token + `;%s;%s\007' "$farshmac_status" "$PWD"
}
precmd_functions+=(_farshmac_precmd)
function _farshmac_preexec() {
  printf '\033]777;` + token + `;busy;%s\007' "$PWD"
}
preexec_functions+=(_farshmac_preexec)
# The panel sends data through a private file, not shell source. The widget
# changes cwd before accepting the user's original line into normal history.
function _farshmac_panel_command() {
  emulate -L zsh
  local farshmac_target farshmac_line farshmac_mode
  if ! {
    IFS= builtin read -r -d '' farshmac_target &&
    IFS= builtin read -r -d '' farshmac_line &&
    IFS= builtin read -r -d '' farshmac_mode
  } < ` + Quote(filepath.Join(dir, "panel-command")) + `; then
    zle -M 'Could not read panel command'
    _farshmac_precmd 1
    return 1
  fi
  /bin/rm -f -- ` + Quote(filepath.Join(dir, "panel-command")) + `
  if [[ "$PWD" != "$farshmac_target" ]] && ! builtin cd -- "$farshmac_target"; then
    zle -M "Cannot enter directory: $farshmac_target"
    _farshmac_precmd 1
    return 1
  fi
  if [[ "$farshmac_mode" == history ]]; then
    zmodload zsh/parameter
    local farshmac_id farshmac_count=0
    {
      for farshmac_id in ${(Onk)history}; do
        builtin printf '%s\0' "$history[$farshmac_id]"
        (( ++farshmac_count >= 500 )) && break
      done
    } > ` + Quote(filepath.Join(dir, "command-history")) + `
    _farshmac_precmd 0
    return
  fi
  if [[ "$farshmac_mode" == cd ]]; then
    _farshmac_precmd
    zle reset-prompt
    return
  fi
  BUFFER=$farshmac_line
  CURSOR=${#BUFFER}
  zle reset-prompt
  zle .accept-line
}
zle -N _farshmac_panel_command
# Plugins may recreate keymaps after startup; precmd rebinds idempotently.
function _farshmac_bindkeys() {
  local farshmac_keymap
  for farshmac_keymap in main emacs viins vicmd; do
    builtin bindkey -M "$farshmac_keymap" ` + Quote(commandKey) + ` _farshmac_panel_command
    builtin bindkey -M "$farshmac_keymap" ` + Quote(historyKey) + ` _farshmac_panel_command
    builtin bindkey -M "$farshmac_keymap" ` + Quote(directoryKey) + ` _farshmac_panel_command
  done
}
_farshmac_bindkeys
unsetopt PROMPT_SP
export ZDOTDIR="$FARSHMAC_USER_ZDOTDIR"
`
	if !customPrompt {
		rc = strings.Replace(rc, "function _farshmac_precmd()", "precmd_functions=()\npreexec_functions=()\nunfunction precmd preexec 2>/dev/null\nunsetopt PROMPT_SUBST\nPROMPT='%n@%m %~ %# '\nRPROMPT=''\nfunction _farshmac_precmd()", 1)
	}
	if err = os.WriteFile(filepath.Join(dir, ".zshrc"), []byte(rc), 0600); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	for _, name := range []string{".zshenv", ".zprofile"} {
		wrapper := "if [[ -r \"$FARSHMAC_USER_ZDOTDIR/" + name + "\" ]]; then source \"$FARSHMAC_USER_ZDOTDIR/" + name + "\"; fi\nexport ZDOTDIR=\"$FARSHMAC_INTEGRATION_DIR\"\n"
		if err = os.WriteFile(filepath.Join(dir, name), []byte(wrapper), 0600); err != nil {
			os.RemoveAll(dir)
			return nil, err
		}
	}
	cmd := exec.Command("/bin/zsh", "-il")
	cmd.Dir = cwd
	userDot := os.Getenv("ZDOTDIR")
	if userDot == "" {
		userDot, _ = os.UserHomeDir()
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor", "ZDOTDIR="+dir, "FARSHMAC_INTEGRATION_DIR="+dir, "FARSHMAC_USER_ZDOTDIR="+userDot)
	if shell != "zsh" {
		cmd, err = bourneShellCommand(shell, dir, token, commandKey, directoryKey, historyKey, customPrompt)
		if err != nil {
			os.RemoveAll(dir)
			return nil, err
		}
		cmd.Dir = cwd
	}
	cmd.Env = append(hostTerminalFree(cmd.Env), "TERM_PROGRAM=FarshMac", "SHELL=/bin/"+shell, "BASH_SILENCE_DEPRECATION_WARNING=1")
	// BSD/macOS ls needs an explicit automatic-color preference in addition to
	// TERM. Set a session default; startup files may override or unset it.
	if _, set := os.LookupEnv("CLICOLOR"); !set && os.Getenv("NO_COLOR") == "" {
		cmd.Env = append(cmd.Env, "CLICOLOR=1")
	}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	s := &Session{Shell: shell, Events: make(chan Event, 64), file: f, cmd: cmd, input: make(chan []byte, 32), done: make(chan struct{}), exited: make(chan struct{}), dir: dir, commandKey: commandKey, directoryKey: directoryKey, historyKey: historyKey}
	go func() {
		for {
			select {
			case b := <-s.input:
				if _, err := f.Write(b); err != nil {
					return
				}
			case <-s.done:
				return
			}
		}
	}()
	go s.read(token)
	return s, nil
}

func (s *Session) send(e Event) bool {
	select {
	case s.Events <- e:
		return true
	case <-s.done:
		return false
	}
}
func (s *Session) read(token string) {
	defer func() {
		_ = s.cmd.Wait()
		close(s.exited)
	}()
	p := parser{prefix: []byte("\x1b]777;" + token + ";")}
	buf := make([]byte, 32768)
	for {
		n, err := s.file.Read(buf)
		if n > 0 {
			for _, event := range p.feed(buf[:n]) {
				if event.Ready {
					event.Commands = s.readCommands()
				}
				if !s.send(event) {
					return
				}
			}
		}
		if err != nil {
			if len(p.pending) > 0 {
				s.send(Event{Data: p.pending})
			}
			if err == io.EOF || err == syscall.EIO {
				err = nil
			}
			s.send(Event{Exit: true, Err: err})
			return
		}
	}
}
func (s *Session) Write(data string) error {
	if len(data) > 1024*1024 {
		return fmt.Errorf("paste exceeds 1 MiB")
	}
	select {
	case s.input <- []byte(data):
		return nil
	case <-s.done:
		return fmt.Errorf("shell closed")
	default:
		return fmt.Errorf("terminal input queue is full")
	}
}

// Run submits a panel command to the shell integration without exposing cwd plumbing in the
// terminal or shell history. The caller must wait for the shell's Ready event.
func (s *Session) Run(cwd, command string) error {
	return s.request(cwd, command, "run", s.commandKey)
}

// ChangeDir preserves the editable command line and does not add a history entry.
// Like Run, it must only be called at a ready prompt.
func (s *Session) ChangeDir(cwd string) error {
	return s.request(cwd, "", "cd", s.directoryKey)
}

func requestError(cwd, command string) error {
	switch {
	case len(command) > 1024*1024:
		return fmt.Errorf("command exceeds 1 MiB")
	case len(cwd) > 1024*1024:
		return fmt.Errorf("directory path exceeds 1 MiB")
	case len(cwd)+len(command) > 1024*1024:
		return fmt.Errorf("command and directory exceed 1 MiB")
	case !filepath.IsAbs(cwd):
		return fmt.Errorf("directory must be absolute")
	case strings.ContainsRune(cwd, 0):
		return fmt.Errorf("directory contains NUL")
	case strings.ContainsRune(command, 0):
		return fmt.Errorf("command contains NUL")
	}
	return nil
}

// CancelRequest discards a panel request the shell never consumed, e.g. when
// a plugin removed the integration key binding.
// It reports whether an unconsumed request was removed.
func (s *Session) CancelRequest() bool {
	return os.Remove(filepath.Join(s.dir, "panel-command")) == nil
}

func (s *Session) request(cwd, command, mode, key string) error {
	if err := requestError(cwd, command); err != nil {
		return err
	}
	request := filepath.Join(s.dir, "panel-command")
	f, err := os.OpenFile(request, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		// The shell consumes the file only at a prompt, so a leftover file
		// always belongs to a lost integration key. Replace it once.
		_ = os.Remove(request)
		f, err = os.OpenFile(request, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	}
	if err != nil {
		return err
	}
	_, err = f.WriteString(cwd + "\x00" + command + "\x00" + mode + "\x00")
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = s.Write(key)
	}
	if err != nil {
		_ = os.Remove(request)
	}
	return err
}

func (s *Session) Resize(cols, rows int) error {
	return pty.Setsize(s.file, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// shellKillGrace is how long Close waits after SIGHUP before SIGKILL.
const shellKillGrace = 2 * time.Second

func (s *Session) Close() {
	s.once.Do(func() {
		close(s.done)
		pid := s.cmd.Process.Pid
		_ = syscall.Kill(-pid, syscall.SIGHUP)
		_ = s.file.Close()
		_ = os.RemoveAll(s.dir)
		// An interactive bash can survive SIGHUP and the closed PTY (seen with
		// a pending vi-mode line); never leave it behind as an orphan.
		go func() {
			select {
			case <-s.exited:
			case <-time.After(shellKillGrace):
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		}()
	})
}
func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

// parser removes only our private prompt marker. All other escape sequences,
// including alternate-screen, mouse, colors, and cursor queries reach xterm.
type parser struct{ prefix, pending []byte }

func (p *parser) feed(data []byte) []Event {
	p.pending = append(p.pending, data...)
	var out []Event
	for len(p.pending) > 0 {
		i := bytes.Index(p.pending, p.prefix)
		if i < 0 {
			keep := 0
			for n := 1; n < len(p.prefix) && n <= len(p.pending); n++ {
				if bytes.Equal(p.pending[len(p.pending)-n:], p.prefix[:n]) {
					keep = n
				}
			}
			if count := len(p.pending) - keep; count > 0 {
				out = append(out, Event{Data: bytes.Clone(p.pending[:count])})
				p.pending = bytes.Clone(p.pending[count:])
			}
			break
		}
		if i > 0 {
			out = append(out, Event{Data: bytes.Clone(p.pending[:i])})
			p.pending = p.pending[i:]
		}
		end := bytes.IndexByte(p.pending, 7)
		if end < 0 {
			if len(p.pending) > 65536 {
				out = append(out, Event{Data: bytes.Clone(p.pending)})
				p.pending = nil
			}
			break
		}
		parts := strings.SplitN(string(p.pending[len(p.prefix):end]), ";", 2)
		if len(parts) == 2 {
			status, _ := strconv.Atoi(parts[0])
			out = append(out, Event{Ready: parts[0] != "busy", Running: parts[0] == "busy", Status: status, Cwd: parts[1]})
		}
		p.pending = bytes.Clone(p.pending[end+1:])
	}
	return out
}

// hostTerminalFree drops variables inherited from the terminal FarshMac was
// launched from (e.g. `go run` in Terminal.app). Left in place, macOS
// /etc/bashrc_Apple_Terminal and /etc/zshrc_Apple_Terminal treat the embedded
// shell as a Terminal.app session: per-session history replaces HISTFILE and
// session files are written under ~/.bash_sessions.
func hostTerminalFree(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if name == "TERM_PROGRAM" || name == "TERM_PROGRAM_VERSION" || name == "TERM_SESSION_ID" {
			continue
		}
		out = append(out, kv)
	}
	return out
}
