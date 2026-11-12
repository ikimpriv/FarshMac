package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func acrossFixture(t *testing.T) (src, dst string) {
	t.Helper()
	root := t.TempDir()
	src, dst = filepath.Join(root, "src"), filepath.Join(root, "dst")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "bad", "nested/c"} {
		put(t, filepath.Join(src, name), name)
	}
	return src, dst
}

func assertMovedExcept(t *testing.T, src, dst, kept string) {
	t.Helper()
	for _, name := range []string{"a", "bad", "nested/c"} {
		_, inSource := os.Lstat(filepath.Join(src, name))
		b, inDest := os.ReadFile(filepath.Join(dst, name))
		if name == kept {
			if inSource != nil || !os.IsNotExist(inDest) {
				t.Fatalf("%s should stay in source: %v %v", name, inSource, inDest)
			}
			continue
		}
		if !os.IsNotExist(inSource) || inDest != nil || string(b) != name {
			t.Fatalf("%s not moved: %v %v %q", name, inSource, inDest, b)
		}
	}
	if kept == "" {
		if _, err := os.Lstat(src); !os.IsNotExist(err) {
			t.Fatal("empty source folder left", err)
		}
	}
}

func TestCrossVolumeMoveSkipsSingleFileAndKeepsRest(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged user")
	}
	src, dst := acrossFixture(t)
	bad := filepath.Join(src, "bad")
	if err := os.Chmod(bad, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(bad, 0600)
	o := NewOperation("Move")
	var failures []Failure
	o.Failure = func(f Failure) ErrorAction { failures = append(failures, f); return ErrorSkip }
	// Exercise the EXDEV branch directly without mounting a test volume.
	if err := moveAcrossVolumes(src, dst, o); err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 || failures[0].Operation != "move" || failures[0].Source != bad || failures[0].Destination != filepath.Join(dst, "bad") {
		t.Fatalf("failures %+v", failures)
	}
	assertMovedExcept(t, src, dst, "bad")
	if p := o.Snapshot(); p.Skipped != 1 {
		t.Fatal("skipped", p.Skipped)
	}
}

func TestCrossVolumeMoveRetryRepeatsOnlyFailedFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged user")
	}
	src, dst := acrossFixture(t)
	bad := filepath.Join(src, "bad")
	if err := os.Chmod(bad, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(bad, 0600)
	o := NewOperation("Move")
	calls := map[string]int{}
	o.metadataCopier = func(s, d string, _ *Operation) error { calls[filepath.Base(s)]++; return nil }
	o.Failure = func(f Failure) ErrorAction {
		if err := os.Chmod(f.Source, 0600); err != nil {
			t.Fatal(err)
		}
		return ErrorRetry
	}
	if err := moveAcrossVolumes(src, dst, o); err != nil {
		t.Fatal(err)
	}
	assertMovedExcept(t, src, dst, "")
	for _, name := range []string{"a", "bad", "c", "nested", "src"} {
		if calls[name] != 1 {
			t.Fatalf("%s copied %d times: %v", name, calls[name], calls)
		}
	}
}

func TestCrossVolumeMoveMetadataSkipKeepsOnlyThatFile(t *testing.T) {
	src, dst := acrossFixture(t)
	o := NewOperation("Move")
	o.metadataCopier = func(s, d string, _ *Operation) error {
		if filepath.Base(s) == "bad" {
			return errors.New("cannot retain ACL")
		}
		return nil
	}
	o.MetadataFailure = func(s, d string, err error) error {
		o.SkipItem(s)
		return ErrSkipped
	}
	if err := moveAcrossVolumes(src, dst, o); err != nil {
		t.Fatal(err)
	}
	assertMovedExcept(t, src, dst, "bad")
}
