package aispaces

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Invocation-local overrides; never change the user's CLI config, approvals,
// credential storage or sandbox. Explicit "default" opts into host tooling.
func codexContextArgs(mode string) []string {
	if mode == "default" {
		return nil
	}
	args := []string{"-c", "features.apps=false", "-c", "features.plugins=false", "-c", "skills.bundled.enabled=false"}
	home, _ := os.UserHomeDir()
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	var disabled []string
	for _, dir := range []string{filepath.Join(codexHome, "skills"), filepath.Join(home, ".agents", "skills")} {
		// Only the supported host skill locations, not an unbounded home scan.
		for _, pattern := range []string{"*/SKILL.md", ".system/*/SKILL.md"} {
			matches, _ := filepath.Glob(filepath.Join(dir, pattern))
			for _, file := range matches {
				disabled = append(disabled, "{path="+strconv.Quote(file)+",enabled=false}")
			}
		}
	}
	sort.Strings(disabled)
	if len(disabled) > 0 {
		args = append(args, "-c", "skills.config=["+strings.Join(disabled, ",")+"]")
	}
	return args
}
