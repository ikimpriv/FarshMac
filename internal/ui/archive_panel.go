package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"farshmac/internal/fsops"
)

func (p *Panel) filesystemPath() string {
	if p.Remote != nil {
		return p.RemoteLocalPath
	}
	if p.Archive != nil {
		return filepath.Dir(p.Archive.Path)
	}
	return p.Path
}
func (p *Panel) loadArchiveDirectory(path string) error {
	rel, ok := p.Archive.Relative(path)
	if !ok {
		return fmt.Errorf("outside archive")
	}
	if rel != "." {
		m, ok := p.Archive.Members[rel]
		if !ok || !m.IsDir() {
			return fmt.Errorf("not an archive directory")
		}
	}
	if err := p.Archive.Check(); err != nil {
		return err
	}
	var files []Entry
	var size int64
	for _, name := range p.Archive.Children[rel] {
		if err := p.readOperation.Check(); err != nil {
			return err
		}
		m := p.Archive.Members[name]
		if p.IgnoreDSStore && m.Name() == ".DS_Store" || p.HideHidden && strings.HasPrefix(m.Name(), ".") {
			continue
		}
		if !m.IsDir() && !matchesMask(m.Name(), p.activeFilter(), p.MaskGroups) {
			continue
		}
		files = append(files, Entry{Name: m.Name(), Info: m, Dir: m.IsDir()})
		size += m.Size()
	}
	state := p.beginLoad(path)
	p.cancelPanelLoad()
	p.cancelTree()
	p.sortEntries(files)
	p.installEntries(files, state)
	p.TotalBytes = size
	p.ItemCount = len(files)
	p.Descriptions = nil
	p.InfoText = "Archive (read-only)\nF5 extracts selected files/folders\n.. exits the archive"
	return nil
}
func (a *App) openArchive(index int, path string) {
	target := path
	if p := &a.Panels[index]; p.Archive != nil && p.Archive.Path == path {
		target = p.Path
	}
	a.openArchiveAt(index, path, target)
}
func (a *App) openArchiveAt(index int, path, target string) {
	a.backgroundTask("Opening archive", func(o *fsops.Operation) (func(), func(), error) {
		catalog, err := fsops.ReadArchiveIndex(path, o)
		return func() {
			p := &a.Panels[index]
			p.cancelPanelLoad()
			p.cancelTree()
			p.Remote = nil
			p.Archive = catalog
			// Archive directories use file views, never a native filesystem tree.
			if p.View == "Tree panel" || p.View == "Info panel" || p.View == "Quick view" {
				p.View = "Full"
			}
			rel, inside := catalog.Relative(target)
			member, found := catalog.Members[rel]
			if !inside || rel != "." && (!found || !member.IsDir()) {
				target = catalog.Path
			}
			a.error(p.Load(target))
		}, nil, err
	})
}
func (a *App) archiveReadOnly() bool {
	if a.Panels[a.Active].Archive == nil {
		return false
	}
	a.Status = "Archive is read-only. Use F5 to copy files out."
	return true
}
func (a *App) enterArchiveMember() {
	p := &a.Panels[a.Active]
	if len(p.Entries) == 0 {
		return
	}
	e := p.Entries[p.Cursor]
	if e.Dir {
		a.error(p.Load(p.Current()))
		return
	}
	if fsops.ArchiveFormat(e.Name) != "" {
		a.Status = "Copy the nested archive out with F5 to open it"
		return
	}
	a.viewArchiveMember()
}
func (a *App) viewArchiveMember() {
	p := &a.Panels[a.Active]
	if len(p.Entries) == 0 || p.Entries[p.Cursor].Dir {
		return
	}
	catalog, virtual := p.Archive, p.Current()
	name, ok := catalog.Relative(virtual)
	if !ok {
		return
	}
	if catalog.Members[name].Mode()&os.ModeSymlink != 0 {
		a.Status = "Use F5 to copy the archive link"
		return
	}
	settings := a.Settings
	a.backgroundTask("Opening archive member", func(o *fsops.Operation) (func(), func(), error) {
		temp, err := os.MkdirTemp("", "farshmac-archive-view-*")
		if err != nil {
			return nil, nil, err
		}
		retained := false
		var doc *Document
		cleanup := func() {
			if !retained {
				if doc != nil && doc.File != nil {
					doc.File.Close()
				}
				os.RemoveAll(temp)
			}
		}
		content := filepath.Join(temp, "content")
		if err = catalog.ExtractSelection([]string{name}, content, o); err != nil {
			return nil, cleanup, err
		}
		worker := &App{Settings: settings, Operation: o}
		worker.openDocument(filepath.Join(content, name), false)
		doc = worker.Doc
		if doc == nil {
			return nil, cleanup, fmt.Errorf("%s", worker.Status)
		}
		doc.Path = virtual
		doc.ReadOnly = true
		doc.ArchiveSource = true
		doc.TempDir = temp
		doc.ScreenID = 0
		return func() { retained = true; a.showDocument(doc) }, cleanup, nil
	})
}
func (a *App) matchOtherPanel() {
	src := &a.Panels[a.Active]
	dst := &a.Panels[1-a.Active]
	dst.cancelPanelLoad()
	dst.cancelTree()
	previous := *dst
	dst.Remote, dst.RemoteLocalPath = src.Remote, src.RemoteLocalPath
	dst.Archive = src.Archive
	if dst.View == "Tree panel" || dst.View == "Info panel" || dst.View == "Quick view" {
		dst.View = "Full"
	}
	if err := dst.loadSource(src.Path, previous, "Other panel kept: "); err != nil {
		a.Status = "Other panel kept: " + err.Error()
		return
	}
	if len(src.Entries) > 0 {
		name := src.Entries[src.Cursor].Name
		if !dst.pendingPosition(name, "") {
			for i, e := range dst.Entries {
				if e.Name == name {
					dst.Cursor = i
					break
				}
			}
		}
	}
	dst.Hidden = false
	a.previewKey = ""
}
func (a *App) togglePanel(index int) {
	a.Panels[index].Hidden = !a.Panels[index].Hidden
	if a.Panels[index].Hidden && a.Active == index && !a.Panels[1-index].Hidden {
		a.Active = 1 - index
	}
	// A read abandoned while hidden would otherwise strand the panel with
	// only ".." and "Reading folder…" in its footer.
	if p := &a.Panels[index]; !p.Hidden && p.load == nil && p.tree == nil && strings.HasPrefix(p.Err, "Reading ") {
		a.error(p.Load(p.Path))
	}
}

func (a *App) rereadPanel(index int) {
	p := &a.Panels[index]
	a.infoKeys = [2]string{}
	if p.Archive != nil {
		a.openArchive(index, p.Archive.Path)
	} else {
		a.error(p.Load(p.Path))
	}
}
