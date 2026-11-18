package kube

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Credentials live only in memory. Persist the authentication method, never the
// password. Each SSH invocation gets a private, short-lived askpass socket.
func prepareSSHSecret(cmd *exec.Cmd, p Profile) (func(), error) {
	if p.SSHSecret == "" {
		return func() {}, nil
	}
	dir, err := os.MkdirTemp("", "farshmac-askpass-*")
	if err != nil {
		return nil, err
	}
	socket := filepath.Join(dir, "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	script := filepath.Join(dir, "askpass")
	// nc is included with macOS. The secret is neither an argument nor an
	// environment variable. Only the owner can access this directory/socket.
	text := "#!/bin/sh\nexec /usr/bin/nc -U " + shellQuote(socket) + "\n"
	if err = os.WriteFile(script, []byte(text), 0700); err != nil {
		listener.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	var once sync.Once
	cleanup := func() { once.Do(func() { listener.Close(); os.RemoveAll(dir) }) }
	go func() {
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			_, _ = io.WriteString(conn, p.SSHSecret+"\n")
			conn.Close()
		}
	}()
	cmd.Env = append(os.Environ(), "SSH_ASKPASS="+script, "SSH_ASKPASS_REQUIRE=force", "DISPLAY=farshmac")
	return cleanup, nil
}
func ProbeSSH(ctx context.Context, p Profile) error {
	_, err := sshCommand(ctx, p, "true", 1024)
	return err
}

// ParseSSHAddress accepts a host/alias, host:port, or ssh://user@host:port.
// No API endpoint is inferred from this address.
func ParseSSHAddress(address, user string) (Profile, error) {
	p := Profile{SSHUser: strings.TrimSpace(user)}
	address = strings.TrimSpace(address)
	if strings.HasPrefix(address, "ssh://") {
		address = strings.TrimPrefix(address, "ssh://")
	}
	if at := strings.LastIndex(address, "@"); at >= 0 {
		if p.SSHUser == "" {
			p.SSHUser = address[:at]
		}
		address = address[at+1:]
	}
	if host, port, e := net.SplitHostPort(address); e == nil {
		p.SSHHost = host
		p.SSHPort = port
	} else {
		p.SSHHost = strings.Trim(address, "[]")
	}
	if err := p.ValidateSSH(); err != nil {
		return p, fmt.Errorf("SSH server: %w", err)
	}
	return p, nil
}

var sshSecrets sync.Map

func sshAuthIdentity(p Profile) string {
	return strings.Join([]string{p.SSHHost, p.SSHUser, p.SSHPort, p.SSHAuth, p.SSHKeyFile, p.SSHConfigFile}, "\x00")
}
func RememberSSHAuthentication(p Profile) {
	if p.SSHSecret != "" {
		sshSecrets.Store(sshAuthIdentity(p), p.SSHSecret)
	}
}
func withSSHSecret(p Profile) Profile {
	if p.SSHSecret == "" {
		if s, ok := sshSecrets.Load(sshAuthIdentity(p)); ok {
			p.SSHSecret = s.(string)
		}
	}
	return p
}
