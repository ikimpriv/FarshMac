package docker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSSHContextValidation(t *testing.T) {
	for _, o := range []SSHContextOptions{{Name: "ok", Host: "127.0.0.1", User: "alice", Port: "2222"}, {Name: "v6", Host: "2001:db8::1"}, {Name: "alias", Host: "build-host", Alias: true}} {
		if e := o.Validate(); e != nil {
			t.Fatal(o, e)
		}
	}
	for _, o := range []SSHContextOptions{{Name: "default", Host: "host"}, {Name: "bad/name", Host: "host"}, {Name: "ok", Host: "-oProxyCommand=bad"}, {Name: "ok", Host: "ssh://host"}, {Name: "ok", Host: "host\nHost *"}, {Name: "ok", Host: "host", Port: "0"}, {Name: "ok", Host: "host", User: "a b"}, {Name: "ok", Host: "host", Alias: true, IdentityFile: "key"}} {
		if e := o.Validate(); e == nil {
			t.Fatal("accepted", o)
		}
	}
	if got := (SSHContextOptions{Host: "2001:db8::1", User: "alice", Port: "2222"}).Endpoint(); got != "ssh://alice@[2001:db8::1]:2222" {
		t.Fatal(got)
	}
}
func contextFixture(t *testing.T, fail bool) (Client, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	binary := filepath.Join(home, "docker")
	script := "#!/bin/sh\ncase \"$1 $2\" in\n 'context ls') printf 'default\\n';;\n 'context create') printf '%s\\n' \"$@\" > \"$HOME/args\";"
	if fail {
		script += "exit 1"
	} else {
		script += "exit 0"
	}
	script += ";;\n 'context inspect') exit 1;;\n *) exit 2;;\nesac\n"
	if e := os.WriteFile(binary, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	return Client{Binary: binary}, home
}
func TestCreateSSHContextKeyAndRollback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "rollback"}[fail], func(t *testing.T) {
			client, home := contextFixture(t, fail)
			sshDir := filepath.Join(home, ".ssh")
			os.Mkdir(sshDir, 0700)
			original := "Host existing\n  HostName example.invalid\nHost *\n  User fallback\n"
			config := filepath.Join(sshDir, "config")
			os.WriteFile(config, []byte(original), 0600)
			key := filepath.Join(home, "key with spaces")
			os.WriteFile(key, []byte("fixture"), 0600)
			err := client.CreateSSHContext(context.Background(), SSHContextOptions{Name: "remote", Host: "192.0.2.1", User: "alice", Port: "2222", IdentityFile: key})
			b, _ := os.ReadFile(config)
			if fail {
				if err == nil || string(b) != original {
					t.Fatal(err, string(b))
				}
				return
			}
			if err != nil || !strings.HasSuffix(string(b), original) {
				t.Fatal(err, string(b))
			}
			args, _ := os.ReadFile(filepath.Join(home, "args"))
			if !strings.Contains(string(args), "host=ssh://farshmac-docker-") {
				t.Fatal(string(args))
			}
			alias := strings.TrimSpace(strings.SplitN(strings.SplitN(string(b), "Host ", 2)[1], "\n", 2)[0])
			// Check the real OpenSSH parser, including precedence over Host * defaults.
			out, e := exec.Command("ssh", "-G", "-F", config, alias).Output()
			if e != nil {
				t.Fatal(e)
			}
			for _, want := range []string{"hostname 192.0.2.1", "user alice", "port 2222", "identityfile " + key} {
				if !strings.Contains(string(out), want) {
					t.Fatalf("missing %q", want)
				}
			}
		})
	}
}
func TestCreateSSHContextAliasDoesNotEditSSH(t *testing.T) {
	c, home := contextFixture(t, false)
	if err := c.CreateSSHContext(context.Background(), SSHContextOptions{Name: "remote", Host: "existing-alias", Alias: true}); err != nil {
		t.Fatal(err)
	}
	if _, e := os.Stat(filepath.Join(home, ".ssh")); !os.IsNotExist(e) {
		t.Fatal("alias mode changed SSH files", e)
	}
	args, _ := os.ReadFile(filepath.Join(home, "args"))
	if !strings.Contains(string(args), "host=ssh://existing-alias") {
		t.Fatal(string(args))
	}
}
