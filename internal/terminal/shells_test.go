package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSelectableShells(t *testing.T) {
	for _, shell := range []string{"bash", "sh", "zsh"} {
		for _, editing := range []string{"emacs", "vi", "vicmd"} {
			t.Run(shell+"/"+editing, func(t *testing.T) {
				root := t.TempDir()
				t.Setenv("HOME", root)
				t.Setenv("ZDOTDIR", root)
				history := filepath.Join(root, "history")
				rc := "HISTFILE=" + Quote(history) + "\nHISTSIZE=100\nPS1='TEST> '\nalias panel_alias=\"printf 'ALIAS_OK\\\\n'\"\n"
				if shell == "zsh" {
					rc += "SAVEHIST=100\nsetopt INC_APPEND_HISTORY\n"
				}
				if editing != "emacs" {
					if shell == "zsh" {
						rc += "bindkey -v\n"
					} else {
						rc += "set -o vi\n"
					}
				}
				for _, name := range []string{".bashrc", ".zshrc", "shrc"} {
					if err := os.WriteFile(filepath.Join(root, name), []byte(rc), 0600); err != nil {
						t.Fatal(err)
					}
				}
				t.Setenv("ENV", filepath.Join(root, "shrc"))
				sub := filepath.Join(root, "folder ' каталог $(not-command)")
				if err := os.Mkdir(sub, 0700); err != nil {
					t.Fatal(err)
				}
				s, err := Start(root, 160, 24, shell)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				waitReady(t, s)
				pending := "UNFINISHED_DONT_RUN\x1b[D\x1b[D"
				if editing == "vicmd" {
					pending += "\x1b"
				}
				if err = s.Write(pending); err != nil {
					t.Fatal(err)
				}
				command := "export FARSHMAC_PERSIST=kept; panel_alias"
				if err = s.Run(sub, command); err != nil {
					t.Fatal(err)
				}
				out, event := waitReady(t, s)
				want, _ := filepath.EvalSymlinks(sub)
				actual, _ := filepath.EvalSymlinks(event.Cwd)
				if actual != want || event.Status != 0 || !strings.Contains(out, "\r\nALIAS_OK\r\n") {
					t.Fatalf("command failed: %+v %q", event, out)
				}
				for _, hidden := range []string{"_farshmac_panel_command", "builtin cd", "panel-command"} {
					if strings.Contains(out, hidden) {
						t.Fatalf("integration leaked: %q", out)
					}
				}
				// Bash normally saves history on exit; explicitly flush while testing.
				if shell != "zsh" {
					if err = s.Run(sub, "history -w"); err != nil {
						t.Fatal(err)
					}
					waitReady(t, s)
				}
				data, err := os.ReadFile(history)
				if err != nil || !strings.HasPrefix(string(data), command+"\n") || strings.Contains(string(data), "_farshmac_") {
					t.Fatalf("wrong history %q %v", data, err)
				}

				if err = s.Run(root, "printf 'VALUE:%s\\n' \"$FARSHMAC_PERSIST\"\nprintf 'KIND:%s\\n' \"$SHELL\""); err != nil {
					t.Fatal(err)
				}
				out, _ = waitReady(t, s)
				if !strings.Contains(out, "\r\nVALUE:kept\r\n") || !strings.Contains(out, "\r\nKIND:/bin/"+shell+"\r\n") {
					t.Fatalf("state or shell lost %q", out)
				}
				if err = s.Run(filepath.Join(root, "missing"), "printf SHOULD_NOT_RUN"); err != nil {
					t.Fatal(err)
				}
				out, _ = waitReady(t, s)
				if strings.Contains(out, "SHOULD_NOT_RUN") {
					t.Fatal("ran in incorrect directory", out)
				}
				if err = s.Run(root, "sh -c "+Quote("printf 'SLEEP_STARTED\\n'; exec sleep 30")); err != nil {
					t.Fatal(err)
				}
				var output strings.Builder
				timeout := time.After(5 * time.Second)
				for !strings.Contains(output.String(), "\r\nSLEEP_STARTED\r\n") {
					select {
					case event := <-s.Events:
						output.Write(event.Data)
					case <-timeout:
						t.Fatal("sleep did not start", output.String())
					}
				}
				if err = s.Write("\x03"); err != nil {
					t.Fatal(err)
				}
				waitReady(t, s)
				if err = s.Run(root, "printf 'RECOVERED\\n'"); err != nil {
					t.Fatal(err)
				}
				out, _ = waitReady(t, s)
				if !strings.Contains(out, "\r\nRECOVERED\r\n") {
					t.Fatal("failed to recover", out)
				}
				if editing == "emacs" {
					if err = s.Write("printf 'TYPED_OK\\n'\r"); err != nil {
						t.Fatal(err)
					}
					out, event = waitReady(t, s)
					if event.Status != 0 || !strings.Contains(out, "\r\nTYPED_OK\r\n") {
						t.Fatal("interactive typing failed after panel commands", out)
					}
				}
			})
		}
	}
}

func TestDefaultShellIsBash(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	s, err := Start(root, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Shell != "bash" {
		t.Fatal(s.Shell)
	}
	waitReady(t, s)
}
