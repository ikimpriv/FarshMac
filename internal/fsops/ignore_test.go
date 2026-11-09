package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func ignoreFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "source")
	for _, name := range []string{"file", ".hidden", ".DS_Store", "nested/file", "nested/.DS_Store", ".git/config"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0640); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func assertIgnoredCopy(t *testing.T, path string, ignored bool) {
	t.Helper()
	for _, name := range []string{"file", ".hidden", "nested/file", ".git/config", ".DS_Store", "nested/.DS_Store"} {
		_, err := os.Stat(filepath.Join(path, name))
		if ignored && filepath.Base(name) == ".DS_Store" {
			if !os.IsNotExist(err) {
				t.Fatalf("not ignored: %s (%v)", name, err)
			}
		} else if err != nil {
			t.Fatal(name, err)
		}
	}
}

func TestIgnoreDSStoreCopyMoveRemove(t *testing.T) {
	for _, ignored := range []bool{false, true} {
		src := ignoreFixture(t)
		o := NewOperation("Copy")
		o.IgnoreDSStore = ignored
		if err := o.Scan([]string{src}); err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(t.TempDir(), "copy")
		if err := Copy(src, dst, o); err != nil {
			t.Fatal(err)
		}
		assertIgnoredCopy(t, dst, ignored)
		if p := o.Snapshot(); p.Files != p.TotalFiles || p.Bytes != p.TotalBytes {
			t.Fatal(p)
		}
		original, _ := os.Stat(src)
		moved := filepath.Join(t.TempDir(), "moved")
		if err := Transfer(src, moved, true, func(string, string) int { t.Fatal("unexpected conflict"); return 2 }, o); err != nil {
			t.Fatal(err)
		}
		actual, err := os.Stat(moved)
		if err != nil || !os.SameFile(original, actual) {
			t.Fatal("move was not a rename", err)
		}
		assertIgnoredCopy(t, moved, false)
		if err := Remove(moved, o); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(moved); !os.IsNotExist(err) {
			t.Fatal("metadata prevented removal", err)
		}
	}
}

func TestIgnoreDSStoreArchiveRoundTrip(t *testing.T) {
	for _, ext := range []string{"zip", "tar.gz", "tar.bz2", "tar.xz", "7z"} {
		t.Run(ext, func(t *testing.T) {
			src := ignoreFixture(t)
			archive := filepath.Join(t.TempDir(), "all."+ext)
			if err := CreateArchive([]string{src}, archive); err != nil {
				t.Fatal(err)
			}
			o := NewOperation("Extract")
			o.IgnoreDSStore = true
			dst := filepath.Join(t.TempDir(), "extract")
			if err := ExtractArchive(archive, dst, o); err != nil {
				t.Fatal(err)
			}
			assertIgnoredCopy(t, filepath.Join(dst, "source"), true)
			index, err := ReadArchiveIndex(archive, nil)
			if err != nil {
				t.Fatal(err)
			}
			selected := filepath.Join(t.TempDir(), "selected")
			if err := index.ExtractSelection([]string{"source"}, selected, o); err != nil {
				t.Fatal(err)
			}
			assertIgnoredCopy(t, filepath.Join(selected, "source"), true)
			filtered := filepath.Join(t.TempDir(), "filtered."+ext)
			if err := CreateArchive([]string{src}, filtered, o); err != nil {
				t.Fatal(err)
			}
			all := filepath.Join(t.TempDir(), "all")
			if err := ExtractArchive(filtered, all); err != nil {
				t.Fatal(err)
			}
			assertIgnoredCopy(t, filepath.Join(all, "source"), true)
		})
	}
}
