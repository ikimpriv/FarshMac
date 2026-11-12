package remotefs_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
	"farshmac/internal/testnet"
	"github.com/pkg/sftp"
)

func TestSFTPReplaceWithoutPosixRenameExplains(t *testing.T) {
	// The extension list is process-global; tests in this package are sequential.
	if err := sftp.SetSFTPExtensions("hardlink@openssh.com", "statvfs@openssh.com"); err != nil {
		t.Fatal(err)
	}
	defer sftp.SetSFTPExtensions("hardlink@openssh.com", "posix-rename@openssh.com", "statvfs@openssh.com")
	c := testnet.SFTP(t)
	f, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	writeRemote(t, f, "/file", "old")
	local := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(local, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	o := fsops.NewOperation("Copy")
	defer o.Cancel()
	o.Collision = func(fsops.Collision) fsops.Resolution { return fsops.Resolution{Action: fsops.Replace} }
	err = remotefs.Copy(remotefs.Endpoint{}, remotefs.Endpoint{Remote: &c}, []remotefs.Pair{{Source: local, Destination: "/file"}}, remotefs.CopyOptions{Workers: 1}, o)
	if err == nil || !strings.Contains(err.Error(), "cannot replace an existing file atomically (no posix-rename); delete /file first or choose Rename") {
		t.Fatal(err)
	}
	r, err := f.Open("/file")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if b, err := io.ReadAll(r); err != nil || string(b) != "old" {
		t.Fatalf("destination changed: %q %v", b, err)
	}
	entries, err := f.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".farshmac-part-") {
			t.Fatal("temporary output leaked", entry.Name())
		}
	}
}
