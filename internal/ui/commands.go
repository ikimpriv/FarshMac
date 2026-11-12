package ui

import (
	"farshmac/internal/fsops"
	"farshmac/internal/preview"
	"farshmac/internal/terminal"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func appendUnique(list []string, s string, limit int) []string {
	var out []string
	for _, v := range list {
		if v != s {
			out = append(out, v)
		}
	}
	out = append(out, s)
	limit = max(1, limit)
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}
func (a *App) report(title, text string) {
	d := newDocument(title, text)
	d.Help = true
	d.markSaved()
	a.showDocument(d)
}
func (a *App) backgroundProgress(label string, fn func(*fsops.Operation) (func(), error)) {
	a.backgroundTask(label, func(o *fsops.Operation) (func(), func(), error) {
		done, err := fn(o)
		return done, nil, err
	})
}
func (a *App) findFiles(folders bool) {
	if a.remoteUnsupported() {
		return
	}
	title := "Find file"
	if folders {
		title = "Find folder"
	}
	root := a.Panels[a.Active].Path
	a.form(title, []Field{{Label: "Name masks", Value: "*"}, {Label: "Containing text"}, {Label: "Search subfolders", Toggle: true, Checked: true}, {Label: "Case sensitive text", Toggle: true}}, func(f []Field) {
		mask, needle := f[0].Value, f[1].Value
		groups := a.Settings.MaskGroups
		if err := validateMasks(mask, groups); err != nil {
			a.error(err)
			return
		}
		if !f[3].Checked {
			needle = strings.ToLower(needle)
		}
		a.backgroundProgress("Searching "+root, func(o *fsops.Operation) (func(), error) {
			var paths []string
			skipped := 0
			limit := false
			err := fsops.WalkDir(root, o, func(path string, d fs.DirEntry, e error) error {
				if err := o.Check(); err != nil {
					return err
				}
				o.Add(path, 0)
				if e != nil {
					skipped++
					if d != nil && d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if path == root {
					return nil
				}
				if d.IsDir() && !f[2].Checked && filepath.Dir(path) != root {
					return filepath.SkipDir
				}
				if matchesMask(d.Name(), mask, groups) && (!folders || d.IsDir()) {
					match := needle == ""
					if !match && !d.IsDir() {
						info, e := d.Info()
						if e == nil && info.Mode().IsRegular() {
							match, e = fileContains(path, needle, f[3].Checked, o)
							if e != nil {
								if o.Check() != nil {
									return fsops.ErrCancelled
								}
								skipped++
							}
						}
					}
					if match {
						paths = append(paths, path)
						if len(paths) >= 10000 {
							limit = true
							return fs.SkipAll
						}
					}
				}
				if d.IsDir() && !f[2].Checked {
					return filepath.SkipDir
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			return func() {
				if len(paths) == 0 {
					a.Status = fmt.Sprintf("No matches (%d unreadable paths)", skipped)
					return
				}
				labels := make([]string, len(paths))
				for i, p := range paths {
					labels[i], _ = filepath.Rel(root, p)
				}
				caption := fmt.Sprintf("%s — %d found, %d unreadable", title, len(paths), skipped)
				if limit {
					caption += " (limit)"
				}
				a.choose(caption, labels, func(i int) { a.revealPath(paths[i]) })
			}, nil
		})
	})
	a.Form.Primary = "Find"
	a.Form.CheckColumns = 2
}
func fileContains(path, needle string, sensitive bool, ops ...*fsops.Operation) (bool, error) {
	var o *fsops.Operation
	if len(ops) > 0 {
		o = ops[0]
	}
	f, e := os.Open(path)
	if e != nil {
		return false, e
	}
	defer f.Close()
	buf := make([]byte, 64*1024)
	tail := ""
	for {
		if e := o.Check(); e != nil {
			return false, e
		}
		n, err := f.Read(buf)
		if n > 0 {
			s := tail + string(buf[:n])
			test := s
			if !sensitive {
				test = strings.ToLower(s)
			}
			if strings.Contains(test, needle) {
				return true, nil
			}
			keep := min(len(s), len(needle)*4+4)
			tail = s[len(s)-keep:]
		}
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
}
func (a *App) revealPath(path string) {
	info, err := os.Stat(path)
	if err != nil {
		a.error(err)
		return
	}
	p := &a.Panels[a.Active]
	if info.IsDir() {
		a.error(p.Load(path))
		return
	}
	if err = p.Load(filepath.Dir(path)); err != nil {
		a.error(err)
		return
	}
	if p.pendingPosition(filepath.Base(path), "") {
		return
	}
	for i, e := range p.Entries {
		if e.Name == filepath.Base(path) {
			p.Cursor = i
			return
		}
	}
	a.Status = "File is hidden by the current panel filter"
}
func (a *App) historyMenu(kind string) {
	if kind == "Commands" {
		items := append([]string{}, a.history...)
		reverse(items)
		if len(items) == 0 {
			a.Status = "Command history is empty"
			return
		}
		a.choose("Command history", items, func(i int) { a.Command = items[i] })
		return
	}
	if kind == "Files" {
		records := append([]viewRecord{}, a.viewHistory...)
		if len(records) == 0 {
			a.Status = "View/edit history is empty"
			return
		}
		items := make([]string, len(records))
		for i, r := range records {
			prefix := "View "
			if r.Edit {
				prefix = "Edit "
			}
			items[len(items)-1-i] = prefix + r.Path
		}
		a.choose("View/edit history", items, func(i int) { r := records[len(records)-1-i]; a.openDocument(r.Path, r.Edit) })
		return
	}
	paths := append([]string{}, a.folderHistory...)
	for _, p := range a.Panels {
		for _, s := range p.History {
			paths = appendUnique(paths, s, a.Settings.HistoryLimit)
		}
	}
	a.folderHistory = paths
	reverse(paths)
	a.choose("Folder history", paths, func(i int) { a.error(a.Panels[a.Active].Load(paths[i])) })
}
func reverse(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
func (a *App) compareFolders() {
	for i := range a.Panels {
		a.Panels[i].RestoreMarked = cloneMarks(a.Panels[i].Marked)
		a.Panels[i].Marked = map[string]bool{}
	}
	for i := range a.Panels {
		other := map[string]Entry{}
		for _, e := range a.Panels[1-i].Entries {
			other[e.Name] = e
		}
		for _, e := range a.Panels[i].Entries {
			if e.Dir {
				continue
			}
			o, ok := other[e.Name]
			if !ok || o.Dir || e.Info.Size() != o.Info.Size() || e.Info.ModTime().After(o.Info.ModTime()) {
				a.Panels[i].Marked[e.Name] = true
			}
		}
	}
	a.Status = "Marked files missing, different in size, or newer than the opposite panel (non-recursive)"
}
func (a *App) fileAttributes() {
	if a.remoteUnsupported() {
		return
	}
	if a.archiveReadOnly() {
		return
	}
	paths := a.Panels[a.Active].Selected()
	if len(paths) == 0 {
		return
	}
	info, err := os.Lstat(paths[0])
	if err != nil {
		a.error(err)
		return
	}
	a.form("File attributes", []Field{{Label: "Permissions (octal)", Value: fmt.Sprintf("%03o", info.Mode().Perm())}, {Label: "Modified (local)", Value: info.ModTime().Format("2006-01-02 15:04:05")}}, func(f []Field) {
		mode, e := strconv.ParseUint(f[0].Value, 8, 12)
		if e != nil || mode > 0777 {
			a.Status = "Permissions must be 000–777"
			return
		}
		stamp, e := time.ParseInLocation("2006-01-02 15:04:05", f[1].Value, time.Local)
		if e != nil {
			a.error(e)
			return
		}
		a.workProgress("Updating attributes", func(o *fsops.Operation) error {
			for _, p := range paths {
				if err := o.Check(); err != nil {
					return err
				}
				o.Add(p, 0)
				info, e := os.Lstat(p)
				if e != nil {
					return e
				}
				if info.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("attributes of symlinks are not changed: %s", p)
				}
				if e = os.Chmod(p, os.FileMode(mode)); e != nil {
					return e
				}
				if e = os.Chtimes(p, stamp, stamp); e != nil {
					return e
				}
			}
			return nil
		})
	})
}
func (a *App) createLink() {
	if a.remoteUnsupported() {
		return
	}
	if a.archiveReadOnly() {
		return
	}
	src := a.Panels[a.Active].Current()
	if src == "" {
		return
	}
	dst := filepath.Join(a.Panels[1-a.Active].Path, filepath.Base(src))
	a.form("Create link", []Field{{Label: "Link name", Value: dst}, {Label: "Symbolic link", Toggle: true, Checked: true}}, func(f []Field) {
		target := expand(f[0].Value, a.Panels[a.Active].Path)
		var e error
		if f[1].Checked {
			e = os.Symlink(src, target)
		} else {
			e = os.Link(src, target)
		}
		a.error(e)
		a.refresh()
	})
}
func (a *App) applyCommand() {
	if a.remoteUnsupported() {
		return
	}
	if a.archiveReadOnly() {
		return
	}
	paths := a.Panels[a.Active].Selected()
	if len(paths) == 0 {
		return
	}
	a.prompt("Apply command", "Use {} for a shell-quoted filename", "", func(s string) {
		if strings.TrimSpace(s) == "" {
			return
		}
		var commands []string
		for _, p := range paths {
			if strings.Contains(s, "{}") {
				commands = append(commands, strings.ReplaceAll(s, "{}", terminal.Quote(p)))
			} else {
				commands = append(commands, s+" "+terminal.Quote(p))
			}
		}
		a.Command = strings.Join(commands, "\n")
		a.execute()
	})
}
func (a *App) runUserCommand(command string) {
	if a.remoteUnsupported() {
		return
	}
	panel := &a.Panels[a.Active]
	p := panel.Current()
	if panel.Archive != nil {
		if _, inside := panel.Archive.Relative(p); inside {
			a.Status = "Use F5 to copy this archive item out before running commands on it"
			return
		}
	}
	a.Command = strings.ReplaceAll(command, "{}", terminal.Quote(p))
	a.execute()
}
func (a *App) userMenu() {
	items := []string{}
	for _, c := range a.Settings.UserMenu {
		items = append(items, c.Name)
	}
	if len(items) == 0 {
		a.editUserMenu()
		return
	}
	a.choose("User menu", items, func(i int) { a.runUserCommand(a.Settings.UserMenu[i].Command) })
}
func (a *App) editUserMenu() {
	items := []string{"Add command…"}
	for _, c := range a.Settings.UserMenu {
		items = append(items, c.Name)
	}
	a.choose("Edit user menu", items, func(i int) {
		c := UserCommand{}
		if i > 0 {
			c = a.Settings.UserMenu[i-1]
		}
		a.form("User command — {} inserts filename", []Field{{Label: "Label", Value: c.Name}, {Label: "Command (empty deletes)", Value: c.Command}}, func(f []Field) {
			c = UserCommand{f[0].Value, f[1].Value}
			if i > 0 {
				a.Settings.UserMenu = append(a.Settings.UserMenu[:i-1], a.Settings.UserMenu[i:]...)
			}
			if c.Name != "" && c.Command != "" {
				a.Settings.UserMenu = append(a.Settings.UserMenu, c)
			}
			a.saveSetup()
		})
	})
}
func (a *App) fileAssociations() {
	items := []string{"Add association…", "Open current file with macOS default app"}
	for _, c := range a.Settings.Associations {
		items = append(items, c.Mask+" → "+c.Command)
	}
	a.choose("File associations", items, func(i int) {
		if i == 1 {
			a.openSystemSelected() // Same remote/archive checks and error reporting as Shift+Enter.
			return
		}
		c := Association{}
		if i > 1 {
			c = a.Settings.Associations[i-2]
		}
		a.form("File association — {} inserts filename", []Field{{Label: "Masks", Value: c.Mask}, {Label: "Run command", Value: c.Command}, {Label: "Viewer command", Value: c.Viewer}, {Label: "Editor command", Value: c.Editor}}, func(f []Field) {
			if err := validateMasks(f[0].Value, a.Settings.MaskGroups); err != nil {
				a.error(err)
				return
			}
			c = Association{Mask: f[0].Value, Command: f[1].Value, Viewer: f[2].Value, Editor: f[3].Value}
			if i > 1 {
				a.Settings.Associations = append(a.Settings.Associations[:i-2], a.Settings.Associations[i-1:]...)
			}
			if c.Mask != "" && (c.Command != "" || c.Viewer != "" || c.Editor != "") {
				a.Settings.Associations = append(a.Settings.Associations, c)
			}
			a.saveSetup()
		})
	})
}
func (a *App) taskList() {
	a.backgroundProgress("Reading processes", func(o *fsops.Operation) (func(), error) {
		out, err := exec.CommandContext(o.Context(), "/bin/ps", "-axo", "pid,ppid,%cpu,%mem,comm").Output()
		return func() { a.report("Processes", string(out)) }, err
	})
}
func (a *App) treeMenu() {
	if a.remoteUnsupported() {
		return
	}
	index := a.Active
	root := a.Panels[index].Path
	options := a.Panels[index].treeOptions()
	a.backgroundProgress("Reading folder tree", func(o *fsops.Operation) (func(), error) {
		options.check = o.Check
		options.progress = func(path string, _ int) { o.Add(path, 0) }
		result := scanTree(root, options)
		if result.err != nil {
			return nil, result.err
		}
		paths := []string{root}
		labels := []string{treeDisplayName(root)}
		branches := treeLabels(result.entries)
		for _, entry := range result.entries {
			paths = append(paths, filepath.Join(root, entry.Name))
			labels = append(labels, branches[entry.Name])
		}
		return func() {
			a.Status = result.status(options.depth)
			title := "Folder tree"
			if result.limited || result.skipped > 0 {
				title += " (partial)"
			}
			a.choose(title, labels, func(i int) { a.error(a.Panels[index].Load(paths[i])) })
		}, nil
	})
}
func (a *App) updatePreview() {
	if a.Doc != nil || a.TermVisible || a.Busy {
		return
	}
	index := -1
	for i, p := range a.Panels {
		if p.View == "Quick view" && !p.Hidden {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	source := &a.Panels[1-index]
	if source.Remote != nil {
		a.previewImage = ""
		a.previewText = "Remote file: use F3 to download and view"
		return
	}
	path := source.Current()
	key := path
	if len(source.Entries) > 0 {
		e := source.Entries[source.Cursor]
		if e.Info != nil {
			key += fmt.Sprint(e.Info.Size(), e.Info.ModTime())
		}
	}
	if a.previewPending {
		if key != a.previewKey && a.previewOperation != nil {
			a.previewOperation.Cancel()
			a.previewKey = ""
			a.previewImage = ""
		}
		return
	}
	if key == a.previewKey {
		return
	}
	a.previewKey = key
	a.previewPath = path
	a.previewPending = true
	o := fsops.NewOperation("Quick view")
	o.IgnoreDSStore = a.Settings.IgnoreDSStore
	hideHidden := !a.Settings.ShowHidden
	a.previewOperation = o
	a.previewText = "Loading…"
	a.previewImage = ""
	go func() {
		text := ""
		imageData := ""
		info, e := os.Stat(path)
		if e != nil {
			text = fmt.Sprint(e)
		} else if info.IsDir() {
			var entries []os.DirEntry
			e := fsops.ReadDirectory(path, o, func(entry os.DirEntry) error {
				if hideHidden {
					info, _ := entry.Info()
					if hiddenEntry(entry.Name(), info) {
						return nil
					}
				}
				entries = append(entries, entry)
				if len(entries) >= 101 {
					return fs.SkipAll
				}
				return nil
			})
			if e == fs.SkipAll {
				e = nil
			}
			text = fmt.Sprintf("%s\nDirectory (preview, up to 100 entries)\n", path)
			if e != nil {
				text += e.Error()
			} else {
				for i, entry := range entries {
					if i == 100 {
						text += "…"
						break
					}
					text += entry.Name() + "\n"
				}
			}
		} else if info.Mode().IsRegular() && preview.Supported(path) {
			r, err := preview.Run(o.Context(), path)
			text = fmt.Sprintf("%s\n%d bytes · %s", filepath.Base(path), info.Size(), info.ModTime().Format("2006-01-02 15:04"))
			if err != nil {
				text += "\nPreview unavailable (unsupported, damaged or limit exceeded)"
			} else {
				imageData = r.Data
				if r.Pages > 0 {
					text += fmt.Sprintf("\nPDF · page 1 of %d", r.Pages)
				} else {
					text += fmt.Sprintf("\n%s · %d × %d pixels", r.Format, r.Width, r.Height)
				}
			}
		} else if info.Mode().IsRegular() {
			f, e := os.Open(path)
			if e != nil {
				text = e.Error()
			} else {
				b, e := io.ReadAll(o.Reader(io.LimitReader(f, 16*1024)))
				f.Close()
				text = string(b)
				if e != nil {
					text += e.Error()
				}
			}
		}
		a.jobs <- func() {
			if a.previewKey == key && o.Check() == nil {
				a.previewText = text
				a.previewImage = imageData
			}
			a.previewOperation = nil
			a.previewPending = false
			a.dirty = true
		}
	}()
}
func sortedKeys(m map[string]string) []string {
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
