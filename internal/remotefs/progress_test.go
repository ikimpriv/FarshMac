package remotefs_test

import (
	"context"
	"testing"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
	"farshmac/internal/testnet"
)

func TestRemoteDeleteProgressCountsItems(t *testing.T) {
	c := testnet.SFTP(t)
	f, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = f.Mkdir("/tree"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c"} {
		writeRemote(t, f, "/tree/"+name, name)
	}
	o := fsops.NewOperation("Delete")
	defer o.Cancel()
	o.Streaming()
	if err = remotefs.RemoveTree(f, "/tree", o); err != nil {
		t.Fatal(err)
	}
	p := o.Snapshot()
	if p.TotalFiles != 4 || p.Files != 4 || p.TotalBytes != 3 || p.Path == "" || !p.Streaming {
		t.Fatalf("progress %+v", p)
	}
}

func TestRemoteMoveProgressShowsPath(t *testing.T) {
	c := testnet.SFTP(t)
	f, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	writeRemote(t, f, "/from", "data")
	o := fsops.NewOperation("Move")
	defer o.Cancel()
	o.Streaming()
	if err = remotefs.Move(f, remotefs.RemoteEndpoint(&c), "/from", "/to", o); err != nil {
		t.Fatal(err)
	}
	p := o.Snapshot()
	if p.Path != "/from" || p.TotalFiles != 1 || p.Files != 1 || p.FileSize != 4 {
		t.Fatalf("progress %+v", p)
	}
}
