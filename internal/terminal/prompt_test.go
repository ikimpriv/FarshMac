package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptModePreservesProfile(t *testing.T) {
	for _, shell := range []string{"bash", "sh", "zsh"} {
		for _, custom := range []bool{false, true} {
			t.Run(shell+map[bool]string{false: "/simple", true: "/custom"}[custom], func(t *testing.T) {
				root := t.TempDir()
				t.Setenv("HOME", root)
				t.Setenv("ZDOTDIR", root)
				marker := filepath.Join(root, "hook-ran")
				rc := "export FARSHMAC_PROFILE_VALUE=loaded\nalias profile_alias=\"printf 'PROFILE:%s\\\\n' \\\"$FARSHMAC_PROFILE_VALUE\\\"\"\n"
				if shell == "zsh" {
					rc += "precmd() { : > " + Quote(marker) + "; }\n"
				} else {
					rc += "PROMPT_COMMAND=" + Quote(": > "+Quote(marker)) + "\n"
				}
				for _, name := range []string{".bashrc", ".zshrc", "shrc"} {
					if err := os.WriteFile(filepath.Join(root, name), []byte(rc), 0600); err != nil {
						t.Fatal(err)
					}
				}
				t.Setenv("ENV", filepath.Join(root, "shrc"))
				s, err := StartConfigured(root, 120, 24, shell, custom)
				if err != nil {
					t.Fatal(err)
				}
				defer closeAndWait(s)
				waitReady(t, s)
				_, err = os.Stat(marker)
				if custom && err != nil {
					t.Fatal("ready reported before custom hook")
				}
				if !custom && !os.IsNotExist(err) {
					t.Fatal("simple prompt ran user hook")
				}
				if err = s.Run(root, "profile_alias"); err != nil {
					t.Fatal(err)
				}
				out, _ := waitReady(t, s)
				if !strings.Contains(out, "\r\nPROFILE:loaded\r\n") {
					t.Fatalf("profile lost: %q", out)
				}
			})
		}
	}
}
