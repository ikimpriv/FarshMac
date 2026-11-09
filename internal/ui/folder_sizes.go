package ui

import (
	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

type folderSizeResult struct {
	Stale                   bool
	Bytes                   int64
	Files, Dirs, Unreadable int
}

func (p *Panel) entrySize(e Entry) int64 {
	if e.Dir {
		if s, ok := p.folderSizes[e.Name]; ok {
			return s.Bytes
		}
		return 0
	}
	if e.Info != nil {
		return e.Info.Size()
	}
	return 0
}
func (a *App) folderSize() { a.calculateFolderSizes(false) }
func (a *App) calculateFolderSizes(all bool) {
	if a.Busy || a.closed {
		return
	}
	index := a.Active
	p := &a.Panels[index]
	base := p.Path
	remote, archive := p.Remote, p.Archive
	var config remotefs.Config
	if remote != nil {
		config = *remote
	}
	var names []string
	for _, e := range p.Entries {
		if e.Dir && e.Name != ".." && (all || p.Marked[e.Name]) {
			names = append(names, e.Name)
		}
	}
	if len(names) == 0 && !all && len(p.Entries) > 0 && p.Entries[p.Cursor].Dir {
		names = []string{p.Entries[p.Cursor].Name}
	}
	if len(names) == 0 {
		return
	}
	a.backgroundProgress("Calculating folder sizes", func(o *fsops.Operation) (func(), error) {
		walk := fsops.WalkDir
		if archive != nil {
			if err := archive.Check(); err != nil {
				return nil, err
			}
			walk = func(root string, o *fsops.Operation, visit fs.WalkDirFunc) error {
				return walkArchiveSizes(archive, root, o, visit)
			}
		} else if remote != nil {
			client, err := config.Dial(o.Context())
			if err != nil {
				return nil, err
			}
			defer client.Close()
			walk = func(root string, o *fsops.Operation, visit fs.WalkDirFunc) error {
				return walkRemoteSizes(client, root, o, visit, 0, nil)
			}
		}
		workers := min(4, len(names))
		if remote != nil {
			workers = 1
		} // A remote protocol session is owned by one worker.
		results, stopped := measureFolders(base, names, o, walk, workers)
		return func() {
			p := &a.Panels[index]
			if p.Path != base || p.Remote != remote || p.Archive != archive {
				return
			}
			cache := make(map[string]folderSizeResult, len(p.folderSizes)+len(results))
			for name, r := range p.folderSizes {
				cache[name] = r
			}
			unreadable := 0
			for name, r := range results {
				cache[name] = r
				unreadable += r.Unreadable
			}
			p.folderSizes = cache
			state := p.snapshot()
			p.sortEntries(p.Entries)
			for i, e := range p.Entries {
				if e.Name == state.CursorName {
					p.Cursor = i
				}
				if e.Name == state.TopName {
					p.Offset = i
				}
			}
			a.Status = fmt.Sprintf("Calculated %d folders", len(results))
			if unreadable > 0 {
				a.Status += fmt.Sprintf("; %d unreadable entries (>= means partial size)", unreadable)
			}
			if stopped != nil {
				a.Status += " · " + stopped.Error()
			}
		}, nil
	})
	if a.task != nil {
		a.task.applyCompleted = true
	}
}

func walkArchiveSizes(a *fsops.ArchiveIndex, root string, o *fsops.Operation, visit fs.WalkDirFunc) error {
	rel, ok := a.Relative(root)
	if !ok {
		return fmt.Errorf("outside archive")
	}
	var walk func(string) error
	walk = func(name string) error {
		if err := o.Check(); err != nil {
			return err
		}
		m := a.Members[name]
		if name == "." {
			m = fsops.ArchiveMember{Path: ".", FileMode: os.ModeDir}
		}
		if o.Ignored(m.Name()) {
			return nil
		}
		path := filepath.Join(a.Path, name)
		if err := visit(path, fs.FileInfoToDirEntry(m), nil); err != nil {
			return err
		}
		for _, child := range a.Children[name] {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(rel)
}
func walkRemoteSizes(f remotefs.FS, root string, o *fsops.Operation, visit fs.WalkDirFunc, depth int, info os.FileInfo) error {
	if err := o.Check(); err != nil {
		return err
	}
	if o.Ignored(root) {
		return nil
	}
	if depth > 128 {
		return visit(root, nil, fmt.Errorf("remote directory depth exceeds 128"))
	}
	var err error
	if info == nil {
		info, err = f.Lstat(root)
	}
	if err != nil {
		if e := o.Check(); e != nil {
			return e
		}
		return visit(root, nil, err)
	}
	d := fs.FileInfoToDirEntry(info)
	if err = visit(root, d, nil); err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	entries, err := f.ReadDir(root)
	if err != nil {
		if e := o.Check(); e != nil {
			return e
		}
		return visit(root, d, err)
	}
	if len(entries) > 100000 {
		return visit(root, d, fmt.Errorf("remote folder exceeds 100000 entries"))
	}
	for _, e := range entries {
		if e.Name() == "." || e.Name() == ".." {
			continue
		}
		if !remotefs.ValidName(e.Name()) {
			return fmt.Errorf("server returned an unsafe filename")
		}
		if err = walkRemoteSizes(f, filepath.Join(root, e.Name()), o, visit, depth+1, e); err != nil {
			return err
		}
	}
	return o.Check()
}

type sizeWalker func(string, *fsops.Operation, fs.WalkDirFunc) error

func measureFolder(root string, o *fsops.Operation, walk sizeWalker) (r folderSizeResult, err error) {
	var items, bytes int64
	last := root
	flush := func() { o.ScanProgress(last, items, bytes); items, bytes = 0, 0 }
	defer flush()
	err = walk(root, o, func(path string, d fs.DirEntry, e error) error {
		items++
		last = path
		if items >= 64 {
			flush()
		}
		if e != nil {
			r.Unreadable++
			return nil
		}
		if d.IsDir() {
			r.Dirs++
			return nil
		}
		info, e := d.Info()
		if e != nil {
			r.Unreadable++
			return nil
		}
		r.Files++
		r.Bytes += info.Size()
		bytes += info.Size()
		return nil
	})
	return
}
func measureFolders(base string, names []string, o *fsops.Operation, walk sizeWalker, workers int) (map[string]folderSizeResult, error) {
	type measured struct {
		name string
		size folderSizeResult
		err  error
	}
	workers = max(1, min(workers, len(names)))
	jobs := make(chan string)
	done := make(chan measured, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				if o.Check() != nil {
					return
				}
				root := filepath.Join(base, name)
				if name == ".." {
					root = base
				}
				size, err := measureFolder(root, o, walk)
				done <- measured{name, size, err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, name := range names {
			select {
			case jobs <- name:
			case <-o.Context().Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(done) }()
	results := make(map[string]folderSizeResult, len(names))
	var stopped error
	for v := range done {
		if v.err != nil {
			if stopped == nil {
				stopped = v.err
			}
			continue
		}
		results[v.name] = v.size
	}
	if err := o.Check(); err != nil {
		stopped = err
	}
	return results, stopped
}
