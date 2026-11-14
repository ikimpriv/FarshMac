package remotefs_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
	"farshmac/internal/testnet"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for i := 0; i < 12; i++ {
		p := filepath.Join(root, "folder", fmt.Sprintf("file-%02d.txt", i))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, bytes.Repeat([]byte{byte(i)}, 65536), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "folder", ".DS_Store"), []byte("metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "folder", ".hidden"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}
func copyTree(t *testing.T, source, target remotefs.Endpoint, src, dst string) {
	t.Helper()
	o := fsops.NewOperation("Copy")
	defer o.Cancel()
	o.IgnoreDSStore = true
	o.CopyMetadata = &fsops.MetadataOptions{}
	err := remotefs.Copy(source, target, []remotefs.Pair{{Source: src, Destination: dst}}, remotefs.CopyOptions{Workers: 4, Conflict: func(string, string) int { return 0 }}, o)
	if err != nil {
		t.Fatal(err)
	}
	p := o.Snapshot()
	if p.Bytes != p.TotalBytes || p.Files != p.TotalFiles || p.Active != 0 {
		t.Fatalf("progress %+v", p)
	}
}
func TestFTPAndSFTPCopyRoundTrip(t *testing.T) {
	for _, protocol := range []string{"ftp", "sftp"} {
		t.Run(protocol, func(t *testing.T) {
			var c remotefs.Config
			var ftp *testnet.FTPServer
			if protocol == "ftp" {
				ftp = testnet.FTP(t)
				c = ftp.Config
			} else {
				c = testnet.SFTP(t)
			}
			source := fixture(t)
			remote := remotefs.RemoteEndpoint(&c)
			copyTree(t, remotefs.Endpoint{}, remote, filepath.Join(source, "folder"), "/uploaded")
			if ftp != nil && ftp.Peak.Load() < 3 {
				t.Fatalf("transfers not parallel: peak %d", ftp.Peak.Load())
			}
			dst := filepath.Join(t.TempDir(), "download")
			copyTree(t, remote, remotefs.Endpoint{}, "/uploaded", dst)
			for i := 0; i < 12; i++ {
				name := fmt.Sprintf("file-%02d.txt", i)
				want, _ := os.ReadFile(filepath.Join(source, "folder", name))
				got, err := os.ReadFile(filepath.Join(dst, name))
				if err != nil || !bytes.Equal(want, got) {
					t.Fatal(name, err)
				}
			}
			if _, err := os.Stat(filepath.Join(dst, ".DS_Store")); !os.IsNotExist(err) {
				t.Fatal("DS_Store copied")
			}
			if data, err := os.ReadFile(filepath.Join(dst, ".hidden")); err != nil || string(data) != "keep" {
				t.Fatal("hidden file excluded", err)
			}
			f, err := c.Dial(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err = f.Rename("/uploaded", "/renamed"); err != nil {
				t.Fatal(err)
			}
			if err = remotefs.RemoveTree(f, "/renamed", fsops.NewOperation("Delete")); err != nil {
				t.Fatal(err)
			}
			if _, err = f.Lstat("/renamed"); !os.IsNotExist(err) {
				t.Fatal("remote removal", err)
			}
		})
	}
}
func TestServerToServerCopy(t *testing.T) {
	a := testnet.SFTP(t)
	b := testnet.FTP(t)
	source := fixture(t)
	copyTree(t, remotefs.Endpoint{}, remotefs.RemoteEndpoint(&a), filepath.Join(source, "folder"), "/source")
	copyTree(t, remotefs.RemoteEndpoint(&a), remotefs.RemoteEndpoint(&b.Config), "/source", "/target")
	got, err := os.ReadFile(filepath.Join(b.Root, "target", "file-04.txt"))
	if err != nil || len(got) != 65536 || got[0] != 4 {
		t.Fatal("server-to-server", err)
	}
}
func TestHostKeysAndSecrets(t *testing.T) {
	c := testnet.SFTP(t)
	key, err := ssh.ParsePublicKey(c.HostKey)
	if err != nil {
		t.Fatal(err)
	}
	c.HostKey = nil
	verify, err := remotefs.HostKeyCallback(c)
	if err != nil {
		t.Fatal(err)
	}
	var unknown *remotefs.UnknownHost
	if err = verify(c.Address(), &net.TCPAddr{}, key); !errors.As(err, &unknown) || unknown.Fingerprint != ssh.FingerprintSHA256(key) {
		t.Fatal("unknown key not surfaced", err)
	}
	known := filepath.Join(t.TempDir(), "known_hosts")
	if err = os.WriteFile(known, []byte(knownhosts.Line([]string{knownhosts.Normalize(c.Address())}, key)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	verify, err = remotefs.HostKeyCallback(c, known)
	if err != nil {
		t.Fatal(err)
	}
	if err = verify(c.Address(), &net.TCPAddr{}, key); err != nil {
		t.Fatal(err)
	}
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	other, _ := ssh.NewSignerFromKey(priv)
	if err = verify(c.Address(), &net.TCPAddr{}, other.PublicKey()); err == nil || errors.As(err, &unknown) {
		t.Fatal("changed key accepted or treated as new")
	}
	c.KeyPassphrase = "private-secret"
	data, _ := json.Marshal(c)
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "HostKey") {
		t.Fatal("credentials persisted", string(data))
	}
	if _, err = remotefs.Parse("ftp://user:secret@localhost/", ""); err == nil {
		t.Fatal("password in URL accepted")
	}
}
func TestCancelBlockedFTPHandshake(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	connected := make(chan struct{})
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		close(connected)
		_, _ = io.Copy(io.Discard, c)
	}()
	c := remotefs.Config{Protocol: "ftp", Host: "127.0.0.1", Port: l.Addr().(*net.TCPAddr).Port, User: "test", Password: "secret"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		f, err := c.Dial(ctx)
		if f != nil {
			f.Close()
		}
		done <- err
	}()
	<-connected
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancel succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel blocked on network")
	}
}
func TestLocalParallelCopyAndDestinationSafety(t *testing.T) {
	src := filepath.Join(fixture(t), "folder")
	dst := filepath.Join(t.TempDir(), "copy")
	copyTree(t, remotefs.Endpoint{}, remotefs.Endpoint{}, src, dst)
	o := fsops.NewOperation("Copy")
	defer o.Cancel()
	err := remotefs.Copy(remotefs.Endpoint{}, remotefs.Endpoint{}, []remotefs.Pair{{Source: src, Destination: filepath.Join(src, "inside")}}, remotefs.CopyOptions{Workers: 4}, o)
	if err == nil {
		t.Fatal("recursive self-copy accepted")
	}
}

func TestSameServerCopyAndRecursiveRejection(t *testing.T) {
	for _, protocol := range []string{"ftp", "sftp"} {
		t.Run(protocol, func(t *testing.T) {
			var c remotefs.Config
			if protocol == "ftp" {
				c = testnet.FTP(t).Config
			} else {
				c = testnet.SFTP(t)
			}
			endpoint := remotefs.RemoteEndpoint(&c)
			copyTree(t, remotefs.Endpoint{}, endpoint, filepath.Join(fixture(t), "folder"), "/source")
			copyTree(t, endpoint, endpoint, "/source", "/copy")
			o := fsops.NewOperation("Copy")
			defer o.Cancel()
			err := remotefs.Copy(endpoint, endpoint, []remotefs.Pair{{Source: "/source", Destination: "/source/inside"}}, remotefs.CopyOptions{Workers: 2}, o)
			if err == nil {
				t.Fatal("recursive remote copy allowed")
			}
		})
	}
}

func TestCancelTransferKeepsOriginalDestination(t *testing.T) {
	server := testnet.FTP(t)
	if err := os.WriteFile(filepath.Join(server.Root, "source"), bytes.Repeat([]byte("data"), 1024*1024), 0600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "original")
	if err := os.WriteFile(dst, []byte("keep original"), 0600); err != nil {
		t.Fatal(err)
	}
	o := fsops.NewOperation("Copy")
	defer o.Cancel()
	done := make(chan error, 1)
	go func() {
		done <- remotefs.Copy(remotefs.RemoteEndpoint(&server.Config), remotefs.Endpoint{}, []remotefs.Pair{{Source: "/source", Destination: dst}}, remotefs.CopyOptions{Workers: 1, Conflict: func(string, string) int { return 0 }}, o)
	}()
	deadline := time.After(3 * time.Second)
	for server.Active.Load() == 0 {
		select {
		case err := <-done:
			t.Fatalf("transfer finished before cancellation: %v", err)
		case <-deadline:
			t.Fatal("transfer did not start")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	o.Cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled copy succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("transfer did not cancel")
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "keep original" {
		t.Fatal("original overwritten", err)
	}
	entries, err := os.ReadDir(filepath.Dir(dst))
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary local file left behind", err)
	}
}

func TestRemoteSaveRefusesChangedDestination(t *testing.T) {
	c := testnet.SFTP(t)
	f, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	write := func(value string) {
		t.Helper()
		w, err := f.Create("/file")
		if err != nil {
			t.Fatal(err)
		}
		_, err = w.Write([]byte(value))
		if err != nil {
			t.Fatal(err)
		}
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	write("old")
	old, err := f.Lstat("/file")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Remove("/file", false); err != nil {
		t.Fatal(err)
	}
	write("new external version")
	source := filepath.Join(t.TempDir(), "edited")
	if err = os.WriteFile(source, []byte("my edits"), 0600); err != nil {
		t.Fatal(err)
	}
	o := fsops.NewOperation("Save")
	defer o.Cancel()
	err = remotefs.Copy(remotefs.Endpoint{}, remotefs.RemoteEndpoint(&c), []remotefs.Pair{{Source: source, Destination: "/file"}}, remotefs.CopyOptions{Workers: 1, ExpectedDestination: old, Conflict: func(string, string) int { return 0 }}, o)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatal("changed remote file overwritten", err)
	}
	r, err := f.Open("/file")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil || string(data) != "new external version" {
		t.Fatal("external edit was lost", err)
	}
}

func TestConnectionURLRoundTrip(t *testing.T) {
	original := remotefs.Config{Protocol: "sftp", Host: "localhost", Port: 22, User: "demo", Directory: "/folder#100% space"}
	got, err := remotefs.Parse(original.ServerURL(), original.User)
	if err != nil || got.Directory != original.Directory || got.User != original.User {
		t.Fatal("connection URL lost path", got, err)
	}
	got, err = remotefs.Parse("sftp://remote-user@localhost/", "local-default")
	if err != nil || got.User != "remote-user" {
		t.Fatal("URL username ignored", got, err)
	}
}

func TestRemoteCopyCollisionDecisions(t *testing.T) {
	for _, protocol := range []string{"ftp", "sftp"} {
		t.Run(protocol, func(t *testing.T) {
			var c remotefs.Config
			if protocol == "ftp" {
				c = testnet.FTP(t).Config
			} else {
				c = testnet.SFTP(t)
			}
			local := filepath.Join(t.TempDir(), "file")
			if e := os.WriteFile(local, []byte("new"), 0600); e != nil {
				t.Fatal(e)
			}
			f, e := c.Dial(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			defer f.Close()
			for _, choice := range []int{0, 1, 2} {
				t.Run(fmt.Sprint(choice), func(t *testing.T) {
					dst := fmt.Sprintf("/file-%d", choice)
					w, e := f.Create(dst)
					if e != nil {
						t.Fatal(e)
					}
					if _, e = w.Write([]byte("old")); e != nil {
						t.Fatal(e)
					}
					if e = w.Close(); e != nil {
						t.Fatal(e)
					}
					o := fsops.NewOperation("Copy")
					defer o.Cancel()
					err := remotefs.Copy(remotefs.Endpoint{}, remotefs.RemoteEndpoint(&c), []remotefs.Pair{{Source: local, Destination: dst}}, remotefs.CopyOptions{Workers: 3, Conflict: func(string, string) int { return choice }}, o)
					if choice == 2 {
						if !errors.Is(err, fsops.ErrCancelled) {
							t.Fatal(err)
						}
					} else if err != nil {
						t.Fatal(err)
					}
					r, e := f.Open(dst)
					if e != nil {
						t.Fatal(e)
					}
					b, e := io.ReadAll(r)
					r.Close()
					want := "old"
					if choice == 0 {
						want = "new"
					}
					if e != nil || string(b) != want {
						t.Fatalf("got %q %v", b, e)
					}
					if b, e := os.ReadFile(local); e != nil || string(b) != "new" {
						t.Fatal("source changed", e)
					}
					entries, e := f.ReadDir("/")
					if e != nil {
						t.Fatal(e)
					}
					for _, entry := range entries {
						if strings.HasPrefix(entry.Name(), ".farshmac-part-") {
							t.Fatal("temporary output leaked", entry.Name())
						}
					}
				})
			}
		})
	}
}

func TestRemoteAppendAndMoveMerge(t *testing.T) {
	for _, protocol := range []string{"ftp", "sftp"} {
		t.Run(protocol, func(t *testing.T) {
			var c remotefs.Config
			if protocol == "ftp" {
				c = testnet.FTP(t).Config
			} else {
				c = testnet.SFTP(t)
			}
			remote := remotefs.RemoteEndpoint(&c)
			f, e := c.Dial(context.Background())
			if e != nil {
				t.Fatal(e)
			}
			defer f.Close()
			write := func(p, data string) {
				t.Helper()
				w, e := f.Create(p)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = w.Write([]byte(data)); e != nil {
					t.Fatal(e)
				}
				if e = w.Close(); e != nil {
					t.Fatal(e)
				}
			}
			read := func(p, want string) {
				t.Helper()
				r, e := f.Open(p)
				if e != nil {
					t.Fatal(e)
				}
				b, e := io.ReadAll(r)
				r.Close()
				if e != nil || string(b) != want {
					t.Fatalf("%s: %q %v", p, b, e)
				}
			}
			local := filepath.Join(t.TempDir(), "file")
			if e = os.WriteFile(local, []byte("new"), 0600); e != nil {
				t.Fatal(e)
			}
			write("/append", "old")
			o := fsops.NewOperation("Copy")
			defer o.Cancel()
			o.MetadataFailure = func(string, string, error) error { return nil }
			o.Collision = func(fsops.Collision) fsops.Resolution { return fsops.Resolution{Action: fsops.Append} }
			if e = remotefs.Copy(remotefs.Endpoint{}, remote, []remotefs.Pair{{Source: local, Destination: "/append"}}, remotefs.CopyOptions{Workers: 2}, o); e != nil {
				t.Fatal(e)
			}
			read("/append", "oldnew")
			for _, p := range []string{"/source", "/dest"} {
				if e = f.Mkdir(p); e != nil {
					t.Fatal(e)
				}
			}
			write("/source/collision", "new")
			write("/source/unique", "unique")
			write("/dest/collision", "old")
			write("/dest/keep", "keep")
			o = fsops.NewOperation("Move")
			defer o.Cancel()
			o.MetadataFailure = func(string, string, error) error { return nil }
			o.Collision = func(fsops.Collision) fsops.Resolution { return fsops.Resolution{Action: fsops.Skip} }
			if e = remotefs.Move(f, remote, "/source", "/dest", o); e != nil {
				t.Fatal(e)
			}
			read("/source/collision", "new")
			read("/dest/collision", "old")
			read("/dest/unique", "unique")
			read("/dest/keep", "keep")
			o.Collision = func(fsops.Collision) fsops.Resolution { return fsops.Resolution{Action: fsops.Append} }
			if e = remotefs.Move(f, remote, "/source", "/dest", o); e != nil {
				t.Fatal(e)
			}
			read("/dest/collision", "oldnew")
			if _, e = f.Lstat("/source"); !os.IsNotExist(e) {
				t.Fatal("source folder retained", e)
			}
		})
	}
}
