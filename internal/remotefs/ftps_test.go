package remotefs

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// Exercise both control and passive data TLS, including certificate rejection.
func TestExplicitFTPSProtectsDataConnection(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	serverTLS := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	dataTLS := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2; i++ {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				var wire net.Conn = conn
				reader := bufio.NewReader(wire)
				reply := func(s string) { fmt.Fprint(wire, s+"\r\n") }
				reply("220 TLS FTP test")
				var passive net.Listener
				defer func() {
					if passive != nil {
						passive.Close()
					}
				}()
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					cmd, _, _ := strings.Cut(strings.TrimSpace(line), " ")
					switch cmd {
					case "AUTH":
						reply("234 TLS ready")
						secure := tls.Server(conn, serverTLS)
						if secure.Handshake() != nil {
							return
						}
						wire = secure
						reader = bufio.NewReader(wire)
					case "USER":
						reply("331 Password")
					case "PASS":
						reply("230 Logged in")
					case "FEAT":
						reply("211-Features\r\n MLST type*;size*;modify*;\r\n UTF8\r\n211 End")
					case "TYPE", "OPTS", "PBSZ", "PROT":
						reply("200 OK")
					case "EPSV":
						passive, err = net.Listen("tcp", "127.0.0.1:0")
						if err != nil {
							return
						}
						reply(fmt.Sprintf("229 Entering Extended Passive Mode (|||%d|)", passive.Addr().(*net.TCPAddr).Port))
					case "MLSD":
						reply("150 Data connection")
						d, err := passive.Accept()
						passive.Close()
						passive = nil
						if err != nil {
							return
						}
						_ = d.SetDeadline(time.Now().Add(3 * time.Second))
						secure := tls.Server(d, serverTLS)
						err = secure.Handshake()
						dataTLS <- err
						if err == nil {
							fmt.Fprint(secure, "type=file;size=4;modify=20260101000000; encrypted.txt\r\n")
						}
						secure.Close()
						reply("226 Complete")
					case "QUIT":
						reply("221 Bye")
						return
					default:
						reply("502 Unsupported")
					}
				}
			}()
		}
	}()
	c := Config{Protocol: "ftps", Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, User: "demo", Password: "secret"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if f, err := c.Dial(ctx); err == nil {
		f.Close()
		t.Fatal("untrusted certificate accepted")
	}
	f, err := c.dialFTPWithTLS(ctx, &tls.Config{ServerName: c.Host, RootCAs: roots, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	entries, err := f.ReadDir("/")
	if err != nil || len(entries) != 1 || entries[0].Name() != "encrypted.txt" {
		t.Fatalf("FTPS listing: %v, %v", entries, err)
	}
	if err := <-dataTLS; err != nil {
		t.Fatal("data channel was not TLS", err)
	}
	f.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
}
