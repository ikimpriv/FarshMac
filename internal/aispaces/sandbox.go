package aispaces

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Agents run inside a macOS sandbox. An agent can write only to its scratch
// folder and cannot read the user's home folder or other volumes: project
// files reach it only through the Env's MCP server, which is what keeps each
// Env's context separate. Exceptions are what the CLI itself needs: its own
// configuration and session store (but no other project's sessions and not
// the shared prompt history), its installation, the login keychain, and its
// own temporary folder. The keychain is writable too: the CLI stores its
// refreshed login there, and without that it is logged out within hours.
//
// Not restricted: the network (the CLI must reach its provider) and reading
// outside the home folder (system files, tools, /private/var/folders).

const sandboxExec = "/usr/bin/sandbox-exec"

func sbQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func realPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

// claudeProject is the folder name Claude Code derives from a working folder.
func claudeProject(dir string) string {
	return regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(dir, "-")
}

// sandboxProfile confines an agent whose working folder is scratch. readable
// lists extra files or folders it must be able to read (its own executable,
// the MCP gateway).
func sandboxProfile(scratch string, readable ...string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	home, scratch = realPath(home), realPath(scratch)
	uid := strconv.Itoa(os.Getuid())
	project := claudeProject(scratch)
	var ancestors []string
	for p := filepath.Dir(scratch); ; p = filepath.Dir(p) {
		ancestors = append(ancestors, "(literal "+sbQuote(p)+")")
		if p == "/" || p == "." {
			break
		}
	}
	var extra []string
	for _, p := range readable {
		if p = realPath(p); p != "" {
			extra = append(extra, "(subpath "+sbQuote(p)+")")
		}
	}
	config := filepath.Join(home, ".claude")
	// The CLI's own state, minus other projects' sessions and the shared prompt history.
	own := fmt.Sprintf(`(require-all (subpath %s)
      (require-not (literal %s))
      (require-not (require-all (subpath %s) (require-not (subpath %s)))))
    (regex #"^%s/\.claude\.json")
    (subpath %s) (subpath %s)`,
		sbQuote(config), sbQuote(filepath.Join(config, "history.jsonl")),
		sbQuote(filepath.Join(config, "projects")), sbQuote(filepath.Join(config, "projects", project)),
		regexp.QuoteMeta(home),
		sbQuote(filepath.Join(home, ".codex")),
		sbQuote(filepath.Join("/private/tmp", "claude-"+uid, project)))
	// What the CLI reads in every session of the user, sandboxed or not: an
	// agent that could write there would change the user's own defaults (a
	// model or effort chosen in its console is saved as the default for new
	// sessions) or plant instructions, commands and hooks that later run
	// outside the sandbox.
	var shared []string
	// /model can persist a new default in Codex too. A failed save must not
	// change the user's other sessions; recent CLIs keep it session-local.
	shared = append(shared, "(literal "+sbQuote(filepath.Join(home, ".codex", "config.toml"))+")")
	if configHome := os.Getenv("CODEX_HOME"); configHome != "" {
		shared = append(shared, "(literal "+sbQuote(filepath.Join(realPath(configHome), "config.toml"))+")")
	}
	for _, name := range []string{"settings.json", "settings.local.json", "CLAUDE.md", "keybindings.json"} {
		shared = append(shared, "(literal "+sbQuote(filepath.Join(config, name))+")")
	}
	for _, name := range []string{"commands", "agents", "skills", "plugins", "hooks", "output-styles", "rules"} {
		shared = append(shared, "(subpath "+sbQuote(filepath.Join(config, name))+")")
	}
	return fmt.Sprintf(`(version 1)
(allow default)
(deny file-read-data
  (require-all
    (require-any (subpath %s) (subpath "/Volumes") (subpath %s))
    (require-not (require-any
      (subpath %s) %s
      %s
      (literal %s) (subpath %s) (subpath %s) (subpath %s)
      %s))))
(deny file-write*
  (require-not (require-any
    (subpath "/dev") (subpath "/private/var/folders") (subpath %s) (subpath %s)
    (regex #"^/private/tmp/claude-[0-9a-f]+-cwd$")
    %s)))
(deny file-write* %s)
`,
		sbQuote(home), sbQuote("/private/tmp/claude-"+uid),
		sbQuote(scratch), strings.Join(ancestors, " "),
		own,
		sbQuote("/private/tmp/claude-"+uid), sbQuote(filepath.Join(home, ".local")), sbQuote(filepath.Join(home, "Library", "Keychains")), sbQuote(filepath.Join(home, ".nvm")),
		strings.Join(extra, " "),
		sbQuote(scratch), sbQuote(filepath.Join(home, "Library", "Keychains")),
		own, strings.Join(shared, " ")), nil
}
