package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
)

// macOS /bin/bash and /bin/sh are Bash 3.2. Use bind -x plus a private
// readline macro; READLINE_LINE/READLINE_POINT require newer Bash versions.
func bourneShellCommand(shell, dir, token, commandKey, directoryKey, historyKey string, customPrompt bool) (*exec.Cmd, error) {
	startup := `if [ -r /etc/profile ]; then . /etc/profile; fi
farshmac_profile_found=
for farshmac_profile in "$HOME/.bash_profile" "$HOME/.bash_login" "$HOME/.profile"; do
  if [ -r "$farshmac_profile" ]; then . "$farshmac_profile"; farshmac_profile_found=1; break; fi
done
if [ -z "$farshmac_profile_found" ] && [ -r "$HOME/.bashrc" ]; then . "$HOME/.bashrc"; fi
`
	if shell == "sh" {
		startup = `if [ -n "$FARSHMAC_USER_ENV" ] && [ -r "$FARSHMAC_USER_ENV" ]; then . "$FARSHMAC_USER_ENV"; fi
`
	}
	if !customPrompt {
		startup += "\nunset PROMPT_COMMAND\nPS1='\\u@\\h \\w \\$ '\n"
	}
	rc := startup + `
_farshmac_prompt() {
  local farshmac_status=${1:-$?}
  printf '\033]777;` + token + `;%s;%s\007' "$farshmac_status" "$PWD"
}
# Report readiness after user prompt hooks, not before potentially slow work.
_farshmac_user_prompt_command=${PROMPT_COMMAND:-:}
_farshmac_before_prompt() {
  local farshmac_status=${_farshmac_cd_status:-$?}
  unset _farshmac_cd_status
  builtin eval -- "$_farshmac_user_prompt_command"
  _farshmac_prompt "$farshmac_status"
}
PROMPT_COMMAND=_farshmac_before_prompt
_farshmac_panel_command() {
  local farshmac_target farshmac_line farshmac_status farshmac_mode
  if ! { IFS= builtin read -r -d '' farshmac_target && IFS= builtin read -r -d '' farshmac_line && IFS= builtin read -r -d '' farshmac_mode; } < ` + Quote(filepath.Join(dir, "panel-command")) + `; then
    printf '\nCould not read panel command\n'
    _farshmac_prompt 1; return
  fi
  /bin/rm -f -- ` + Quote(filepath.Join(dir, "panel-command")) + `
  if [ "$PWD" != "$farshmac_target" ] && ! builtin cd -- "$farshmac_target"; then
    if [ "$farshmac_mode" = cd ]; then _farshmac_cd_status=1; else _farshmac_prompt 1; fi
    return
  fi
  if [ "$farshmac_mode" = history ]; then
    (HISTTIMEFORMAT='FARSHMAC_HISTORY_ENTRY:' builtin history 500) > ` + Quote(filepath.Join(dir, "command-history")) + `
    _farshmac_prompt 0
    return
  fi
  if [ "$farshmac_mode" = cd ]; then
    _farshmac_cd_status=0
    return
  fi
  printf '\n%s\n' "$farshmac_line"
  # Bash 3.2 history -s replaces the previous entry when called from bind -x.
  # Reinsert that entry first, then append the new command.
  local farshmac_previous farshmac_number farshmac_last
  farshmac_previous=$(HISTTIMEFORMAT= builtin history 1)
  if [[ "$farshmac_previous" =~ ^[[:space:]]*([0-9]+)[\ *][[:space:]] ]]; then
    farshmac_number=${BASH_REMATCH[1]}
    farshmac_previous=${farshmac_previous:${#BASH_REMATCH[0]}}
    builtin history -s -- "$farshmac_previous"
    farshmac_last=$(HISTTIMEFORMAT= builtin history 1)
    if [[ "$farshmac_last" =~ ^[[:space:]]*([0-9]+) ]] && [ "${BASH_REMATCH[1]}" -gt "$farshmac_number" ]; then
      builtin history -d "${BASH_REMATCH[1]}"
    fi
  fi
  builtin history -s -- "$farshmac_line"
  printf '\033]777;` + token + `;busy;%s\007' "$PWD"
  builtin eval -- "$farshmac_line"
  farshmac_status=$?
  builtin history -a
  printf '\033]777;` + token + `;%s;%s\007' "$farshmac_status" "$PWD"
}
for farshmac_keymap in emacs-standard vi-insert vi-command; do
  builtin bind -m "$farshmac_keymap" '"\C-x\C-k": kill-whole-line'
  builtin bind -m "$farshmac_keymap" -x '"\C-x\C-g":_farshmac_panel_command'
  builtin bind -m "$farshmac_keymap" '"` + commandKey + `":"\C-x\C-k\C-x\C-g"'
  builtin bind -m "$farshmac_keymap" '"` + historyKey + `":"\C-x\C-g"'
  # Bash 3.2 caches the expanded PS1 during bind -x. Accept an empty line
  # to recompute it after cd, then restore the editable line from the kill ring.
  builtin bind -m "$farshmac_keymap" '"\C-x\C-e": end-of-line'
  builtin bind -m "$farshmac_keymap" '"\C-x\C-i": quoted-insert'
  builtin bind -m "$farshmac_keymap" '"\C-x\C-b": backward-delete-char'
  builtin bind -m "$farshmac_keymap" '"\C-x\C-j": accept-line'
  builtin bind -m "$farshmac_keymap" '"\C-x\C-y": yank'
  builtin bind -m "$farshmac_keymap" '"` + directoryKey + `":"\C-x\C-g\C-x\C-e\C-x\C-i \C-x\C-k\C-x\C-j\C-x\C-y\C-x\C-b"'
done
unset farshmac_keymap farshmac_profile farshmac_profile_found
`
	if shell == "sh" {
		rc += "if [ -n \"$FARSHMAC_USER_ENV\" ]; then export ENV=\"$FARSHMAC_USER_ENV\"; else unset ENV; fi\n"
	}
	path := filepath.Join(dir, "shellrc")
	if err := os.WriteFile(path, []byte(rc), 0600); err != nil {
		return nil, err
	}
	var cmd *exec.Cmd
	if shell == "sh" {
		cmd = exec.Command("/bin/sh", "-i")
		cmd.Env = append(os.Environ(), "FARSHMAC_USER_ENV="+os.Getenv("ENV"), "ENV="+path)
	} else {
		cmd = exec.Command("/bin/bash", "--noprofile", "--rcfile", path, "-i")
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, "TERM=xterm-256color", "COLORTERM=truecolor")
	return cmd, nil
}
