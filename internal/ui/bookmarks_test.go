package ui

import (
	"farshmac/internal/remotefs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func selectName(t *testing.T, p *Panel, name string) {
	t.Helper()
	for i, e := range p.Entries {
		if e.Name == name {
			p.Cursor = i
			return
		}
	}
	t.Fatal("entry not listed", name)
}

func TestFolderBookmarksRememberCursor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	left, right := t.TempDir(), t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		os.WriteFile(filepath.Join(left, name), []byte(name), 0644)
	}
	a, err := New(left, right)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	selectName(t, &a.Panels[0], "b.txt")
	// Option changes e.key ("£" on US); the physical code names the digit.
	a.key(Message{Key: "£", Code: "Digit4", Meta: true, Alt: true})
	if b := a.Bookmarks[4]; b.Kind != "folder" || b.Path != left || b.Cursor != "b.txt" {
		t.Fatalf("Cmd+Option+4 saved %+v (%s)", b, a.Status)
	}
	a.Active = 1
	a.key(Message{Key: "4", Code: "Digit4", Meta: true})
	p := &a.Panels[1]
	if p.Path != left || p.Entries[p.Cursor].Name != "b.txt" {
		t.Fatal("Cmd+4 did not open the folder on the file", p.Path, p.Entries[p.Cursor].Name, a.Status)
	}
	// Cmd+digit is no longer a function key, and Shift/Ctrl variants do nothing.
	a.key(Message{Key: "5", Code: "Digit5", Meta: true})
	if a.Form != nil || a.Dialog != nil || a.Status != "Bookmark 5 is empty" {
		t.Fatal("Cmd+5 must be bookmark 5, not F5", a.Status)
	}
	a.key(Message{Key: "⁄", Code: "Digit4", Meta: true, Alt: true, Shift: true})
	a.key(Message{Key: "$", Code: "Digit4", Ctrl: true, Shift: true})
	if a.Bookmarks[4].Cursor != "b.txt" || len(a.Bookmarks) != 1 {
		t.Fatal("Shift/Ctrl variants changed bookmarks")
	}
	// Slot 0, and a cursor file that is gone.
	a.key(Message{Key: "º", Code: "Digit0", Meta: true, Alt: true})
	if a.Bookmarks[0].Path != left {
		t.Fatal("Cmd+Option+0 did not save")
	}
	os.Remove(filepath.Join(left, "b.txt"))
	a.Panels[1].Load(right)
	a.key(Message{Key: "4", Code: "Digit4", Meta: true})
	if p.Path != left {
		t.Fatal("Cmd+4 did not jump", p.Path)
	}
	a.key(Message{Key: "9", Code: "Digit9", Meta: true})
	if a.Status != "Bookmark 9 is empty" {
		t.Fatal("empty slot", a.Status)
	}
	a.setBookmark(5, bookmark{Kind: "folder", Path: right})
	os.RemoveAll(right)
	a.key(Message{Key: "5", Code: "Digit5", Meta: true})
	if p.Path != left || !strings.Contains(a.Status, "no such file") {
		t.Fatal("missing folder must keep the panel", p.Path, a.Status)
	}
	// Remote bookmarks carry no secrets and reopen the connection dialog.
	a.Panels[0].Remote = &remotefs.Config{Protocol: "sftp", Host: "test", Port: 22, User: "demo", Password: "TOP_SECRET"}
	a.Panels[0].Path = "/remote/folder"
	a.Active = 0
	a.key(Message{Key: "™", Code: "Digit2", Meta: true, Alt: true})
	if b := a.Bookmarks[2]; b.Remote == nil || b.Remote.Password != "" || b.Path != "/remote/folder" {
		t.Fatalf("remote bookmark %+v", b)
	}
	a.Panels[0].Remote = nil
	a.key(Message{Key: "2", Code: "Digit2", Meta: true})
	if a.Form == nil || a.Form.Fields[0].Value != "sftp://test:22/remote/folder" {
		t.Fatal("remote bookmark did not open the connection dialog")
	}
}

func TestBookmarksCrossPanelsAndDocuments(t *testing.T) {
	a := textApp(t, "one\ntwo\nthree\n", true)
	doc := a.Doc
	a.key(Message{Key: "ArrowDown"})
	a.key(Message{Key: "£", Code: "Digit3", Meta: true, Alt: true})
	a.key(Message{Key: "Escape"})
	if a.Doc != nil {
		t.Fatal("editor still open")
	}
	// From the panels, a document bookmark opens the editor at the position.
	a.key(Message{Key: "3", Code: "Digit3", Meta: true})
	if a.Doc == nil || a.Doc.Path != doc.Path || !a.Doc.Edit || a.Doc.Pos(a.Doc.Cursor).Row != 1 {
		t.Fatal("document bookmark from panels", a.Status)
	}
	// From the editor, a folder bookmark returns to the panels on the file.
	a.setBookmark(7, bookmark{Kind: "folder", Path: filepath.Dir(doc.Path), Cursor: "file.txt"})
	a.key(Message{Key: "7", Code: "Digit7", Meta: true})
	p := &a.Panels[a.Active]
	if a.Doc != nil || p.Path != filepath.Dir(doc.Path) || p.Entries[p.Cursor].Name != "file.txt" {
		t.Fatal("folder bookmark from the editor", a.Doc != nil, p.Path, a.Status)
	}
	if len(a.Documents) != 1 || a.Documents[0].Path != doc.Path {
		t.Fatal("editor screen should stay open behind the panels")
	}
	// The menu lists both kinds.
	a.bookmarksMenu()
	labels := strings.Join(a.Form.Choice, "\n")
	if !strings.Contains(labels, "3  Edit "+doc.Path+":2") || !strings.Contains(labels, "7  "+filepath.Join(filepath.Dir(doc.Path), "file.txt")) {
		t.Fatal("menu labels", labels)
	}
}

func TestKubernetesFolderBookmarksRoundTrip(t *testing.T) {
	a, _ := kubeApp(t)
	// A local bookmark must switch out of the Kubernetes backend as well.
	local := a.Panels[1].Path
	a.Active = 1
	a.key(Message{Key: "1", Code: "Digit1", Meta: true, Alt: true})
	a.Active = 0
	enterKube(t, a, "shop")
	selectName(t, &a.Panels[0], "pods")
	a.key(Message{Key: "2", Code: "Digit2", Meta: true, Alt: true})
	b := a.Bookmarks[2]
	if b.Connection == nil || b.Connection.Cluster == nil || b.Connection.Kind != "kubernetes" || b.Path != "/shop" || b.Cursor != "pods" {
		t.Fatalf("cluster bookmark lost context: %+v", b)
	}
	profile := *b.Connection.Cluster
	a.key(Message{Key: "1", Code: "Digit1", Meta: true})
	waitNetwork(t, a)
	if a.Panels[0].Kube != nil || a.Panels[0].Path != local {
		t.Fatal("local bookmark retained cluster backend", a.Panels[0].Path)
	}
	// Reconnect from a local panel, not just within the original cluster view.
	a.key(Message{Key: "2", Code: "Digit2", Meta: true})
	waitNetwork(t, a)
	p := &a.Panels[0]
	if p.Kube == nil || kubeProfileKey(p.Kube.profile) != kubeProfileKey(profile) || p.Path != "/shop" || p.Entries[p.Cursor].Name != "pods" {
		t.Fatal("cluster bookmark not restored", p.Path, p.Err, a.Status)
	}
	// Cluster-scoped paths also retain their source and can open in the other panel.
	a.error(p.Load("/@cluster/namespaces"))
	waitNetwork(t, a)
	selectName(t, p, "shop")
	a.key(Message{Key: "3", Code: "Digit3", Meta: true, Alt: true})
	a.Active = 1
	a.key(Message{Key: "3", Code: "Digit3", Meta: true})
	waitNetwork(t, a)
	p = &a.Panels[1]
	if p.Kube == nil || p.Path != "/@cluster/namespaces" || p.Entries[p.Cursor].Name != "shop" {
		t.Fatal("cross-panel cluster bookmark not restored", p.Path, p.Err, a.Status)
	}
}
