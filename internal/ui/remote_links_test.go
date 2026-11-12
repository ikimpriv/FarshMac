package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"farshmac/internal/remotefs"
	"farshmac/internal/testnet"
)

func seedRemoteLinks(t *testing.T, c remotefs.Config) {
	t.Helper()
	seedRemote(t, c)
	f, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Symlink("/folder", "/dirlink"); err != nil {
		t.Fatal(err)
	}
	if err := f.Symlink("/folder/file.txt", "/filelink"); err != nil {
		t.Fatal(err)
	}
	if err := f.Symlink("/missing", "/broken"); err != nil {
		t.Fatal(err)
	}
}

func remoteEntry(t *testing.T, p *Panel, name string) Entry {
	t.Helper()
	for _, e := range p.Entries {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("missing remote entry %q", name)
	return Entry{}
}

func TestRemoteSymlinkedDirectoryEntersAsFolder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := testnet.SFTP(t)
	seedRemoteLinks(t, c)
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.connectRemote(0, c)
	waitNetwork(t, a)
	p := &a.Panels[0]
	if !remoteEntry(t, p, "dirlink").Dir || remoteEntry(t, p, "filelink").Dir || remoteEntry(t, p, "broken").Dir {
		t.Fatal("symlink targets not resolved", p.Entries)
	}
	selectEntry(t, p, "dirlink")
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if p.Path != "/dirlink" || !panelContains(p, "file.txt") {
		t.Fatal("did not enter linked folder", p.Path, a.Status)
	}
}

func TestRemoteLinkBeyondLimitResolvesOnEnter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	old := remoteLinkResolveLimit
	remoteLinkResolveLimit = 0
	defer func() { remoteLinkResolveLimit = old }()
	c := testnet.SFTP(t)
	seedRemoteLinks(t, c)
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.connectRemote(0, c)
	waitNetwork(t, a)
	p := &a.Panels[0]
	if remoteEntry(t, p, "dirlink").Dir {
		t.Fatal("limit 0 must leave links unresolved")
	}
	selectEntry(t, p, "dirlink")
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if p.Path != "/dirlink" {
		t.Fatal("Enter did not resolve the link to a folder", p.Path, a.Status)
	}
}

func TestFTPStatFollowsFolders(t *testing.T) {
	s := testnet.FTP(t)
	if err := os.Mkdir(filepath.Join(s.Root, "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Root, "file"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := s.Config.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	stater, ok := f.(remotefs.Stater)
	if !ok {
		t.Fatal("FTP must follow links via Stat")
	}
	if info, err := stater.Stat("/dir"); err != nil || !info.IsDir() {
		t.Fatal("folder", info, err)
	}
	if info, err := stater.Stat("/file"); err != nil || info.IsDir() {
		t.Fatal("file", info, err)
	}
}
