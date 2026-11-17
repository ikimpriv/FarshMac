// Package cloudfiles materializes macOS cloud placeholders before ordinary file
// access. Provider coordination runs in a cancellable helper process so a stalled
// provider cannot block the UI or strand a goroutine per cancelled request.
package cloudfiles

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const HelperFlag = "--cloud-file-helper"

type result struct{ Path, Error string }

// Candidate does not read file data or contact a provider. Ordinary local files
// keep their existing fast path. Stat also recognizes placeholders through links.
func Candidate(path string) bool { _, needed := candidate(path); return needed }
func candidate(path string) (string, bool) {
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && dataless(info) {
		return path, true
	}
	// Older iCloud Drive versions expose .name.ext.icloud instead of name.ext.
	// Never reinterpret an arbitrary local file solely because of its extension.
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, "Library", "Mobile Documents") + string(os.PathSeparator)
	absolute, _ := filepath.Abs(path)
	name := filepath.Base(path)
	if strings.HasPrefix(absolute, root) && strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".icloud") && len(name) > len("..icloud") {
		target := filepath.Join(filepath.Dir(path), strings.TrimSuffix(strings.TrimPrefix(name, "."), ".icloud"))
		return target, true
	}
	return path, false
}

func Prepare(ctx context.Context, path string) (string, error) {
	target, needed := candidate(path)
	if !needed {
		return path, ctx.Err()
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return runHelper(ctx, exe, target)
}
func runHelper(ctx context.Context, exe, path string) (string, error) {
	cmd := exec.CommandContext(ctx, exe, HelperFlag, path)
	cmd.WaitDelay = time.Second
	var output bytes.Buffer
	cmd.Stdout = &boundedOutput{b: &output}
	// The helper reports provider errors as JSON; never collect unbounded logs.
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	var r result
	if e := json.Unmarshal(output.Bytes(), &r); e != nil {
		if err != nil {
			return "", fmt.Errorf("cloud download: %w", err)
		}
		return "", fmt.Errorf("invalid cloud download response: %w", e)
	}
	if r.Error != "" {
		return "", fmt.Errorf("cloud download: %s", r.Error)
	}
	if err != nil {
		return "", fmt.Errorf("cloud download: %w", err)
	}
	if r.Path == "" || !filepath.IsAbs(r.Path) {
		return "", fmt.Errorf("cloud provider returned an invalid path")
	}
	return r.Path, nil
}

type boundedOutput struct{ b *bytes.Buffer }

func (w *boundedOutput) Write(p []byte) (int, error) {
	if w.b.Len()+len(p) > 64*1024 {
		return 0, fmt.Errorf("cloud response exceeds limit")
	}
	return w.b.Write(p)
}

// HelperMain is dispatched before any UI initialization.
func HelperMain(path string) int {
	resolved, err := materialize(path)
	r := result{Path: resolved}
	if err != nil {
		r.Error = err.Error()
	}
	if json.NewEncoder(os.Stdout).Encode(r) != nil {
		return 1
	}
	if err != nil {
		return 1
	}
	return 0
}
