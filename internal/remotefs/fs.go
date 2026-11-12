// Package remotefs provides operation-scoped FTP/SFTP sessions. A session is
// owned by one worker; cancellation closes its control and data connections.
package remotefs

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Protocol, Host, User, Directory, KeyFile string
	Port                                     int
	Password                                 string `json:"-"`
	KeyPassphrase                            string `json:"-"`
	HostKey                                  []byte `json:"-"` // Explicitly trusted for this connection only.
	CertFingerprint                          string `json:"-"` // FTPS certificate trusted for this connection only.
	PlainFTPAccepted                         bool   `json:",omitempty"`
}

func Parse(address, user string) (Config, error) {
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return Config{}, fmt.Errorf("invalid server address")
	}
	if u.Scheme == "ssh" {
		u.Scheme = "sftp"
	}
	if u.Scheme != "sftp" && u.Scheme != "ftp" && u.Scheme != "ftps" {
		return Config{}, fmt.Errorf("use sftp://host, ftp://host or ftps://host")
	}
	if u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" {
		return Config{}, fmt.Errorf("invalid server address")
	}
	if u.User != nil {
		if _, set := u.User.Password(); set {
			return Config{}, fmt.Errorf("enter passwords in the password field, not the URL")
		}
		user = u.User.Username()
	}
	port := 21
	if u.Scheme == "sftp" {
		port = 22
	}
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return Config{}, fmt.Errorf("invalid port")
		}
	}
	dir := u.Path
	if dir == "" {
		dir = "/"
	}
	c := Config{Protocol: u.Scheme, Host: u.Hostname(), Port: port, User: user, Directory: path.Clean(dir)}
	if strings.ContainsAny(c.Host+c.User+c.Directory, "\x00\r\n") {
		return Config{}, fmt.Errorf("invalid control character in address")
	}
	if c.Host, err = hostToASCII(c.Host); err != nil {
		return Config{}, err
	}
	return c, nil
}

// ServerURL keeps the username in the separate form field and escapes paths.
func (c Config) ServerURL() string {
	u := url.URL{Scheme: c.Protocol, Host: c.Address(), Path: c.Directory}
	return u.String()
}
func (c Config) Address() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }
func (c Config) Label() string {
	u := url.URL{Scheme: c.Protocol, Host: c.Address()}
	if c.User != "" {
		u.User = url.User(c.User)
	}
	label := u.String()
	if display := hostToUnicode(c.Host); display != c.Host {
		label = strings.Replace(label, c.Address(), net.JoinHostPort(display, strconv.Itoa(c.Port)), 1)
	}
	return label
}

type FS interface {
	ReadDir(string) ([]os.FileInfo, error)
	Lstat(string) (os.FileInfo, error)
	// Stat follows a final symbolic link.
	Stat(string) (os.FileInfo, error)
	Open(string) (io.ReadCloser, error)
	Create(string) (io.WriteCloser, error)
	Mkdir(string) error
	Remove(string, bool) error
	Rename(string, string) error
	Chmod(string, os.FileMode) error
	Chtimes(string, time.Time) error
	Readlink(string) (string, error)
	Symlink(string, string) error
	Close() error
}

// Stater is implemented by filesystems that can follow symbolic links, so a
// link to a folder can be told apart from a link to a file.
type Stater interface {
	Stat(string) (os.FileInfo, error)
}

// FollowDir lets a symbolic link to a directory act as that directory, as in
// Far Manager. Links to files and broken links keep their Lstat information.
func FollowDir(f FS, p string, info os.FileInfo) os.FileInfo {
	if info == nil || info.Mode()&os.ModeSymlink == 0 {
		return info
	}
	if target, err := f.Stat(p); err == nil && target.IsDir() {
		return target
	}
	return info
}

func (c Config) Dial(ctx context.Context) (FS, error) {
	if c.Protocol == "sftp" {
		return c.dialSFTP(ctx)
	}
	if c.Protocol == "ftp" || c.Protocol == "ftps" {
		return c.dialFTP(ctx)
	}
	return nil, fmt.Errorf("unsupported protocol")
}

func SafePath(p string) error {
	if strings.ContainsAny(p, "\x00\r\n") {
		return fmt.Errorf("path contains a protocol control character")
	}
	return nil
}

// Validate names received from servers before joining them to any local path.
func ValidName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00\r\n")
}

type socketSet struct {
	peer   string
	mu     sync.Mutex
	closed bool
	conns  map[*deadlineConn]bool
	stop   func() bool
}
type deadlineConn struct {
	net.Conn
	owner *socketSet
}

func (c *deadlineConn) Read(b []byte) (int, error) {
	_ = c.Conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	return c.Conn.Read(b)
}
func (c *deadlineConn) Write(b []byte) (int, error) {
	_ = c.Conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return c.Conn.Write(b)
}
func (c *deadlineConn) Close() error {
	c.owner.mu.Lock()
	delete(c.owner.conns, c)
	c.owner.mu.Unlock()
	return c.Conn.Close()
}
func newSockets(ctx context.Context) *socketSet {
	s := &socketSet{conns: map[*deadlineConn]bool{}}
	s.stop = context.AfterFunc(ctx, func() { s.close() })
	return s
}
func (s *socketSet) dial(ctx context.Context, network, address string) (net.Conn, error) {
	c, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		c.Close()
		return nil, context.Canceled
	}
	d := &deadlineConn{Conn: c, owner: s}
	if network == "tcp" && s.peer == "" {
		s.peer = c.RemoteAddr().String()
	}
	s.conns[d] = true
	return d, nil
}

func (s *socketSet) identity() string { s.mu.Lock(); defer s.mu.Unlock(); return s.peer }
func (s *socketSet) close() {
	s.mu.Lock()
	s.closed = true
	for c := range s.conns {
		_ = c.Conn.Close()
	}
	clear(s.conns)
	s.mu.Unlock()
}
func (s *socketSet) finish() { s.stop(); s.close() }
