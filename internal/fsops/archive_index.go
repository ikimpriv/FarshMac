package fsops

import (
	"archive/tar"
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ArchiveIndex is immutable after construction. Only metadata is retained;
// compressed payloads stay in the original file until an extraction is requested.
type ArchiveIndex struct {
	Path     string
	identity os.FileInfo
	Members  map[string]ArchiveMember
	Children map[string][]string
}
type ArchiveMember struct {
	Path     string
	Bytes    int64
	FileMode os.FileMode
	Modified time.Time
	Link     string
	Hardlink bool
}

func (m ArchiveMember) Name() string       { return filepath.Base(m.Path) }
func (m ArchiveMember) Size() int64        { return m.Bytes }
func (m ArchiveMember) Mode() os.FileMode  { return m.FileMode }
func (m ArchiveMember) ModTime() time.Time { return m.Modified }
func (m ArchiveMember) IsDir() bool        { return m.FileMode.IsDir() }
func (m ArchiveMember) Sys() any           { return nil }
func (a *ArchiveIndex) Check() error {
	info, err := os.Stat(a.Path)
	if err != nil {
		return err
	}
	if !os.SameFile(info, a.identity) || info.Size() != a.identity.Size() || !info.ModTime().Equal(a.identity.ModTime()) {
		return fmt.Errorf("archive changed on disk; leave and reopen it")
	}
	return nil
}
func (a *ArchiveIndex) Relative(path string) (string, bool) {
	rel, err := filepath.Rel(a.Path, filepath.Clean(path))
	return rel, err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// ErrEncryptedArchive: archives are never opened with a password prompt.
var ErrEncryptedArchive = errors.New("encrypted archives are not supported")

const zipEncrypted = 0x1 // General-purpose flag bit 0.

func ReadArchiveIndex(src string, o *Operation) (*ArchiveIndex, error) {
	src, err := filepath.Abs(src)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular archive")
	}
	a := &ArchiveIndex{Path: src, identity: info, Members: map[string]ArchiveMember{}, Children: map[string][]string{}}
	budget := 0
	var add func(ArchiveMember, bool) error
	add = func(m ArchiveMember, real bool) error {
		if err := o.Check(); err != nil {
			return err
		}
		name, e := safeArchiveName(m.Path)
		if e != nil {
			return e
		}
		if name == "." {
			if m.IsDir() {
				return nil
			}
			return fmt.Errorf("invalid archive root")
		}
		if len(name) > 4096 || strings.Count(name, "/") > 64 {
			return fmt.Errorf("archive path too long/deep")
		}
		m.Path = name
		if old, ok := a.Members[name]; ok {
			if !old.IsDir() || !m.IsDir() {
				return fmt.Errorf("duplicate/conflicting archive entry: %s", name)
			}
			if real {
				a.Members[name] = m
			}
			return nil
		}
		if len(a.Members) >= archiveEntryLimit || budget+len(name)+len(m.Link) > 16<<20 {
			return fmt.Errorf("archive index limit (100,000 entries / 16 MiB names)")
		}
		parent := filepath.Dir(name)
		if parent != "." {
			if old, ok := a.Members[parent]; ok {
				if !old.IsDir() {
					return fmt.Errorf("archive parent is not a directory: %s", parent)
				}
			} else {
				if e := add(ArchiveMember{Path: parent, FileMode: os.ModeDir | 0755, Modified: info.ModTime()}, false); e != nil {
					return e
				}
			}
		}
		if len(a.Members) >= archiveEntryLimit || budget+len(name)+len(m.Link) > 16<<20 {
			return fmt.Errorf("archive index limit")
		}
		budget += len(name) + len(m.Link)
		a.Members[name] = m
		a.Children[parent] = append(a.Children[parent], name)
		return nil
	}
	o.Streaming()
	if ArchiveFormat(src) == "zip" {
		f, e := os.Open(src)
		if e != nil {
			return nil, e
		}
		defer f.Close()
		z, e := zip.NewReader(archiveCheckedReaderAt{f, o}, info.Size())
		if e != nil {
			return nil, e
		}
		if len(z.File) > archiveEntryLimit {
			return nil, fmt.Errorf("archive exceeds %d entries", archiveEntryLimit)
		}
		for _, f := range z.File {
			if f.Flags&zipEncrypted != 0 {
				return nil, fmt.Errorf("%w: %s", ErrEncryptedArchive, src)
			}
			if err = add(ArchiveMember{Path: f.Name, Bytes: f.FileInfo().Size(), FileMode: f.Mode(), Modified: f.Modified}, true); err != nil {
				return nil, err
			}
			o.Add(f.Name, 0)
			o.Begin(f.Name, 0)
			o.Complete(f.Name)
		}
	} else {
		err = withArchive(src, o, func(r *tar.Reader) error {
			for count := 0; ; count++ {
				if e := o.Check(); e != nil {
					return e
				}
				h, e := r.Next()
				if e == io.EOF {
					return nil
				}
				if e != nil {
					return e
				}
				if count >= archiveEntryLimit {
					return fmt.Errorf("archive entry limit")
				}
				switch h.Typeflag {
				case tar.TypeDir, tar.TypeReg, tar.TypeRegA, tar.TypeSymlink, tar.TypeLink:
				default:
					return fmt.Errorf("unsupported archive entry: %s", h.Name)
				}
				m := ArchiveMember{Path: h.Name, Bytes: h.Size, FileMode: h.FileInfo().Mode(), Modified: h.ModTime, Link: h.Linkname, Hardlink: h.Typeflag == tar.TypeLink}
				if e = add(m, true); e != nil {
					return e
				}
				o.Add(h.Name, h.Size)
				o.Begin(h.Name, h.Size)
				if _, e = copyProgress(io.Discard, r, o); e != nil {
					return e
				}
				o.Complete(h.Name)
			}
		})
		if err != nil {
			return nil, err
		}
	}
	for _, names := range a.Children {
		if err = o.Check(); err != nil {
			return nil, err
		}
		sort.Strings(names)
	}
	if err = a.Check(); err != nil {
		return nil, err
	}
	o.Ready()
	return a, nil
}

// ExtractSelection extracts selected subtrees (plus hard-link dependencies)
// into a new private destination. The caller publishes only the requested roots.
func (a *ArchiveIndex) ExtractSelection(names []string, dst string, o *Operation) error {
	if err := a.Check(); err != nil {
		return err
	}
	selected := map[string]bool{}
	for _, name := range names {
		clean, err := safeArchiveName(name)
		if err != nil {
			return err
		}
		if _, ok := a.Members[clean]; !ok {
			return fmt.Errorf("archive entry not found: %s", name)
		}
		selected[clean] = true
	}
	if len(selected) == 0 {
		return fmt.Errorf("no archive entries selected")
	}
	include := func(name string) bool {
		for p := name; p != "."; p = filepath.Dir(p) {
			if selected[p] {
				return true
			}
		}
		return false
	}
	// Resolve dependencies once, using the bounded index, not repeated full scans.
	var depend func(string, map[string]bool) error
	depend = func(name string, visiting map[string]bool) error {
		if err := o.Check(); err != nil {
			return err
		}
		m, ok := a.Members[name]
		if !ok {
			return fmt.Errorf("missing hard-link target: %s", name)
		}
		if !m.Hardlink {
			return nil
		}
		if visiting[name] || len(visiting) > 64 {
			return fmt.Errorf("cyclic archive hard link")
		}
		visiting[name] = true
		target, err := safeArchiveName(m.Link)
		if err != nil {
			return err
		}
		selected[target] = true
		err = depend(target, visiting)
		delete(visiting, name)
		return err
	}
	for name, m := range a.Members {
		if m.Hardlink && include(name) {
			if err := depend(name, map[string]bool{}); err != nil {
				return err
			}
		}
	}
	if err := extractArchive(a.Path, dst, include, o); err != nil {
		return err
	}
	if err := a.Check(); err != nil {
		os.RemoveAll(dst)
		return err
	}
	return nil
}
