package remotefs_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"farshmac/internal/testnet"
	"golang.org/x/crypto/ssh"
)

func TestSSHStreamCancelBackpressureAndTrust(t *testing.T) {
	c := testnet.SSHShell(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := c.StartTerminal(ctx, "/bin/sh -c 'while :; do printf 012345678901234567890123456789; done'", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	// Stop reading output to exercise the bounded output queue and cancellation.
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < 1000; i++ {
		if err := s.Resize(80+i%20, 24); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("cancel blocked behind output/resize")
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	c.HostKey = wrong.PublicKey().Marshal()
	bad, err := c.StartTerminal(context.Background(), "true", 80, 24)
	if bad != nil {
		bad.Close()
	}
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "key") {
		t.Fatal("host-key mismatch accepted", err)
	}
}

func TestSSHStreamCancellationBeforeConnect(t *testing.T) {
	c := testnet.SSHShell(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, err := c.StartTerminal(ctx, "true", 80, 24)
	if s != nil {
		s.Close()
	}
	if err == nil {
		t.Fatal("cancelled connection succeeded")
	}
}
