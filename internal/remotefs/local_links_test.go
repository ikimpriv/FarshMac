package remotefs_test

import (
	"os"
	"path/filepath"
	"testing"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
)

// Parallel local copies share hard-link groups between workers and treat the
// selected items as one set for smart symbolic links.
func TestParallelLocalCopyKeepsHardlinksAndSmartLinks(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst", "deeper")
	if err := os.MkdirAll(filepath.Join(src, "tree"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "h1"), []byte("linked"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"h2", "tree/h3", "tree/h4"} {
		if err := os.Link(filepath.Join(src, "h1"), filepath.Join(src, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "outside"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{"tree/sibling": "../h1", "tree/out": "../../outside"}
	for name, text := range links {
		if err := os.Symlink(text, filepath.Join(src, name)); err != nil {
			t.Fatal(err)
		}
	}
	var pairs []remotefs.Pair
	for _, name := range []string{"h1", "h2", "tree"} {
		pairs = append(pairs, remotefs.Pair{Source: filepath.Join(src, name), Destination: filepath.Join(dst, name)})
	}
	o := fsops.NewOperation("Copy")
	if err := remotefs.Copy(remotefs.Endpoint{}, remotefs.Endpoint{}, pairs, remotefs.CopyOptions{Workers: 4}, o); err != nil {
		t.Fatal(err)
	}
	first, err := os.Lstat(filepath.Join(dst, "h1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"h2", "tree/h3", "tree/h4"} {
		if info, err := os.Lstat(filepath.Join(dst, name)); err != nil || !os.SameFile(first, info) {
			t.Fatal("hard link group split", name, err)
		}
	}
	if text, _ := os.Readlink(filepath.Join(dst, "tree", "sibling")); text != "../h1" {
		t.Fatal("link into the selection rewritten", text)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "tree", "out")); err != nil || string(b) != "outside" {
		t.Fatal("outside link dangling", err)
	}
}
