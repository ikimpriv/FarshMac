package docker

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SSHContextOptions struct {
	Name, Host, User, Port, IdentityFile string
	Alias                                bool
}

var contextNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
var sshUserPattern = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$`)
var sshConfigMu sync.Mutex

func (o SSHContextOptions) Validate() error {
	if !contextNamePattern.MatchString(o.Name) || o.Name == "default" {
		return fmt.Errorf("enter a context name using letters, digits, dot, dash or underscore (not default)")
	}
	if o.Host == "" || strings.HasPrefix(o.Host, "-") || strings.ContainsAny(o.Host, " \t\r\n/@%\\\"'[]*?!,=") {
		return fmt.Errorf("enter a hostname, IP address or SSH alias without a URL prefix")
	}
	for _, c := range o.Host {
		if c < 33 || c > 126 {
			return fmt.Errorf("use an ASCII hostname or SSH alias")
		}
	}
	if strings.Contains(o.Host, ":") && net.ParseIP(o.Host) == nil {
		return fmt.Errorf("enter the port in the Port field")
	}
	if o.User != "" && !sshUserPattern.MatchString(o.User) {
		return fmt.Errorf("invalid SSH user")
	}
	if o.Port != "" {
		n, e := strconv.Atoi(o.Port)
		if e != nil || n < 1 || n > 65535 {
			return fmt.Errorf("SSH port must be between 1 and 65535")
		}
	}
	if o.Alias && (o.IdentityFile != "" || o.User != "" || o.Port != "") {
		return fmt.Errorf("SSH alias mode uses the user, port and key from SSH configuration")
	}
	if strings.ContainsAny(o.IdentityFile, "\r\n\x00") {
		return fmt.Errorf("invalid key path")
	}
	return nil
}
func (o SSHContextOptions) Endpoint() string {
	host := o.Host
	if o.Port != "" {
		host = net.JoinHostPort(host, o.Port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	u := url.URL{Scheme: "ssh", Host: host}
	if o.User != "" {
		u.User = url.User(o.User)
	}
	return u.String()
}

// CreateSSHContext creates a standard CLI context. An explicit identity uses a
// private, uniquely named SSH Host stanza; private key contents are never copied.
func (c Client) CreateSSHContext(ctx context.Context, o SSHContextOptions) error {
	if err := o.Validate(); err != nil {
		return err
	}
	names, err := c.Names(ctx, "context")
	if err != nil {
		return err
	}
	for _, name := range names {
		if name == o.Name {
			return fmt.Errorf("Docker context %q already exists", name)
		}
	}
	sshConfigMu.Lock()
	defer sshConfigMu.Unlock()
	endpoint := o.Endpoint()
	rollback := func() error { return nil }
	if o.IdentityFile != "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return e
		}
		key := o.IdentityFile
		if strings.HasPrefix(key, "~/") {
			key = filepath.Join(home, key[2:])
		}
		key, e = filepath.Abs(key)
		if e != nil {
			return e
		}
		info, e := os.Stat(key)
		if e != nil {
			return fmt.Errorf("SSH key: %w", e)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("SSH key must be a regular file")
		}
		f, e := os.Open(key)
		if e != nil {
			return fmt.Errorf("SSH key: %w", e)
		}
		f.Close()
		sshDir := filepath.Join(home, ".ssh")
		if e = os.MkdirAll(sshDir, 0700); e != nil {
			return e
		}
		config := filepath.Join(sshDir, "config")
		before, mode, exists, e := readSSHConfig(config)
		if e != nil {
			return e
		}
		alias := "farshmac-docker-" + strings.ToLower(rand.Text())
		quote := func(s string) string {
			return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%").Replace(s) + `"`
		}
		block := "# FarshMac Docker context: " + o.Name + "\nHost " + alias + "\n  HostName " + o.Host + "\n"
		if o.User != "" {
			block += "  User " + o.User + "\n"
		}
		if o.Port != "" {
			block += "  Port " + o.Port + "\n"
		}
		block += "  IdentityFile " + quote(key) + "\n  IdentitiesOnly yes\nHost *\n# End FarshMac Docker context\n\n"
		after := append([]byte(block), before...)
		if e = replaceSSHConfig(config, before, exists, after, mode); e != nil {
			return e
		}
		rollback = func() error {
			current, _, _, e := readSSHConfig(config)
			if e != nil {
				return e
			}
			if !bytes.Equal(current, after) {
				return fmt.Errorf("SSH config changed concurrently; unused alias %s was retained", alias)
			}
			if !exists {
				return os.Remove(config)
			}
			return replaceSSHConfig(config, after, true, before, mode)
		}
		endpoint = (SSHContextOptions{Host: alias}).Endpoint()
	}
	_, err = c.Run(ctx, "context", "create", o.Name, "--description", "SSH connection created by FarshMac", "--docker", "host="+endpoint)
	if err != nil {
		// Cancellation may arrive after Docker committed its context. Keep the SSH
		// stanza if the context now exists, rather than break a successfully saved one.
		checkCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if result, e := c.Run(checkCtx, "context", "inspect", "--format", `{{(index .Endpoints "docker").Host}}`, o.Name); e == nil && strings.TrimSpace(string(result)) == endpoint {
			return fmt.Errorf("context %q was saved, but creation was interrupted; select it from Docker contexts", o.Name)
		}
		if e := rollback(); e != nil {
			return fmt.Errorf("%w; %v", err, e)
		}
	}
	return err
}
func readSSHConfig(name string) ([]byte, os.FileMode, bool, error) {
	st, e := os.Lstat(name)
	if os.IsNotExist(e) {
		return nil, 0600, false, nil
	}
	if e != nil {
		return nil, 0, false, e
	}
	if !st.Mode().IsRegular() {
		return nil, 0, false, fmt.Errorf("SSH config is not a regular file; configure an SSH alias manually and select alias mode")
	}
	if st.Size() > 4<<20 {
		return nil, 0, false, fmt.Errorf("SSH config exceeds 4 MiB")
	}
	b, e := os.ReadFile(name)
	return b, st.Mode().Perm(), true, e
}
func replaceSSHConfig(name string, before []byte, existed bool, after []byte, mode os.FileMode) error {
	current, _, exists, e := readSSHConfig(name)
	if e != nil {
		return e
	}
	if exists != existed || !bytes.Equal(current, before) {
		return fmt.Errorf("SSH config changed; retry creating the context")
	}
	f, e := os.CreateTemp(filepath.Dir(name), ".farshmac-ssh-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = f.Chmod(mode); e != nil {
		return e
	}
	if _, e = f.Write(after); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	current, _, exists, e = readSSHConfig(name)
	if e != nil {
		return e
	}
	if exists != existed || !bytes.Equal(current, before) {
		return fmt.Errorf("SSH config changed; retry creating the context")
	}
	if !existed {
		return os.Link(f.Name(), name)
	}
	return os.Rename(f.Name(), name)
}
