package ui

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"farshmac/internal/fsops"
	"farshmac/internal/podtoast"
	"farshmac/internal/remotefs"
)

// browseStrategy provides operation-scoped access to a real or virtual tree.
// Boundary policy belongs to the source, not to the search/tree UI. A boundary
// remains a selectable result but its children must never be requested.
type browseStrategy interface {
	ReadDir(string, func(Entry) error) error
	Open(string) (io.ReadCloser, error)
	Descend(string, Entry) bool
	Close() error
}

func (p *Panel) browse(o *fsops.Operation) (browseStrategy, error) { return p.backend().Browse(p, o) }
func (localBackend) Browse(p *Panel, o *fsops.Operation) (browseStrategy, error) {
	return newLocalBrowse(p.Path, o)
}
func (archiveBackend) Browse(p *Panel, o *fsops.Operation) (browseStrategy, error) {
	return &archiveBrowse{p.Archive, o}, nil
}
func (remoteBackend) Browse(p *Panel, o *fsops.Operation) (browseStrategy, error) {
	f, err := p.endpoint().Open(o.Context())
	if err != nil {
		return nil, err
	}
	return &remoteBrowse{f, o}, nil
}
func (kubeBackend) Browse(p *Panel, o *fsops.Operation) (browseStrategy, error) {
	if p.endpoint().Opener != nil {
		return remoteBackend{}.Browse(p, o)
	}
	return &kubeBrowse{k: p.Kube, o: o}, nil
}
func (dockerBackend) Browse(p *Panel, o *fsops.Operation) (browseStrategy, error) {
	if p.endpoint().Opener != nil {
		return remoteBackend{}.Browse(p, o)
	}
	return &dockerBrowse{d: p.Docker, o: o}, nil
}

type localBrowse struct {
	o          *fsops.Operation
	boundaries map[string]bool
}

func (b *localBrowse) ReadDir(dir string, visit func(Entry) error) error {
	return fsops.ReadDirectory(dir, b.o, func(d fs.DirEntry) error {
		full := filepath.Join(dir, d.Name())
		// Do not stat a network mount: even metadata access may block on a server.
		if b.boundaries[full] {
			return visit(Entry{Name: d.Name(), Dir: true})
		}
		var info os.FileInfo = browseTypeInfo{d.Name(), d.Type()}
		if d.IsDir() {
			var err error
			info, err = d.Info()
			if err != nil {
				return err
			}
		}
		return visit(Entry{Name: d.Name(), Dir: d.IsDir(), Info: info})
	})
}
func (b *localBrowse) Open(path string) (io.ReadCloser, error) {
	if err := b.o.Check(); err != nil {
		return nil, err
	}
	return os.Open(path)
}
func (b *localBrowse) Descend(path string, e Entry) bool {
	return e.Dir && !b.boundaries[filepath.Clean(path)] && (e.Info == nil || e.Info.Mode()&os.ModeSymlink == 0)
}
func (b *localBrowse) Close() error { return nil }

type remoteBrowse struct {
	remotefs.FS
	o *fsops.Operation
}

func (b *remoteBrowse) ReadDir(dir string, visit func(Entry) error) error {
	if err := b.o.Check(); err != nil {
		return err
	}
	list, err := b.FS.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(list) > 100000 {
		return fmt.Errorf("directory exceeds 100,000 entries")
	}
	for _, info := range list {
		if err := b.o.Check(); err != nil {
			return err
		}
		if info.Name() == "." || info.Name() == ".." {
			continue
		}
		if !remotefs.ValidName(info.Name()) {
			return fmt.Errorf("unsafe remote filename")
		}
		if b.o.Ignored(info.Name()) {
			continue
		}
		if err := visit(Entry{Name: info.Name(), Dir: info.IsDir(), Info: info}); err != nil {
			return err
		}
	}
	return nil
}
func (b *remoteBrowse) Descend(_ string, e Entry) bool {
	if info, ok := e.Info.(podtoast.Entry); ok && info.MountPoint && info.Mount != nil && networkFilesystem(info.Mount.Filesystem) {
		return false
	}
	return e.Dir && (e.Info == nil || e.Info.Mode()&os.ModeSymlink == 0)
}

type archiveBrowse struct {
	index *fsops.ArchiveIndex
	o     *fsops.Operation
}

func (b *archiveBrowse) ReadDir(dir string, visit func(Entry) error) error {
	rel, ok := b.index.Relative(dir)
	if !ok {
		return fmt.Errorf("outside archive")
	}
	for _, name := range b.index.Children[rel] {
		if err := b.o.Check(); err != nil {
			return err
		}
		m := b.index.Members[name]
		if b.o.Ignored(m.Name()) {
			continue
		}
		if err := visit(Entry{Name: m.Name(), Dir: m.IsDir(), Info: m}); err != nil {
			return err
		}
	}
	return nil
}
func (b *archiveBrowse) Open(string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("content search inside archives is not supported; extract first")
}
func (b *archiveBrowse) Descend(_ string, e Entry) bool {
	return e.Dir && (e.Info == nil || e.Info.Mode()&os.ModeSymlink == 0)
}
func (b *archiveBrowse) Close() error { return nil }

type browseOptions struct {
	Recursive            bool
	MaxDepth, MaxEntries int
}
type browseStats struct {
	Visited, Skipped, Boundaries int
	Limited                      bool
}

var errBrowseLimit = errors.New("browse limit")

// walkBrowse shares the same boundary and budget semantics across search and
// tree views. No symlink traversal or implicit entry into another source.
func walkBrowse(root string, b browseStrategy, o *fsops.Operation, opts browseOptions, visit func(string, Entry) error) (browseStats, error) {
	var stats browseStats
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 64
	}
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = 1000000
	}
	var walk func(string, int) error
	walk = func(dir string, depth int) error {
		if err := o.Check(); err != nil {
			return err
		}
		err := b.ReadDir(dir, func(e Entry) error {
			if err := o.Check(); err != nil {
				return err
			}
			if e.Name == "." || e.Name == ".." || strings.Contains(e.Name, "/") {
				return fmt.Errorf("unsafe tree entry")
			}
			stats.Visited++
			if stats.Visited > opts.MaxEntries {
				stats.Limited = true
				return errBrowseLimit
			}
			name := filepath.Join(dir, e.Name)
			o.Add(name, 0)
			if err := visit(name, e); err != nil {
				if errors.Is(err, fs.SkipDir) {
					return nil
				}
				return err
			}
			if e.Dir && opts.Recursive {
				if !b.Descend(name, e) {
					stats.Boundaries++
					return nil
				}
				if depth >= opts.MaxDepth {
					stats.Limited = true
					return nil
				}
				return walk(name, depth+1)
			}
			return nil
		})
		if err != nil && o.Check() == nil && !errors.Is(err, fs.SkipAll) && !errors.Is(err, errBrowseLimit) {
			if dir == root {
				return err
			}
			stats.Skipped++
			return nil
		}
		return err
	}
	err := walk(root, 1)
	if errors.Is(err, fs.SkipAll) || errors.Is(err, errBrowseLimit) {
		err = nil
	}
	return stats, err
}

func networkFilesystem(name string) bool {
	switch strings.ToLower(name) {
	case "nfs", "nfs4", "cifs", "smbfs", "smb3", "sshfs", "fuse.sshfs", "9p", "ceph", "glusterfs", "fuse.glusterfs":
		return true
	}
	return false
}

// Files only need their type during traversal. Avoid a stat per ordinary file;
// content search opens only matching regular files.
type browseTypeInfo struct {
	name string
	mode os.FileMode
}

func (i browseTypeInfo) Name() string       { return i.name }
func (i browseTypeInfo) Mode() os.FileMode  { return i.mode }
func (i browseTypeInfo) Size() int64        { return 0 }
func (i browseTypeInfo) ModTime() time.Time { return time.Time{} }
func (i browseTypeInfo) IsDir() bool        { return i.mode.IsDir() }
func (i browseTypeInfo) Sys() any           { return nil }
