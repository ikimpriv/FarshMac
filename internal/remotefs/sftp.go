package remotefs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

type UnknownHost struct {
	Host, Fingerprint string
	Key               []byte
	Type              string
}

func (e *UnknownHost) Error() string { return "Unknown SSH host " + e.Host + " " + e.Fingerprint }

func HostKeyCallback(c Config, files ...string) (ssh.HostKeyCallback, error) {
	if len(c.HostKey) > 0 {
		key, err := ssh.ParsePublicKey(c.HostKey)
		if err != nil {
			return nil, err
		}
		return pinnedHostKey(key), nil
	}
	var existing []string
	for _, f := range files {
		if _, err := os.Stat(f); err == nil {
			existing = append(existing, f)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	verify, err := knownhosts.New(existing...)
	if err != nil {
		return nil, err
	}
	return func(host string, addr net.Addr, key ssh.PublicKey) error {
		err := verify(host, addr, key)
		var unknown *knownhosts.KeyError
		if errors.As(err, &unknown) && len(unknown.Want) == 0 {
			return &UnknownHost{host, ssh.FingerprintSHA256(key), key.Marshal(), key.Type()}
		}
		if err != nil {
			// Known-key mismatch and revoked keys are never auto-accepted.
			return hostKeyChanged(host, key, err)
		}
		return nil
	}, nil
}

type sftpFS struct {
	client  *sftp.Client
	ssh     *ssh.Client
	sockets *socketSet
	ctx     context.Context
}

func (f *sftpFS) Identity() string                   { return "ssh:" + f.sockets.identity() }
func (f *sftpFS) Canonical(p string) (string, error) { return f.client.RealPath(p) }

func (c Config) dialSFTP(ctx context.Context) (FS, error) {
	sockets := newSockets(ctx)
	ok := false
	defer func() {
		if !ok {
			sockets.finish()
		}
	}()
	home, _ := os.UserHomeDir()
	verify, err := HostKeyCallback(c, filepath.Join(home, ".ssh", "known_hosts"), filepath.Join(home, ".ssh", "known_hosts2"), "/etc/ssh/ssh_known_hosts", "/etc/ssh/ssh_known_hosts2")
	if err != nil {
		return nil, err
	}
	keys, keyHint, err := defaultPrivateKeys(c, home)
	if err != nil {
		return nil, err
	}
	if socket := os.Getenv("SSH_AUTH_SOCK"); socket != "" {
		if conn, e := sockets.dial(ctx, "unix", socket); e == nil {
			if signers, e := agent.NewClient(conn).Signers(); e == nil {
				keys = append(keys, signers...)
			}
		}
	}
	var auth []ssh.AuthMethod
	if len(keys) > 0 {
		auth = append(auth, ssh.PublicKeys(keys...))
	}
	if c.Password != "" {
		auth = append(auth, ssh.Password(c.Password), ssh.KeyboardInteractive(passwordChallenge(c.Password)))
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf("no usable SSH identity: provide a password, private key or ssh-agent. %s", keyHint)
	}
	conn, err := sockets.dial(ctx, "tcp", c.Address())
	if err != nil {
		return nil, err
	}
	cc, ch, req, err := ssh.NewClientConn(conn, c.Address(), &ssh.ClientConfig{User: c.User, Auth: auth, HostKeyCallback: verify, HostKeyAlgorithms: hostKeyAlgorithms(c, verify), Timeout: 15 * time.Second})
	if err != nil {
		if keyHint != "" {
			return nil, fmt.Errorf("%w (%s)", err, keyHint)
		}
		return nil, err
	}
	sc := ssh.NewClient(cc, ch, req)
	client, err := sftp.NewClient(sc, sftp.UseConcurrentReads(false), sftp.UseConcurrentWrites(false))
	if err != nil {
		sc.Close()
		return nil, err
	}
	ok = true
	return &sftpFS{client, sc, sockets, ctx}, nil
}
func (f *sftpFS) ReadDir(p string) ([]os.FileInfo, error) { return f.client.ReadDirContext(f.ctx, p) }
func (f *sftpFS) Lstat(p string) (os.FileInfo, error)     { return f.client.Lstat(p) }
func (f *sftpFS) Open(p string) (io.ReadCloser, error)    { return f.client.Open(p) }
func (f *sftpFS) Create(p string) (io.WriteCloser, error) {
	return f.client.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
}
func (f *sftpFS) Mkdir(p string) error { return f.client.Mkdir(p) }
func (f *sftpFS) Remove(p string, dir bool) error {
	if dir {
		return f.client.RemoveDirectory(p)
	}
	return f.client.Remove(p)
}
func (f *sftpFS) Rename(a, b string) error {
	if _, ok := f.client.HasExtension("posix-rename@openssh.com"); ok {
		return f.client.PosixRename(a, b)
	}
	return f.client.Rename(a, b)
}
func (f *sftpFS) Chmod(p string, m os.FileMode) error { return f.client.Chmod(p, m) }
func (f *sftpFS) Chtimes(p string, t time.Time) error { return f.client.Chtimes(p, t, t) }
func (f *sftpFS) Readlink(p string) (string, error)   { return f.client.ReadLink(p) }
func (f *sftpFS) Symlink(target, p string) error      { return f.client.Symlink(target, p) }
func (f *sftpFS) Close() error                        { f.sockets.finish(); _ = f.client.Close(); return f.ssh.Close() }
