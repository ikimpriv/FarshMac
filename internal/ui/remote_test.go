package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"farshmac/internal/remotefs"
	"farshmac/internal/testnet"
	"github.com/pkg/sftp"
)

func waitNetwork(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		a.Poll()
		if a.Form != nil && a.Form.Title == "Cannot preserve metadata" {
			a.Form.Focus = 1 // "For all"
			if a.Form.Choice[0] == "Continue" {
				a.Form.Focus = 0
			}
			a.key(Message{Key: "Enter"})
		}
		if !a.Busy && a.Panels[0].load == nil && a.Panels[1].load == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("network UI did not settle", a.Status)
}
func seedRemote(t *testing.T, c remotefs.Config) {
	t.Helper()
	f, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = f.Mkdir("/folder"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"file.txt", ".hidden", ".DS_Store"} {
		w, err := f.Create("/folder/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte("remote data")); err != nil {
			t.Fatal(err)
		}
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
func TestRemotePanelConnectNavigateCopyEditAndDisconnect(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := testnet.SFTP(t)
	seedRemote(t, c)
	local, right := t.TempDir(), t.TempDir()
	a, err := New(local, right)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Settings.IgnoreDSStore = true
	a.connectRemote(0, c)
	waitNetwork(t, a)
	p := &a.Panels[0]
	if p.Remote == nil || p.Path != "/" {
		t.Fatal("not connected", a.Status)
	}
	a.calculateFolderSizes(true)
	waitNetwork(t, a)
	if size, ok := p.folderSizes["folder"]; !ok || size.Bytes != 22 {
		t.Fatal("SFTP folder size", p.folderSizes, a.Status)
	}
	selectEntry(t, p, "folder")
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if panelContains(p, ".DS_Store") || !panelContains(p, ".hidden") {
		t.Fatal("remote display filter")
	}
	selectEntry(t, p, "file.txt")
	a.key(Message{Key: "F5"})
	if a.Form == nil || len(a.Form.Fields) != 4 {
		t.Fatal("remote copy options")
	}
	a.Form.Fields[1].Checked = false
	a.Form.Fields[2].Checked = false
	a.Form.Fields[3].Value = "3"
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if data, err := os.ReadFile(filepath.Join(right, "file.txt")); err != nil || string(data) != "remote data" {
		t.Fatal("download", err, a.Status)
	}
	selectEntry(t, p, "file.txt")
	a.key(Message{Key: "F4"})
	waitNetwork(t, a)
	if a.Doc == nil || !a.Doc.Edit || a.Doc.Source == nil {
		t.Fatal("remote editor", a.Status)
	}
	a.Doc.Insert("edited ")
	a.Doc.modified()
	want := a.Doc.Text
	a.save()
	waitNetwork(t, a)
	if !strings.HasSuffix(a.Doc.displayPath(), "/folder/file.txt") {
		t.Fatal("remote document lost display name", a.Doc.displayPath())
	}
	if a.Doc.Dirty {
		t.Fatal("remote save failed", a.Status)
	}
	f, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := f.Open("/folder/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(data) != want {
		t.Fatal("upload", string(data), err)
	}
	a.closeDocument()
	a.key(Message{Key: "F7"})
	a.Dialog.Input = "created"
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if filepath.Base(p.Current()) != "created" {
		t.Fatal("mkdir cursor", p.Current(), a.Status)
	}
	a.key(Message{Key: "F8"})
	if a.Form == nil || !strings.Contains(a.Form.Title, "permanently") {
		t.Fatal("remote delete did not explain Trash")
	}
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	selectEntry(t, p, "..")
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if filepath.Base(p.Current()) != "folder" {
		t.Fatal("remote parent cursor")
	}
	settings, _ := json.Marshal(a.Settings)
	if strings.Contains(string(settings), "secret") || strings.Contains(string(settings), "HostKey") {
		t.Fatal("credentials persisted")
	}
	a.disconnectRemote(0)
	waitNetwork(t, a)
	if p.Remote != nil || p.Path != local {
		t.Fatal("disconnect lost local folder", p.Path)
	}
}
func TestRemoteUnknownHostConfirmationAndMasking(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := testnet.SFTP(t)
	c.HostKey = nil
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.connectRemote(0, c)
	waitNetwork(t, a)
	if a.Form == nil || a.Form.Title != "Unknown SSH host key" || !strings.Contains(a.Form.Message, "SHA256:") {
		t.Fatal("missing host-key confirmation", a.Status)
	}
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	if a.Panels[0].Remote == nil || len(a.Panels[0].Remote.HostKey) == 0 {
		t.Fatal("trust once not pinned", a.Status)
	}
	a.connectionDialog(c)
	a.Form.Fields[2].Value = "never-render-me"
	a.Form.Fields[4].Value = "nor-this-secret"
	g := a.Draw()
	var rendered strings.Builder
	for _, cell := range g.Cells {
		rendered.WriteString(cell.Text)
	}
	if strings.Contains(rendered.String(), "never-render-me") || strings.Contains(rendered.String(), "nor-this-secret") {
		t.Fatal("password visible")
	}
}
func TestRemoteCommandsDoNotModifyLocalPaths(t *testing.T) {
	root := t.TempDir()
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Panels[0].Remote = &remotefs.Config{Protocol: "sftp", Host: "test", Port: 22}
	for _, run := range []func(){a.newFile, a.fileAttributes, a.createLink, a.describeFiles, a.archiveCreate} {
		run()
		if a.Form != nil || a.Dialog != nil || a.Busy {
			t.Fatal("local-only action started on remote panel")
		}
	}
}

func TestConnectionProfilesPersistWithoutSaveSetup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := &App{Settings: defaultSettings()}
	c := remotefs.Config{Protocol: "sftp", Host: "example.org", Port: 22, User: "demo", Directory: "/work", KeyFile: "/keys/demo", Password: "secret", KeyPassphrase: "secret"}
	if err := a.saveConnectionProfile(c); err != nil {
		t.Fatal(err)
	}
	b := &App{Settings: defaultSettings()}
	b.LoadSetup()
	if len(b.Settings.RemoteConnections) != 1 || b.Settings.RemoteConnections[0].KeyFile != c.KeyFile {
		t.Fatal("profile lost on restart")
	}
	raw, err := os.ReadFile(profilesPath())
	if err != nil || strings.Contains(string(raw), "secret") {
		t.Fatal("secrets stored", err)
	}
	b.networkMenu()
	if len(b.Form.Choice) != 3 {
		t.Fatal("saved connection missing from menu")
	}
	b.Form.Select(1)
	if b.Form.Title != "FTP / SFTP connection" || b.Form.Fields[1].Value != "demo" {
		t.Fatal("profile not selected")
	}
	c.KeyFile = "/keys/updated"
	if err := a.saveConnectionProfile(c); err != nil {
		t.Fatal(err)
	}
	b.loadConnectionProfiles()
	if len(b.Settings.RemoteConnections) != 1 || b.Settings.RemoteConnections[0].KeyFile != "/keys/updated" {
		t.Fatal("profile update duplicated")
	}
	c.Directory = "/updated"
	if err := a.saveConnectionProfile(c); err != nil {
		t.Fatal(err)
	}
	b.loadConnectionProfiles()
	if len(b.Settings.RemoteConnections) != 2 || b.Settings.RemoteConnections[1].Directory != "/updated" {
		t.Fatal("profiles for different folders do not coexist")
	}
}

func TestF5LocalToSFTPFileAndDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := testnet.SFTP(t)
	for _, active := range []int{0, 1} {
		t.Run(fmt.Sprintf("active=%d", active), func(t *testing.T) {
			local := t.TempDir()
			for name, data := range map[string]string{"upload ' файл.txt": "local upload", "folder/nested.txt": "nested upload"} {
				file := filepath.Join(local, name)
				if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
			}
			a, err := New(local, local)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			a.Settings.CopyPermissions = active == 1
			a.Settings.CopyAttributes = active == 1
			a.Settings.ConfirmOverwrite = false
			a.Settings.CopyWorkers = 3
			a.Active = active
			a.connectRemote(1-active, c)
			waitNetwork(t, a)
			for _, name := range []string{"upload ' файл.txt", "folder"} {
				selectEntry(t, &a.Panels[active], name)
				a.key(Message{Key: "F5"})
				if a.Form == nil {
					t.Fatalf("no copy form: %s", a.Status)
				}
				a.key(Message{Key: "Enter"})
				waitNetwork(t, a)
				f, err := c.Dial(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				target := "/" + name
				want := "local upload"
				if name == "folder" {
					target += "/nested.txt"
					want = "nested upload"
				}
				r, err := f.Open(target)
				if err != nil {
					f.Close()
					t.Fatalf("upload failed: %v, status %s", err, a.Status)
				}
				got, err := io.ReadAll(r)
				r.Close()
				f.Close()
				if err != nil || string(got) != want {
					t.Fatalf("upload contents: %q %v", got, err)
				}
			}
		})
	}
}

func TestRemoteCopyAsksMetadataOncePerOperation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := testnet.SFTP(t)
	local := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(local, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := New(local, local)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.connectRemote(1, c)
	waitNetwork(t, a)
	a.Panels[0].Marked = map[string]bool{"a": true, "b": true, "c": true}
	a.key(Message{Key: "F5"})
	a.Form.Fields[1].Checked, a.Form.Fields[2].Checked = true, true
	a.Form.Fields[3].Value = "1"
	a.key(Message{Key: "Enter"})
	asked := 0
	deadline := time.Now().Add(10 * time.Second)
	for a.Busy && time.Now().Before(deadline) {
		a.Poll()
		if a.Form != nil && a.Form.Title == "Cannot preserve metadata" {
			asked++
			if strings.Join(a.Form.Choice, ",") != "Continue,Cancel" || !strings.Contains(a.Form.Message, "keep only mode and modification time") {
				t.Fatalf("notice %q: %s", a.Form.Choice, a.Form.Message)
			}
			chooseOperation(t, a, "Continue")
		}
		time.Sleep(time.Millisecond)
	}
	if a.Busy || asked != 1 {
		t.Fatalf("asked %d times; busy=%v status=%s", asked, a.Busy, a.Status)
	}
	f, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, name := range []string{"a", "b", "c"} {
		if _, err := f.Lstat("/" + name); err != nil {
			t.Fatal(name, err, a.Status)
		}
	}
}

type failSetstat struct{ sftp.FileCmder }

func (f failSetstat) Filecmd(r *sftp.Request) error {
	if r.Method == "Setstat" {
		return os.ErrPermission
	}
	return f.FileCmder.Filecmd(r)
}

func TestRemoteChmodFailureStillAsksPerFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := testnet.SFTP(t, func(h *sftp.Handlers) { h.FileCmd = failSetstat{h.FileCmd} })
	local := t.TempDir()
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(local, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := New(local, local)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.connectRemote(1, c)
	waitNetwork(t, a)
	a.Panels[0].Marked = map[string]bool{"a": true, "b": true}
	a.key(Message{Key: "F5"})
	a.Form.Fields[1].Checked, a.Form.Fields[2].Checked = true, false
	a.Form.Fields[3].Value = "1"
	a.key(Message{Key: "Enter"})
	notices, failures := 0, 0
	deadline := time.Now().Add(10 * time.Second)
	for a.Busy && time.Now().Before(deadline) {
		a.Poll()
		if a.Form != nil && a.Form.Title == "Cannot preserve metadata" {
			if a.Form.Choice[0] == "Continue" {
				notices++
				chooseOperation(t, a, "Continue")
			} else {
				failures++
				if !strings.Contains(a.Form.Message, "permission denied") {
					t.Fatal("real failure hidden:", a.Form.Message)
				}
				chooseOperation(t, a, "Copy anyway")
			}
		}
		time.Sleep(time.Millisecond)
	}
	if a.Busy || notices != 1 || failures != 2 {
		t.Fatalf("notices=%d failures=%d busy=%v status=%s", notices, failures, a.Busy, a.Status)
	}
}

type denySFTPWrites struct{}

func (denySFTPWrites) Filewrite(*sftp.Request) (io.WriterAt, error) { return nil, os.ErrPermission }
func (denySFTPWrites) Filecmd(*sftp.Request) error                  { return os.ErrPermission }

func TestSFTPWriteFailuresStayVisible(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := testnet.SFTP(t, func(h *sftp.Handlers) { h.FilePut = denySFTPWrites{}; h.FileCmd = denySFTPWrites{} })
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "upload.txt"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(local, local)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.connectRemote(1, c)
	waitNetwork(t, a)
	selectEntry(t, &a.Panels[0], "upload.txt")
	a.key(Message{Key: "F5"})
	a.key(Message{Key: "Enter"})
	waitOperationQuestion(t, a, "Error")
	if a.Form == nil || !strings.Contains(a.Form.Message, "permission denied") {
		t.Fatalf("upload failure only flashed: %q form=%+v", a.Status, a.Form)
	}
	a.key(Message{Key: "Escape"})
	waitNetwork(t, a)
	a.Active = 1
	a.key(Message{Key: "F7"})
	a.Dialog.Input = "created"
	a.key(Message{Key: "Enter"})
	waitOperationQuestion(t, a, "Error")
	if a.Form == nil || !strings.Contains(a.Form.Message, "permission denied") {
		t.Fatalf("mkdir failure only flashed: %q form=%+v", a.Status, a.Form)
	}
	a.key(Message{Key: "Escape"})
	waitNetwork(t, a)
}
