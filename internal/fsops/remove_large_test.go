package fsops

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRemoveLargeDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "remove")
	os.Mkdir(root, 0700)
	for i := 0; i < 2200; i++ {
		os.WriteFile(filepath.Join(root, fmt.Sprintf("entry-%04d", i)), nil, 0600)
	}
	o := NewOperation("delete")
	if err := o.Scan([]string{root}); err != nil {
		t.Fatal(err)
	}
	if err := Remove(root, o); err != nil {
		entries, _ := os.ReadDir(root)
		t.Fatalf("remove: %v; remaining %d", err, len(entries))
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("root remained: %v", err)
	}
}

func TestRemoveRetriesRecreatedFinderMetadata(t *testing.T) {
	root := filepath.Join(t.TempDir(), "remove")
	os.Mkdir(root, 0700)
	os.WriteFile(filepath.Join(root, "file"), []byte("x"), 0600)
	o := NewOperation("delete")
	o.IgnoreDSStore = true
	o.Scan([]string{root})
	attempts := 0
	err := removeTreeWith(root, o, func(path string) error {
		if path == root {
			attempts++
			if attempts == 1 {
				if e := os.WriteFile(filepath.Join(root, ".DS_Store"), nil, 0600); e != nil {
					t.Fatal(e)
				}
			}
		}
		return os.Remove(path)
	})
	if err != nil || attempts != 2 {
		t.Fatal(attempts, err)
	}
	if _, e := os.Lstat(root); !os.IsNotExist(e) {
		t.Fatal("root remained", e)
	}
}
func TestRemoveRetryIsBoundedAndCancellable(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "remove")
			os.Mkdir(root, 0700)
			o := NewOperation("delete")
			attempts := 0
			err := removeTreeWith(root, o, func(path string) error {
				if path == root {
					attempts++
					os.WriteFile(filepath.Join(root, "recreated"), nil, 0600)
					if cancel {
						o.Cancel()
					}
				}
				return os.Remove(path)
			})
			if err == nil || attempts > 3 {
				t.Fatal("unbounded retries or suppressed error", attempts, err)
			}
			if cancel && err != ErrCancelled {
				t.Fatal("cancel ignored", err)
			}
		})
	}
}

func TestRemoveRetryDoesNotFollowReplacement(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "remove")
	os.Mkdir(root, 0700)
	replacement := filepath.Join(base, "other")
	os.Mkdir(replacement, 0700)
	os.WriteFile(filepath.Join(replacement, "keep"), nil, 0600)
	calls := 0
	err := removeTreeWith(root, NewOperation("delete"), func(path string) error {
		if path == root {
			calls++
			if calls == 1 {
				os.Rename(root, filepath.Join(base, "old"))
				os.Symlink(replacement, root)
				return syscall.ENOTEMPTY
			}
		}
		return os.Remove(path)
	})
	if err == nil || calls != 1 {
		t.Fatal("retried replacement", calls, err)
	}
	if _, err = os.Stat(filepath.Join(replacement, "keep")); err != nil {
		t.Fatal("replacement target affected", err)
	}
}
