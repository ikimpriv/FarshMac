package ui

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"farshmac/internal/remotefs"
	"farshmac/internal/testnet"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func remoteApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_AUTH_SOCK", "")
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}
func chooseButton(t *testing.T, a *App, label string) {
	t.Helper()
	if a.Form == nil {
		t.Fatal("no dialog for", label, a.Status)
	}
	for i, c := range a.Form.Choice {
		if c == label {
			a.Form.Focus = i
			a.key(Message{Key: "Enter"})
			return
		}
	}
	t.Fatalf("%q not in %v", label, a.Form.Choice)
}
func TestRemoteHostKeyMismatchShowsWarningDialog(t *testing.T) {
	a := remoteApp(t)
	c := testnet.SFTP(t)
	c.HostKey = nil
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	other, _ := ssh.NewSignerFromKey(priv)
	home, _ := os.UserHomeDir()
	os.MkdirAll(filepath.Join(home, ".ssh"), 0700)
	os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte(knownhosts.Line([]string{knownhosts.Normalize(c.Address())}, other.PublicKey())+"\n"), 0600)
	a.connectRemote(0, c)
	waitNetwork(t, a)
	if a.Form == nil || a.Form.Title != "SSH host key changed" || len(a.Form.Choice) != 1 || a.Form.Choice[0] != "OK" || a.Panels[0].Remote != nil {
		t.Fatal("mismatch not refused with warning", a.Status, a.Form)
	}
	if !strings.Contains(a.Form.Message, "known_hosts:1") || !strings.Contains(a.Form.Message, ssh.FingerprintSHA256(other.PublicKey())) || !strings.Contains(a.Form.Message, "man-in-the-middle") {
		t.Fatal("warning lacks details", a.Form.Message)
	}
	a.key(Message{Key: "Enter"})
	if a.Status != "Connection refused: host key mismatch" {
		t.Fatal(a.Status)
	}
}
func TestRemoteRevokedHostKeyRefused(t *testing.T) {
	a := remoteApp(t)
	c := testnet.SFTP(t)
	key, _ := ssh.ParsePublicKey(c.HostKey)
	c.HostKey = nil
	home, _ := os.UserHomeDir()
	os.MkdirAll(filepath.Join(home, ".ssh"), 0700)
	os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte("@revoked * "+string(ssh.MarshalAuthorizedKey(key))), 0600)
	a.connectRemote(0, c)
	waitNetwork(t, a)
	if a.Form == nil || a.Form.Title != "SSH host key revoked" || len(a.Form.Choice) != 1 || a.Panels[0].Remote != nil {
		t.Fatal("revoked key not refused", a.Status)
	}
}
func TestTrustAndSaveAppendsKnownHosts(t *testing.T) {
	a := remoteApp(t)
	c := testnet.SFTP(t)
	key, _ := ssh.ParsePublicKey(c.HostKey)
	c.HostKey = nil
	a.connectRemote(0, c)
	waitNetwork(t, a)
	if a.Form == nil || !strings.Contains(a.Form.Message, key.Type()) {
		t.Fatal("unknown host dialog lacks key type", a.Form)
	}
	chooseButton(t, a, "Trust and save")
	waitNetwork(t, a)
	data, err := os.ReadFile(remotefs.KnownHostsPath())
	if err != nil || !strings.Contains(string(data), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))) || a.Panels[0].Remote == nil {
		t.Fatal("known_hosts not written", err, a.Status)
	}
	a.disconnectRemote(0)
	a.connectRemote(0, c)
	waitNetwork(t, a)
	if a.Form != nil || a.Panels[0].Remote == nil {
		t.Fatal("saved host asked again", a.Status)
	}
}
func TestTrustAndSaveReportsWriteError(t *testing.T) {
	a := remoteApp(t)
	c := testnet.SFTP(t)
	c.HostKey = nil
	home, _ := os.UserHomeDir()
	os.Mkdir(filepath.Join(home, ".ssh"), 0500)
	t.Cleanup(func() { os.Chmod(filepath.Join(home, ".ssh"), 0700) })
	a.connectRemote(0, c)
	waitNetwork(t, a)
	chooseButton(t, a, "Trust and save")
	waitNetwork(t, a)
	if a.Panels[0].Remote == nil || !strings.Contains(a.Status, "known_hosts not updated") {
		t.Fatal("write error not reported or session trust lost", a.Status)
	}
}
func TestConnectionFailureDialogOffersEditAndRetry(t *testing.T) {
	a := remoteApp(t)
	c := remotefs.Config{Protocol: "sftp", Host: "127.0.0.1", Port: closedPort(t), User: "demo", Password: "pw", Directory: "/data"}
	a.connectRemote(0, c)
	waitNetwork(t, a)
	if a.Form == nil || a.Form.Title != "Connection failed" || strings.Join(a.Form.Choice, ",") != "Edit,Retry,Cancel" {
		t.Fatal("no failure dialog", a.Status)
	}
	chooseButton(t, a, "Retry")
	waitNetwork(t, a)
	if a.Form == nil || a.Form.Title != "Connection failed" {
		t.Fatal("retry did not reconnect")
	}
	chooseButton(t, a, "Edit")
	if a.Form == nil || a.Form.Title != "FTP / SFTP connection" || !strings.Contains(a.Form.Fields[0].Value, "/data") || a.Form.Fields[1].Value != "demo" || a.Form.Fields[2].Value != "pw" {
		t.Fatal("edit lost values", a.Form)
	}
	var rendered strings.Builder
	for _, cell := range a.Draw().Cells {
		rendered.WriteString(cell.Text)
	}
	if strings.Contains(rendered.String(), "pw") {
		t.Fatal("password rendered")
	}
}
func TestConnectionCancelHasNoDialog(t *testing.T) {
	a := remoteApp(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	a.connectRemote(0, remotefs.Config{Protocol: "sftp", Host: "127.0.0.1", Port: l.Addr().(*net.TCPAddr).Port, User: "u", Password: "p", Directory: "/"})
	time.Sleep(50 * time.Millisecond)
	a.cancelOperation()
	waitNetwork(t, a)
	if a.Form != nil || a.Status != "Connection cancelled" {
		t.Fatal("cancel showed dialog or wrong status", a.Status)
	}
}
func TestEncryptedKeyReopensDialogOnPassphraseField(t *testing.T) {
	a := remoteApp(t)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	block, _ := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("right"))
	key := filepath.Join(t.TempDir(), "id_test")
	os.WriteFile(key, pem.EncodeToMemory(block), 0600)
	c := remotefs.Config{Protocol: "sftp", Host: "127.0.0.1", Port: closedPort(t), User: "u", Directory: "/", KeyFile: key}
	a.connectRemote(0, c)
	waitNetwork(t, a)
	if a.Form == nil || a.Form.Title != "FTP / SFTP connection" || a.Form.Focus != 4 || a.Status != "Key id_test is passphrase protected" {
		t.Fatal("passphrase not requested", a.Status)
	}
	a.Form.Fields[4].Value = "wrong"
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if a.Form == nil || a.Form.Focus != 4 || a.Status != "Wrong key passphrase" {
		t.Fatal("wrong passphrase not reported", a.Status)
	}
	a.Form.Fields[3].Value = filepath.Join(t.TempDir(), "missing")
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if a.Form == nil || a.Form.Focus != 3 || !strings.HasSuffix(a.Status, "no such file") {
		t.Fatal("missing key not reported", a.Status)
	}
}
func TestPlainFTPWarnsOnce(t *testing.T) {
	a := remoteApp(t)
	s := testnet.FTP(t)
	a.connectRemote(0, s.Config)
	if a.Form == nil || a.Form.Title != "Plain FTP" || !a.Form.Remember {
		t.Fatal("no plain FTP warning")
	}
	a.Form.RememberChecked = true
	chooseButton(t, a, "Continue")
	waitNetwork(t, a)
	if a.Panels[0].Remote == nil {
		t.Fatal("not connected", a.Status)
	}
	a.disconnectRemote(0)
	a.connectRemote(0, s.Config)
	if a.Form != nil {
		t.Fatal("remembered warning asked again")
	}
	waitNetwork(t, a)
}
func TestDeleteConnectionProfile(t *testing.T) {
	a := remoteApp(t)
	for _, dir := range []string{"/one", "/two"} {
		if err := a.saveConnectionProfile(remotefs.Config{Protocol: "sftp", Host: "example.org", Port: 22, User: "u", Directory: dir}); err != nil {
			t.Fatal(err)
		}
	}
	a.networkMenu()
	if len(a.Form.Choice) != 4 {
		t.Fatal("profiles with different folders do not coexist", a.Form.Choice)
	}
	a.Form.Focus = 1
	a.key(Message{Key: "F8"})
	if a.Form == nil || a.Form.Title != "Delete connection profile" || !strings.Contains(a.Form.Message, "/one") {
		t.Fatal("no delete confirmation")
	}
	chooseButton(t, a, "Delete")
	b := &App{Settings: defaultSettings()}
	b.loadConnectionProfiles()
	if len(b.Settings.RemoteConnections) != 1 || b.Settings.RemoteConnections[0].Directory != "/two" || a.Form == nil || a.Form.Title != "Network connections" {
		t.Fatal("profile not deleted", b.Settings.RemoteConnections)
	}
}
func TestSaveSetupWritesRemoteDirectoryToConnections(t *testing.T) {
	a := remoteApp(t)
	c := testnet.SFTP(t)
	seedRemote(t, c)
	a.connectRemote(0, c)
	waitNetwork(t, a)
	loadPanel(t, &a.Panels[0], "/folder")
	a.saveSetup()
	b := &App{Settings: defaultSettings()}
	b.loadConnectionProfiles()
	if len(b.Settings.RemoteConnections) != 1 || b.Settings.RemoteConnections[0].Directory != "/folder" {
		t.Fatal("directory not saved", b.Settings.RemoteConnections, a.Status)
	}
}
func TestBrokenConnectionsFileIsRenamedAndRecreated(t *testing.T) {
	a := remoteApp(t)
	os.MkdirAll(filepath.Dir(profilesPath()), 0700)
	os.WriteFile(profilesPath(), []byte("{broken"), 0600)
	a.Settings.RemoteConnections = []remotefs.Config{{Protocol: "sftp", Host: "kept.example", Port: 22, Directory: "/"}}
	a.loadConnectionProfiles()
	if !strings.HasPrefix(a.Status, "connections.json is damaged; saved as connections.json.broken-") {
		t.Fatal(a.Status)
	}
	matches, _ := filepath.Glob(profilesPath() + ".broken-*")
	if len(matches) != 1 {
		t.Fatal("broken file not preserved")
	}
	if err := a.saveConnectionProfile(remotefs.Config{Protocol: "sftp", Host: "new.example", Port: 22, Directory: "/"}); err != nil {
		t.Fatal(err)
	}
	b := &App{Settings: defaultSettings()}
	b.loadConnectionProfiles()
	if len(b.Settings.RemoteConnections) != 2 {
		t.Fatal("connections.json not recreated", b.Settings.RemoteConnections)
	}
}
