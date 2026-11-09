package fsops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestTransferCollisionDecisions(t *testing.T) {
	for _, move := range []bool{false, true} {
		for _, choice := range []int{0, 1, 2, -1} {
			t.Run(fmt.Sprintf("move=%v/choice=%d", move, choice), func(t *testing.T) {
				root := t.TempDir()
				src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
				put(t, src, "new")
				put(t, dst, "old")
				var conflict Conflict
				if choice >= 0 {
					conflict = func(string, string) int { return choice }
				}
				err := Transfer(src, dst, move, conflict)
				if choice == 2 || choice == -1 {
					if !errors.Is(err, ErrCancelled) {
						t.Fatal(err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				want := "old"
				if choice == 0 {
					want = "new"
				}
				if b, e := os.ReadFile(dst); e != nil || string(b) != want {
					t.Fatalf("destination %q %v", b, e)
				}
				b, e := os.ReadFile(src)
				if move && choice == 0 {
					if !os.IsNotExist(e) {
						t.Fatal("source retained", e)
					}
				} else if e != nil || string(b) != "new" {
					t.Fatal("source lost", e)
				}
			})
		}
	}
}

func TestMoveMergeSkipRetainsSkippedSource(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	for _, p := range []string{src, dst} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	put(t, filepath.Join(src, "collision"), "new")
	put(t, filepath.Join(dst, "collision"), "old")
	put(t, filepath.Join(src, "unique"), "unique")
	put(t, filepath.Join(dst, "keep"), "keep")
	if err := Transfer(src, dst, true, func(string, string) int { return 1 }); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]string{filepath.Join(src, "collision"): "new", filepath.Join(dst, "collision"): "old", filepath.Join(dst, "unique"): "unique", filepath.Join(dst, "keep"): "keep"} {
		if b, e := os.ReadFile(p); e != nil || string(b) != want {
			t.Fatal(p, string(b), e)
		}
	}
	if _, e := os.Stat(filepath.Join(src, "unique")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}

func TestTransferTypeCollisionKeepsBoth(t *testing.T) {
	for _, move := range []bool{false, true} {
		for _, sourceDir := range []bool{false, true} {
			t.Run(fmt.Sprintf("move=%v/sourceDir=%v", move, sourceDir), func(t *testing.T) {
				root := t.TempDir()
				src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
				file, dir := src, dst
				if sourceDir {
					file, dir = dst, src
				}
				put(t, file, "file")
				if e := os.Mkdir(dir, 0700); e != nil {
					t.Fatal(e)
				}
				put(t, filepath.Join(dir, "child"), "child")
				if e := Transfer(src, dst, move, func(string, string) int { return 0 }); e == nil {
					t.Fatal("type mismatch accepted")
				}
				if b, e := os.ReadFile(file); e != nil || string(b) != "file" {
					t.Fatal(e)
				}
				if b, e := os.ReadFile(filepath.Join(dir, "child")); e != nil || string(b) != "child" {
					t.Fatal(e)
				}
			})
		}
	}
}

func TestTransferPermissionFailuresKeepOriginals(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission tests require an unprivileged user")
	}
	for _, kind := range []string{"read", "destination-parent", "source-parent"} {
		for _, move := range []bool{false, true} {
			if kind == "read" && move || kind == "source-parent" && !move {
				continue
			}
			t.Run(fmt.Sprintf("%s/move=%v", kind, move), func(t *testing.T) {
				root := t.TempDir()
				left, right := filepath.Join(root, "left"), filepath.Join(root, "right")
				for _, p := range []string{left, right} {
					if e := os.Mkdir(p, 0700); e != nil {
						t.Fatal(e)
					}
				}
				src, dst := filepath.Join(left, "file"), filepath.Join(right, "file")
				put(t, src, "new")
				put(t, dst, "old")
				restricted, mode := src, os.FileMode(0000)
				if kind == "destination-parent" {
					restricted, mode = right, 0500
				}
				if kind == "source-parent" {
					restricted, mode = left, 0500
				}
				if e := os.Chmod(restricted, mode); e != nil {
					t.Fatal(e)
				}
				defer os.Chmod(restricted, 0700)
				err := Transfer(src, dst, move, func(string, string) int { return 0 })
				if !errors.Is(err, os.ErrPermission) {
					t.Fatalf("expected permission error, got %v", err)
				}
				if e := os.Chmod(restricted, 0700); e != nil {
					t.Fatal(e)
				}
				for p, want := range map[string]string{src: "new", dst: "old"} {
					if b, e := os.ReadFile(p); e != nil || string(b) != want {
						t.Fatalf("lost %s: %q %v", p, b, e)
					}
				}
				entries, e := os.ReadDir(right)
				if e != nil || len(entries) != 1 {
					t.Fatalf("temporary output leaked: %v %v", entries, e)
				}
			})
		}
	}
}
