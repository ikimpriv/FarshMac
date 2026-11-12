# FarshMac terminal: version 0.13 audit

This historical audit describes the embedded macOS PTY terminal. A persistent bash
(default), sh, or login zsh receives `TERM=xterm-256color`, `COLORTERM=truecolor`
and `TERM_PROGRAM=FarshMac`; Terminal.app session variables (`TERM_PROGRAM_VERSION`,
`TERM_SESSION_ID`) are not inherited.
External commands share the PTY; ⌘O switches between it and the panels without
stopping processes. Later changes are recorded in [CHANGELOG.md](CHANGELOG.md).
In particular, ⌘⇧H now opens **command history**; the output archive described below
is available separately through the Window menu or the mouse wheel, without a
keyboard shortcut.

## Submitting commands from panels

The directory and original command are two NUL-delimited fields in a private
0600 file inside a 0700 session directory. In zsh, a private key sequence invokes
a ZLE widget in main/emacs/viins/vicmd. It reads the data without eval, changes
directory separately, replaces BUFFER with the user's command, and calls normal
accept-line. History receives the user's text. The directory is never interpolated
as shell code; a failed cd prevents execution.

Bash/sh use a readline macro and `bind -x`, compatible with system Bash 3.2. The
private file contains the directory and command; cd receives the path as an
argument, while shell eval executes the command in the current session. History
receives the user's text; the helper function does not appear on the input line.
Bash loads /etc/profile and the first available ~/.bash_profile, ~/.bash_login,
or ~/.profile, falling back to ~/.bashrc. Sh loads the user's ENV and restores
ENV for child shells. User startup files are not modified. The shell preference
is applied after copying window settings and before starting the PTY. Existing
shells keep running. Save setup persists preferences.

macOS/BSD ls receives CLICOLOR=1 by default when it is unset and NO_COLOR is not
enabled. Shell startup files take precedence; aliases are not replaced. GNU ls
requires --color=auto. TERM describes capabilities; it does not require programs
to enable color. See [Apple's ls implementation](https://github.com/apple-oss-distributions/file_cmds/blob/main/ls/ls.c).

## Shell lifecycle

- If the shell exits, it restarts in the same window with a 1/2/4/8 s backoff.
  After 5 consecutive failures during startup it stops and prints a notice; Enter
  in the terminal or ⌘O retries. When the panel folder is unusable as a working
  directory, the shell starts in `$HOME`, then `/`.
- A 5-second watchdog checks that the shell integration acknowledged a command
  submitted from the panels. Otherwise the status reads "Shell integration did not
  respond; command not sent — check shell key bindings", and nothing is typed into
  the shell.
- Closing a window (⌘W, red button) while a command runs asks **Close window**;
  ⌘Q asks about quitting the application. ⌘W/⌘Q with a form open cancel the form
  first. A busy window that refuses ⌘Q is brought to the front.
- Terminal settings apply to new windows and after the shell in this window exits.

## Emulation and input

Bundled xterm.js 5.5.0 and the official addon-unicode11 0.8.0 require no network
loading. Features include xterm.js ANSI/VT sequences, 16/256 colors, 24-bit RGB,
SGR, cursor positioning and reports, alternate screen, bracketed paste, application
cursor/keypad, mouse reports, UTF-8, and Unicode 11 character widths. Legacy binary
mouse reports use base64 to preserve bytes ≥128 through the UTF-8 bridge. Pasting
uses `Terminal.paste`, including delimiters when bracketed paste is enabled.
See the [xterm.js API](https://xtermjs.org/docs/api/terminal/classes/terminal/).

| Control | Action |
| --- | --- |
| ⌘O | Switch terminal/panels |
| ⌘C / Ctrl+Shift+C | Copy selection |
| ⌘V / Ctrl+Shift+V / Shift+Insert | Paste system clipboard |
| ⌘A | Select the available live buffer, or the current archive page |
| Ctrl+C / ⌘. | Interrupt command |
| Option+drag | Force selection while an application tracks the mouse |
| Right click | Copy / Paste / Select all menu unless an application owns the mouse |
| ⌘⇧H | Command history overlay (terminal mode) |
| ⌘+ / ⌘− / ⌘0 (also ⌘=) | Font size larger / smaller / default (10–24) |
| Window menu / wheel | Open the output archive |
| ↑↓ / PgUp/PgDn / wheel in archive | Scroll archive page |
| Home in archive | Oldest retained page |
| End / Esc / Live in archive | Return to live terminal |

Unmodified mouse events go to applications that enable mouse tracking. Option
allows selection and the context menu in that mode. In panels, ⌘C copies the
command line, or selected/current filenames when the line is empty. ⌘X clears the
copied command line; ⌘V pastes text. This is a text clipboard; F5/F6 perform file
operations.

## Bounded memory and temporary archive

- xterm retains the screen and 4096 scrollback lines. Normal-screen lines leaving
  the top of the screen are archived with text, colors, attributes, and soft-wrap
  flags. Alternate-screen frames are not archived.
- Each window owns a 0700 `farshmac-history-*` system temporary directory, with
  0600 data/index files. Offsets are indexed on disk, avoiding one RAM entry per line.
- Segments are approximately 16 MiB; the total budget is 1 GiB per window. Oldest
  segments expire when full, with an indication in the UI. This is a bounded output
  archive, not an indefinite command log.
- Pages load in the background. The DOM contains one page; requests are capped at
  256 lines and the UI asks for at most the terminal height minus two rows (the
overlay toolbar). Selection/copy stay
  within a page. Archived lines retain their original width; horizontal scrolling
  makes them readable after narrowing the window.
- PTY output uses a bounded queue and frames up to 16 KiB. xterm parses chunks of
  `min(4094, scrollback − 2)` bytes before collecting history. After submitting archive data, Go waits for
  the background write before sending another output frame. A slow disk applies
  backpressure to the PTY instead of growing an unbounded RAM queue.
- Write errors appear in the footer. Live output continues, archiving stops, and
  previously saved pages remain readable.
- Closing a window deletes its archive; application shutdown waits for cleanup.
  A crash may leave temporary files until system cleanup. Output is not restored
  between launches.

Serialization uses xterm buffer/marker APIs. Capturing an OSC-modified palette
uses the internal theme service of pinned version 5.5; recheck it when upgrading.
See [Buffer API](https://xtermjs.org/docs/api/terminal/interfaces/ibuffer/) and
[parser hooks](https://xtermjs.org/docs/guides/hooks/).

## Validation and limits

`terminal.cjs` runs the actual bundled xterm parser: 16/256/RGB colors, attributes,
DSR, bracketed paste, binary/SGR mouse, wide Unicode, 30,000 colored lines without
loss, bounded live buffer, height changes, alternate screen, and scrollback clear.
`input.cjs` and `resize.cjs` exercise input bridging, drag/resize coalescing,
clipboard, and paged history in a VM DOM. Go tests cover privacy, indexing,
rotation, errors, cleanup, window isolation, backpressure, and retained heap over
200,000 lines. A benchmark writes and reads the last page of one million lines.
A real-zsh PTY test covers `tput colors`, resize, process interruption, and vim.

These checks do not replace end-to-end Cocoa/WebKit testing with trackpads and
different keyboards. Full compatibility with every xterm extension is not claimed:
Sixel/Kitty graphics and file transfer are absent. Archived lines are static;
blinking and active OSC 8 links are not replayed. Complex ZWJ/emoji graphemes and
IME input require further validation.
