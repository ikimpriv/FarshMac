package fsops

import (
	"archive/tar"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func encryptedZIP(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	put(t, filepath.Join(base, "file"), "secret")
	file := filepath.Join(base, "encrypted.zip")
	cmd := exec.Command("/usr/bin/zip", "-q", "-P", "fixture-password", file, "file")
	cmd.Dir = base
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %s: %v", out, err)
	}
	return file
}

func TestEncryptedZIPIndexRefusedWithMessage(t *testing.T) {
	file := encryptedZIP(t)
	o := NewOperation("Opening archive")
	defer o.Cancel()
	_, err := ReadArchiveIndex(file, o)
	if !errors.Is(err, ErrEncryptedArchive) || err.Error() != "encrypted archives are not supported: "+file {
		t.Fatal(err)
	}
}

func TestEncryptedZIPMemberExtractionMessage(t *testing.T) {
	file := encryptedZIP(t)
	o := NewOperation("Copy")
	defer o.Cancel()
	visited := false
	err := withArchiveSelection(file, o, func(string) bool { return true }, func(*tar.Reader) error { visited = true; return nil })
	if !errors.Is(err, ErrEncryptedArchive) || visited {
		t.Fatal(err, visited)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
}
