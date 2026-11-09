package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangeDirPreservesCommand(t *testing.T) {
	for _, shell := range []string{"bash", "sh", "zsh"} {
		for _, editing := range []string{"emacs", "vi", "vicmd"} {
			t.Run(shell+"/"+editing, func(t *testing.T) {
				root := t.TempDir()
				t.Setenv("HOME", root)
				t.Setenv("ZDOTDIR", root)
				t.Setenv("ENV", filepath.Join(root, "shrc"))
				rc := ""
				if editing != "emacs" {
					if shell == "zsh" {
						rc = "bindkey -v\n"
					} else {
						rc = "set -o vi\n"
					}
				}
				for _, name := range []string{".bashrc", ".zshrc", "shrc"} {
					if err := os.WriteFile(filepath.Join(root, name), []byte(rc), 0600); err != nil {
						t.Fatal(err)
					}
				}
				target := filepath.Join(root, "folder ' каталог")
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
				s, err := Start(root, 120, 24, shell)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				waitReady(t, s)
				// Leave a partial command on the editable line, then complete it after cd.
				if err := s.Write("printf 'PRESERVED:%s\\n' "); err != nil {
					t.Fatal(err)
				}
				if editing == "vicmd" {
					if err := s.Write("\x1b"); err != nil {
						t.Fatal(err)
					}
				}
				if err := s.ChangeDir(target); err != nil {
					t.Fatal(err)
				}
				_, ready := waitReady(t, s)
				got, _ := filepath.EvalSymlinks(ready.Cwd)
				want, _ := filepath.EvalSymlinks(target)
				if got != want {
					t.Fatalf("cwd %q, want %q", got, want)
				}
				if editing == "vicmd" && shell == "zsh" {
					if err := s.Write("A"); err != nil {
						t.Fatal(err)
					}
				}
				if err := s.Write("\"$PWD\"\r"); err != nil {
					t.Fatal(err)
				}
				out, _ := waitReady(t, s)
				if !strings.Contains(out, "\r\nPRESERVED:"+ready.Cwd+"\r\n") {
					t.Fatalf("lost editable command: %q", out)
				}
				// Switching again with an empty line must not resurrect the kill ring.
				if err := s.ChangeDir(root); err != nil {
					t.Fatal(err)
				}
				waitReady(t, s)
				if err := s.Write("printf 'EMPTY_OK\\n'\r"); err != nil {
					t.Fatal(err)
				}
				emptyOut, _ := waitReady(t, s)
				if !strings.Contains(emptyOut, "\r\nEMPTY_OK\r\n") || strings.Contains(emptyOut, "PRESERVED:") {
					t.Fatalf("empty line resurrected text: %q", emptyOut)
				}
				if err := s.ChangeDir(filepath.Join(root, "missing")); err != nil {
					t.Fatal(err)
				}
				_, ready = waitReady(t, s)
				if ready.Status == 0 {
					t.Fatal("missing directory reported success")
				}
				if err := s.Run(target, "history"); err != nil {
					t.Fatal(err)
				}
				out, _ = waitReady(t, s)
				if strings.Contains(out, "_farshmac_") || strings.Contains(out, "builtin cd") || strings.Contains(out, "panel-command") {
					t.Fatalf("history leaked integration: %q", out)
				}
			})
		}
	}
}
