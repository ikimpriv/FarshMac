package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInteractiveCommandHistory(t *testing.T) {
	for _, shell := range []string{"bash", "sh", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			t.Setenv("ZDOTDIR", root)
			rc := "HISTSIZE=100\nSAVEHIST=100\nPS1='TEST> '\n"
			for _, name := range []string{".bashrc", ".zshrc", "shrc"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(rc), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("ENV", filepath.Join(root, "shrc"))
			s, err := StartConfigured(root, 100, 24, shell, false)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			waitReady(t, s)
			command := "printf 'DIRECT_COMMAND\\n'"
			if err = s.Write(command + "\r"); err != nil {
				t.Fatal(err)
			}
			waitReady(t, s)
			multiline := "printf 'FIRST\\n'\nprintf 'SECOND\\n'"
			if err = s.Run(root, multiline); err != nil {
				t.Fatal(err)
			}
			waitReady(t, s)
			if err = s.RequestCommands(root); err != nil {
				t.Fatal(err)
			}
			out, e := waitReady(t, s)
			if len(e.Commands) < 2 {
				t.Fatalf("history: %#v, output %q", e.Commands, out)
			}
			if e.Commands[0] != multiline || e.Commands[1] != command {
				t.Fatalf("wrong history: %#v", e.Commands)
			}
			if strings.Contains(out, "FARSHMAC_HISTORY_ENTRY") || strings.Contains(out, "DIRECT_COMMAND") {
				t.Fatalf("history leaked into output: %q", out)
			}
			if err = s.Write("printf PRESERVED"); err != nil {
				t.Fatal(err)
			}
			if err = s.RequestCommands(root); err != nil {
				t.Fatal(err)
			}
			_, again := waitReady(t, s)
			if len(again.Commands) != len(e.Commands) {
				t.Fatalf("request added itself to history: %#v", again.Commands)
			}
			if err = s.Write("_INPUT\r"); err != nil {
				t.Fatal(err)
			}
			preserved, _ := waitReady(t, s)
			if !strings.Contains(preserved, "PRESERVED_INPUT") {
				t.Fatalf("editable input lost: %q", preserved)
			}

		})
	}
}
