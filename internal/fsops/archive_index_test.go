package fsops

import (
	"archive/tar"
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArchiveIndexAndSelectionFormats(t *testing.T) {
	for _, ext := range []string{"zip", "tar", "tar.gz", "tar.bz2", "tar.xz", "7z"} {
		t.Run(ext, func(t *testing.T) {
			root := t.TempDir()
			src := filepath.Join(root, "source")
			os.MkdirAll(filepath.Join(src, "dir"), 0700)
			put(t, filepath.Join(src, "dir", "кириллица.txt"), "selected")
			put(t, filepath.Join(src, "skip.txt"), "not selected")
			arc := filepath.Join(root, "test."+ext)
			if err := CreateArchive([]string{src}, arc); err != nil {
				t.Fatal(err)
			}
			index, err := ReadArchiveIndex(arc, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(index.Children["source"]) != 2 || !index.Members["source/dir"].IsDir() {
				t.Fatalf("index: %+v", index.Members)
			}
			dst := filepath.Join(root, "out")
			if err = index.ExtractSelection([]string{"source/dir"}, dst, nil); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(dst, "source/dir/кириллица.txt"))
			if err != nil || string(got) != "selected" {
				t.Fatal(string(got), err)
			}
			if _, err = os.Stat(filepath.Join(dst, "source/skip.txt")); !os.IsNotExist(err) {
				t.Fatal("unselected file extracted")
			}
			if err = index.ExtractSelection([]string{"../escape"}, filepath.Join(root, "bad"), nil); err == nil {
				t.Fatal("unsafe selection accepted")
			}
			op := NewOperation("extract")
			op.Cancel()
			if err = index.ExtractSelection([]string{"source/dir"}, filepath.Join(root, "cancel"), op); !errors.Is(err, ErrCancelled) {
				t.Fatal(err)
			}
			if _, err = os.Stat(filepath.Join(root, "cancel")); !os.IsNotExist(err) {
				t.Fatal("cancel published output")
			}
			put(t, arc, "changed")
			if err = index.Check(); err == nil {
				t.Fatal("stale index accepted")
			}
		})
	}
}
func indexZIP(t *testing.T, names []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for _, name := range names {
		out, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		out.Write([]byte("text"))
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}
func TestArchiveIndexImplicitDirectoriesAndBadPaths(t *testing.T) {
	src := indexZIP(t, []string{"a/b/c.txt", "a/second.txt"})
	index, err := ReadArchiveIndex(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Children["."]) != 1 || !index.Members["a"].IsDir() || !index.Members["a/b"].IsDir() {
		t.Fatal("implicit dirs missing")
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err = index.ExtractSelection([]string{"a/b/c.txt"}, dst, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dst, "a/second.txt")); !os.IsNotExist(err) {
		t.Fatal("sibling extracted")
	}
	for _, names := range [][]string{{"../escape"}, {"/absolute"}, {"C:/drive"}, {"a", "a/b"}, {"same", "same"}, {strings.Repeat("deep/", 66) + "file"}} {
		if _, err = ReadArchiveIndex(indexZIP(t, names), nil); err == nil {
			t.Fatalf("unsafe/conflicting index accepted: %v", names)
		}
	}
}
func TestArchiveIndexCancel(t *testing.T) {
	src := indexZIP(t, []string{"a/b/c.txt"})
	op := NewOperation("index")
	op.Cancel()
	if _, err := ReadArchiveIndex(src, op); !errors.Is(err, ErrCancelled) {
		t.Fatal(err)
	}
}

func TestZIPSelectionVerifiesSelectedPayloadOnly(t *testing.T) {
	src := indexZIP(t, []string{"good.txt", "bad.txt"})
	z, err := zip.OpenReader(src)
	if err != nil {
		t.Fatal(err)
	}
	offset, err := z.File[1].DataOffset()
	if err != nil {
		t.Fatal(err)
	}
	z.Close()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	data[offset] ^= 0xff
	if err = os.WriteFile(src, data, 0600); err != nil {
		t.Fatal(err)
	}
	index, err := ReadArchiveIndex(src, nil)
	if err != nil {
		t.Fatal("central directory must still be browsable", err)
	}
	if err = index.ExtractSelection([]string{"good.txt"}, filepath.Join(t.TempDir(), "good"), nil); err != nil {
		t.Fatal("read unselected corrupt payload", err)
	}
	dst := filepath.Join(t.TempDir(), "bad")
	if err = index.ExtractSelection([]string{"bad.txt"}, dst, nil); err == nil {
		t.Fatal("selected corrupt data accepted")
	}
	if _, err = os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("corrupt output published")
	}
}

func TestZIPSelectionStopsProducerOnConsumerFailure(t *testing.T) {
	src := indexZIP(t, []string{"a.txt", "b.txt"})
	failure := errors.New("consumer stopped")
	done := make(chan error, 1)
	go func() {
		done <- withArchiveSelection(src, nil, func(string) bool { return true }, func(*tar.Reader) error { return failure })
	}()
	select {
	case err := <-done:
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ZIP producer remained blocked")
	}
}
