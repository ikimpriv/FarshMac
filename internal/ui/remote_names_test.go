package ui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
	"farshmac/internal/testnet"
)

func TestRemoteInvalidUTF8NamesAreDisplayedButNotCopied(t *testing.T) {
	a := remoteApp(t)
	c := testnet.SFTP(t)
	f, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	w, err := f.Create("/bad\xff.txt")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("x"))
	w.Close()
	f.Close()
	a.connectRemote(0, c)
	waitNetwork(t, a)
	const want = "Some names are not valid UTF-8; copying them is refused"
	for i := 0; i < 100 && a.Status != want; i++ {
		a.Poll()
		time.Sleep(5 * time.Millisecond)
	}
	if a.Status != want {
		t.Fatal(a.Status)
	}
	o := fsops.NewOperation("copy")
	err = remotefs.Copy(remotefs.Endpoint{Remote: &c}, remotefs.Endpoint{}, []remotefs.Pair{{Source: "/bad\xff.txt", Destination: filepath.Join(t.TempDir(), "bad.txt")}}, remotefs.CopyOptions{Workers: 1}, o)
	if err == nil || !strings.Contains(err.Error(), "filename is not valid UTF-8: 626164ff2e747874") {
		t.Fatal("invalid name copied", err)
	}
}
