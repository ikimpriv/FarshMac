package kube

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Use OpenSSH so aliases, ProxyJump, IdentityFile, agent and known_hosts have
// exactly the same meaning as in the user's terminal. No passwords are saved.
func (p Profile) ValidateSSH() error { _, e := p.sshArgs(); return e }

func (p Profile) sshArgs() ([]string, error) {
	if p.SSHHost == "" || strings.HasPrefix(p.SSHHost, "-") || strings.ContainsAny(p.SSHHost, " \t\r\n/@%\\\"'[]*?!,=\x00") {
		return nil, fmt.Errorf("enter an SSH hostname, IP address or ~/.ssh/config alias")
	}
	if strings.Contains(p.SSHHost, ":") && net.ParseIP(p.SSHHost) == nil {
		return nil, fmt.Errorf("enter the SSH port separately")
	}
	for _, r := range p.SSHHost {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-:", r)) {
			return nil, fmt.Errorf("invalid SSH hostname")
		}
	}
	if strings.HasPrefix(p.SSHUser, "-") || strings.ContainsAny(p.SSHUser, " \t\r\n/@\x00") {
		return nil, fmt.Errorf("invalid SSH user")
	}
	batch := "yes"
	if p.SSHSecret != "" {
		batch = "no"
	}
	args := []string{"-T", "-o", "BatchMode=" + batch, "-o", "NumberOfPasswordPrompts=1", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=15", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "-o", "ForwardAgent=no", "-o", "ClearAllForwardings=yes"}
	if p.SSHAuth == "key" {
		args = append(args, "-o", "PreferredAuthentications=publickey")
	}
	if p.SSHAuth == "password" {
		args = append(args, "-o", "PreferredAuthentications=keyboard-interactive,password", "-o", "PubkeyAuthentication=no")
	}
	if p.SSHPort != "" {
		n, e := strconv.Atoi(p.SSHPort)
		if e != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("SSH port must be 1–65535")
		}
		args = append(args, "-p", p.SSHPort)
	}
	if p.SSHUser != "" {
		args = append(args, "-l", p.SSHUser)
	}
	if p.SSHKeyFile != "" {
		if strings.ContainsAny(p.SSHKeyFile, "\x00\r\n") {
			return nil, fmt.Errorf("invalid SSH identity path")
		}
		args = append(args, "-i", expandLocalHome(p.SSHKeyFile))
	}
	if p.SSHTrustFile != "" {
		args = append(args, "-o", "UserKnownHostsFile="+strconv.Quote(p.SSHTrustFile)+" "+strconv.Quote(expandLocalHome("~/.ssh/known_hosts")))
	}
	if p.SSHConfigFile != "" {
		if strings.ContainsAny(p.SSHConfigFile, "\x00\r\n") {
			return nil, fmt.Errorf("invalid SSH config path")
		}
		args = append(args, "-F", expandLocalHome(p.SSHConfigFile))
	}
	return args, nil
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

type boundedSSHBuffer struct {
	mu    sync.Mutex
	b     bytes.Buffer
	limit int
}

func (b *boundedSSHBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.b.Len()+len(p) > b.limit {
		return 0, fmt.Errorf("SSH output exceeds %d bytes", b.limit)
	}
	return b.b.Write(p)
}
func (b *boundedSSHBuffer) bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.b.Bytes())
}
func sshCommand(ctx context.Context, p Profile, script string, limit int) ([]byte, error) {
	p = withSSHSecret(p)
	args, err := p.sshArgs()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/ssh", append(args, p.SSHHost, script)...)
	cleanup, err := prepareSSHSecret(cmd, p)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	cmd.WaitDelay = time.Second
	out := &boundedSSHBuffer{limit: limit}
	diagnostic := &boundedSSHBuffer{limit: 16 << 10}
	cmd.Stdout = out
	cmd.Stderr = diagnostic
	if err = cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("SSH %s: %w: %s", p.SSHHost, err, strings.TrimSpace(string(diagnostic.bytes())))
	}
	return out.bytes(), nil
}

// An SSH TCP stream is owned by its net.Conn, not by the request that opened
// it. HTTP pooling and streams may outlive that request. net.Pipe supplies real
// deadlines for SPDY/WebSocket, and Close reaps the SSH child. No local listening
// port, unencrypted proxy or on-disk kubeconfig is needed.
type sshAPIConn struct {
	net.Conn
	peer       net.Conn
	cmd        *exec.Cmd
	stop       sync.Once
	diagnostic *boundedSSHBuffer
}

func (c *sshAPIConn) Close() error {
	c.stop.Do(func() {
		c.Conn.Close()
		c.peer.Close()
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
	})
	return nil
}
func (c *sshAPIConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if err != nil {
		if text := strings.TrimSpace(string(c.diagnostic.bytes())); text != "" {
			err = fmt.Errorf("SSH API transport: %s: %w", text, err)
		}
	}
	return n, err
}
func (p Profile) dialSSHAPI(ctx context.Context, network, address string) (net.Conn, error) {
	p = withSSHSecret(p)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("SSH API requires TCP")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return nil, fmt.Errorf("invalid API address %q", address)
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return nil, fmt.Errorf("invalid API port")
	}
	args, err := p.sshArgs()
	if err != nil {
		return nil, err
	}
	args = append(args, "-W", net.JoinHostPort(host, port), p.SSHHost)
	cmd := exec.Command("/usr/bin/ssh", args...)
	diagnostic := &boundedSSHBuffer{limit: 16 << 10}
	cmd.Stderr = diagnostic
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	cleanup, err := prepareSSHSecret(cmd, p)
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		cleanup()
		stdin.Close()
		stdout.Close()
		return nil, err
	}
	local, peer := net.Pipe()
	c := &sshAPIConn{Conn: local, peer: peer, cmd: cmd, diagnostic: diagnostic}
	go func() { _, _ = io.Copy(stdin, peer); stdin.Close() }()
	go func() { _, _ = io.Copy(peer, stdout); _ = cmd.Wait(); cleanup(); c.Close() }()
	if err = ctx.Err(); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func validateSSHServer(server string) error {
	u, err := url.Parse(server)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return fmt.Errorf("invalid Kubernetes API server URL")
	}
	return nil
}
