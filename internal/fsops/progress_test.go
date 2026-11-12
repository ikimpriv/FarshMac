package fsops

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyAndMoveProgress(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source")
	os.Mkdir(src, 0700)
	os.WriteFile(filepath.Join(src, "data"), bytes.Repeat([]byte("x"), 1024*1024), 0600)
	os.Symlink("data", filepath.Join(src, "link"))
	o := NewOperation("Copy")
	if e := o.Scan([]string{src}); e != nil {
		t.Fatal(e)
	}
	if e := Copy(src, filepath.Join(root, "copy"), o); e != nil {
		t.Fatal(e)
	}
	p := o.Snapshot()
	if p.Bytes != p.TotalBytes || p.Files != p.TotalFiles || p.TotalBytes != 1024*1024 {
		t.Fatal(p)
	}
	o = NewOperation("Move")
	o.Scan([]string{src})
	if e := Transfer(src, filepath.Join(root, "moved"), true, func(string, string) int { return 0 }, o); e != nil {
		t.Fatal(e)
	}
	p = o.Snapshot()
	if p.Bytes != p.TotalBytes || p.Files != p.TotalFiles {
		t.Fatal("rename progress", p)
	}
}

type cancellingReader struct {
	o *Operation
	n int
}

func (r *cancellingReader) Read(b []byte) (int, error) {
	if r.n > 0 {
		r.o.Cancel()
	}
	r.n++
	copy(b, "hello")
	return 5, nil
}
func TestProgressCancellationAndCleanup(t *testing.T) {
	o := NewOperation("cancel")
	_, e := copyProgress(io.Discard, &cancellingReader{o: o}, o)
	if !errors.Is(e, ErrCancelled) {
		t.Fatal(e)
	}
	root := t.TempDir()
	src := filepath.Join(root, "source")
	os.WriteFile(src, []byte("original"), 0600)
	dst := filepath.Join(root, "dest")
	if e = Copy(src, dst, o); !errors.Is(e, ErrCancelled) {
		t.Fatal(e)
	}
	if _, e = os.Stat(dst); !os.IsNotExist(e) {
		t.Fatal("cancel published destination")
	}
	if e = Remove(src, o); !errors.Is(e, ErrCancelled) {
		t.Fatal(e)
	}
	if b, _ := os.ReadFile(src); string(b) != "original" {
		t.Fatal("cancel removed source")
	}
}
func TestArchiveAndDeleteProgress(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "data")
	os.WriteFile(src, bytes.Repeat([]byte("abc"), 10000), 0600)
	archive := filepath.Join(root, "archive.zip")
	o := NewOperation("zip")
	if e := CreateZIP([]string{src}, archive, o); e != nil {
		t.Fatal(e)
	}
	p := o.Snapshot()
	if p.Bytes != p.TotalBytes || p.Files != p.TotalFiles {
		t.Fatal(p)
	}
	o = NewOperation("extract")
	dst := filepath.Join(root, "out")
	if e := ExtractArchive(archive, dst, o); e != nil {
		t.Fatal(e)
	}
	p = o.Snapshot()
	if p.Bytes != p.TotalBytes || p.Files != p.TotalFiles {
		t.Fatal(p)
	}
	o = NewOperation("delete")
	o.Scan([]string{dst})
	if e := Remove(dst, o); e != nil {
		t.Fatal(e)
	}
	p = o.Snapshot()
	if p.Bytes != p.TotalBytes || p.Files != p.TotalFiles {
		t.Fatal(p)
	}
}
