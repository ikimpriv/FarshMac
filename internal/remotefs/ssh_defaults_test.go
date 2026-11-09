package remotefs

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKnownHostAlgorithmPreference(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	c := Config{Protocol: "sftp", Host: "example.test", Port: 22}
	file := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.HashHostname(knownhosts.Normalize(c.Address())) + " " + string(ssh.MarshalAuthorizedKey(signer.PublicKey()))
	if err = os.WriteFile(file, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	verify, err := HostKeyCallback(c, file)
	if err != nil {
		t.Fatal(err)
	}
	algorithms := hostKeyAlgorithms(c, verify)
	if len(algorithms) == 0 || algorithms[0] != ssh.KeyAlgoED25519 {
		t.Fatal("known algorithm not preferred", algorithms)
	}
	c.HostKey = signer.PublicKey().Marshal()
	if hostKeyAlgorithms(c, verify)[0] != ssh.KeyAlgoED25519 {
		t.Fatal("pinned algorithm ignored")
	}
}
func TestDefaultPrivateKeys(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := ssh.MarshalPrivateKeyWithPassphrase(key, "", []byte("passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".ssh", "id_ed25519")
	os.WriteFile(path, pem.EncodeToMemory(encrypted), 0600)
	keys, hint, err := defaultPrivateKeys(Config{}, home)
	if err != nil || len(keys) != 0 || hint == "" {
		t.Fatal("encrypted key hint", err)
	}
	keys, _, err = defaultPrivateKeys(Config{KeyPassphrase: "passphrase"}, home)
	if err != nil || len(keys) != 1 {
		t.Fatal("default encrypted identity", err)
	}
	plain, err := ssh.MarshalPrivateKey(key, "")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, pem.EncodeToMemory(plain), 0600)
	keys, _, err = defaultPrivateKeys(Config{}, home)
	if err != nil || len(keys) != 1 {
		t.Fatal("default identity not loaded", err)
	}
	if _, _, err = defaultPrivateKeys(Config{KeyFile: filepath.Join(home, "missing")}, home); err == nil {
		t.Fatal("explicit missing key ignored")
	}
}

// Explicit opt-in read-only smoke test against the user's requested server.
func TestConfiguredSFTPReadOnly(t *testing.T) {
	address := os.Getenv("FARSHMAC_SFTP_CHECK")
	if address == "" {
		t.Skip("live server check not requested")
	}
	c, err := Parse(address, os.Getenv("USER"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	f, err := c.Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	entries, err := f.ReadDir(c.Directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("SFTP authenticated and directory read successfully (%d entries)", len(entries))
}
