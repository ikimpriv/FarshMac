package aispaces

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// An agent cannot run a compiler or tests on the virtual filesystem, and it
// has no access to the real folders. workspace_pull copies a project folder
// into the agent's scratch, where its own tools work; workspace_push copies
// the result back. The server does the copying, so file contents never pass
// through the model a second time, and read-only and hidden rules still hold.

const workspaceLimit = 5000 // files per pull or push

func (s *Store) agentDir(e *Env, agent string) string {
	return filepath.Join(s.envDir(e), "agents", agent)
}

// workspace is where the copy of a virtual folder lives inside the scratch.
func (s *Store) workspace(e *Env, agent, virtual string) string {
	return filepath.Join(s.agentDir(e, agent), "scratch", "work", filepath.FromSlash(clean(virtual)))
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// manifest remembers what was pulled, to notice a file somebody else changed since.
func (s *Store) manifest(e *Env, agent string) (map[string]string, string) {
	file := filepath.Join(s.agentDir(e, agent), "workspace.json")
	m := map[string]string{}
	if data, err := os.ReadFile(file); err == nil {
		json.Unmarshal(data, &m)
	}
	return m, file
}

func skipped(name string) bool {
	return name == ".git" || name == "node_modules" || name == ".DS_Store"
}

func workspacePull(s *Store, session *mcpSession, fs *FS, virtual string) (string, error) {
	virtual = clean(virtual)
	info, err := fs.Lstat(virtual)
	if err != nil {
		return "", err
	}
	local := s.workspace(session.env, session.agent, virtual)
	seen, file := s.manifest(session.env, session.agent)
	count, size := 0, int64(0)
	var copyIn func(string, string, os.FileInfo) error
	copyIn = func(v, target string, info os.FileInfo) error {
		if info.IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			entries, err := fs.ReadDir(v)
			if err != nil {
				return err
			}
			for _, child := range entries {
				if skipped(child.Name()) {
					continue
				}
				if err := copyIn(path.Join(v, child.Name()), filepath.Join(target, child.Name()), child); err != nil {
					return err
				}
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if count++; count > workspaceLimit {
			return fmt.Errorf("quota_exceeded: more than %d files; pull a subfolder", workspaceLimit)
		}
		r, err := fs.Open(v)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return err
		}
		size += int64(len(data))
		seen[v] = digest(data)
		if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	}
	if err = copyIn(virtual, local, info); err != nil {
		return "", err
	}
	data, _ := json.Marshal(seen)
	os.WriteFile(file, data, 0600)
	note := ""
	if fs.writable(virtual) != nil {
		note = " It is read-only: you can read and run it here, but workspace_push will refuse it."
	}
	return fmt.Sprintf("Copied %d file(s), %d bytes, from %s to your scratch folder:\n%s\nWork on that copy with your own tools (edit, run, test). When it is right, call workspace_push with the same path: do not retype files with vfs_write.%s", count, size, virtual, local, note), nil
}

func workspacePush(s *Store, session *mcpSession, fs *FS, virtual string) (string, error) {
	virtual = clean(virtual)
	local := s.workspace(session.env, session.agent, virtual)
	info, err := os.Lstat(local)
	if err != nil {
		return "", fmt.Errorf("not_found: nothing was pulled for %s; create your files under %s or call workspace_pull first", virtual, local)
	}
	if err = fs.writable(virtual); err != nil {
		return "", err
	}
	seen, file := s.manifest(session.env, session.agent)
	type change struct {
		virtual string
		data    []byte
	}
	var changes []change
	var conflicts, unchanged []string
	count := 0
	var walk func(string, string, os.FileInfo) error
	walk = func(target, v string, info os.FileInfo) error {
		if info.IsDir() {
			entries, err := os.ReadDir(target)
			if err != nil {
				return err
			}
			for _, child := range entries {
				if skipped(child.Name()) {
					continue
				}
				childInfo, err := child.Info()
				if err != nil {
					return err
				}
				if err := walk(filepath.Join(target, child.Name()), path.Join(v, child.Name()), childInfo); err != nil {
					return err
				}
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil // links are never followed out of the scratch folder
		}
		if count++; count > workspaceLimit {
			return fmt.Errorf("quota_exceeded: more than %d files; push a subfolder", workspaceLimit)
		}
		data, err := os.ReadFile(target)
		if err != nil {
			return err
		}
		var current []byte
		exists := false
		if r, err := fs.Open(v); err == nil {
			current, _ = io.ReadAll(r)
			r.Close()
			exists = true
		}
		switch {
		case exists && bytes.Equal(current, data):
			unchanged = append(unchanged, v)
		case exists && seen[v] != digest(current):
			// Changed in the project since the pull (or never pulled) and different here.
			conflicts = append(conflicts, v)
		default:
			changes = append(changes, change{v, data})
		}
		return nil
	}
	if err = walk(local, virtual, info); err != nil {
		return "", err
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return "", fmt.Errorf("conflict: nothing was pushed. These files changed in the project after you pulled them: %s. Call workspace_pull for them again (it overwrites your copy; keep your edits elsewhere first), or write them with vfs_edit", strings.Join(conflicts, ", "))
	}
	var done []string
	for _, c := range changes {
		if dir := path.Dir(c.virtual); dir != "/" {
			if _, err := fs.Lstat(dir); err != nil {
				if err = mkdirAll(fs, dir); err != nil {
					return "", err
				}
			}
		}
		old, _, readErr := fs.ReadFile(c.virtual, 8<<20)
		if err := fs.WriteFile(c.virtual, c.data); err != nil {
			return "", fmt.Errorf("%v (already pushed: %s)", err, strings.Join(done, ", "))
		}
		// The agent edited its copy with its own tools: the server cannot
		// tell what it knows of the result, so the next read is a full one.
		session.forget(c.virtual)
		if real, shared := source(c.virtual, fs.bindings()); shared {
			session.touched(real, old, readErr == nil, c.data)
		}
		seen[c.virtual] = digest(c.data)
		done = append(done, c.virtual)
	}
	data, _ := json.Marshal(seen)
	os.WriteFile(file, data, 0600)
	if len(done) == 0 {
		return fmt.Sprintf("Nothing to push: all %d file(s) already match the project.", len(unchanged)), nil
	}
	ref := fs.Ref(virtual)
	if ref != "" {
		ref = " Reference for other agents: " + ref
	}
	return fmt.Sprintf("Pushed %d file(s) to %s: %s. %d unchanged. Files you deleted locally are not removed from the project; use vfs_remove.%s", len(done), virtual, strings.Join(done, ", "), len(unchanged), ref), nil
}

func mkdirAll(fs *FS, dir string) error {
	if dir == "/" {
		return nil
	}
	if _, err := fs.Lstat(dir); err == nil {
		return nil
	}
	if err := mkdirAll(fs, path.Dir(dir)); err != nil {
		return err
	}
	return fs.Mkdir(dir)
}
