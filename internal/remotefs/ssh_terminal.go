package remotefs

import (
	"context"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHStream owns a dedicated SSH connection; closing a shell never interrupts
// file transfers. Authentication and host verification are shared with SFTP.
type SSHStream struct {
	session *ssh.Session
	stdin   io.WriteCloser
	cancel  context.CancelFunc
	ctx     context.Context
	out     chan []byte
	done    chan struct{}
	err     error
	sizes   chan [2]int
}

func (c Config) StartTerminal(parent context.Context, command string, cols, rows int) (*SSHStream, error) {
	if c.Protocol != "sftp" {
		return nil, fmt.Errorf("%s does not support an interactive shell; connect using SFTP/SSH", c.Protocol)
	}
	ctx, cancel := context.WithCancel(parent)
	timeout := time.AfterFunc(30*time.Second, cancel)
	defer timeout.Stop()
	success := false
	defer func() {
		if !success {
			cancel()
		}
	}()
	client, sockets, err := c.dialSSH(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !success {
			client.Close()
			sockets.finish()
		}
	}()
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("SSH shell unavailable: %w", err)
	}
	defer func() {
		if !success {
			session.Close()
		}
	}()
	if err = session.RequestPty("xterm-256color", max(1, rows), max(1, cols), ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 38400, ssh.TTY_OP_OSPEED: 38400}); err != nil {
		return nil, fmt.Errorf("server does not allow an interactive SSH terminal: %w", err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return nil, err
	}
	s := &SSHStream{session: session, stdin: stdin, cancel: cancel, ctx: ctx, out: make(chan []byte, 16), done: make(chan struct{}), sizes: make(chan [2]int, 1)}
	session.Stdout, session.Stderr = s, s
	if err = session.Start(command); err != nil {
		return nil, fmt.Errorf("server does not allow SSH command execution (it may be SFTP-only): %w", err)
	}
	success = true
	go func() {
		defer close(s.done)
		defer close(s.out)
		defer cancel()
		defer sockets.finish()
		defer client.Close()
		defer session.Close()
		s.err = session.Wait()
	}()
	// socketSet has bounded I/O deadlines. Keep an idle shell alive and let a
	// missing response terminate a disconnected session instead of hanging it.
	go func() {
		tick := time.NewTicker(10 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case size := <-s.sizes:
				if err := session.WindowChange(size[1], size[0]); err != nil {
					cancel()
					return
				}
			case <-tick.C:
				if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	return s, nil
}

// Write implements the bounded output sink used by ssh.Session. Terminal input
// has a separate method, so it cannot be confused with remote output.
func (s *SSHStream) Write(data []byte) (int, error) {
	original := len(data)
	for len(data) > 0 {
		n := min(len(data), 32<<10)
		chunk := append([]byte(nil), data[:n]...)
		select {
		case s.out <- chunk:
			data = data[n:]
		case <-s.ctx.Done():
			return original - len(data), s.ctx.Err()
		}
	}
	return original, nil
}
func (s *SSHStream) Input(data []byte) error { _, err := s.stdin.Write(data); return err }
func (s *SSHStream) Resize(cols, rows int) error {
	select {
	case <-s.done:
		return fmt.Errorf("SSH shell closed")
	default:
	}
	size := [2]int{max(1, cols), max(1, rows)}
	select {
	case s.sizes <- size:
		return nil
	default:
	}
	select {
	case <-s.sizes:
	default:
	}
	select {
	case s.sizes <- size:
	default:
	}
	return nil
}
func (s *SSHStream) Output() <-chan []byte { return s.out }
func (s *SSHStream) Done() <-chan struct{} { return s.done }
func (s *SSHStream) Err() error            { <-s.done; return s.err }
func (s *SSHStream) Close()                { s.cancel() }
