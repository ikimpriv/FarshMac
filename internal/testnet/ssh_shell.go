package testnet

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"os/exec"
	"sync"
	"testing"

	"farshmac/internal/remotefs"
	"github.com/creack/pty"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SSHShell is an isolated localhost SSH server with a real PTY. Commands are
// supplied only by tests; the SFTP subsystem remains an in-memory filesystem.
func SSHShell(t testing.TB) remotefs.Config {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PasswordCallback: func(meta ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if meta.User() == "test" && string(p) == "secret" {
			return nil, nil
		}
		return nil, fmt.Errorf("invalid login")
	}}
	cfg.AddHostKey(signer)
	l, track := listener(t)
	handlers := sftp.InMemHandler()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			track(c)
			go func() {
				defer c.Close()
				sc, ch, req, err := ssh.NewServerConn(c, cfg)
				if err != nil {
					return
				}
				defer sc.Close()
				go ssh.DiscardRequests(req)
				for incoming := range ch {
					if incoming.ChannelType() != "session" {
						incoming.Reject(ssh.UnknownChannelType, "session required")
						continue
					}
					stream, requests, err := incoming.Accept()
					if err != nil {
						continue
					}
					go func() {
						defer stream.Close()
						cols, rows := uint32(80), uint32(24)
						for r := range requests {
							switch r.Type {
							case "pty-req":
								var v struct {
									Term                      string
									Cols, Rows, Width, Height uint32
									Modes                     string
								}
								err := ssh.Unmarshal(r.Payload, &v)
								r.Reply(err == nil, nil)
								if err == nil {
									cols, rows = v.Cols, v.Rows
								}
							case "subsystem":
								var v struct{ Name string }
								ssh.Unmarshal(r.Payload, &v)
								r.Reply(v.Name == "sftp", nil)
								if v.Name == "sftp" {
									server := sftp.NewRequestServer(stream, handlers)
									server.Serve()
									server.Close()
									return
								}
							case "exec":
								var v struct{ Command string }
								if ssh.Unmarshal(r.Payload, &v) != nil {
									r.Reply(false, nil)
									continue
								}
								ctx, cancel := context.WithCancel(context.Background())
								cmd := exec.CommandContext(ctx, "/bin/sh", "-c", v.Command)
								cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/", "TERM=xterm-256color"}
								pt, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
								if err != nil {
									cancel()
									r.Reply(false, nil)
									return
								}
								r.Reply(true, nil)
								var ptyMu sync.Mutex
								closed := false
								go func() { io.Copy(pt, stream); cancel() }()
								go func() {
									for req := range requests {
										if req.Type == "window-change" {
											var v struct{ Cols, Rows, Width, Height uint32 }
											if ssh.Unmarshal(req.Payload, &v) == nil {
												ptyMu.Lock()
												if !closed {
													pty.Setsize(pt, &pty.Winsize{Cols: uint16(v.Cols), Rows: uint16(v.Rows)})
												}
												ptyMu.Unlock()
											}
										}
										if req.WantReply {
											req.Reply(false, nil)
										}
									}
								}()
								io.Copy(stream, pt)
								err = cmd.Wait()
								code := uint32(0)
								if err != nil {
									code = 1
								}
								stream.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{code}))
								ptyMu.Lock()
								closed = true
								pt.Close()
								ptyMu.Unlock()
								cancel()
								return
							default:
								r.Reply(false, nil)
							}
						}
					}()
				}
			}()
		}
	}()
	return remotefs.Config{Protocol: "sftp", Host: "127.0.0.1", Port: l.Addr().(*net.TCPAddr).Port, User: "test", Password: "secret", Directory: "/", HostKey: signer.PublicKey().Marshal()}
}
