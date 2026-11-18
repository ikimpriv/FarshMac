package kube

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type SSHHostKeys struct {
	Address      string
	Fingerprints []string
	Lines        []string
}

func ScanSSHHostKeys(ctx context.Context, p Profile) (SSHHostKeys, error) {
	args, err := p.sshArgs()
	if err != nil {
		return SSHHostKeys{}, err
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/ssh", append(args, "-G", p.SSHHost)...)
	out, err := cmd.Output()
	if err != nil {
		return SSHHostKeys{}, fmt.Errorf("cannot resolve SSH host settings: %w", err)
	}
	host, port, alias := p.SSHHost, "22", ""
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "hostname":
			host = f[1]
		case "port":
			port = f[1]
		case "hostkeyalias":
			if f[1] != "none" {
				alias = f[1]
			}
		case "proxyjump", "proxycommand":
			if f[1] != "none" {
				return SSHHostKeys{}, fmt.Errorf("confirm this jump/proxy host using ssh in Terminal, then retry")
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	cmd = exec.CommandContext(ctx, "/usr/bin/ssh-keyscan", "-T", "5", "-p", port, host)
	buffer := &boundedSSHBuffer{limit: 32 << 10}
	cmd.Stdout = buffer
	if err = cmd.Run(); err != nil {
		return SSHHostKeys{}, fmt.Errorf("cannot read host key for %s: %w", host, err)
	}
	address := net.JoinHostPort(host, port)
	if alias != "" {
		address = alias
	}
	result := SSHHostKeys{Address: address}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(buffer.bytes()), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, _, _, e := ssh.ParseAuthorizedKey([]byte(fields[1] + " " + fields[2]))
		if e != nil {
			continue
		}
		fingerprint := ssh.FingerprintSHA256(key)
		if seen[fingerprint] {
			continue
		}
		seen[fingerprint] = true
		result.Fingerprints = append(result.Fingerprints, key.Type()+" "+fingerprint)
		result.Lines = append(result.Lines, knownhosts.Line([]string{address}, key))
	}
	if len(result.Lines) == 0 {
		return result, fmt.Errorf("server did not return a supported host key")
	}
	return result, nil
}
func TrustSSHHostKeys(keys SSHHostKeys) (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "FarshMac")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	name := filepath.Join(dir, "ssh_known_hosts")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = f.WriteString(strings.Join(keys.Lines, "\n") + "\n")
	return name, err
}
