package terminal

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// RequestCommands snapshots the interactive shell's history without executing
// a command or adding integration plumbing to that history. Call at a prompt.
func (s *Session) RequestCommands(cwd string) error {
	return s.request(cwd, "", "history", s.historyKey)
}

var historyEntry = regexp.MustCompile(`(?m)^[ \t]*[0-9]+[ *]+(?:FARSHMAC_HISTORY_ENTRY:|\?\?)`)

func (s *Session) readCommands() []string {
	path := filepath.Join(s.dir, "command-history")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	defer os.Remove(path)
	data, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil {
		return []string{}
	}
	truncated := len(data) > 4<<20
	var entries []string
	if s.Shell == "zsh" {
		entries = strings.Split(string(data), "\x00")
		if truncated {
			entries = entries[:len(entries)-1]
		}
	} else {
		entries = historyEntry.Split(string(data), -1)[1:]
		if truncated && len(entries) > 0 {
			entries = entries[:len(entries)-1]
		}
		for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
			entries[i], entries[j] = entries[j], entries[i]
		}
	}
	result := make([]string, 0, len(entries))
	seen := make(map[string]bool)
	for _, entry := range entries {
		if s.Shell != "zsh" {
			entry = strings.TrimSuffix(entry, "\n")
		}
		if strings.TrimSpace(entry) == "" || seen[entry] {
			continue
		}
		seen[entry] = true
		result = append(result, entry)
	}
	return result
}
