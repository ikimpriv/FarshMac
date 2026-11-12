package ui

import (
	"bufio"
	"bytes"
	"farshmac/internal/fsops"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type panelOptions struct {
	DisableSortGroups                                             bool
	View, Sort, Filter                                            string
	Reverse, CaseSensitive, MixDirectories, SelectedFirst, Hidden bool
}

var panelViews = []string{"Brief", "Medium", "Full", "Wide", "Detailed", "Descriptions", "Long descriptions", "File owners", "File links", "Alternative full", "Info panel", "Tree panel", "Quick view"}

func panelViewLabel(view string) string {
	if view == "File links" {
		return "File links (hard links)"
	}
	return view
}
func panelViewLabels() []string {
	labels := make([]string, len(panelViews))
	for i, view := range panelViews {
		labels[i] = panelViewLabel(view)
	}
	return labels
}

func expandMasks(s string, groups map[string]string) string {
	for k, v := range groups {
		s = strings.ReplaceAll(s, "<"+k+">", v)
	}
	return s
}
func validateMasks(s string, groups map[string]string) error {
	s = expandMasks(s, groups)
	if strings.ContainsAny(s, "<>") {
		return fmt.Errorf("unknown mask group")
	}
	for _, m := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' || r == '|' }) {
		if _, err := filepath.Match(strings.TrimSpace(m), ""); err != nil {
			return err
		}
	}
	return nil
}
func matchesMask(name, mask string, groups map[string]string) bool {
	mask = expandMasks(mask, groups)
	parts := strings.SplitN(mask, "|", 2)
	match := func(s string) bool {
		for _, m := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' }) {
			m = strings.TrimSpace(m)
			if m == "*.*" {
				m = "*"
			}
			if ok, _ := filepath.Match(strings.ToLower(m), strings.ToLower(name)); ok {
				return true
			}
		}
		return false
	}
	return (strings.TrimSpace(parts[0]) == "" || match(parts[0])) && (len(parts) == 1 || !match(parts[1]))
}
func (p *Panel) sortEntries(files []Entry) {
	compare := func(a, b Entry) int {
		if a.Name == ".." {
			return -1
		}
		if b.Name == ".." {
			return 1
		}
		if !p.MixDirectories && a.Dir != b.Dir {
			if a.Dir {
				return -1
			}
			return 1
		}
		if p.SelectedFirst && p.Marked[a.Name] != p.Marked[b.Name] {
			if p.Marked[a.Name] {
				return -1
			}
			return 1
		}
		group := func(name string) int {
			for i, m := range strings.Split(p.SortGroups, ";") {
				if m != "" && matchesMask(name, m, p.MaskGroups) {
					return i
				}
			}
			return 10000
		}
		ga, gb := 0, 0
		if !p.DisableSortGroups && p.Sort != "Unsorted" {
			ga, gb = group(a.Name), group(b.Name)
		}
		if ga != gb {
			if ga < gb {
				return -1
			}
			return 1
		}
		if p.Sort == "Unsorted" {
			return 0
		}
		cmp := 0
		switch p.Sort {
		case "Created", "Accessed", "Owner":
			cmp = compareStat(a, b, p.Sort)
		case "Description":
			cmp = strings.Compare(p.Descriptions[a.Name], p.Descriptions[b.Name])
		case "Extension":
			cmp = strings.Compare(strings.ToLower(filepath.Ext(a.Name)), strings.ToLower(filepath.Ext(b.Name)))
		case "Size":
			if p.entrySize(a) < p.entrySize(b) {
				cmp = -1
			} else if p.entrySize(a) > p.entrySize(b) {
				cmp = 1
			}
		case "Modified":
			if a.Info.ModTime().Before(b.Info.ModTime()) {
				cmp = -1
			} else if a.Info.ModTime().After(b.Info.ModTime()) {
				cmp = 1
			}
		}
		if cmp == 0 {
			an, bn := a.Name, b.Name
			if !p.CaseSensitive {
				an, bn = strings.ToLower(an), strings.ToLower(bn)
			}
			cmp = strings.Compare(an, bn)
		}
		if p.Reverse {
			return -cmp
		}
		return cmp
	}
	sort.SliceStable(files, func(i, j int) bool { return compare(files[i], files[j]) < 0 })
}
func (a *App) sortMenu(index int) {
	p := &a.Panels[index]
	names := []string{"Name", "Extension", "Size", "Modified", "Reverse order", "Case sensitive", "Directories first", "Selected first", "Unsorted", "Created", "Accessed", "Description", "Owner"}
	a.choose("Sort mode", names, func(i int) {
		switch i {
		case 0, 1, 2, 3, 8, 9, 10, 11, 12:
			p.Sort = names[i]
		case 4:
			p.Reverse = !p.Reverse
		case 5:
			p.CaseSensitive = !p.CaseSensitive
		case 6:
			p.MixDirectories = !p.MixDirectories
		case 7:
			p.SelectedFirst = !p.SelectedFirst
		}
		a.error(p.Load(p.Path))
	})
}
func (a *App) filterPanel(index int) {
	p := &a.Panels[index]
	a.prompt("File panel filter", "Include masks | exclude masks; empty shows all", p.Filter, func(s string) {
		if err := validateMasks(s, a.Settings.MaskGroups); err != nil {
			a.error(err)
			return
		}
		p.Filter = s
		a.error(p.Load(p.Path))
	})
}
func (a *App) selectGroup(action string) {
	p := &a.Panels[a.Active]
	if action == "Restore" {
		p.Marked, p.RestoreMarked = cloneMarks(p.RestoreMarked), cloneMarks(p.Marked)
		return
	}
	if action == "Invert" {
		a.changeSelection(a.Settings.SelectFolders, func(_ Entry, on bool) bool { return !on })
		return
	}
	a.prompt(action+" group", "Masks: *.go;*.md | *_test.go (exclude)", "*", func(s string) {
		if err := validateMasks(s, a.Settings.MaskGroups); err != nil {
			a.error(err)
			return
		}
		a.changeSelection(a.Settings.SelectFolders, func(e Entry, on bool) bool {
			if matchesMask(e.Name, s, a.Settings.MaskGroups) {
				return action == "Select"
			}
			return on
		})
	})
}
func cloneMarks(m map[string]bool) map[string]bool {
	n := map[string]bool{}
	for k, v := range m {
		n[k] = v
	}
	return n
}
func readDescriptions(path string, ops ...*fsops.Operation) map[string]string {
	var o *fsops.Operation
	if len(ops) > 0 {
		o = ops[0]
	}
	out := map[string]string{}
	info, e := os.Stat(path)
	if e != nil || !info.Mode().IsRegular() {
		return out
	}
	f, e := os.Open(path)
	if e != nil {
		return out
	}
	defer f.Close()
	s := bufio.NewScanner(o.Reader(f))
	s.Buffer(make([]byte, 4096), 1024*1024)
	for s.Scan() {
		line := s.Text()
		name, desc := "", ""
		if strings.HasPrefix(line, "\"") {
			if end := strings.Index(line[1:], "\""); end >= 0 {
				name = line[1 : 1+end]
				desc = strings.TrimSpace(line[end+2:])
			}
		} else {
			if i := strings.IndexAny(line, " \t"); i >= 0 {
				name = line[:i]
				desc = strings.TrimSpace(line[i:])
			}
		}
		if name != "" {
			out[name] = desc
		}
	}
	return out
}
func (a *App) describeFiles() {
	if !a.requireFilePanel() {
		return
	}

	if a.remoteUnsupported() {
		return
	}
	if a.archiveReadOnly() {
		return
	}
	p := &a.Panels[a.Active]
	paths := p.Selected()
	if len(paths) == 0 {
		return
	}
	root := p.Path
	filename := a.Settings.DescriptionFile
	a.prompt("Describe files", "Description (empty removes it)", p.Descriptions[filepath.Base(paths[0])], func(s string) {
		a.workProgress("Updating descriptions", func(o *fsops.Operation) error {
			descriptions := readDescriptions(filepath.Join(root, filename), o)
			for _, path := range paths {
				if err := o.Check(); err != nil {
					return err
				}
				if s == "" {
					delete(descriptions, filepath.Base(path))
				} else {
					descriptions[filepath.Base(path)] = s
				}
			}
			keys := make([]string, 0, len(descriptions))
			for k := range descriptions {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var b strings.Builder
			for _, k := range keys {
				if err := o.Check(); err != nil {
					return err
				}
				if strings.ContainsAny(k, "\n\r\"") {
					return fmt.Errorf("this filename cannot be stored in descript.ion")
				}
				name := k
				if strings.ContainsAny(k, " \t") {
					name = "\"" + k + "\""
				}
				fmt.Fprintf(&b, "%s %s\n", name, descriptions[k])
			}
			return writeConfigFile(filepath.Join(root, filename), []byte(b.String()), o)
		})
	})
}
func writeConfigFile(path string, data []byte, ops ...*fsops.Operation) error {
	var o *fsops.Operation
	if len(ops) > 0 {
		o = ops[0]
	}
	if err := o.Check(); err != nil {
		return err
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".farshmac-config-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = io.Copy(f, o.Reader(bytes.NewReader(data)))
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if err := o.Check(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (a *App) setView(index int, view string) {
	// Quick view only previews the other panel, so it works for remote
	// panels like Ctrl+Q; Tree and Info panel need a local folder.
	if a.Panels[index].Remote != nil && (view == "Tree panel" || view == "Info panel") {
		a.Status = "Choose a file view for remote panels"
		return
	}
	p := &a.Panels[index]
	if p.Archive != nil && view == "Tree panel" {
		a.Status = "Tree view is unavailable inside archives"
		return
	}
	p.View = view
	p.Hidden = false
	if p.Remote == nil || view != "Quick view" {
		a.error(p.Load(p.Path))
	}
	a.previewKey = ""
}
func (a *App) volumes(index int) {
	paths := []string{"/"}
	home, _ := os.UserHomeDir()
	paths = append(paths, home)
	items, _ := os.ReadDir("/Volumes")
	for _, e := range items {
		paths = append(paths, filepath.Join("/Volumes", e.Name()))
	}
	a.choose("Volumes and home", paths, func(i int) { a.openVolume(index, paths[i]) })
}

// openVolume switches a panel to a local volume. The volume is probed before
// the panel leaves its current source, so an unmounted or unreadable volume
// keeps a remote/archive panel intact. A hidden panel is shown first.
func (a *App) openVolume(index int, path string) {
	f, err := os.Open(path)
	if err == nil {
		_, err = f.ReadDir(1)
		f.Close()
		if err == io.EOF {
			err = nil
		}
	}
	if err != nil {
		a.error(err)
		return
	}
	p := &a.Panels[index]
	p.Hidden = false
	a.Active = index
	p.cancelPanelLoad()
	p.cancelTree()
	previous := *p
	p.Remote, p.RemoteLocalPath, p.Archive = nil, "", nil
	a.error(p.loadSource(path, previous, ""))
}
func (a *App) folderShortcuts() {
	names := []string{"Save current folder…"}
	for i := 1; i <= 9; i++ {
		p := a.Settings.Shortcuts[i]
		if c := a.Settings.ShortcutRemotes[i]; c != nil {
			p = c.Label() + p
		}
		names = append(names, strconv.Itoa(i)+"  "+p)
	}
	a.choose("Folder shortcuts", names, func(i int) {
		if i > 0 {
			a.goFolderShortcut(i)
			return
		}
		a.prompt("Save folder shortcut", "Slot 1–9", "1", func(s string) {
			i, e := strconv.Atoi(s)
			if e != nil || i < 1 || i > 9 {
				a.Status = "Choose 1–9"
				return
			}
			a.saveFolderShortcut(i)
		})
	})
}
