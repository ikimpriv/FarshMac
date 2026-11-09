package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrashCancellationFailureAndDirectoryProgress(t *testing.T) {
	root, bin := t.TempDir(), t.TempDir()
	folder := filepath.Join(root, "folder")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "child"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	o := NewOperation("Trash")
	o.Cancel()
	if err := trashWith(folder, o, func(string) error { t.Fatal("called after cancel"); return nil }); !errors.Is(err, ErrCancelled) {
		t.Fatal(err)
	}
	failure := errors.New("volume has no Trash")
	if err := trashWith(folder, nil, func(string) error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(folder, "child")); err != nil {
		t.Fatal("failed trash removed content", err)
	}
	o = NewOperation("Trash")
	o.Add(folder, 0)
	o.Ready()
	if err := trashWith(folder, o, func(path string) error { return os.Rename(path, filepath.Join(bin, "folder")) }); err != nil {
		t.Fatal(err)
	}
	p := o.Snapshot()
	if p.TotalFiles != 1 || p.Files != 1 || p.TotalBytes != 0 {
		t.Fatal("Trash must count top-level items without scanning", p)
	}
	if b, err := os.ReadFile(filepath.Join(bin, "folder", "child")); err != nil || string(b) != "data" {
		t.Fatal("folder content lost", err)
	}
}

// Opt-in native integration: move only generated fixtures into the real Trash,
// inspect the exact returned destinations, then move them back for test cleanup.
func TestNativeTrashRoundTrip(t *testing.T) {
	if os.Getenv("FARSHMAC_TEST_NATIVE_TRASH") != "1" {
		t.Skip("requires system Trash access")
	}
	root := t.TempDir()
	name := "farshmac-trash-test-" + filepath.Base(filepath.Dir(root))
	parent := t
	results := map[string]string{}
	for _, kind := range []string{"file", "duplicate", "folder", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			sourceDir := filepath.Join(root, kind)
			if err := os.Mkdir(sourceDir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(sourceDir, name)
			switch kind {
			case "folder":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, "child"), []byte(kind), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("missing-target", path); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(path, []byte(kind), 0600); err != nil {
					t.Fatal(err)
				}
			}
			result, err := recycleNativeResult(path)
			if err != nil {
				t.Fatal(err)
			}
			if result == "" || !strings.HasPrefix(filepath.Base(result), name) {
				t.Fatalf("unexpected Trash destination %q", result)
			}
			// Parent cleanup keeps earlier fixtures in Trash to exercise collisions.
			topCleanup := func() {
				if err := os.Rename(result, path); err != nil {
					parent.Errorf("restore generated fixture %s: %v", result, err)
				}
			}
			// Registered on the parent, after its TempDir cleanup.
			parent.Cleanup(topCleanup)
			results[kind] = result
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatal("source not moved")
			}
			switch kind {
			case "folder":
				if b, err := os.ReadFile(filepath.Join(result, "child")); err != nil || string(b) != kind {
					t.Fatal("folder content changed", err)
				}
			case "symlink":
				if target, err := os.Readlink(result); err != nil || target != "missing-target" {
					t.Fatal("symlink not preserved", err)
				}
			default:
				if b, err := os.ReadFile(result); err != nil || string(b) != kind {
					t.Fatal("file content changed", err)
				}
			}
		})
	}
	if results["file"] == results["duplicate"] {
		t.Fatal("Trash reused a destination")
	}
	for _, kind := range []string{"file", "duplicate"} {
		if b, err := os.ReadFile(results[kind]); err != nil || string(b) != kind {
			t.Fatal("Trash collision overwrote content", err)
		}
	}

}
