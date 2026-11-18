package kube

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"k8s.io/client-go/rest"
)

// A real OpenSSH client talks to a disposable Go SSH server: no personal SSH
// configuration, keys, hosts or Kubernetes clusters are used.
func sshFixture(t *testing.T, remoteHome, forward string) (Profile, *[]string, *sync.Mutex) {
	t.Helper()
	_, hostKey, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(hostKey)
	_, identity, _ := ed25519.GenerateKey(rand.Reader)
	key, _ := ssh.MarshalPrivateKey(identity, "")
	dir := t.TempDir()
	identityPath := filepath.Join(dir, "identity")
	os.WriteFile(identityPath, pem.EncodeToMemory(key), 0600)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	hosts := filepath.Join(dir, "known_hosts")
	os.WriteFile(hosts, []byte(knownhosts.Line([]string{listener.Addr().String()}, signer.PublicKey())+"\n"), 0600)
	config := filepath.Join(dir, "config")
	os.WriteFile(config, []byte("Host *\n  UserKnownHostsFile "+hosts+"\n  GlobalKnownHostsFile /dev/null\n  IdentityAgent none\n  IdentitiesOnly yes\n  LogLevel ERROR\n"), 0600)
	sc := &ssh.ServerConfig{PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) { return nil, nil }}
	sc.PasswordCallback = func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if string(password) == "fixture-password" {
			return nil, nil
		}
		return nil, fmt.Errorf("invalid password")
	}
	sc.AddHostKey(signer)
	life, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var destinations []string
	conns := map[net.Conn]bool{}
	t.Cleanup(func() {
		cancel()
		listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for c := range conns {
			c.Close()
		}
	})
	go func() {
		for {
			raw, e := listener.Accept()
			if e != nil {
				return
			}
			mu.Lock()
			conns[raw] = true
			mu.Unlock()
			go func() {
				defer raw.Close()
				defer func() { mu.Lock(); delete(conns, raw); mu.Unlock() }()
				server, chans, reqs, e := ssh.NewServerConn(raw, sc)
				if e != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(reqs)
				for incoming := range chans {
					switch incoming.ChannelType() {
					case "direct-tcpip":
						var request struct {
							Host       string
							Port       uint32
							Origin     string
							OriginPort uint32
						}
						ssh.Unmarshal(incoming.ExtraData(), &request)
						address := net.JoinHostPort(request.Host, fmt.Sprint(request.Port))
						mu.Lock()
						destinations = append(destinations, address)
						mu.Unlock()
						target := address
						if forward != "" {
							target = forward
						}
						socket, e := net.Dial("tcp", target)
						if e != nil {
							incoming.Reject(ssh.ConnectionFailed, e.Error())
							continue
						}
						channel, requests, e := incoming.Accept()
						if e != nil {
							socket.Close()
							continue
						}
						go ssh.DiscardRequests(requests)
						go func() {
							defer channel.Close()
							defer socket.Close()
							go func() { io.Copy(socket, channel); socket.Close() }()
							io.Copy(channel, socket)
						}()
					case "session":
						channel, requests, e := incoming.Accept()
						if e != nil {
							continue
						}
						go func() {
							defer channel.Close()
							for r := range requests {
								if r.Type != "exec" {
									r.Reply(false, nil)
									continue
								}
								var payload struct{ Command string }
								ssh.Unmarshal(r.Payload, &payload)
								r.Reply(true, nil)
								cmd := exec.CommandContext(life, "/bin/sh", "-c", payload.Command)
								cmd.Env = []string{"HOME=" + remoteHome, "PATH=/usr/bin:/bin"}
								cmd.Dir = remoteHome
								cmd.Stdout = channel
								cmd.Stderr = channel.Stderr()
								status := uint32(0)
								if cmd.Run() != nil {
									status = 1
								}
								channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
								return
							}
						}()
					default:
						incoming.Reject(ssh.UnknownChannelType, "unsupported")
					}
				}
			}()
		}
	}()
	return Profile{SSHHost: host, SSHPort: port, SSHUser: "fixture", SSHKeyFile: identityPath, SSHConfigFile: config}, &destinations, &mu
}
func writeConfigFixture(t *testing.T, name, server, ca string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprintf("apiVersion: v1\nkind: Config\nclusters:\n- name: kind\n  cluster:\n    server: %s\n    certificate-authority-data: %s\ncontexts:\n- name: kind-test\n  context:\n    cluster: kind\n    user: fixture\ncurrent-context: kind-test\nusers:\n- name: fixture\n  user:\n    token: synthetic-test-token\n", server, ca)
	if err := os.WriteFile(name, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestSSHRemoteDiscoveryAndTLSAPI(t *testing.T) {
	remote := t.TempDir()
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-test-token" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"gitVersion":"v1.35.0-fixture"}`)
	}))
	defer api.Close()
	target := strings.TrimPrefix(api.URL, "https://")
	ca := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: api.Certificate().Raw}))
	// Port 1 is deliberately inaccessible on this Mac. Only the fake SSH server
	// can reach the API mapped to it; hostname verification remains enabled.
	config := filepath.Join(remote, ".kube", "config")
	writeConfigFixture(t, config, "https://127.0.0.1:1", ca)
	os.WriteFile(filepath.Join(remote, ".kube", "not-a-config"), []byte("notes"), 0600)
	p, destinations, mu := sshFixture(t, remote, target)
	p.RemoteConfig = true
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sources, _, err := DiscoverConfigs(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Contexts[0] != "kind-test" {
		t.Fatal(sources)
	}
	p.Kubeconfig = sources[0].Path
	p.Context = "kind-test"
	client, err := Connect(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if client.ServerVersion() != "v1.35.0-fixture" || client.Config().Insecure || client.Config().Host != "https://127.0.0.1:1" {
		t.Fatal("server identity or TLS changed")
	}
	mu.Lock()
	found := len(*destinations) > 0 && (*destinations)[0] == "127.0.0.1:1"
	mu.Unlock()
	if !found {
		t.Fatal("wrong SSH destination")
	}
	// A different certificate name must fail, even though TCP forwarding works.
	cfg := client.Config()
	cfg.ServerName = "wrong.invalid"
	if other, err := NewFromConfig(ctx, cfg, p); err == nil {
		other.Close()
		t.Fatal("TLS verification bypassed")
	}
}
func TestSSHConfigReferencesAndExecRejection(t *testing.T) {
	remote := t.TempDir()
	p, _, _ := sshFixture(t, remote, "")
	p.RemoteConfig = true
	p.Kubeconfig = "~/.kube/config"
	p.Context = "kind-test"
	file := filepath.Join(remote, ".kube", "config")
	writeConfigFixture(t, file, "https://127.0.0.1:6443", "")
	b, _ := os.ReadFile(file)
	text := strings.Replace(string(b), "certificate-authority-data: ", "certificate-authority: ca.pem", 1)
	text = strings.Replace(text, "token: synthetic-test-token", "client-certificate: client.pem\n    client-key: key.pem\n    tokenFile: token", 1)
	os.WriteFile(file, []byte(text), 0600)
	for name, data := range map[string]string{"ca.pem": "ca bytes", "client.pem": "cert bytes", "key.pem": "private test bytes", "token": "test-token\n"} {
		os.WriteFile(filepath.Join(remote, ".kube", name), []byte(data), 0600)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg, err := remoteClientConfig(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if string(cfg.Clusters["kind"].CertificateAuthorityData) != "ca bytes" || cfg.AuthInfos["fixture"].Token != "test-token" || string(cfg.AuthInfos["fixture"].ClientKeyData) != "private test bytes" {
		t.Fatal("remote relative credentials not resolved")
	}
	text = strings.Replace(text, "client-certificate: client.pem", "exec:\n      apiVersion: client.authentication.k8s.io/v1\n      command: must-never-run\n    client-certificate: client.pem", 1)
	os.WriteFile(file, []byte(text), 0600)
	if _, err = remoteClientConfig(ctx, p); err == nil || !strings.Contains(err.Error(), "exec credential plugin") {
		t.Fatal("remote exec was not rejected", err)
	}
}
func TestSSHStreamsBridgeAndCancellation(t *testing.T) {
	remote := t.TempDir()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "stream fixture") }))
	defer server.Close()
	p, _, _ := sshFixture(t, remote, strings.TrimPrefix(server.URL, "https://"))
	cfg := &rest.Config{Host: "https://127.0.0.1:1", Dial: p.dialSSHAPI, TLSClientConfig: rest.TLSClientConfig{CAData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	endpoint, _ := url.Parse(cfg.Host)
	bridged, u, closeBridge, err := streamDialBridge(ctx, cfg, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridge()
	client, err := rest.HTTPClientFor(bridged)
	if err != nil {
		t.Fatal(err)
	}
	client.Timeout = 5 * time.Second
	response, err := client.Get(u.String())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(b) != "stream fixture" || bridged.ServerName != "127.0.0.1" {
		t.Fatal("stream TLS bridge failed")
	}
	cancel()
	closeBridge()
	if c, e := net.DialTimeout("tcp", u.Host, time.Second); e == nil {
		c.Close()
		t.Fatal("listener survived cancellation")
	}
}
func TestLocalConfigDiscoveryAndSSHIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", "")
	for _, name := range []string{"config", "other.yaml", "nested/k3s.yaml", "cache/ignore.yaml"} {
		writeConfigFixture(t, filepath.Join(home, ".kube", name), "https://127.0.0.1:6443", "")
	}
	sources, _, err := DiscoverConfigs(context.Background(), Profile{})
	if err != nil || len(sources) != 3 {
		t.Fatal(len(sources), err)
	}
	a := Profile{Kubeconfig: "same", Context: "default"}
	b := a
	b.SSHHost = "one"
	c := b
	c.SSHHost = "two"
	d := b
	d.RemoteConfig = true
	if a.Identity() == b.Identity() || b.Identity() == c.Identity() || b.Identity() == d.Identity() {
		t.Fatal("different connections share identity")
	}
	for _, host := range []string{"-oProxyCommand=bad", "host with spaces", "user@host", "host;bad\n"} {
		if (Profile{SSHHost: host}).ValidateSSH() == nil {
			t.Fatal(host)
		}
	}
}

func TestSSHWebSocketExec(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{Subprotocols: []string{"v5.channel.k8s.io"}}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		ws.WriteMessage(websocket.BinaryMessage, append([]byte{1}, []byte("hello from remote pod")...))
		ws.WriteMessage(websocket.BinaryMessage, append([]byte{3}, []byte(`{"status":"Success"}`)...))
		ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}))
	defer server.Close()
	p, _, _ := sshFixture(t, t.TempDir(), strings.TrimPrefix(server.URL, "https://"))
	cfg := &rest.Config{Host: "https://127.0.0.1:1", Dial: p.dialSSHAPI, TLSClientConfig: rest.TLSClientConfig{CAData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})}}
	endpoint, _ := url.Parse(cfg.Host + "/exec")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var stdout bytes.Buffer
	executor := &apiExecutor{cfg: cfg}
	if err := executor.streamURL(ctx, endpoint, ExecOptions{Stdout: &stdout}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "hello from remote pod" {
		t.Fatal(stdout.String())
	}
}
func TestSSHDiscoveryCancellationAndHostKeyRejection(t *testing.T) {
	p, _, _ := sshFixture(t, t.TempDir(), "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sshCommand(ctx, p, "sleep 10", 1024); err == nil {
		t.Fatal("cancel ignored")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
	started := time.Now()
	_, err := sshCommand(ctx, p, "sleep 30", 1024)
	cancel()
	if err == nil || time.Since(started) > 2*time.Second {
		t.Fatal("running SSH command did not cancel promptly", err)
	}
	// Replace the trusted host database with an empty one. Batch mode must refuse
	// the unknown host, not silently accept a new key or show an invisible prompt.
	os.WriteFile(p.SSHConfigFile, []byte("Host *\n UserKnownHostsFile /dev/null\n GlobalKnownHostsFile /dev/null\n IdentityAgent none\n"), 0600)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := sshCommand(ctx, p, "true", 1024); err == nil || !strings.Contains(err.Error(), "Host key verification failed") {
		t.Fatal("host key verification not enforced", err)
	}
}

func TestSSHPasswordAskpassIsPrivateAndReusable(t *testing.T) {
	p, _, _ := sshFixture(t, t.TempDir(), "")
	p.SSHAuth = "password"
	p.SSHSecret = "fixture-password"
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := ProbeSSH(ctx, p); err != nil {
		t.Fatal(err)
	}
	RememberSSHAuthentication(p)
	p.SSHSecret = ""
	if err := ProbeSSH(ctx, p); err != nil {
		t.Fatal("session credential not reused", err)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "fixture-password") {
		t.Fatal("password serialized")
	}
}

func TestSSHAddressParsing(t *testing.T) {
	for _, test := range []struct{ input, user, host, port, wantUser string }{
		{"server", "", "server", "", ""}, {"server:2222", "alice", "server", "2222", "alice"}, {"ssh://alice@server:2222", "", "server", "2222", "alice"}, {"[::1]:2222", "", "::1", "2222", ""}, {"my-alias", "bob", "my-alias", "", "bob"},
	} {
		p, e := ParseSSHAddress(test.input, test.user)
		if e != nil || p.SSHHost != test.host || p.SSHPort != test.port || p.SSHUser != test.wantUser {
			t.Fatal(test, p, e)
		}
	}
}
func TestSSHExplicitHostTrust(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p, _, _ := sshFixture(t, t.TempDir(), "")
	os.WriteFile(p.SSHConfigFile, []byte("Host *\n UserKnownHostsFile /dev/null\n GlobalKnownHostsFile /dev/null\n IdentityAgent none\n"), 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	keys, e := ScanSSHHostKeys(ctx, p)
	if e != nil {
		t.Fatal(e)
	}
	if len(keys.Fingerprints) == 0 {
		t.Fatal("missing fingerprints")
	}
	p.SSHTrustFile, e = TrustSSHHostKeys(keys)
	if e != nil {
		t.Fatal(e)
	}
	if e = ProbeSSH(ctx, p); e != nil {
		t.Fatal("explicit trust not applied", e)
	}
}
