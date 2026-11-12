package fsops

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func symlink(t *testing.T, text, path string) {
	t.Helper()
	if err := os.Symlink(text, path); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
}

func readLink(t *testing.T, path string) string {
	t.Helper()
	text, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

// linkFixture: src/{f.txt, d/keep, lf -> f.txt, ld -> d, abs -> <abs f.txt>,
// broken -> missing}, ext/keep, and folder src/tree with tree/out ->
// ../../ext/keep, tree/in -> sub/x, tree/sub/x. Destination folders are dst
// (sibling of src) and dst/deeper/deepest (a different depth).
func linkFixture(t *testing.T) (root string) {
	t.Helper()
	root, _ = filepath.EvalSymlinks(t.TempDir())
	src := filepath.Join(root, "src")
	mkdir(t, filepath.Join(src, "d"))
	mkdir(t, filepath.Join(src, "tree", "sub"))
	mkdir(t, filepath.Join(root, "ext"))
	mkdir(t, filepath.Join(root, "dst", "deeper", "deepest"))
	put(t, filepath.Join(src, "f.txt"), "file")
	put(t, filepath.Join(src, "d", "keep"), "dir")
	put(t, filepath.Join(root, "ext", "keep"), "ext")
	put(t, filepath.Join(src, "tree", "sub", "x"), "x")
	symlink(t, "f.txt", filepath.Join(src, "lf"))
	symlink(t, "d", filepath.Join(src, "ld"))
	symlink(t, filepath.Join(src, "f.txt"), filepath.Join(src, "abs"))
	symlink(t, "missing", filepath.Join(src, "broken"))
	symlink(t, "../../ext/keep", filepath.Join(src, "tree", "out"))
	symlink(t, "sub/x", filepath.Join(src, "tree", "in"))
	return root
}

func assertReads(t *testing.T, path, want string) {
	t.Helper()
	if b, err := os.ReadFile(path); err != nil || string(b) != want {
		t.Fatalf("%s: %q %v", path, b, err)
	}
}

func TestSmartRelativeLinksStayValid(t *testing.T) {
	for _, move := range []bool{false, true} {
		for _, dir := range []string{"dst", "dst/deeper/deepest"} {
			root := linkFixture(t)
			dst := filepath.Join(root, dir)
			o := NewOperation("Copy")
			o.TransferRoots([]string{filepath.Join(root, "src", "lf"), filepath.Join(root, "src", "ld")})
			for _, name := range []string{"lf", "ld"} {
				if err := Transfer(filepath.Join(root, "src", name), filepath.Join(dst, name), move, nil, o); err != nil {
					t.Fatal(err)
				}
			}
			assertReads(t, filepath.Join(dst, "lf"), "file")
			assertReads(t, filepath.Join(dst, "ld", "keep"), "dir")
			if text := readLink(t, filepath.Join(dst, "lf")); filepath.IsAbs(text) {
				t.Fatal("same-volume rewrite must stay relative", text)
			}
			if !move && readLink(t, filepath.Join(root, "src", "lf")) != "f.txt" {
				t.Fatal("source link changed")
			}
		}
	}
}

func TestSmartLinkIntoTransferredSetIsVerbatim(t *testing.T) {
	for _, move := range []bool{false, true} {
		root := linkFixture(t)
		dst := filepath.Join(root, "dst")
		o := NewOperation("Copy")
		src := filepath.Join(root, "src")
		o.TransferRoots([]string{filepath.Join(src, "f.txt"), filepath.Join(src, "lf")})
		for _, name := range []string{"f.txt", "lf"} {
			if err := Transfer(filepath.Join(src, name), filepath.Join(dst, name), move, nil, o); err != nil {
				t.Fatal(err)
			}
		}
		if text := readLink(t, filepath.Join(dst, "lf")); text != "f.txt" {
			t.Fatal("link into the set rewritten", text)
		}
		assertReads(t, filepath.Join(dst, "lf"), "file")
	}
}

func TestSmartNestedLinkOutsideFolderRewritten(t *testing.T) {
	for _, move := range []bool{false, true} {
		root := linkFixture(t)
		src := filepath.Join(root, "src", "tree")
		dst := filepath.Join(root, "dst", "deeper", "tree")
		o := NewOperation("Copy")
		if err := Transfer(src, dst, move, nil, o); err != nil {
			t.Fatal(err)
		}
		assertReads(t, filepath.Join(dst, "out"), "ext")
		if text := readLink(t, filepath.Join(dst, "out")); text != "../../../ext/keep" {
			t.Fatal("outside link", text)
		}
		if text := readLink(t, filepath.Join(dst, "in")); text != "sub/x" {
			t.Fatal("inside link rewritten", text)
		}
		if !move && readLink(t, filepath.Join(src, "out")) != "../../ext/keep" {
			t.Fatal("source changed")
		}
	}
}

func TestSmartRenameInPlaceKeepsLinks(t *testing.T) {
	root := linkFixture(t)
	src := filepath.Join(root, "src")
	o := NewOperation("Move")
	if err := Transfer(filepath.Join(src, "tree"), filepath.Join(src, "renamed"), true, nil, o); err != nil {
		t.Fatal(err)
	}
	if text := readLink(t, filepath.Join(src, "renamed", "out")); text != "../../ext/keep" {
		t.Fatal(text)
	}
}

func TestSmartAbsoluteAndBrokenLinksVerbatim(t *testing.T) {
	for _, move := range []bool{false, true} {
		root := linkFixture(t)
		src := filepath.Join(root, "src")
		dst := filepath.Join(root, "dst", "deeper")
		abs := filepath.Join(src, "f.txt")
		o := NewOperation("Copy")
		o.TransferRoots([]string{filepath.Join(src, "abs"), filepath.Join(src, "broken")})
		for _, name := range []string{"abs", "broken"} {
			if err := Transfer(filepath.Join(src, name), filepath.Join(dst, name), move, nil, o); err != nil {
				t.Fatal(err)
			}
		}
		if readLink(t, filepath.Join(dst, "abs")) != abs || readLink(t, filepath.Join(dst, "broken")) != "missing" {
			t.Fatal("verbatim links changed")
		}
	}
}

func TestAsIsKeepsLinkText(t *testing.T) {
	for _, move := range []bool{false, true} {
		root := linkFixture(t)
		dst := filepath.Join(root, "dst", "deeper")
		o := NewOperation("Copy")
		o.Symlinks = SymlinksAsIs
		if err := Transfer(filepath.Join(root, "src", "lf"), filepath.Join(dst, "lf"), move, nil, o); err != nil {
			t.Fatal(err)
		}
		if err := Transfer(filepath.Join(root, "src", "tree"), filepath.Join(dst, "tree"), move, nil, o); err != nil {
			t.Fatal(err)
		}
		if readLink(t, filepath.Join(dst, "lf")) != "f.txt" || readLink(t, filepath.Join(dst, "tree", "out")) != "../../ext/keep" {
			t.Fatal("as-is link text changed")
		}
	}
}

func TestSmartStagedReplaceUsesPublishedLocation(t *testing.T) {
	root := linkFixture(t)
	dst := filepath.Join(root, "dst", "deeper", "lf")
	put(t, dst, "old")
	o := NewOperation("Copy")
	if err := Transfer(filepath.Join(root, "src", "lf"), dst, false, func(string, string) int { return Replace }, o); err != nil {
		t.Fatal(err)
	}
	assertReads(t, dst, "file")
	if text := readLink(t, dst); text != "../../src/f.txt" {
		t.Fatal(text)
	}
}

func TestSmartLinkTextAcrossVolumes(t *testing.T) {
	roots := []string{"/a/src/tree"}
	exists := func(string) bool { return true }
	for _, c := range []struct {
		text, origin, parent string
		same                 bool
		want                 string
	}{
		{"../../ext/keep", "/a/src/tree", "/b/dst/tree", false, "/a/ext/keep"},
		{"../../ext/keep", "/a/src/tree", "/a/dst/deeper/tree", true, "../../../ext/keep"},
		{"sub/x", "/a/src/tree", "/b/dst/tree", false, "sub/x"},
		{"/abs/path", "/a/src", "/b", false, "/abs/path"},
		{"f.txt", "/a/src", "/a/src", true, "f.txt"},
	} {
		if got := smartLinkText(c.text, c.origin, c.parent, roots, c.same, exists); got != c.want {
			t.Fatalf("%+v: %q", c, got)
		}
	}
	if got := smartLinkText("missing", "/a/src", "/b", nil, false, func(string) bool { return false }); got != "missing" {
		t.Fatal("broken link rewritten", got)
	}
}

func TestCrossVolumeMoveRewritesOutsideLinks(t *testing.T) {
	root := linkFixture(t)
	dst := filepath.Join(root, "dst", "deeper", "tree")
	o := NewOperation("Move")
	// Exercise the EXDEV branch directly without mounting a test volume.
	if err := moveAcrossVolumes(filepath.Join(root, "src", "tree"), dst, o); err != nil {
		t.Fatal(err)
	}
	assertReads(t, filepath.Join(dst, "out"), "ext")
	if readLink(t, filepath.Join(dst, "in")) != "sub/x" {
		t.Fatal("inside link rewritten")
	}
	if _, err := os.Lstat(filepath.Join(root, "src", "tree")); !os.IsNotExist(err) {
		t.Fatal("source left", err)
	}
}

func TestCopyContentsDereferencesAndSurvivesLoop(t *testing.T) {
	root := linkFixture(t)
	src := filepath.Join(root, "src")
	symlink(t, "..", filepath.Join(src, "d", "loop"))
	o := NewOperation("Copy")
	o.Symlinks = SymlinksContents
	var failures []Failure
	o.Failure = func(f Failure) ErrorAction { failures = append(failures, f); return ErrorSkip }
	dst := filepath.Join(root, "dst")
	o.TransferRoots([]string{filepath.Join(src, "lf"), filepath.Join(src, "ld"), filepath.Join(src, "broken")})
	for _, name := range []string{"lf", "ld", "broken"} {
		if err := Transfer(filepath.Join(src, name), filepath.Join(dst, name), false, nil, o); err != nil {
			t.Fatal(name, err)
		}
	}
	for _, name := range []string{"lf", "ld", "ld/keep"} {
		if info, err := os.Lstat(filepath.Join(dst, name)); err != nil || info.Mode()&os.ModeSymlink != 0 {
			t.Fatal(name, "not dereferenced", err)
		}
	}
	assertReads(t, filepath.Join(dst, "lf"), "file")
	assertReads(t, filepath.Join(dst, "ld", "keep"), "dir")
	if _, err := os.Lstat(filepath.Join(dst, "ld", "loop")); !os.IsNotExist(err) {
		t.Fatal("loop copied", err)
	}
	if _, err := os.Lstat(filepath.Join(dst, "broken")); !os.IsNotExist(err) {
		t.Fatal("broken link copied", err)
	}
	if len(failures) != 2 || !strings.Contains(failures[0].Err.Error(), "loop") || !strings.Contains(failures[1].Err.Error(), "broken") {
		t.Fatalf("failures %+v", failures)
	}
	if readLink(t, filepath.Join(src, "ld")) != "d" {
		t.Fatal("source changed")
	}
}

func TestCopyContentsInsideFolderAndMove(t *testing.T) {
	for _, move := range []bool{false, true} {
		root := linkFixture(t)
		dst := filepath.Join(root, "dst", "tree")
		o := NewOperation("Copy")
		o.Symlinks = SymlinksContents
		if err := Transfer(filepath.Join(root, "src", "tree"), dst, move, nil, o); err != nil {
			t.Fatal(err)
		}
		for name, want := range map[string]string{"out": "ext", "in": "x"} {
			if info, err := os.Lstat(filepath.Join(dst, name)); err != nil || !info.Mode().IsRegular() {
				t.Fatal(move, name, "not dereferenced", err)
			}
			assertReads(t, filepath.Join(dst, name), want)
		}
		assertReads(t, filepath.Join(root, "ext", "keep"), "ext")
	}
}

func nlink(t *testing.T, path string) uint16 {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Sys().(*syscall.Stat_t).Nlink
}

func TestCopyPreservesHardlinks(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	mkdir(t, filepath.Join(src, "tree"))
	mkdir(t, dst)
	put(t, filepath.Join(src, "h1"), "linked")
	if err := os.Link(filepath.Join(src, "h1"), filepath.Join(src, "h2")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(src, "tree", "a"), "tree")
	if err := os.Link(filepath.Join(src, "tree", "a"), filepath.Join(src, "tree", "b")); err != nil {
		t.Fatal(err)
	}
	o := NewOperation("Copy")
	for _, name := range []string{"h1", "h2", "tree"} {
		if err := Transfer(filepath.Join(src, name), filepath.Join(dst, name), false, nil, o); err != nil {
			t.Fatal(err)
		}
	}
	for _, pair := range [][2]string{{"h1", "h2"}, {"tree/a", "tree/b"}} {
		a, _ := os.Lstat(filepath.Join(dst, pair[0]))
		b, _ := os.Lstat(filepath.Join(dst, pair[1]))
		if a == nil || b == nil || !os.SameFile(a, b) || nlink(t, filepath.Join(dst, pair[0])) != 2 {
			t.Fatal("hard link not preserved", pair)
		}
	}
	if nlink(t, filepath.Join(src, "h1")) != 2 {
		t.Fatal("source links changed")
	}
	assertReads(t, filepath.Join(dst, "h2"), "linked")
}

func TestHardlinkFailedFirstCopyLetsNextNameCopy(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	put(t, filepath.Join(root, "h1"), "linked")
	if err := os.Link(filepath.Join(root, "h1"), filepath.Join(root, "h2")); err != nil {
		t.Fatal(err)
	}
	o := NewOperation("Copy")
	info, _ := os.Lstat(filepath.Join(root, "h1"))
	linked, release, err := o.copyHardlink(filepath.Join(root, "h1"), filepath.Join(root, "c1"), info)
	if linked || release == nil || err != nil {
		t.Fatal("first name must own the copy", linked, err)
	}
	release(false)
	linked, release, err = o.copyHardlink(filepath.Join(root, "h2"), filepath.Join(root, "c2"), info)
	if linked || release == nil || err != nil {
		t.Fatal("after a failed copy the next name copies", linked, err)
	}
	release(false)
}

func TestCollisionReportsSourceLink(t *testing.T) {
	root := linkFixture(t)
	dst := filepath.Join(root, "dst", "lf")
	put(t, dst, "old")
	var seen Collision
	o := NewOperation("Copy")
	o.Collision = func(c Collision) Resolution { seen = c; return Resolution{Action: Skip} }
	if err := Transfer(filepath.Join(root, "src", "lf"), dst, false, nil, o); err != nil {
		t.Fatal(err)
	}
	if seen.SourceLink != "f.txt" || seen.SourceBroken {
		t.Fatalf("%+v", seen)
	}
}
