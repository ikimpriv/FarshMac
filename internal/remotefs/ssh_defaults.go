package remotefs

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// Prefer algorithms already trusted for this host, as OpenSSH does. Verification
// still checks the actual received key; this never accepts a changed key.
func hostKeyAlgorithms(c Config, verify ssh.HostKeyCallback) []string {
	var keys []ssh.PublicKey
	if len(c.HostKey) > 0 {
		if key, err := ssh.ParsePublicKey(c.HostKey); err == nil {
			keys = append(keys, key)
		}
	} else {
		probe, _ := ssh.NewPublicKey(ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)))
		err := verify(c.Address(), &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: c.Port}, probe)
		// knownhosts exposes the matching records through KeyError, including hashed hosts.
		keys = keysFromHostError(err)
	}
	known := map[string]bool{}
	for _, k := range keys {
		known[k.Type()] = true
		if k.Type() == ssh.KeyAlgoRSA {
			known[ssh.KeyAlgoRSASHA256] = true
			known[ssh.KeyAlgoRSASHA512] = true
		}
	}
	defaults := ssh.SupportedAlgorithms().HostKeys
	var result []string
	for _, alg := range defaults {
		if known[alg] {
			result = append(result, alg)
		}
	}
	for _, alg := range defaults {
		if !known[alg] {
			result = append(result, alg)
		}
	}
	return result
}

func readPrivateKey(path, passphrase string) (ssh.Signer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	defer clear(b)
	if err != nil {
		return nil, err
	}
	if len(b) > 1024*1024 {
		return nil, fmt.Errorf("SSH key file is too large")
	}
	if passphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase(b, []byte(passphrase))
	}
	return ssh.ParsePrivateKey(b)
}
func defaultPrivateKeys(c Config, home string) ([]ssh.Signer, string, error) {
	if c.KeyFile != "" {
		key, err := readPrivateKey(c.KeyFile, c.KeyPassphrase)
		if err != nil {
			return nil, "", &KeyFileError{c.KeyFile, err}
		}
		return []ssh.Signer{key}, "", nil
	}
	var keys []ssh.Signer
	var unavailable []string
	for _, name := range []string{"id_rsa", "id_ecdsa", "id_ed25519"} {
		path := filepath.Join(home, ".ssh", name)
		key, err := readPrivateKey(path, c.KeyPassphrase)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			var encrypted *ssh.PassphraseMissingError
			if errors.As(err, &encrypted) {
				unavailable = append(unavailable, name+" requires a passphrase or ssh-agent")
			} else {
				unavailable = append(unavailable, name+": "+err.Error())
			}
			continue
		}
		keys = append(keys, key)
	}
	return keys, strings.Join(unavailable, "; "), nil
}

func keysFromHostError(err error) []ssh.PublicKey {
	var mismatch *knownhosts.KeyError
	if !errors.As(err, &mismatch) {
		return nil
	}
	var keys []ssh.PublicKey
	for _, record := range mismatch.Want {
		keys = append(keys, record.Key)
	}
	return keys
}
