package aispaces

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FS is the effective view of an Env. A referenced host file or folder is a
// live, writable mount: reads and writes below it go straight to the real
// path. Everything outside the references lives in the Env's private folder.
// Its methods match remotefs.FS.
type FS struct {
	s   *Store
	env *Env
}

type namedInfo struct {
	os.FileInfo
	name string
}

func (i namedInfo) Name() string { return i.name }

type dirInfo string

func (i dirInfo) Name() string       { return string(i) }
func (i dirInfo) Size() int64        { return 0 }
func (i dirInfo) Mode() os.FileMode  { return os.ModeDir | 0755 }
func (i dirInfo) ModTime() time.Time { return time.Time{} }
func (i dirInfo) IsDir() bool        { return true }
func (i dirInfo) Sys() any           { return nil }

func clean(p string) string { return path.Clean("/" + p) }
func under(p, root string) bool {
	return p == root || root == "/" || strings.HasPrefix(p, root+"/")
}

func (f *FS) private(p string) string {
	return filepath.Join(f.s.envDir(f.env), "overlay", filepath.FromSlash(p))
}

func (f *FS) bindings() []Binding {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	return append([]Binding(nil), f.env.Bindings...)
}

// cover finds the reference that covers p.
func cover(p string, bindings []Binding) (Binding, bool) {
	best := -1
	for i, b := range bindings {
		if under(p, b.Virtual) && (best < 0 || len(b.Virtual) > len(bindings[best].Virtual)) {
			best = i
		}
	}
	if best < 0 {
		return Binding{}, false
	}
	return bindings[best], true
}

// source maps p into the reference that covers it.
func source(p string, bindings []Binding) (string, bool) {
	b, ok := cover(p, bindings)
	if !ok {
		return "", false
	}
	return filepath.Join(b.Source, filepath.FromSlash(strings.TrimPrefix(p, b.Virtual))), true
}

// writable refuses changes below a reference attached read-only.
func (f *FS) writable(p string) error {
	if b, ok := cover(clean(p), f.bindings()); ok && b.ReadOnly {
		return fmt.Errorf("read_only: %s is attached read-only", b.Virtual)
	}
	return nil
}

// Reference reports that p is a reference or a folder holding one: such an
// entry is attached and detached by the user, not changed like a file.
func (f *FS) Reference(p string) bool {
	p = clean(p)
	for _, b := range f.bindings() {
		if under(b.Virtual, p) {
			return true
		}
	}
	return false
}

// real is the host path behind p: inside a reference when one covers p,
// otherwise in the Env's private folder.
func (f *FS) real(p string) (string, bool) {
	if src, ok := source(p, f.bindings()); ok {
		return src, true
	}
	return f.private(p), false
}

// HostPath is for host UI navigation only, never exposed through the Env MCP.
// Synthetic folders have no single backing location; hidden entries stay hidden.
func (f *FS) HostPath(p string) (string, error) {
	p = clean(p)
	if f.hidden(p) {
		return "", &os.PathError{Op: "stat", Path: p, Err: os.ErrNotExist}
	}
	full, _ := f.real(p)
	if _, err := os.Lstat(full); err != nil {
		return "", err
	}
	return full, nil
}

// hidden reports an entry inside a reference that was removed from this Env.
// The real file stays; this Env and its agents no longer see it.
func (f *FS) hidden(p string) bool {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	for _, h := range f.env.Hidden {
		if under(p, h) {
			return true
		}
	}
	return false
}

// unhide makes p visible again, for example when it is created anew.
func (f *FS) unhide(p string) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	kept := f.env.Hidden[:0]
	for _, h := range f.env.Hidden {
		if !under(h, p) && !under(p, h) {
			kept = append(kept, h)
		}
	}
	if len(kept) != len(f.env.Hidden) {
		f.env.Hidden = kept
		f.s.save()
	}
}

// HiddenBelow lists the entries removed from this Env at or below dir.
func (f *FS) HiddenBelow(dir string) []string {
	dir = clean(dir)
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	var list []string
	for _, h := range f.env.Hidden {
		if under(h, dir) {
			list = append(list, h)
		}
	}
	sort.Strings(list)
	return list
}

// Restore shows the entries removed from this Env at or below dir again.
func (f *FS) Restore(dir string) error {
	dir = clean(dir)
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	kept := f.env.Hidden[:0]
	for _, h := range f.env.Hidden {
		if !under(h, dir) {
			kept = append(kept, h)
		}
	}
	f.env.Hidden = kept
	return f.s.save()
}

// synthetic reports a folder that exists only because a reference lives below it.
func (f *FS) synthetic(p string) bool {
	if p == "/" {
		return true
	}
	for _, b := range f.bindings() {
		if b.Virtual != p && under(b.Virtual, p) {
			return true
		}
	}
	return false
}

func (f *FS) Lstat(p string) (os.FileInfo, error) {
	p = clean(p)
	if p != "/" && !f.hidden(p) {
		full, _ := f.real(p)
		if info, err := os.Lstat(full); err == nil {
			return namedInfo{info, path.Base(p)}, nil
		}
	}
	if f.synthetic(p) {
		return dirInfo(path.Base(p)), nil
	}
	return nil, &os.PathError{Op: "stat", Path: p, Err: os.ErrNotExist}
}
func (f *FS) Stat(p string) (os.FileInfo, error) { return f.Lstat(p) }

func (f *FS) ReadDir(p string) ([]os.FileInfo, error) {
	p = clean(p)
	info, err := f.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", p)
	}
	found := map[string]os.FileInfo{}
	full, _ := f.real(p)
	if list, err := os.ReadDir(full); err == nil {
		for _, d := range list {
			if f.hidden(path.Join(p, d.Name())) {
				continue
			}
			if info, err := d.Info(); err == nil {
				found[d.Name()] = info
			}
		}
	}
	for _, b := range f.bindings() {
		if b.Virtual == p || !under(b.Virtual, p) {
			continue
		}
		name := strings.SplitN(strings.TrimPrefix(strings.TrimPrefix(b.Virtual, p), "/"), "/", 2)[0]
		if child := path.Join(p, name); child == b.Virtual {
			if info, err := os.Lstat(b.Source); err == nil {
				found[name] = namedInfo{info, name}
			}
		} else if found[name] == nil {
			found[name] = dirInfo(name)
		}
	}
	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]os.FileInfo, len(names))
	for i, name := range names {
		result[i] = found[name]
	}
	return result, nil
}

func (f *FS) Open(p string) (io.ReadCloser, error) {
	if f.hidden(clean(p)) {
		return nil, &os.PathError{Op: "open", Path: clean(p), Err: os.ErrNotExist}
	}
	full, _ := f.real(clean(p))
	return os.Open(full)
}

func (f *FS) Create(p string) (io.WriteCloser, error) {
	p = clean(p)
	if err := f.writable(p); err != nil {
		return nil, err
	}
	parent, err := f.Lstat(path.Dir(p))
	if err != nil {
		return nil, err
	}
	if !parent.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", path.Dir(p))
	}
	full, sourced := f.real(p)
	if !sourced {
		if err = os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return nil, err
		}
	}
	f.unhide(p)
	return os.Create(full)
}

func (f *FS) Mkdir(p string) error {
	p = clean(p)
	if err := f.writable(p); err != nil {
		return err
	}
	if _, err := f.Lstat(p); err == nil {
		return &os.PathError{Op: "mkdir", Path: p, Err: os.ErrExist}
	}
	if _, err := f.Lstat(path.Dir(p)); err != nil {
		return err
	}
	full, sourced := f.real(p)
	if sourced {
		if f.hidden(p) {
			f.unhide(p)
			if info, err := os.Lstat(full); err == nil && info.IsDir() {
				return nil // the real folder is shown again
			}
		}
		return os.Mkdir(full, 0755)
	}
	return os.MkdirAll(full, 0755)
}

// Remove takes p out of the Env. A reference at or below p is detached. An
// entry inside a reference is only hidden from this Env and its agents: the
// real file is not deleted. A private file of the Env is deleted.
func (f *FS) Remove(p string, _ bool) error {
	p = clean(p)
	if p == "/" {
		return fmt.Errorf("cannot remove the Env root")
	}
	if _, err := f.Lstat(p); err != nil {
		return err
	}
	detached := false
	for _, b := range f.bindings() {
		detached = detached || under(b.Virtual, p)
	}
	if detached {
		os.RemoveAll(f.private(p))
		f.s.mu.Lock()
		defer f.s.mu.Unlock()
		kept := f.env.Bindings[:0]
		for _, b := range f.env.Bindings {
			if !under(b.Virtual, p) {
				kept = append(kept, b)
			}
		}
		f.env.Bindings = kept
		return f.s.save()
	}
	full, sourced := f.real(p)
	if sourced {
		// The real entry stays where it is: it only leaves this Env.
		f.s.mu.Lock()
		defer f.s.mu.Unlock()
		f.env.Hidden = append(f.env.Hidden, p)
		return f.s.save()
	}
	return os.RemoveAll(full)
}

func (f *FS) Rename(from, to string) error {
	from, to = clean(from), clean(to)
	info, err := f.Lstat(from)
	if err != nil {
		return err
	}
	if under(to, from) && to != from {
		return fmt.Errorf("cannot move a folder into itself")
	}
	if _, err = f.Lstat(path.Dir(to)); err != nil {
		return err
	}
	for _, b := range f.bindings() {
		if b.Virtual == from {
			// A reference is renamed only in the Env; its source keeps its name.
			f.s.mu.Lock()
			defer f.s.mu.Unlock()
			for i, b := range f.env.Bindings {
				if under(b.Virtual, from) {
					f.env.Bindings[i].Virtual = to + strings.TrimPrefix(b.Virtual, from)
				}
			}
			return f.s.save()
		}
		if under(b.Virtual, from) {
			return fmt.Errorf("a folder that contains references cannot be renamed")
		}
	}
	if err = f.writable(from); err != nil {
		return err
	}
	if err = f.writable(to); err != nil {
		return err
	}
	f.unhide(to)
	a, _ := f.real(from)
	b, sourced := f.real(to)
	if !sourced {
		if err = os.MkdirAll(filepath.Dir(b), 0755); err != nil {
			return err
		}
	}
	if os.Rename(a, b) == nil {
		return nil
	}
	// Different volumes (private folder ↔ reference, or two references).
	if err = f.copyTree(from, to, info); err != nil {
		return err
	}
	return os.RemoveAll(a)
}

func (f *FS) copyTree(from, to string, info os.FileInfo) error {
	if info.IsDir() {
		if err := f.Mkdir(to); err != nil && !os.IsExist(err) {
			return err
		}
		list, err := f.ReadDir(from)
		if err != nil {
			return err
		}
		for _, child := range list {
			if err = f.copyTree(path.Join(from, child.Name()), path.Join(to, child.Name()), child); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	in, err := f.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := f.Create(to)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func (f *FS) Chmod(p string, mode os.FileMode) error {
	if err := f.writable(p); err != nil {
		return err
	}
	full, _ := f.real(clean(p))
	return os.Chmod(full, mode)
}
func (f *FS) Chtimes(p string, t time.Time) error {
	if err := f.writable(p); err != nil {
		return err
	}
	full, _ := f.real(clean(p))
	return os.Chtimes(full, t, t)
}
func (f *FS) Readlink(p string) (string, error) {
	return "", fmt.Errorf("links are not followed inside an AI Env")
}
func (f *FS) Symlink(string, string) error {
	return fmt.Errorf("links cannot be created inside an AI Env")
}
func (f *FS) Close() error { return nil }

// Sources describes the references of the Env.
func (f *FS) Sources() []Binding { return f.bindings() }

// ReadFile returns at most limit bytes of a file.
func (f *FS) ReadFile(p string, limit int64) ([]byte, bool, error) {
	r, err := f.Open(p)
	if err != nil {
		return nil, false, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(b)) > limit {
		return b[:limit], true, nil
	}
	return b, false, nil
}

func (f *FS) WriteFile(p string, data []byte) error {
	w, err := f.Create(p)
	if err != nil {
		return err
	}
	if _, err = w.Write(data); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// Ref is the portable name of p: the ID of the reference that covers it plus
// the path inside it. It names the same real file in every Env of the Space,
// whatever virtual path each Env attached the folder at. Private files have none.
func (f *FS) Ref(p string) string {
	p = clean(p)
	b, ok := cover(p, f.bindings())
	if !ok || b.ID == "" {
		return ""
	}
	return "@" + b.ID + strings.TrimPrefix(p, b.Virtual)
}
