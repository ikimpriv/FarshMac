// Package testnet runs isolated protocol servers for integration tests only.
package testnet

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"farshmac/internal/remotefs"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func listener(t testing.TB) (net.Listener, func(net.Conn)) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	t.Cleanup(func() {
		l.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	})
	return l, func(c net.Conn) { mu.Lock(); conns = append(conns, c); mu.Unlock() }
}

func SFTP(t testing.TB, configure ...func(*sftp.Handlers)) remotefs.Config {
	t.Helper()
	return SFTPWithAuth(t, nil, configure...)
}

// SFTPWithAuth lets a test replace the server's authentication callbacks.
func SFTPWithAuth(t testing.TB, auth func(*ssh.ServerConfig), configure ...func(*sftp.Handlers)) remotefs.Config {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{PasswordCallback: func(meta ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if meta.User() == "test" && string(p) == "secret" {
			return nil, nil
		}
		return nil, fmt.Errorf("invalid login")
	}}
	config.AddHostKey(signer)
	if auth != nil {
		auth(config)
	}
	handlers := sftp.InMemHandler()
	for _, configure := range configure {
		configure(&handlers)
	}
	l, track := listener(t)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			track(c)
			go func() {
				defer c.Close()
				sc, ch, requests, err := ssh.NewServerConn(c, config)
				if err != nil {
					return
				}
				defer sc.Close()
				go ssh.DiscardRequests(requests)
				for channel := range ch {
					if channel.ChannelType() != "session" {
						channel.Reject(ssh.UnknownChannelType, "session required")
						continue
					}
					stream, req, err := channel.Accept()
					if err != nil {
						continue
					}
					go func() {
						defer stream.Close()
						for r := range req {
							ok := r.Type == "subsystem" && len(r.Payload) >= 4 && string(r.Payload[4:]) == "sftp"
							r.Reply(ok, nil)
							if ok {
								server := sftp.NewRequestServer(stream, handlers)
								_ = server.Serve()
								server.Close()
								return
							}
						}
					}()
				}
			}()
		}
	}()
	port := l.Addr().(*net.TCPAddr).Port
	return remotefs.Config{Protocol: "sftp", Host: "127.0.0.1", Port: port, User: "test", Password: "secret", Directory: "/", HostKey: signer.PublicKey().Marshal()}
}

type FTPServer struct {
	Config       remotefs.Config
	Root         string
	Active, Peak atomic.Int32
	Delay        time.Duration
}

func FTP(t testing.TB) *FTPServer {
	t.Helper()
	root := t.TempDir()
	l, track := listener(t)
	s := &FTPServer{Config: remotefs.Config{Protocol: "ftp", Host: "127.0.0.1", Port: l.Addr().(*net.TCPAddr).Port, User: "test", Password: "secret", Directory: "/"}, Root: root, Delay: 20 * time.Millisecond}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			track(c)
			go s.serve(c, track)
		}
	}()
	return s
}
func (s *FTPServer) serve(c net.Conn, track func(net.Conn)) {
	defer c.Close()
	reply := func(format string, args ...any) { fmt.Fprintf(c, format+"\r\n", args...) }
	reply("220 Test FTP")
	var passive net.Listener
	defer func() {
		if passive != nil {
			passive.Close()
		}
	}()
	var rename string
	cwd := "/"
	local := func(p string) string { return filepath.Join(s.Root, strings.TrimPrefix(filepath.Clean("/"+p), "/")) }
	facts := func(info os.FileInfo) string {
		kind := "file"
		if info.IsDir() {
			kind = "dir"
		}
		return fmt.Sprintf("type=%s;size=%d;modify=%s; %s", kind, info.Size(), info.ModTime().UTC().Format("20060102150405"), info.Name())
	}
	reader := bufio.NewScanner(c)
	for reader.Scan() {
		cmd, arg, _ := strings.Cut(reader.Text(), " ")
		switch strings.ToUpper(cmd) {
		case "USER":
			reply("331 Password required")
		case "PASS":
			if arg != "secret" {
				reply("530 Login failed")
			} else {
				reply("230 Logged in")
			}
		case "FEAT":
			reply("211-Features\r\n MLST type*;size*;modify*;\r\n UTF8\r\n MFMT\r\n211 End")
		case "TYPE", "OPTS", "NOOP":
			reply("200 OK")
		case "PWD":
			reply("257 %q", cwd)
		case "CWD":
			if info, err := os.Stat(local(arg)); err != nil || !info.IsDir() {
				reply("550 Not a directory")
			} else {
				cwd = filepath.Clean("/" + arg)
				reply("250 Changed")
			}
		case "EPSV":
			if passive != nil {
				passive.Close()
			}
			var err error
			passive, err = net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return
			}
			reply("229 Entering Extended Passive Mode (|||%d|)", passive.Addr().(*net.TCPAddr).Port)
		case "MLST":
			info, err := os.Lstat(local(arg))
			if err != nil {
				reply("550 File not found")
			} else {
				reply("250-Details\r\n %s\r\n250 End", facts(info))
			}
		case "MLSD", "LIST", "RETR", "STOR":
			if passive == nil {
				reply("425 No data connection")
				continue
			}
			if cmd != "STOR" {
				if _, err := os.Stat(local(arg)); err != nil {
					reply("550 File not found")
					continue
				}
			}
			reply("150 Opening data connection")
			d, err := passive.Accept()
			passive.Close()
			passive = nil
			if err != nil {
				return
			}
			track(d)
			active := s.Active.Add(1)
			for {
				old := s.Peak.Load()
				if active <= old || s.Peak.CompareAndSwap(old, active) {
					break
				}
			}
			time.Sleep(s.Delay)
			switch cmd {
			case "MLSD", "LIST":
				entries, e := os.ReadDir(local(arg))
				err = e
				if err == nil {
					for _, entry := range entries {
						info, e := entry.Info()
						if e == nil {
							fmt.Fprintf(d, "%s\r\n", facts(info))
						}
					}
				}
			case "RETR":
				var f *os.File
				f, err = os.Open(local(arg))
				if err == nil {
					_, err = io.Copy(d, f)
					f.Close()
				}
			case "STOR":
				var f *os.File
				f, err = os.Create(local(arg))
				if err == nil {
					_, err = io.Copy(f, d)
					f.Close()
				}
			}
			s.Active.Add(-1)
			d.Close()
			if err != nil {
				reply("451 Transfer failed")
			} else {
				reply("226 Complete")
			}
		case "MKD":
			if err := os.Mkdir(local(arg), 0755); err != nil {
				reply("550 Cannot create")
			} else {
				reply("257 Created")
			}
		case "DELE", "RMD":
			if err := os.Remove(local(arg)); err != nil {
				reply("550 Cannot remove")
			} else {
				reply("250 Removed")
			}
		case "RNFR":
			rename = local(arg)
			reply("350 Ready")
		case "RNTO":
			if err := os.Rename(rename, local(arg)); err != nil {
				reply("550 Cannot rename")
			} else {
				reply("250 Renamed")
			}
		case "MFMT":
			stamp, p, _ := strings.Cut(arg, " ")
			when, err := time.Parse("20060102150405", stamp)
			if err == nil {
				err = os.Chtimes(local(p), when, when)
			}
			if err != nil {
				reply("550 Cannot set time")
			} else {
				reply("213 Modified")
			}
		case "SIZE":
			info, err := os.Stat(local(arg))
			if err != nil {
				reply("550 File not found")
			} else {
				reply("213 %s", strconv.FormatInt(info.Size(), 10))
			}
		case "QUIT":
			reply("221 Bye")
			return
		default:
			reply("502 Not implemented")
		}
	}
}
