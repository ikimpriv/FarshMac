package ui

import (
	"context"
	"farshmac/internal/fsops"
	"farshmac/internal/preview"
	"farshmac/internal/terminal"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// LaunchPlan separates dispatch from execution so filenames never become shell syntax.
type LaunchPlan struct {
	Args           []string
	Command        string
	Internal, Edit bool
}

func externalPlan(path, command string, edit bool) LaunchPlan {
	if strings.TrimSpace(command) == "" {
		if edit {
			return LaunchPlan{Args: []string{"/usr/bin/open", "-t", path}, Edit: true}
		}
		return LaunchPlan{Args: []string{"/usr/bin/open", path}}
	}
	quoted := terminal.Quote(path)
	if strings.Contains(command, "{}") {
		command = strings.ReplaceAll(command, "{}", quoted)
	} else {
		command += " " + quoted
	}
	return LaunchPlan{Command: command, Edit: edit}
}
func (a *App) viewPlan(path string, edit, alternate, forceInternal bool) LaunchPlan {
	if forceInternal {
		return LaunchPlan{Internal: true, Edit: edit}
	}
	external, command := a.Settings.ExternalViewerDefault, a.Settings.ExternalViewer
	if edit {
		external, command = a.Settings.ExternalEditorDefault, a.Settings.ExternalEditor
	}
	// Per-file view/edit associations override the default; Alt reverses the result.
	associated := false
	for _, assoc := range a.Settings.Associations {
		if matchesMask(filepath.Base(path), assoc.Mask, a.Settings.MaskGroups) {
			c := assoc.Viewer
			if edit {
				c = assoc.Editor
			}
			if c != "" {
				command = c
				associated = true
			}
			break
		}
	}
	useExternal := external || associated
	if alternate {
		useExternal = !useExternal
	}
	if !useExternal {
		return LaunchPlan{Internal: true, Edit: edit}
	}
	return externalPlan(path, command, edit)
}
func (a *App) launch(path string, plan LaunchPlan) {
	if plan.Internal {
		a.openDocument(path, plan.Edit)
		return
	}
	if plan.Command != "" {
		a.submitCommand(plan.Command, false)
		return
	}
	if len(plan.Args) > 0 {
		args := append([]string{}, plan.Args...)
		run := a.runExternal
		if run == nil {
			run = func(ctx context.Context, args []string) ([]byte, error) {
				return exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
			}
		}
		a.workProgress("Opening "+filepath.Base(path), func(o *fsops.Operation) error {
			out, e := run(o.Context(), args)
			if e != nil {
				return fmt.Errorf("%w: %s", e, strings.TrimSpace(string(out)))
			}
			return nil
		})
	}
}

// Shift+Enter uses Finder's default application, ignoring Far associations and
// the command line. Pass the absolute path as one argument, never shell syntax.
func (a *App) openSystemSelected() {
	if !a.requireFilePanel() {
		return
	}
	p := &a.Panels[a.Active]
	if !p.can(capSystemOpen) {
		a.Status = "Copy remote files to a local panel before opening them with macOS"
		return
	}
	path := p.Current()
	if path == "" {
		return
	}
	if p.virtual(path) {
		a.Status = "Use F5 to copy this archive item out before opening it with macOS"
		return
	}
	a.launch(path, externalPlan(path, "", false))
}
func (a *App) openConfigured(edit, alternate, forceInternal bool) {
	if !a.requireFilePanel() {
		return
	}
	p0 := &a.Panels[a.Active]
	if !edit && len(p0.Entries) > 0 && p0.Entries[p0.Cursor].Dir {
		a.folderSize()
		return
	}
	p := &a.Panels[a.Active]
	if p.backend().Open(a, p, edit) {
		return
	}
	path := p.Current()
	if path == "" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		a.openFailed(path, edit, err)
		return
	}
	if info.IsDir() {
		if edit {
			a.fileAttributes()
		} else {
			a.folderSize()
		}
		return
	}
	plan := a.viewPlan(path, edit, alternate, forceInternal)
	if plan.Internal && !edit && info.Mode().IsRegular() && preview.Supported(path) {
		a.openImage(path)
		return
	}
	a.launch(path, plan)
}
func (a *App) enterFile() {
	if !a.requireFilePanel() {
		return
	}

	p := &a.Panels[a.Active]
	if p.backend().Enter(a, p) {
		return
	}
	path := p.Current()
	if path == "" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		a.error(err)
		return
	}
	if info.IsDir() {
		// Bundles (.app) are folders here; Shift+Enter launches them.
		a.error(p.Load(path))
		return
	}
	if fsops.ArchiveFormat(path) != "" {
		a.openArchive(a.Active, path)
		return
	}
	for _, assoc := range a.Settings.Associations {
		if assoc.Command != "" && matchesMask(filepath.Base(path), assoc.Mask, a.Settings.MaskGroups) {
			a.launch(path, externalPlan(path, assoc.Command, false))
			return
		}
	}
	if info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
		a.submitCommand(terminal.Quote(path), false)
		return
	}
	// Enter uses the operating-system association; F3 is the viewer.
	a.launch(path, externalPlan(path, "", false))
}
func (a *App) submitCommand(command string, record bool) {
	saved := a.Command
	a.Command = command
	a.execute()
	if !record {
		a.Command = saved
	}
}
func (a *App) focusCreatedDirectory(panel int, path string) {
	p := &a.Panels[panel]
	p.ForcedVisiblePath = path
	parent := filepath.Dir(path)
	if err := p.Load(parent); err != nil {
		a.error(err)
		return
	}
	name := filepath.Base(path)
	if p.pendingPosition(name, "") {
		a.Active = panel
		return
	}
	for i, e := range p.Entries {
		if e.Name == name {
			p.Cursor = i
			a.Active = panel
			return
		}
	}
	a.Status = "Folder created; hidden by panel settings"
}
