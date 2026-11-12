package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func linkedDirectory(t *testing.T) (root, target, link string) {
	t.Helper()
	root = t.TempDir()
	target, link = filepath.Join(root, "target"), filepath.Join(root, "link")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(target, "keep"), "keep")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	return root, target, link
}

func assertLink(t *testing.T, link, target string) {
	t.Helper()
	if got, err := os.Readlink(link); err != nil || got != target {
		t.Fatalf("link replaced: %q %v", got, err)
	}
}

func TestTransferIntoSymlinkedDirectory(t *testing.T) {
	for _, move := range []bool{false, true} {
		root, target, link := linkedDirectory(t)
		file := filepath.Join(root, "file")
		put(t, file, "file")
		if err := Transfer(file, filepath.Join(link, "file"), move, nil); err != nil {
			t.Fatal(err)
		}
		// A folder named like the link, as when F5 suggests "<dest>/<name>".
		folder := filepath.Join(root, "src", "link")
		if err := os.MkdirAll(folder, 0700); err != nil {
			t.Fatal(err)
		}
		put(t, filepath.Join(folder, "child"), "child")
		if err := Transfer(folder, link, move, func(string, string) int { return Cancel }); err != nil {
			t.Fatal("merge into linked directory", err)
		}
		assertLink(t, link, target)
		for name, want := range map[string]string{"keep": "keep", "file": "file", "child": "child"} {
			if b, err := os.ReadFile(filepath.Join(target, name)); err != nil || string(b) != want {
				t.Fatalf("move=%v %s: %q %v", move, name, b, err)
			}
		}
		_, err := os.Lstat(folder)
		if move != os.IsNotExist(err) {
			t.Fatalf("move=%v source state %v", move, err)
		}
	}
}

func TestMoveIntoSymlinkedDirectoryKeepsLink(t *testing.T) {
	root, target, link := linkedDirectory(t)
	file := filepath.Join(root, "file")
	put(t, file, "file")
	asked := false
	err := Transfer(file, link, true, func(string, string) int { asked = true; return Replace })
	if err == nil || !asked {
		t.Fatalf("file onto linked directory must collide and refuse replacement: %v asked=%v", err, asked)
	}
	assertLink(t, link, target)
	if b, err := os.ReadFile(file); err != nil || string(b) != "file" {
		t.Fatal("source lost", err)
	}
}

func TestCollisionWithFileSymlinkStillAsks(t *testing.T) {
	root := t.TempDir()
	src, real, link, broken := filepath.Join(root, "src"), filepath.Join(root, "real"), filepath.Join(root, "link"), filepath.Join(root, "broken")
	put(t, src, "new")
	put(t, real, "old")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), broken); err != nil {
		t.Fatal(err)
	}
	for _, dst := range []string{link, broken} {
		var seen Collision
		o := NewOperation("Copy")
		o.Collision = func(c Collision) Resolution { seen = c; return Resolution{Action: Skip} }
		if err := Transfer(src, dst, false, nil, o); err != nil {
			t.Fatal(err)
		}
		if seen.Destination != dst || seen.DestinationLink == "" || seen.DestinationBroken != (dst == broken) {
			t.Fatalf("collision %+v", seen)
		}
		if info, err := os.Lstat(dst); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("link changed", err)
		}
	}
}
