package ui

import (
	"farshmac/internal/fsops"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectoryPreviewSummary(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "sub"), 0700)
	os.WriteFile(filepath.Join(root, "sub", "file"), []byte("hello"), 0600)
	os.WriteFile(filepath.Join(root, ".hidden"), []byte("xx"), 0600)
	os.Symlink(root, filepath.Join(root, "loop"))
	text := directoryPreview(root, fsops.NewOperation("test"), fsops.WalkDir, true, nil)
	for _, want := range []string{"Folders:        1", "Files:          3", "Symbolic links: 1", "Complete", "Size on disk:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	remote := directoryPreview(root, fsops.NewOperation("remote"), fsops.WalkDir, false, nil)
	for _, hidden := range []string{"FS block", "Unavailable", "Size on disk:", "Unreadable:"} {
		if strings.Contains(remote, hidden) {
			t.Fatalf("irrelevant field %s: %s", hidden, remote)
		}
	}
	o := fsops.NewOperation("cancel")
	o.Cancel()
	if text = directoryPreview(root, o, fsops.WalkDir, true, nil); !strings.Contains(text, "Incomplete") {
		t.Fatal(text)
	}
}

func TestNiceMetricMaximum(t *testing.T) {
	for _, tc := range []struct {
		v, want float64
		bytes   bool
	}{
		{.945, 1, false}, {1, 1, false}, {1.01, 2, false}, {.009, .01, false},
		{483.8 * 1024 * 1024, 512 * 1024 * 1024, true}, {725.8 * 1024 * 1024, 1024 * 1024 * 1024, true},
	} {
		if got := niceMetricMaximum(tc.v, tc.bytes); got != tc.want {
			t.Fatalf("%g => %g, want %g", tc.v, got, tc.want)
		}
	}
}
