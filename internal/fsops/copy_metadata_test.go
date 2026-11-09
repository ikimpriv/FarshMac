package fsops

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func aclText(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("/bin/ls", "-lde", path).CombinedOutput()
	if err != nil {
		t.Fatal(string(out), err)
	}
	_, acl, _ := strings.Cut(string(out), "\n")
	return acl
}

func TestCopyMetadataOptionsIndependent(t *testing.T) {
	for _, permissions := range []bool{false, true} {
		for _, attributes := range []bool{false, true} {
			t.Run(fmt.Sprintf("permissions=%v/attributes=%v", permissions, attributes), func(t *testing.T) {
				root := t.TempDir()
				src := filepath.Join(root, "source")
				dst := filepath.Join(root, "copy")
				if err := os.Mkdir(src, 0700); err != nil {
					t.Fatal(err)
				}
				file := filepath.Join(src, "file")
				if err := os.WriteFile(file, []byte("contents"), 0600); err != nil {
					t.Fatal(err)
				}
				stamp := time.Unix(1500000000, 123456789)
				for _, path := range []string{src, file} {
					if out, err := exec.Command("/usr/bin/xattr", "-w", "com.farshmac.copy-test", "value", path).CombinedOutput(); err != nil {
						t.Fatal(string(out), err)
					}
					if out, err := exec.Command("/bin/chmod", "+a", "everyone allow readattr,readextattr,readsecurity", path).CombinedOutput(); err != nil {
						t.Fatal(string(out), err)
					}
					if err := os.Chmod(path, 0701); err != nil {
						t.Fatal(err)
					}
					if err := os.Chtimes(path, stamp, stamp); err != nil {
						t.Fatal(err)
					}
					if err := syscall.Chflags(path, 0x8000); err != nil {
						t.Fatal(err)
					}
				}
				// Compare with actual creation defaults instead of changing umask.
				probeFile := filepath.Join(root, "probe-file")
				probeDir := filepath.Join(root, "probe-dir")
				if err := os.WriteFile(probeFile, nil, 0666); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(probeDir, 0777); err != nil {
					t.Fatal(err)
				}
				fp, _ := os.Stat(probeFile)
				dp, _ := os.Stat(probeDir)
				o := NewOperation("Copy")
				o.CopyMetadata = &MetadataOptions{permissions, attributes}
				if err := o.Scan([]string{src}); err != nil {
					t.Fatal(err)
				}
				if err := Copy(src, dst, o); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{".", "file"} {
					s, d := filepath.Join(src, name), filepath.Join(dst, name)
					si, _ := os.Stat(s)
					di, err := os.Stat(d)
					if err != nil {
						t.Fatal(err)
					}
					wantMode := fp.Mode().Perm()
					if name == "." {
						wantMode = dp.Mode().Perm()
					}
					if permissions {
						wantMode = si.Mode().Perm()
					}
					if di.Mode().Perm() != wantMode {
						t.Fatalf("%s mode %o want %o", name, di.Mode().Perm(), wantMode)
					}
					wantACL := ""
					if permissions {
						wantACL = aclText(t, s)
					}
					if got := aclText(t, d); got != wantACL {
						t.Fatalf("ACL %q want %q", got, wantACL)
					}
					out, err := exec.Command("/usr/bin/xattr", "-p", "com.farshmac.copy-test", d).CombinedOutput()
					if attributes {
						if err != nil || string(out) != "value\n" {
							t.Fatal("attribute missing", string(out), err)
						}
						if !di.ModTime().Equal(stamp) || di.Sys().(*syscall.Stat_t).Flags != si.Sys().(*syscall.Stat_t).Flags {
							t.Fatal("flags/timestamp lost")
						}
					} else {
						if err == nil || di.ModTime().Equal(stamp) || di.Sys().(*syscall.Stat_t).Flags&0x8000 != 0 {
							t.Fatal("unchecked attributes leaked")
						}
					}
				}
				if data, err := os.ReadFile(filepath.Join(dst, "file")); err != nil || string(data) != "contents" {
					t.Fatal(string(data), err)
				}
				if _, err := os.Stat(file); err != nil {
					t.Fatal("copy removed source", err)
				}
				if p := o.Snapshot(); p.Files != p.TotalFiles || p.Bytes != p.TotalBytes {
					t.Fatal(p)
				}
			})
		}
	}
}

func TestCopyMetadataFailureKeepsSourceAndOverwriteTarget(t *testing.T) {
	for _, accept := range []bool{false, true} {
		root := t.TempDir()
		src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
		put(t, src, "new")
		put(t, dst, "old")
		o := NewOperation("Copy")
		o.CopyMetadata = &MetadataOptions{true, true}
		o.metadataCopier = func(string, string, *Operation) error { return errors.New("attributes unsupported") }
		asked := false
		o.MetadataFailure = func(string, string, error) error {
			asked = true
			if accept {
				return nil
			}
			return ErrCancelled
		}
		err := Transfer(src, dst, false, func(string, string) int { return 0 }, o)
		if !asked {
			t.Fatal("metadata failure ignored")
		}
		if accept && err != nil || !accept && !errors.Is(err, ErrCancelled) {
			t.Fatal(err)
		}
		want := "old"
		if accept {
			want = "new"
		}
		if data, _ := os.ReadFile(dst); string(data) != want {
			t.Fatal("destination changed", string(data))
		}
		if data, _ := os.ReadFile(src); string(data) != "new" {
			t.Fatal("source changed")
		}
		entries, _ := os.ReadDir(root)
		if len(entries) != 2 {
			t.Fatal("temporary files leaked")
		}
	}
}
