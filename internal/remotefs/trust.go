package remotefs

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// KnownHostKey describes one known_hosts record that disagrees with the key a
// server presented. File and Line are empty for a key pinned only in memory.
type KnownHostKey struct {
	File        string
	Line        int
	Type        string
	Fingerprint string
}

// HostKeyChanged is returned when the received key contradicts a known key or
// matches a revoked one. It is never recoverable from the UI.
type HostKeyChanged struct {
	Host, Type, Fingerprint string
	Want                    []KnownHostKey
	Revoked                 bool
	err                     error
}

func (e *HostKeyChanged) Error() string {
	if e.Revoked {
		return "SSH host key of " + e.Host + " is revoked"
	}
	return "SSH host key of " + e.Host + " does not match known_hosts"
}
func (e *HostKeyChanged) Unwrap() error { return e.err }

func hostKeyChanged(host string, key ssh.PublicKey, err error) error {
	changed := &HostKeyChanged{Host: host, Type: key.Type(), Fingerprint: ssh.FingerprintSHA256(key), err: err}
	var revoked *knownhosts.RevokedError
	if errors.As(err, &revoked) {
		changed.Revoked = true
		changed.Want = append(changed.Want, knownKey(revoked.Revoked))
		return changed
	}
	var mismatch *knownhosts.KeyError
	if errors.As(err, &mismatch) && len(mismatch.Want) > 0 {
		for _, want := range mismatch.Want {
			changed.Want = append(changed.Want, knownKey(want))
		}
		return changed
	}
	return err
}
func knownKey(k knownhosts.KnownKey) KnownHostKey {
	return KnownHostKey{File: k.Filename, Line: k.Line, Type: k.Key.Type(), Fingerprint: ssh.FingerprintSHA256(k.Key)}
}

// pinnedHostKey replaces ssh.FixedHostKey so a mismatch after "Trust once" is
// reported with the same structured warning as a known_hosts mismatch.
func pinnedHostKey(want ssh.PublicKey) ssh.HostKeyCallback {
	return func(host string, _ net.Addr, key ssh.PublicKey) error {
		if bytes.Equal(key.Marshal(), want.Marshal()) {
			return nil
		}
		return &HostKeyChanged{Host: host, Type: key.Type(), Fingerprint: ssh.FingerprintSHA256(key), Want: []KnownHostKey{{Type: want.Type(), Fingerprint: ssh.FingerprintSHA256(want)}}, err: errors.New("ssh: host key mismatch")}
	}
}

// KnownHostsPath is the per-user file that "Trust and save" appends to.
func KnownHostsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "known_hosts")
}

// AppendKnownHost records a key the user explicitly accepted, as ssh does with
// StrictHostKeyChecking=ask. It never replaces or removes existing records.
func AppendKnownHost(file, address string, key []byte) error {
	pub, err := ssh.ParsePublicKey(key)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	line := knownhosts.Line([]string{knownhosts.Normalize(address)}, pub) + "\n"
	if info, e := f.Stat(); e == nil && info.Size() > 0 {
		last := make([]byte, 1)
		if _, e = f.ReadAt(last, info.Size()-1); e == nil && last[0] != '\n' {
			line = "\n" + line
		}
	}
	_, err = f.WriteString(line)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

// ErrInteractiveCode reports a keyboard-interactive prompt that is not a
// password (for example an OTP), which FarshMac does not answer.
var ErrInteractiveCode = errors.New("server asks for an interactive code; not supported")

// passwordChallenge answers the first hidden keyboard-interactive prompt with
// the password once, so a rejected password cannot loop.
func passwordChallenge(password string) ssh.KeyboardInteractiveChallenge {
	answered := false
	return func(_, _ string, questions []string, echos []bool) ([]string, error) {
		answers := make([]string, len(questions))
		for i := range questions {
			if i < len(echos) && echos[i] {
				return nil, ErrInteractiveCode
			}
			if !answered {
				answers[i] = password
				answered = true
			}
		}
		return answers, nil
	}
}

// KeyFileError wraps a failure to read an explicitly configured private key.
type KeyFileError struct {
	Path string
	Err  error
}

func (e *KeyFileError) Error() string {
	return fmt.Sprintf("cannot read SSH private key %s: %v", e.Path, e.Err)
}
func (e *KeyFileError) Unwrap() error { return e.Err }

// UnknownCertificate is returned when an FTPS certificate does not chain to a
// system root (or does not match the host). Trusting it pins its SHA-256.
type UnknownCertificate struct {
	Host, Fingerprint, Subject, Reason string
	NotAfter                           time.Time
}

func (e *UnknownCertificate) Error() string {
	return "untrusted FTPS certificate for " + e.Host + " " + e.Fingerprint + ": " + e.Reason
}

func CertificateFingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:]), "=")
}

// verifyFTPSCertificate performs the standard chain and host-name checks, or
// accepts exactly the pinned certificate. The handshake setting
// InsecureSkipVerify only moves verification here; it is never skipped.
func verifyFTPSCertificate(host, pin string, roots *x509.CertPool, unknown **UnknownCertificate) func(tls.ConnectionState) error {
	return func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return errors.New("FTPS server sent no certificate")
		}
		leaf := cs.PeerCertificates[0]
		fingerprint := CertificateFingerprint(leaf.Raw)
		if pin != "" {
			if fingerprint == pin {
				return nil
			}
			return fmt.Errorf("FTPS certificate changed: got %s, trusted %s", fingerprint, pin)
		}
		opts := x509.VerifyOptions{DNSName: host, Roots: roots, Intermediates: x509.NewCertPool()}
		for _, c := range cs.PeerCertificates[1:] {
			opts.Intermediates.AddCert(c)
		}
		if _, err := leaf.Verify(opts); err != nil {
			*unknown = &UnknownCertificate{Host: host, Fingerprint: fingerprint, Subject: leaf.Subject.String(), Reason: err.Error(), NotAfter: leaf.NotAfter}
			return *unknown
		}
		return nil
	}
}
