package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCopyFastAndStreamingMetadataProgressAndIndependence(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "Native", true: "Fallback"}[stream], func(t *testing.T) {
			root := t.TempDir()
			src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
			put(t, src, "source data")
			mtime := time.Unix(1600000000, 0)
			os.Chtimes(src, mtime, mtime)
			info, _ := os.Stat(src)
			op := NewOperation("Copy")
			op.Add(src, info.Size())
			op.Ready()
			var err error
			if stream {
				f, e := os.Open(src)
				if e != nil {
					t.Fatal(e)
				}
				defer f.Close()
				op.Begin(src, info.Size())
				err = copyRegularStream(f, dst, info, op)
			} else {
				err = Copy(src, dst, op)
			}
			if err != nil {
				t.Fatal(err)
			}
			copied, e := os.Stat(dst)
			if e != nil {
				t.Fatal(e)
			}
			if copied.Mode().Perm() != info.Mode().Perm() || !copied.ModTime().Equal(info.ModTime()) {
				t.Fatal("metadata changed")
			}
			p := op.Snapshot()
			if p.Bytes != info.Size() || p.FileBytes != info.Size() || p.Files != 1 {
				t.Fatalf("progress: %+v", p)
			}
			put(t, dst, "independent")
			b, _ := os.ReadFile(src)
			if string(b) != "source data" {
				t.Fatal("clone writes affected source")
			}
			if stream {
				f, _ := os.Open(src)
				defer f.Close()
				if copyRegularStream(f, dst, info, nil) == nil {
					t.Fatal("stream overwrote target")
				}
			} else if Copy(src, dst) == nil {
				t.Fatal("clone overwrote target")
			}
			b, _ = os.ReadFile(dst)
			if string(b) != "independent" {
				t.Fatal("existing destination changed")
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 2 {
				t.Fatal("temporary files leaked")
			}
		})
	}
}
func TestCopyCancellationDoesNotPublish(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	put(t, src, "source")
	op := NewOperation("Copy")
	op.Cancel()
	if err := Copy(src, dst, op); !errors.Is(err, ErrCancelled) {
		t.Fatal(err)
	}
	f, _ := os.Open(src)
	defer f.Close()
	info, _ := f.Stat()
	if err := copyRegularStream(f, dst, info, op); !errors.Is(err, ErrCancelled) {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatal("cancel left output")
	}
}
