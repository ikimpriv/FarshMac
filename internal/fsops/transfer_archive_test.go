package fsops

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestTransferMergeAndSymlinkReplacement(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	os.Mkdir(src, 0700)
	os.Mkdir(dst, 0700)
	os.WriteFile(filepath.Join(src, "f"), []byte("new"), 0600)
	os.WriteFile(filepath.Join(dst, "keep"), []byte("keep"), 0600)
	outside := filepath.Join(root, "outside")
	os.WriteFile(outside, []byte("outside"), 0600)
	os.Symlink(outside, filepath.Join(dst, "f"))
	if e := Transfer(src, dst, false, func(string, string) int { return 0 }); e != nil {
		t.Fatal(e)
	}
	for path, want := range map[string]string{outside: "outside", filepath.Join(dst, "f"): "new", filepath.Join(dst, "keep"): "keep"} {
		b, _ := os.ReadFile(path)
		if string(b) != want {
			t.Fatal(path, string(b))
		}
	}
	if e := Transfer(src, filepath.Join(src, "nested"), false, func(string, string) int { return 0 }); e == nil {
		t.Fatal("recursive self-copy allowed")
	}
	if e := Transfer(filepath.Join(src, "f"), filepath.Join(dst, "f"), true, func(string, string) int { return 2 }); !errors.Is(e, ErrCancelled) {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(src, "f")); e != nil {
		t.Fatal("cancel removed source")
	}
}
func TestZIPRoundtripAndTraversal(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	os.Mkdir(src, 0700)
	os.WriteFile(filepath.Join(src, "hello.txt"), []byte("hello"), 0600)
	archive := filepath.Join(root, "out.zip")
	if e := CreateZIP([]string{src}, archive); e != nil {
		t.Fatal(e)
	}
	if _, e := ZIPContents(archive, true); e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(root, "out")
	if e := ExtractZIP(archive, out); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(out, "src", "hello.txt"))
	if string(b) != "hello" {
		t.Fatal(string(b))
	}
	if e := CreateZIP([]string{src}, filepath.Join(src, "inside.zip")); e == nil {
		t.Fatal("archive inside source allowed")
	}
	for _, name := range []string{"../escaped", "/absolute", "nested/../../escape"} {
		bad := filepath.Join(root, "bad.zip")
		f, _ := os.Create(bad)
		z := zip.NewWriter(f)
		w, _ := z.Create(name)
		w.Write([]byte("bad"))
		z.Close()
		f.Close()
		if e := ExtractZIP(bad, filepath.Join(root, "bad-out")); e == nil {
			t.Fatal("unsafe path accepted", name)
		}
	}
	if _, e := os.Stat(filepath.Join(root, "escaped")); !os.IsNotExist(e) {
		t.Fatal("archive escaped")
	}
}
