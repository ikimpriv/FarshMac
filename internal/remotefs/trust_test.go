package remotefs_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"farshmac/internal/remotefs"
	"farshmac/internal/testnet"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func otherHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}
func TestHostKeyMismatchAndRevocationAreStructured(t *testing.T) {
	c := testnet.SFTP(t)
	received, _ := ssh.ParsePublicKey(c.HostKey)
	c.HostKey = nil
	other := otherHostKey(t)
	known := filepath.Join(t.TempDir(), "known_hosts")
	os.WriteFile(known, []byte("# comment\n"+knownhosts.Line([]string{knownhosts.Normalize(c.Address())}, other)+"\n"), 0600)
	verify, err := remotefs.HostKeyCallback(c, known)
	if err != nil {
		t.Fatal(err)
	}
	var changed *remotefs.HostKeyChanged
	err = verify(c.Address(), &net.TCPAddr{}, received)
	if !errors.As(err, &changed) || changed.Revoked || len(changed.Want) != 1 || changed.Want[0].File != known || changed.Want[0].Line != 2 || changed.Want[0].Fingerprint != ssh.FingerprintSHA256(other) || changed.Fingerprint != ssh.FingerprintSHA256(received) {
		t.Fatalf("mismatch not described: %#v", err)
	}
	var keyErr *knownhosts.KeyError
	if !errors.As(err, &keyErr) {
		t.Fatal("original knownhosts error lost")
	}
	os.WriteFile(known, []byte("@revoked * "+strings.TrimSpace(string(ssh.MarshalAuthorizedKey(received)))+"\n"), 0600)
	verify, _ = remotefs.HostKeyCallback(c, known)
	if err = verify(c.Address(), &net.TCPAddr{}, received); !errors.As(err, &changed) || !changed.Revoked {
		t.Fatalf("revoked key not described: %v", err)
	}
	c.HostKey = other.Marshal()
	verify, _ = remotefs.HostKeyCallback(c)
	if err = verify(c.Address(), &net.TCPAddr{}, received); !errors.As(err, &changed) || changed.Want[0].Fingerprint != ssh.FingerprintSHA256(other) {
		t.Fatalf("pinned key mismatch not described: %v", err)
	}
}
func TestAppendKnownHostIsTrustedNextTime(t *testing.T) {
	c := testnet.SFTP(t)
	key := c.HostKey
	c.HostKey = nil
	c.Port = 2222
	known := filepath.Join(t.TempDir(), "ssh", "known_hosts")
	if err := remotefs.AppendKnownHost(known, c.Address(), key); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(known)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("known_hosts mode", err)
	}
	data, _ := os.ReadFile(known)
	if !strings.HasPrefix(string(data), "[127.0.0.1]:2222 ") {
		t.Fatalf("non-standard port not normalized: %q", data)
	}
	verify, _ := remotefs.HostKeyCallback(c, known)
	pub, _ := ssh.ParsePublicKey(key)
	if err = verify(c.Address(), &net.TCPAddr{}, pub); err != nil {
		t.Fatal(err)
	}
}
func TestSFTPKeyboardInteractivePassword(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_AUTH_SOCK", "")
	c := testnet.SFTPWithAuth(t, func(s *ssh.ServerConfig) {
		s.PasswordCallback = nil
		s.KeyboardInteractiveCallback = func(meta ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			answers, err := challenge("", "", []string{"Password: "}, []bool{false})
			if err != nil {
				return nil, err
			}
			if len(answers) == 1 && answers[0] == "secret" {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		}
	})
	f, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
}
func TestKeyboardInteractiveRefusesEchoPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_AUTH_SOCK", "")
	c := testnet.SFTPWithAuth(t, func(s *ssh.ServerConfig) {
		s.PasswordCallback = nil
		s.KeyboardInteractiveCallback = func(meta ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			_, err := challenge("", "", []string{"Verification code: "}, []bool{true})
			return nil, fmt.Errorf("denied: %v", err)
		}
	})
	_, err := c.Dial(context.Background())
	if !errors.Is(err, remotefs.ErrInteractiveCode) {
		t.Fatal("OTP prompt not reported", err)
	}
}
func TestEncryptedKeyFileErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("right"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_test")
	os.WriteFile(path, pem.EncodeToMemory(block), 0600)
	c := remotefs.Config{Protocol: "sftp", Host: "127.0.0.1", Port: 1, User: "u", KeyFile: path}
	_, err = c.Dial(context.Background())
	var keyErr *remotefs.KeyFileError
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &keyErr) || !errors.As(err, &missing) {
		t.Fatal("missing passphrase not typed", err)
	}
	c.KeyPassphrase = "wrong"
	if _, err = c.Dial(context.Background()); !errors.Is(err, x509.IncorrectPasswordError) {
		t.Fatal("wrong passphrase not typed", err)
	}
}
