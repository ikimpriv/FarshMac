package fsops

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCrossVolumeMovePreservesMetadataAndAllContents(t *testing.T) {
	src := ignoreFixture(t)
	file := filepath.Join(src, "file")
	if err := os.Link(file, filepath.Join(src, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("file", filepath.Join(src, "symlink")); err != nil {
		t.Fatal(err)
	}
	wantACL := map[string]string{}
	for _, path := range []string{src, file} {
		if out, err := exec.Command("/usr/bin/xattr", "-w", "com.farshmac.test", "metadata", path).CombinedOutput(); err != nil {
			t.Fatal(string(out), err)
		}
		if out, err := exec.Command("/bin/chmod", "+a", "everyone allow readattr,readextattr,readsecurity", path).CombinedOutput(); err != nil {
			t.Fatal(string(out), err)
		}
		out, err := exec.Command("/bin/ls", "-lde", path).CombinedOutput()
		if err != nil {
			t.Fatal(err)
		}
		_, acl, _ := strings.Cut(string(out), "\n")
		wantACL[filepath.Base(path)] = acl
	}
	stamp := time.Unix(1600000000, 123456789)
	if err := os.Chtimes(file, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0751|os.ModeSetgid); err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "moved")
	o := NewOperation("Move")
	o.IgnoreDSStore = true
	// Exercise the EXDEV branch directly without mounting a test volume.
	if err := moveAcrossVolumes(src, dst, o); err != nil {
		t.Fatal(err)
	}
	if p := o.Snapshot(); p.Files != p.TotalFiles || p.Bytes != p.TotalBytes {
		t.Fatal("move progress counted data twice", p)
	}
	assertIgnoredCopy(t, dst, false)
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source left behind", err)
	}
	actual := filepath.Join(dst, "file")
	info, err := os.Stat(actual)
	if err != nil || info.Mode() != original.Mode() || !info.ModTime().Equal(stamp) {
		t.Fatal(info, err)
	}
	linked, _ := os.Stat(filepath.Join(dst, "hardlink"))
	if !os.SameFile(info, linked) {
		t.Fatal("hard links became independent files")
	}
	if target, err := os.Readlink(filepath.Join(dst, "symlink")); err != nil || target != "file" {
		t.Fatal(target, err)
	}
	for _, path := range []string{dst, actual} {
		out, err := exec.Command("/usr/bin/xattr", "-p", "com.farshmac.test", path).CombinedOutput()
		if err != nil || string(out) != "metadata\n" {
			t.Fatal(string(out), err)
		}
		out, err = exec.Command("/bin/ls", "-lde", path).CombinedOutput()
		_, acl, _ := strings.Cut(string(out), "\n")
		key := filepath.Base(path)
		if path == dst {
			key = filepath.Base(src)
		}
		if err != nil || acl != wantACL[key] || acl == "" {
			t.Fatal("ACL lost", string(out), err)
		}
	}
}

func TestMoveMetadataFailureRetainsSourceUntilConsent(t *testing.T) {
	for _, accept := range []bool{false, true} {
		src := ignoreFixture(t)
		dst := filepath.Join(t.TempDir(), "moved")
		o := NewOperation("Move")
		o.IgnoreDSStore = true
		o.metadataCopier = func(string, string, *Operation) error { return errors.New("filesystem cannot retain ACL") }
		asked := 0
		o.MetadataFailure = func(s, d string, err error) error {
			asked++
			assertIgnoredCopy(t, src, false)
			if accept {
				return nil
			}
			return ErrCancelled
		}
		err := moveAcrossVolumes(src, dst, o)
		if asked == 0 {
			t.Fatal("silent metadata loss")
		}
		if accept {
			if err != nil {
				t.Fatal(err)
			}
			assertIgnoredCopy(t, dst, false)
			if _, err := os.Stat(src); !os.IsNotExist(err) {
				t.Fatal("source remains", err)
			}
		} else {
			if !errors.Is(err, ErrCancelled) {
				t.Fatal(err)
			}
			assertIgnoredCopy(t, src, false)
			if _, err := os.Stat(dst); !os.IsNotExist(err) {
				t.Fatal("partial output left", err)
			}
		}
	}
}
