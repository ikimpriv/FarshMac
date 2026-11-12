package fsops

import (
	"crypto/rand"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// SymlinkMode selects how local copy and move treat symbolic links.
type SymlinkMode int

const (
	// SymlinksSmart keeps every link pointing at the same object: broken and
	// absolute links, and links into the transferred items, are written as is;
	// a relative link to something outside them is rewritten for its new place.
	SymlinksSmart SymlinkMode = iota
	// SymlinksAsIs writes the link text unchanged.
	SymlinksAsIs
	// SymlinksContents copies what the link points to instead of the link.
	SymlinksContents
)

func (m SymlinkMode) String() string {
	switch m {
	case SymlinksAsIs:
		return "As is"
	case SymlinksContents:
		return "Copy contents"
	}
	return "Smart"
}

// ParseSymlinkMode accepts the String form; anything else is Smart.
func ParseSymlinkMode(s string) SymlinkMode {
	for _, m := range []SymlinkMode{SymlinksAsIs, SymlinksContents} {
		if strings.EqualFold(s, m.String()) {
			return m
		}
	}
	return SymlinksSmart
}

func (o *Operation) symlinkMode() SymlinkMode {
	if o == nil {
		return SymlinksAsIs
	}
	return o.Symlinks
}

// TransferRoots records the items selected for this operation. Smart mode
// writes links whose target lies inside them verbatim, since the relative
// structure between them is preserved. Call before starting workers.
func (o *Operation) TransferRoots(paths []string) {
	if o == nil {
		return
	}
	o.roots = make([]string, 0, len(paths))
	for _, path := range paths {
		o.roots = append(o.roots, physicalPath(path))
	}
}

// ensureRoots makes a direct Copy/Transfer of one item its own transferred set.
func (o *Operation) ensureRoots(src string) {
	if o != nil && o.roots == nil {
		o.TransferRoots([]string{src})
	}
}

// physicalPath resolves the parent folder like the kernel does when it
// follows a relative link, keeping the last element itself unresolved.
func physicalPath(path string) string {
	path, _ = filepath.Abs(path)
	if parent, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(parent, filepath.Base(path))
	}
	return path
}

func physicalDir(dir string) string {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real
	}
	return filepath.Clean(dir)
}

func within(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator)) || root == string(filepath.Separator)
}

// smartLinkText returns the text for a link that was in the physical folder
// origin and is now placed in the physical folder parent. The target is kept
// lexical (intermediate links in the link text stay links).
func smartLinkText(text, origin, parent string, roots []string, sameVolume bool, exists func(string) bool) string {
	if text == "" || filepath.IsAbs(text) || origin == parent {
		return text
	}
	target := filepath.Join(origin, text)
	for _, root := range roots {
		if within(target, root) {
			return text
		}
	}
	if exists != nil && !exists(target) {
		return text // Broken: nothing to keep pointing at.
	}
	if !sameVolume {
		return target
	}
	rel, err := filepath.Rel(parent, target)
	if err != nil {
		return target
	}
	return rel
}

func pathExists(path string) bool { _, err := os.Stat(path); return err == nil }

func sameDevice(a, b string) bool {
	var x, y syscall.Stat_t
	if syscall.Stat(a, &x) != nil || syscall.Stat(b, &y) != nil {
		return false
	}
	return x.Dev == y.Dev
}

// copiedLinkText is the text for a copy at dst of the link src (whose text is
// text). dst may be inside a staging folder; the published path counts.
func (o *Operation) copiedLinkText(src, dst, text string) string {
	if o.symlinkMode() != SymlinksSmart || text == "" || filepath.IsAbs(text) {
		return text
	}
	origin := physicalDir(filepath.Dir(src))
	parent := physicalDir(filepath.Dir(o.publishedPath(dst)))
	return smartLinkText(text, origin, parent, o.roots, sameDevice(origin, parent), pathExists)
}

// publishedPath maps a path inside the current staging copy to its final name.
func (o *Operation) publishedPath(path string) string {
	if o == nil || o.stagedFrom == "" {
		return path
	}
	if path == o.stagedFrom || strings.HasPrefix(path, o.stagedFrom+string(filepath.Separator)) {
		return o.stagedTo + strings.TrimPrefix(path, o.stagedFrom)
	}
	return path
}

// copyStaged copies src to the staging path tmp that will be renamed to dst.
func (o *Operation) copyStaged(src, tmp, dst string) error {
	if o == nil {
		return Copy(src, tmp)
	}
	from, to := o.stagedFrom, o.stagedTo
	o.stagedFrom, o.stagedTo = tmp, dst
	defer func() { o.stagedFrom, o.stagedTo = from, to }()
	return Copy(src, tmp, o)
}

// copyLinkContents copies what the link src points to. A broken link or a
// link to one of its own ancestors is an error for this item only.
func (o *Operation) copyLinkContents(src, dst string) error {
	text, _ := os.Readlink(src)
	real, err := filepath.EvalSymlinks(src)
	if err != nil {
		return fmt.Errorf("broken symbolic link %s → %s: %w", src, text, err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if within(physicalDir(filepath.Dir(src)), real) || o.dereferencing(real) {
			return fmt.Errorf("symbolic link loop: %s → %s", src, text)
		}
		o.followed = append(o.followed, real)
		defer func() { o.followed = o.followed[:len(o.followed)-1] }()
	}
	if err = copyEntry(real, dst, o); err == nil {
		o.Complete(src)
	}
	return err
}

func (o *Operation) dereferencing(real string) bool {
	for _, path := range o.followed {
		if path == real {
			return true
		}
	}
	return false
}

// relinkMoved keeps links valid after src was renamed to dst on one volume:
// the moved link itself and, for a folder, links inside it pointing outside.
// In Copy contents mode such links are replaced by copies of their targets.
func (o *Operation) relinkMoved(src, dst string, info os.FileInfo) error {
	mode := o.symlinkMode()
	if mode == SymlinksAsIs || !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	origin, parent := physicalDir(filepath.Dir(src)), physicalDir(filepath.Dir(dst))
	if origin == parent {
		return nil // Renamed in place: every relative link still resolves.
	}
	if !info.IsDir() {
		return o.relinkOne(dst, origin, parent, mode)
	}
	oldRoot, newRoot := filepath.Join(origin, filepath.Base(src)), filepath.Join(parent, filepath.Base(dst))
	return WalkDir(dst, o, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return IgnoreSkipped(o.Attempt("read folder", path, "", func() error { return err }))
		}
		if d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		rel, _ := filepath.Rel(dst, filepath.Dir(path))
		return o.relinkOne(path, filepath.Join(oldRoot, rel), filepath.Join(newRoot, rel), mode)
	})
}

func (o *Operation) relinkOne(path, origin, parent string, mode SymlinkMode) error {
	o.Begin(path, 0)
	return IgnoreSkipped(o.Attempt("update link", path, "", func() error {
		text, err := os.Readlink(path)
		if err != nil {
			return err
		}
		if updated := smartLinkText(text, origin, parent, o.roots, true, pathExists); updated != text {
			if err = replaceLink(path, updated, o); err != nil {
				return err
			}
		}
		if mode != SymlinksContents {
			return nil
		}
		tmp := filepath.Join(filepath.Dir(path), ".farshmac-link-"+rand.Text())
		if err = o.copyLinkContents(path, tmp); err != nil {
			return err
		}
		if err = os.Rename(tmp, path); err != nil {
			_ = os.RemoveAll(tmp)
		}
		return err
	}))
}

// replaceLink atomically swaps the link at path for one with the new text,
// keeping the link's own owner, flags and timestamps where possible.
func replaceLink(path, text string, o *Operation) error {
	tmp := filepath.Join(filepath.Dir(path), ".farshmac-link-"+rand.Text())
	if err := os.Symlink(text, tmp); err != nil {
		return err
	}
	_ = preserveMetadata(path, tmp, o) // Best effort: the link text matters.
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
