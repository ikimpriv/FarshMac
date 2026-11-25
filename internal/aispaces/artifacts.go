package aispaces

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var evidenceID = regexp.MustCompile(`^[0-9a-f]{16}$`)

type RunEvidence struct {
	ID, Env, Agent, Origin, Before, After, Time string
	Command                                     string `json:",omitempty"`
	Exit                                        int
	DurationMS                                  int64
	Output                                      string // bounded diagnostic tail, never interpreted as an exit status
}
type Artifact struct {
	ID, Env, Name, Reference, Snapshot, Created string
	Readers                                     []string
	Runs                                        []RunEvidence
}

var artifactTools = []tool{
	{"artifact_publish", "Publish an immutable bounded project snapshot and server-recorded run evidence. Sharing its ID grants the recipient only this version, not the source folder. For new source only. Review an existing artifact with browser_check (auto-attaches) or artifact_verify for your workspace runs; do not republish.", object([]string{"path", "name"}, prop{"path", "string", pathHelp}, prop{"name", "string", "Short delivery label"}, prop{"runs", "array", "Run evidence IDs, at most 12"})},
	{"artifact_read", "Read a received artifact's manifest/evidence or one immutable text file. Run details show results first, without repeating the resolved scenario. A matching successful run is distinct from product acceptance.", object([]string{"id"}, prop{"id", "string", "Artifact ID received in a message"}, prop{"file", "string", "Optional relative file path"}, prop{"run", "string", "Optional recorded run ID for detailed output"}, prop{"part", "string", "With run: command or output for paged text (byte offsets). Omit for compact metadata and result"}, prop{"offset", "integer", "First line for files; first byte for run part, default 1"}, prop{"limit", "integer", "Up to 300 file lines or 8192 bytes for run part"})},
	{"artifact_verify", "Attach your server-recorded runs to an artifact you received. The server compares tested snapshots; callers cannot supply a pass status or substitute hashes.", object([]string{"id", "runs"}, prop{"id", "string", "Received artifact ID"}, prop{"runs", "array", "Your run IDs, at most 12"})},
	{"artifact_checkout", "Copy a received immutable version into a new private workspace. Returns the path for workspace_run; source bindings remain unchanged.", object([]string{"id"}, prop{"id", "string", "Received artifact ID"})},
}

func init() {
	for i := range artifactTools {
		if p := artifactTools[i].InputSchema["properties"].(map[string]any); p["runs"] != nil {
			p["runs"] = stringList("Server-recorded run IDs (at most 12)")
		}
	}
}

func sessionContext(session *mcpSession) context.Context {
	if session.ctx != nil {
		return session.ctx
	}
	return context.Background()
}
func saveJSON(file string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".record-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}
func (s *Store) recordRun(session *mcpSession, r RunEvidence) (RunEvidence, error) {
	r.ID, r.Env, r.Agent, r.Time = newID(), session.env.ID, session.agent, time.Now().UTC().Format(time.RFC3339Nano)
	err := saveJSON(filepath.Join(s.envDir(session.env), "evidence", r.ID+".json"), r)
	return r, err
}
func (s *Store) readRuns(session *mcpSession, ids []string, snapshot string) ([]RunEvidence, error) {
	if len(ids) > 12 {
		return nil, fmt.Errorf("at most 12 runs")
	}
	out := []RunEvidence{}
	for _, id := range ids {
		if !evidenceID.MatchString(id) {
			return nil, fmt.Errorf("invalid run ID")
		}
		b, err := os.ReadFile(filepath.Join(s.envDir(session.env), "evidence", id+".json"))
		if err != nil {
			return nil, fmt.Errorf("run %s is not recorded in this Env; ask its owner to attach it with artifact_verify, then share the same artifact ID", id)
		}
		var r RunEvidence
		if err = json.Unmarshal(b, &r); err != nil {
			return nil, err
		}
		if r.Env != session.env.ID || r.Before != snapshot || r.After != snapshot {
			return nil, fmt.Errorf("run %s does not match this immutable version (or changed its input); rerun against the final candidate", id)
		}
		out = append(out, r)
	}
	return out, nil
}
func snapshotSkip(name string) bool {
	return skipped(name) || oneOf(name, []string{".cache", ".tmp", ".runs", ".runtime-home"})
}

// Stage only the caller's VFS view. No symlinks, hidden overlay entries or
// unattached host files enter an artifact or browser document root.
func stageFiles(ctx context.Context, fs *FS, base, dest string) error {
	count := 0
	left := int64(128 << 20)
	var copy func(string, string) error
	copy = func(v, rel string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := fs.Lstat(v)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err = os.MkdirAll(filepath.Join(dest, rel), 0700); err != nil {
				return err
			}
			entries, err := fs.ReadDir(v)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if snapshotSkip(entry.Name()) {
					continue
				}
				if err = copy(path.Join(v, entry.Name()), filepath.Join(rel, entry.Name())); err != nil {
					return err
				}
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		count++
		if count > workspaceLimit || info.Size() > left {
			return fmt.Errorf("snapshot limit exceeded; publish a smaller folder")
		}
		data, truncated, err := fs.ReadFile(v, min(left, 16<<20))
		if err != nil {
			return err
		}
		if truncated {
			return fmt.Errorf("file too large: %s", v)
		}
		left -= int64(len(data))
		if left < 0 {
			return fmt.Errorf("snapshot byte limit exceeded")
		}
		return os.WriteFile(filepath.Join(dest, rel), data, info.Mode().Perm())
	}
	info, err := fs.Lstat(base)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("snapshot root must be a folder")
	}
	return copy(base, "")
}
func (s *Store) artifactDir(e *Env) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp := s.spaceOf(e)
	if sp == nil {
		return "", fmt.Errorf("Space not found")
	}
	return filepath.Join(s.root, "spaces", sp.ID, "artifacts"), nil
}
func loadArtifact(dir, id, reader string) (Artifact, error) {
	var a Artifact
	if !evidenceID.MatchString(id) {
		return a, fmt.Errorf("invalid artifact ID")
	}
	b, err := os.ReadFile(filepath.Join(dir, id, "artifact.json"))
	if err != nil {
		return a, fmt.Errorf("artifact not found")
	}
	if err = json.Unmarshal(b, &a); err != nil {
		return a, err
	}
	if a.Env != reader && !oneOf(reader, a.Readers) {
		return a, fmt.Errorf("permission_denied: artifact was not shared with this Env")
	}
	return a, nil
}
func artifactCall(s *Store, session *mcpSession, fs *FS, name string, raw map[string]any) (string, error) {
	if !oneOf(name, []string{"artifact_publish", "artifact_read", "artifact_verify", "artifact_checkout"}) {
		return "", fmt.Errorf("unknown artifact operation")
	}
	var args struct {
		ID, Path, Name, File, Run, Part string
		Runs                            []string
		Offset, Limit                   int
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	dir, err := s.artifactDir(session.env)
	if err != nil {
		return "", err
	}
	if name == "artifact_publish" {
		if args.Name == "" || len(args.Name) > 160 {
			return "", fmt.Errorf("provide a short artifact name")
		}
		base := args.Path
		if strings.HasPrefix(base, "@") {
			base, err = s.resolveRef(session.env, base)
			if err != nil {
				return "", err
			}
		}
		if err = os.MkdirAll(dir, 0700); err != nil {
			return "", err
		}
		id := newID()
		target := filepath.Join(dir, id)
		defer func() {
			if err != nil {
				os.RemoveAll(target)
			}
		}()
		if err = stageFiles(sessionContext(session), fs, base, filepath.Join(target, "files")); err != nil {
			return "", fmt.Errorf("publish requires a VFS folder (not a private checkout); reviewers use browser_check(artifact,spec) or artifact_verify(id,runs): %w", err)
		}
		snapshot := workspaceSnapshot(sessionContext(session), filepath.Join(target, "files"))
		if !strings.HasPrefix(snapshot, "sha256:") {
			err = fmt.Errorf("snapshot unavailable")
			return "", err
		}
		var runs []RunEvidence
		runs, err = s.readRuns(session, args.Runs, snapshot)
		if err != nil {
			return "", err
		}
		a := Artifact{ID: id, Env: session.env.ID, Name: args.Name, Reference: fs.Ref(base), Snapshot: snapshot, Created: time.Now().UTC().Format(time.RFC3339Nano), Runs: runs}
		err = saveJSON(filepath.Join(target, "artifact.json"), a)
		return jsonText(artifactSummary(a)), err
	}
	return readArtifactOperation(s, session, dir, name, args.ID, args.File, args.Run, args.Part, args.Runs, args.Offset, args.Limit)
}
func readOperatorArtifact(s *Store, sp *Space, raw map[string]any) (string, error) {
	var a struct {
		ID, File, Run, Part string
		Offset, Limit       int
	}
	if err := decodeArgs(raw, &a); err != nil {
		return "", err
	}
	dir := filepath.Join(s.root, "spaces", sp.ID, "artifacts")
	return readArtifactOperation(s, &mcpSession{env: &Env{ID: operatorEnv}}, dir, "artifact_read", a.ID, a.File, a.Run, a.Part, nil, a.Offset, a.Limit)
}
func readArtifactOperation(s *Store, session *mcpSession, dir, name, id, file, run, part string, runs []string, offset, limit int) (string, error) {
	args := struct {
		ID, File, Run string
		Runs          []string
		Offset, Limit int
	}{id, file, run, runs, offset, limit}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := loadArtifact(dir, args.ID, session.env.ID)
	if err != nil {
		return "", err
	}
	if part != "" && (name != "artifact_read" || run == "" || !oneOf(part, []string{"command", "output"})) {
		return "", fmt.Errorf("part requires artifact_read with run, and must be command or output")
	}
	if name == "artifact_verify" {
		runs, err := s.readRuns(session, args.Runs, a.Snapshot)
		if err != nil {
			return "", err
		}
		seen := map[string]bool{}
		for _, r := range a.Runs {
			seen[r.ID] = true
		}
		for _, r := range runs {
			if !seen[r.ID] {
				a.Runs = append(a.Runs, r)
			}
		}
		if len(a.Runs) > 64 {
			return "", fmt.Errorf("artifact evidence limit reached")
		}
		if err = saveJSON(filepath.Join(dir, a.ID, "artifact.json"), a); err != nil {
			return "", err
		}
	}
	if name == "artifact_checkout" {
		return checkoutArtifact(s, session, dir, a)
	}
	if args.Run != "" {
		for _, r := range a.Runs {
			if r.ID == args.Run {
				return runEvidenceView(r, part, offset, limit)
			}
		}
		return "", fmt.Errorf("run not attached to this artifact")
	}
	if args.File == "" {
		summary := artifactSummary(a)
		files := []string{}
		root, e := os.OpenRoot(filepath.Join(dir, a.ID, "files"))
		if e != nil {
			return "", e
		}
		defer root.Close()
		e = iofs.WalkDir(root.FS(), ".", func(p string, d iofs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err = sessionContext(session).Err(); err != nil {
				return err
			}
			if d.Type().IsRegular() {
				files = append(files, p)
			}
			if len(files) >= 200 {
				return iofs.SkipAll
			}
			return nil
		})
		summary["files"] = files
		summary["file_list_limit"] = 200
		return jsonText(summary), e
	}
	if name != "artifact_read" {
		return "", fmt.Errorf("file is only valid for artifact_read")
	}
	root, err := os.OpenRoot(filepath.Join(dir, a.ID, "files"))
	if err != nil {
		return "", err
	}
	defer root.Close()
	if args.File == "." || filepath.IsAbs(args.File) || !filepath.IsLocal(args.File) {
		return "", fmt.Errorf("file must be a relative path")
	}
	f, err := root.Open(args.File)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return readEvidenceLines(f, max(1, args.Offset), min(300, max(1, func() int {
		if args.Limit == 0 {
			return 160
		}
		return args.Limit
	}())))
}

// Resolved browser input can be megabytes long. Never put it ahead of the
// result in a bounded read response. Full evidence remains durable and readable.
func runEvidenceView(r RunEvidence, part string, offset, limit int) (string, error) {
	if part != "" {
		text := r.Output
		if part == "command" {
			text = r.Command
		}
		var pretty bytes.Buffer
		if json.Indent(&pretty, []byte(text), "", "  ") == nil {
			text = pretty.String()
		}
		start := max(1, offset) - 1
		if start > len(text) || start < len(text) && !utf8.RuneStart(text[start]) {
			return "", fmt.Errorf("offset must be a valid text byte boundary")
		}
		if limit <= 0 {
			limit = 4096
		}
		end := min(len(text), start+min(limit, 8192))
		for end < len(text) && end > start && !utf8.RuneStart(text[end]) {
			end--
		}
		if end == start && end < len(text) {
			_, n := utf8.DecodeRuneInString(text[start:])
			end += n
		}
		for len(jsonText(text[start:end])) > 6000 {
			end = start + (end-start)/2
			for end > start && !utf8.RuneStart(text[end]) {
				end--
			}
		}
		next := 0
		if end < len(text) {
			next = end + 1
		}
		return jsonText(map[string]any{"part": part, "text": text[start:end], "offset": start + 1, "next_offset": next, "total_bytes": len(text)}), nil
	}
	commandBytes, outputBytes := len(r.Command), len(r.Output)
	r.Command = ""
	// Bound the encoded string, including JSON escapes, for batched env_read.
	truncated := false
	for len(jsonText(r.Output)) > 6000 {
		r.Output = shorten(r.Output, max(1, len(r.Output)/2))
		truncated = true
	}
	return jsonText(struct {
		RunEvidence
		CommandBytes int    `json:"command_bytes"`
		OutputBytes  int    `json:"output_bytes"`
		Truncated    bool   `json:"output_truncated"`
		Detail       string `json:"detail"`
	}{r, commandBytes, outputBytes, truncated, "Read part:command or part:output with byte offset/limit; next_offset continues the detail"}), nil
}

// Called with Store.mu held after the ordinary contact/thread checks.
func (s *Store) shareArtifacts(sp *Space, from *Env, to string, ids []string) (string, error) {
	if len(ids) > 8 {
		return "", fmt.Errorf("at most 8 artifacts per message")
	}
	dir := filepath.Join(s.root, "spaces", sp.ID, "artifacts")
	all := []Artifact{}
	for _, id := range ids {
		a, err := loadArtifact(dir, id, from.ID)
		if err != nil {
			return "", err
		}
		all = append(all, a)
	}
	var text strings.Builder
	for _, a := range all {
		if !oneOf(to, a.Readers) {
			a.Readers = append(a.Readers, to)
			if err := saveJSON(filepath.Join(dir, a.ID, "artifact.json"), a); err != nil {
				return "", err
			}
		}
		fmt.Fprintf(&text, "\nArtifact %s: %s · %s · %d recorded checks. Read with artifact_read. browser_check(artifact,spec) auto-attaches evidence; for workspace runs use artifact_verify. Share this same ID; source access is unchanged.\n", a.ID, a.Name, a.Snapshot, len(a.Runs))
	}
	return text.String(), nil
}

func artifactSummary(a Artifact) map[string]any {
	runs := []map[string]any{}
	for _, r := range a.Runs {
		runs = append(runs, map[string]any{"id": r.ID, "env": r.Env, "agent": r.Agent, "origin": r.Origin, "exit": r.Exit, "duration_ms": r.DurationMS, "time": r.Time})
	}
	return map[string]any{"ID": a.ID, "name": a.Name, "snapshot": a.Snapshot, "runs": runs, "created": a.Created, "acceptance": "Recorded execution evidence; criterion coverage and acceptance require review."}
}
func checkoutArtifact(s *Store, session *mcpSession, dir string, a Artifact) (string, error) {
	scratch := filepath.Join(s.agentDir(session.env, session.agent), "scratch")
	if err := os.MkdirAll(scratch, 0700); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(scratch)
	if err != nil {
		return "", err
	}
	defer root.Close()
	virtual := "/artifacts/" + a.ID + "-" + newID()
	rel := filepath.Join("work", filepath.FromSlash(virtual))
	if err = root.MkdirAll(rel, 0700); err != nil {
		return "", err
	}
	source, err := os.OpenRoot(filepath.Join(dir, a.ID, "files"))
	if err != nil {
		return "", err
	}
	defer source.Close()
	err = iofs.WalkDir(source.FS(), ".", func(p string, d iofs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = sessionContext(session).Err(); err != nil {
			return err
		}
		dest := filepath.Join(rel, p)
		if d.IsDir() {
			return root.MkdirAll(dest, 0700)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("unexpected artifact entry")
		}
		in, err := source.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		info, err := in.Stat()
		if err != nil {
			return err
		}
		out, err := root.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	if err != nil {
		return "", err
	}
	snapshot := workspaceSnapshot(sessionContext(session), s.workspace(session.env, session.agent, virtual))
	if snapshot != a.Snapshot {
		return "", fmt.Errorf("checkout snapshot mismatch")
	}
	return jsonText(map[string]any{"artifact": a.ID, "path": virtual, "snapshot": snapshot, "note": "This path is a private workspace_run path, NOT a VFS path for artifact_publish. For matching checks use artifact_verify(id,runs); browser_check(artifact,spec) needs no checkout and attaches automatically."}), nil
}
