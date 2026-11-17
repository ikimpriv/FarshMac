package cloudfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == HelperFlag {
		os.Exit(HelperMain(os.Args[2]))
	}
	os.Exit(m.Run())
}
func TestNativeCoordinationAndErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("local content"), 0600); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	result, err := runHelper(context.Background(), exe, path)
	if err != nil {
		t.Fatal(err)
	}
	if result != path {
		t.Fatal(result)
	}
	if _, err = runHelper(context.Background(), exe, path+"missing"); err == nil {
		t.Fatal("missing file accepted")
	}
	if _, err = runHelper(context.Background(), exe, filepath.Dir(path)); err == nil {
		t.Fatal("directory accepted")
	}
}
func TestLocalFastPathAndLegacyNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ordinary := filepath.Join(home, ".note.txt.icloud")
	if Candidate(ordinary) {
		t.Fatal("ordinary name treated as a cloud placeholder")
	}
	if got, err := Prepare(context.Background(), ordinary); got != ordinary || err != nil {
		t.Fatal(got, err)
	}
	cloud := filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs", ".note.txt.icloud")
	got, needed := candidate(cloud)
	if !needed || filepath.Base(got) != "note.txt" {
		t.Fatal(got, needed)
	}
}

type flaggedInfo struct {
	os.FileInfo
	flags uint32
}

func (f flaggedInfo) Sys() any { return &syscall.Stat_t{Flags: f.flags} }
func TestDatalessDetection(t *testing.T) {
	if !dataless(flaggedInfo{flags: 0x40000000}) || dataless(flaggedInfo{flags: 0x20}) {
		t.Fatal("incorrect SF_DATALESS detection")
	}
}
func TestStalledProviderCancellation(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := runHelper(ctx, binary, "/fixture")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
		t.Fatal("stalled provider did not cancel", err)
	}
}
func TestProviderErrorPreserved(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s' '{\"Error\":\"Provider offline; sign in to download\"}'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := runHelper(context.Background(), binary, "/fixture")
	if err == nil || !strings.Contains(err.Error(), "Provider offline") {
		t.Fatal(err)
	}
}
