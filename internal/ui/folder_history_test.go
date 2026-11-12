package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"farshmac/internal/fsops"
	"farshmac/internal/testnet"
)

func historyChoice(t *testing.T, a *App, want string) int {
	t.Helper()
	a.historyMenu("Folders")
	if a.Form == nil {
		t.Fatal("no folder history", a.Status)
	}
	for i, c := range a.Form.Choice {
		if c == want {
			return i
		}
	}
	t.Fatalf("history lacks %q: %q", want, a.Form.Choice)
	return -1
}

func pickHistory(t *testing.T, a *App, want string) {
	t.Helper()
	i := historyChoice(t, a, want)
	f := a.Form
	a.Form = nil
	f.Select(i)
}

func TestFolderHistoryKeepsRemoteAndArchiveSources(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := testnet.SFTP(t)
	seedRemote(t, c)
	root := t.TempDir()
	src := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(src, "inner"), 0700); err != nil {
		t.Fatal(err)
	}
	arc := filepath.Join(root, "test.zip")
	if err := fsops.CreateArchive([]string{src}, arc); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	p := &a.Panels[0]

	// Visit a folder inside the archive, then a remote folder.
	selectEntry(t, p, "test.zip")
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	selectEntry(t, p, "source")
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	inside := filepath.Join(arc, "source")
	if p.Archive == nil || p.Path != inside {
		t.Fatal("not inside archive", p.Path)
	}
	a.connectRemote(0, c)
	waitNetwork(t, a)
	selectEntry(t, p, "folder")
	a.key(Message{Key: "Enter"})
	waitNetwork(t, a)
	remoteEntry := c.Label() + "/folder"

	// A local folder from history leaves the remote panel for a local one.
	pickHistory(t, a, root)
	waitNetwork(t, a)
	if p.Remote != nil || p.Archive != nil || p.Path != root {
		t.Fatal("local history entry", p.Remote, p.Path)
	}

	pickHistory(t, a, archiveHistoryPrefix+inside)
	waitNetwork(t, a)
	if p.Archive == nil || p.Path != inside {
		t.Fatal("archive history entry", p.Path, a.Status)
	}

	// The right panel holds a live connection to the same server: reuse it.
	a.connectRemote(1, c)
	waitNetwork(t, a)
	pickHistory(t, a, remoteEntry)
	waitNetwork(t, a)
	if p.Remote == nil || p.Path != "/folder" || p.Archive != nil {
		t.Fatal("remote history entry", p.Remote, p.Path, a.Status)
	}

	a.Panels[1].Remote = nil
	a.Panels[0].Remote = nil
	pickHistory(t, a, remoteEntry)
	if a.Form == nil || !strings.HasPrefix(a.Form.Fields[0].Value, "sftp://127.0.0.1:") || !strings.HasSuffix(a.Form.Fields[0].Value, "/folder") || a.Form.Fields[1].Value != c.User {
		t.Fatal("disconnected remote entry must open the connection dialog")
	}
	a.Form = nil

	os.Remove(arc)
	pickHistory(t, a, archiveHistoryPrefix+inside)
	if a.Status != "Archive not found: "+inside {
		t.Fatal("missing archive", a.Status)
	}

	a.saveSetup()
	raw, err := os.ReadFile(setupPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") {
		t.Fatal("password persisted in folder history")
	}
	var saved savedSetup
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(saved.Folders, "\n"), remoteEntry) {
		t.Fatal("remote history not saved", saved.Folders)
	}
}

func TestFolderHistoryLegacyStringsAreLocal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	legacy := t.TempDir()
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.folderHistory = []string{legacy}
	a.Panels[0].Remote = nil
	pickHistory(t, a, legacy)
	if a.Panels[0].Path != legacy || a.Panels[0].Remote != nil {
		t.Fatal("legacy entry", a.Panels[0].Path)
	}
}
