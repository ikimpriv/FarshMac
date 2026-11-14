package remotefs_test

import (
	"context"
	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
	"farshmac/internal/testnet"
	"os"
	"path/filepath"
	"testing"
)

func TestMoveBetweenLocalAndSFTP(t *testing.T) {
	config := testnet.SFTP(t)
	remote := remotefs.RemoteEndpoint(&config)
	src := filepath.Join(fixture(t), "folder")
	operation := func() *fsops.Operation {
		o := fsops.NewOperation("Move")
		o.MetadataFailure = func(string, string, error) error { return nil }
		t.Cleanup(o.Cancel)
		return o
	}
	if err := remotefs.MoveBetween(remotefs.Endpoint{}, remote, src, "/uploaded", operation()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source retained", err)
	}
	out := filepath.Join(t.TempDir(), "downloaded")
	if err := remotefs.MoveBetween(remote, remotefs.Endpoint{}, "/uploaded", out, operation()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, ".DS_Store")); err != nil {
		t.Fatal("move lost hidden metadata file", err)
	}
	f, err := remote.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.Lstat("/uploaded"); !os.IsNotExist(err) {
		t.Fatal("remote source retained", err)
	}
}

func TestMoveBetweenSkipKeepsSource(t *testing.T) {
	config := testnet.SFTP(t)
	remote := remotefs.RemoteEndpoint(&config)
	src := filepath.Join(t.TempDir(), "file")
	os.WriteFile(src, []byte("original"), 0600)
	f, err := remote.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w, err := f.Create("/exists")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("keep"))
	w.Close()
	o := fsops.NewOperation("Move")
	defer o.Cancel()
	o.Collision = func(fsops.Collision) fsops.Resolution { return fsops.Resolution{Action: fsops.Skip} }
	if err = remotefs.MoveBetween(remotefs.Endpoint{}, remote, src, "/exists", o); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(src); err != nil {
		t.Fatal("skipped source deleted", err)
	}
}
