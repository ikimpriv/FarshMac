package fsops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateArchiveSkipsUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged user")
	}
	for _, name := range []string{"out.zip", "out.tar.gz"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			src := filepath.Join(root, "src")
			if err := os.Mkdir(src, 0700); err != nil {
				t.Fatal(err)
			}
			put(t, filepath.Join(src, "good"), "good")
			bad := filepath.Join(src, "bad")
			put(t, bad, "bad")
			if err := os.Chmod(bad, 0); err != nil {
				t.Fatal(err)
			}
			defer os.Chmod(bad, 0600)
			o := NewOperation("Creating archive")
			defer o.Cancel()
			var failures []Failure
			o.Failure = func(f Failure) ErrorAction { failures = append(failures, f); return ErrorSkip }
			dst := filepath.Join(root, name)
			if err := CreateArchive([]string{src}, dst, o); err != nil {
				t.Fatal(err)
			}
			if len(failures) != 1 || failures[0].Operation != "read" || failures[0].Source != bad {
				t.Fatalf("failures %+v", failures)
			}
			if o.Snapshot().Skipped != 1 {
				t.Fatal("skip not counted")
			}
			listing, err := ArchiveContents(dst, false)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(listing, "src/good") || strings.Contains(listing, "src/bad") {
				t.Fatal(listing)
			}
		})
	}
}
