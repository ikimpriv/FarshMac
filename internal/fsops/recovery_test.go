package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAppendMoveRetriesOnlySourceDeletion(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged user")
	}
	left, right := t.TempDir(), t.TempDir()
	src, dst := filepath.Join(left, "file"), filepath.Join(right, "file")
	put(t, src, "new")
	put(t, dst, "old")
	o := NewOperation("Move")
	defer o.Cancel()
	o.Collision = func(Collision) Resolution { return Resolution{Action: Append} }
	calls := 0
	o.Failure = func(f Failure) ErrorAction {
		calls++
		if f.Operation != "delete source" {
			t.Fatalf("wrong step: %+v", f)
		}
		if e := os.Chmod(left, 0700); e != nil {
			t.Fatal(e)
		}
		return ErrorRetry
	}
	if e := os.Chmod(left, 0500); e != nil {
		t.Fatal(e)
	}
	defer os.Chmod(left, 0700)
	if e := Transfer(src, dst, true, nil, o); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	if b, e := os.ReadFile(dst); e != nil || string(b) != "oldnew" {
		t.Fatalf("append repeated: %q %v", b, e)
	}
	if _, e := os.Stat(src); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}
func TestDeleteSkipKeepsParentAndContinuesSiblings(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "folder")
	if e := os.Mkdir(src, 0700); e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(src, "blocked"), "keep")
	put(t, filepath.Join(src, "ok"), "delete")
	o := NewOperation("Delete")
	defer o.Cancel()
	questions := 0
	o.Failure = func(f Failure) ErrorAction { questions++; return ErrorSkip }
	err := removeTreeWith(src, o, func(p string) error {
		if filepath.Base(p) == "blocked" {
			return os.ErrPermission
		}
		return os.Remove(p)
	})
	if !errors.Is(err, ErrSkipped) {
		t.Fatal(err)
	}
	if questions != 1 {
		t.Fatal("retried skipped child", questions)
	}
	if b, e := os.ReadFile(filepath.Join(src, "blocked")); e != nil || string(b) != "keep" {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(src, "ok")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}
func TestAppendCancellationPreservesOriginal(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	put(t, src, "new")
	put(t, dst, "old")
	o := NewOperation("Copy")
	defer o.Cancel()
	o.Collision = func(Collision) Resolution { o.Cancel(); return Resolution{Action: Append} }
	if e := Transfer(src, dst, false, nil, o); !errors.Is(e, ErrCancelled) {
		t.Fatal(e)
	}
	if b, e := os.ReadFile(dst); e != nil || string(b) != "old" {
		t.Fatal(e)
	}
}

func TestMetadataRetryRetainsStagedCopy(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	put(t, src, "new")
	put(t, dst, "old")
	o := NewOperation("Copy")
	defer o.Cancel()
	o.CopyMetadata = &MetadataOptions{Permissions: true}
	calls := 0
	o.metadataCopier = func(string, string, *Operation) error {
		calls++
		if calls == 1 {
			return os.ErrPermission
		}
		return nil
	}
	o.MetadataFailure = func(string, string, error) error { return ErrRetryMetadata }
	if e := Transfer(src, dst, false, func(string, string) int { return Replace }, o); e != nil {
		t.Fatal(e)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
	if b, e := os.ReadFile(dst); e != nil || string(b) != "new" {
		t.Fatal(e)
	}
}
