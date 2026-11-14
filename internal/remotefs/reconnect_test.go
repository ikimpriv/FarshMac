package remotefs_test

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
	"farshmac/internal/testnet"
)

// dropProxy forwards TCP to the test server and can sever every live session,
// as a network outage would.
type dropProxy struct {
	mu    sync.Mutex
	conns []net.Conn
}

func proxied(t *testing.T, c remotefs.Config) (remotefs.Config, *dropProxy) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &dropProxy{}
	target := c.Address()
	t.Cleanup(func() { l.Close(); p.drop() })
	go func() {
		for {
			in, err := l.Accept()
			if err != nil {
				return
			}
			out, err := net.Dial("tcp", target)
			if err != nil {
				in.Close()
				continue
			}
			p.mu.Lock()
			p.conns = append(p.conns, in, out)
			p.mu.Unlock()
			go func() { _, _ = io.Copy(out, in); out.Close() }()
			go func() { _, _ = io.Copy(in, out); in.Close() }()
		}
	}()
	c.Port = l.Addr().(*net.TCPAddr).Port
	return c, p
}
func (p *dropProxy) drop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		c.Close()
	}
	p.conns = nil
}

func writeRemote(t *testing.T, f remotefs.FS, name, text string) {
	t.Helper()
	w, err := f.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteCopyRetryReconnects(t *testing.T) {
	c, proxy := proxied(t, testnet.SFTP(t))
	direct, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	writeRemote(t, direct, "/two", "old")
	direct.Close()
	root := t.TempDir()
	for _, name := range []string{"one", "two"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	o := fsops.NewOperation("Copy")
	defer o.Cancel()
	dropped, failures := false, 0
	o.Collision = func(fsops.Collision) fsops.Resolution {
		if !dropped {
			dropped = true
			proxy.drop() // The connection dies after the first file.
		}
		return fsops.Resolution{Action: fsops.Replace}
	}
	o.Failure = func(f fsops.Failure) fsops.ErrorAction {
		failures++
		if failures > 3 {
			return fsops.ErrorCancel
		}
		return fsops.ErrorRetry
	}
	pairs := []remotefs.Pair{{Source: filepath.Join(root, "one"), Destination: "/one"}, {Source: filepath.Join(root, "two"), Destination: "/two"}}
	if err := remotefs.Copy(remotefs.Endpoint{}, remotefs.RemoteEndpoint(&c), pairs, remotefs.CopyOptions{Workers: 1}, o); err != nil {
		t.Fatal(err, failures)
	}
	if !dropped || failures == 0 {
		t.Fatal("connection was not interrupted", dropped, failures)
	}
	check, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	for _, name := range []string{"one", "two"} {
		r, err := check.Open("/" + name)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		r.Close()
		if err != nil || string(b) != name {
			t.Fatalf("%s: %q %v", name, b, err)
		}
	}
}

func TestRemoteDeleteRetryReconnects(t *testing.T) {
	c, proxy := proxied(t, testnet.SFTP(t))
	f, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	writeRemote(t, f, "/victim", "data")
	o := fsops.NewOperation("Delete")
	defer o.Cancel()
	session := remotefs.NewSession(remotefs.RemoteEndpoint(&c), o.Context(), o, f)
	defer session.Close()
	dropped, failures := false, 0
	o.BeforeDelete = func(string, os.FileInfo) int {
		if !dropped {
			dropped = true
			proxy.drop()
		}
		return fsops.Replace
	}
	o.Failure = func(fsops.Failure) fsops.ErrorAction {
		failures++
		if failures > 3 {
			return fsops.ErrorCancel
		}
		return fsops.ErrorRetry
	}
	if err := remotefs.RemoveTree(session, "/victim", o); err != nil {
		t.Fatal(err)
	}
	if failures == 0 {
		t.Fatal("connection was not interrupted")
	}
	check, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	if _, err := check.Lstat("/victim"); !os.IsNotExist(err) {
		t.Fatal("not deleted", err)
	}
}
