package ui

import (
	"farshmac/internal/fsops"
	"farshmac/internal/terminal"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Complete only the final shell word. No shell expansion or command execution.
func (a *App) completeCommand() {
	if !a.Settings.CompleteFiles {
		return
	}
	start := strings.LastIndexAny(a.Command, " \t") + 1
	token := a.Command[start:]
	if strings.ContainsAny(token, "'\"`$\\") {
		a.Status = "Completion: use an unquoted path prefix"
		return
	}
	// Complete where the command will run: remote and archive panels execute
	// commands in a local folder, so their virtual path is not readable here.
	base := a.Panels[a.Active].filesystemPath()
	if base == "" {
		base, _ = os.UserHomeDir()
	}
	prefix := filepath.Base(token)
	dir := filepath.Dir(token)
	if token == "" || strings.HasSuffix(token, "/") {
		prefix = ""
		dir = token
	}
	ignoreCase := a.Settings.CompleteIgnoreCase
	collect := func(o *fsops.Operation) (func(), error) {
		var values []string
		target := expand(dir, base)
		err := fsops.ReadDirectory(target, o, func(e os.DirEntry) error {
			n, p := e.Name(), prefix
			if ignoreCase {
				n, p = strings.ToLower(n), strings.ToLower(p)
			}
			if !strings.HasPrefix(n, p) {
				return nil
			}
			name := e.Name()
			if dir != "" && dir != "." {
				name = filepath.Join(dir, name)
			}
			if e.IsDir() {
				name += "/"
			}
			values = append(values, name)
			return nil
		})
		sort.Strings(values)
		if err != nil {
			err = fmt.Errorf("Completion: cannot read %s: %w", target, err)
		}
		return func() { a.applyCompletion(start, values) }, err

	}
	if a.Async {
		a.backgroundProgress("Completing filename", collect)
		return
	}
	o := fsops.NewOperation("Completing filename")
	o.IgnoreDSStore = a.Settings.IgnoreDSStore
	defer o.Cancel()
	done, err := collect(o)
	if err != nil {
		a.error(err)
	} else {
		done()
	}
}
func (a *App) applyCompletion(start int, values []string) {
	if len(values) == 0 {
		a.Status = "No matching paths"
		return
	}
	insert := func(i int) { a.Command = a.Command[:start] + terminal.Quote(values[i]) }
	if len(values) == 1 {
		insert(0)
	} else {
		a.choose("Complete filename", values, insert)
	}
}
