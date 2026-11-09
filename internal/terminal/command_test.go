package terminal

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPanelCommandHasCleanOutputAndShellHistory(t *testing.T) {
	for _, mode := range []string{"emacs", "viins", "vicmd"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("ZDOTDIR", root)
			history := filepath.Join(root, "history")
			rc := "HISTFILE=" + Quote(history) + "\nHISTSIZE=100\nSAVEHIST=100\nsetopt INC_APPEND_HISTORY INTERACTIVE_COMMENTS\nPROMPT='PROMPT %~> '\n"
			if mode != "emacs" {
				rc += "bindkey -v\n"
			}
			rc += "alias panel_alias='print -r -- ALIAS_OK'\n"
			if err := os.WriteFile(filepath.Join(root, ".zshrc"), []byte(rc), 0600); err != nil {
				t.Fatal(err)
			}
			sub := filepath.Join(root, "folder ' каталог $(not-a-command)")
			if err := os.Mkdir(sub, 0700); err != nil {
				t.Fatal(err)
			}
			s, err := Start(root, 160, 24, "zsh")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			waitReady(t, s)
			// The panel must replace the whole unfinished line, even with the
			// cursor in the middle or vi command mode (Ctrl+U does not do that).
			pending := "UNFINISHED_DONT_RUN\x1b[D\x1b[D"
			if mode == "vicmd" {
				pending += "\x1b"
			}
			if err = s.Write(pending); err != nil {
				t.Fatal(err)
			}
			command := "export FARSHMAC_PANEL_VALUE=kept; panel_alias # original command"
			if err = s.Run(sub, command); err != nil {
				t.Fatal(err)
			}
			out, e := waitReady(t, s)
			actual, _ := filepath.EvalSymlinks(e.Cwd)
			want, _ := filepath.EvalSymlinks(sub)
			if actual != want || e.Status != 0 || !strings.Contains(out, "\r\nALIAS_OK\r\n") {
				t.Fatalf("panel command: cwd=%q status=%d output=%q", e.Cwd, e.Status, out)
			}
			for _, plumbing := range []string{"builtin cd", "cmdand cursh>", "_farshmac_panel_command", "panel-command", s.commandKey} {
				if strings.Contains(out, plumbing) {
					t.Fatalf("internal command leaked to terminal: %q", out)
				}
			}
			data, err := os.ReadFile(history)
			if err != nil || string(data) != command+"\n" {
				t.Fatalf("shell history should contain only the user's line: %q, %v", data, err)
			}
			// Commands execute in the persistent shell, not a subshell or eval
			// wrapper, and embedded newlines/backslashes survive the transport.
			if err = s.Run(sub, "print -r -- VALUE:$FARSHMAC_PANEL_VALUE\nprint -r -- 'line \\ literal'"); err != nil {
				t.Fatal(err)
			}
			out, _ = waitReady(t, s)
			if !strings.Contains(out, "\r\nVALUE:kept\r\n") || !strings.Contains(out, "\r\nline \\ literal\r\n") {
				t.Fatalf("persistent state/multiline: %q", out)
			}
			// A removed directory must not run the command in the old cwd or
			// leave the UI permanently waiting for the shell.
			if err = s.Run(filepath.Join(root, "missing"), "print SHOULD_NOT_RUN"); err != nil {
				t.Fatal(err)
			}
			out, _ = waitReady(t, s)
			if strings.Contains(out, "SHOULD_NOT_RUN") {
				t.Fatalf("command executed after failed cd: %q", out)
			}
			if err = s.Run(root, "print RECOVERED"); err != nil {
				t.Fatal(err)
			}
			out, _ = waitReady(t, s)
			if !strings.Contains(out, "\r\nRECOVERED\r\n") {
				t.Fatalf("failed cd left session stuck: %q", out)
			}
		})
	}
}

func TestMacLSAutomaticColorAndRedirection(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ZDOTDIR", root)
	t.Setenv("NO_COLOR", "")
	// Exercise the default even if the test runner itself has color settings.
	old, present := os.LookupEnv("CLICOLOR")
	os.Unsetenv("CLICOLOR")
	t.Cleanup(func() {
		if present {
			os.Setenv("CLICOLOR", old)
		} else {
			os.Unsetenv("CLICOLOR")
		}
	})
	t.Setenv("CLICOLOR_FORCE", "")
	// The mere presence of CLICOLOR_FORCE forces color in BSD ls.
	os.Unsetenv("CLICOLOR_FORCE")
	if err := os.Mkdir(filepath.Join(root, "color-directory"), 0700); err != nil {
		t.Fatal(err)
	}
	s, err := Start(root, 100, 24, "zsh")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	waitReady(t, s)
	if err = s.Run(root, "printf '\\nLIST_START\\n'; /bin/ls -1; printf '\\nLIST_END\\n'"); err != nil {
		t.Fatal(err)
	}
	out, _ := waitReady(t, s)
	start := strings.Index(out, "\r\nLIST_START\r\n")
	end := strings.Index(out, "\r\nLIST_END\r\n")
	if start < 0 || end <= start || !regexp.MustCompile("\x1b\\[[0-9;]*m").MatchString(out[start:end]) {
		t.Fatalf("system ls did not automatically color terminal output: %q", out)
	}
	file := filepath.Join(root, "listing.txt")
	if err = s.Run(root, "/bin/ls -1 > "+Quote(file)); err != nil {
		t.Fatal(err)
	}
	waitReady(t, s)
	data, err := os.ReadFile(file)
	if err != nil || strings.ContainsRune(string(data), '\x1b') || !strings.Contains(string(data), "color-directory") {
		t.Fatalf("redirected ls must remain plain text: %q %v", data, err)
	}
}
