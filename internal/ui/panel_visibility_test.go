package ui

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"farshmac/internal/remotefs"
)

func visibilityFixture(t *testing.T) (*App, string) {
	t.Helper()
	left, right := t.TempDir(), t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(left, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(left, "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	a, err := New(left, right)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a, left
}

func TestQuickViewPanelIgnoresFileKeys(t *testing.T) {
	a, left := visibilityFixture(t)
	a.Active = 1
	a.key(Message{Key: "q", Ctrl: true}) // left panel becomes Quick view
	a.Active = 0
	p := &a.Panels[0]
	if p.View != "Quick view" {
		t.Fatal("quick view not shown", p.View)
	}
	selectEntry(t, p, "dir")
	cursor, entries := p.Cursor, len(p.Entries)
	for _, m := range []Message{
		{Key: "Enter"}, {Key: "F8"}, {Key: "F5"}, {Key: "F6"}, {Key: "F7"}, {Key: "Delete"},
		{Key: "Insert"}, {Key: " "}, {Key: "a", Alt: true}, {Key: "Enter", Shift: true},
		{Key: "PageDown", Ctrl: true}, {Key: "F6", Alt: true}, {Key: "f", Ctrl: true},
		{Key: "Insert", Ctrl: true}, {Key: "ArrowDown", Shift: true}, {Key: "Enter", Meta: true},
	} {
		a.Status = ""
		a.key(m)
		if a.Dialog != nil || a.Form != nil || a.FastFind != nil {
			t.Fatalf("%+v opened a modal on the invisible panel", m)
		}
		if p.Path != left || p.Cursor != cursor || len(p.Entries) != entries || len(p.Marked) != 0 || a.Command != "" || a.Clipboard != nil {
			t.Fatalf("%+v acted on invisible entries: path=%s cursor=%d marks=%v command=%q", m, p.Path, p.Cursor, p.Marked, a.Command)
		}
		if a.Status != "Switch to a file panel first (Tab)" {
			t.Fatalf("%+v status %q", m, a.Status)
		}
	}
	a.Panels[1].View = "Quick view"
	a.key(Message{Key: "Tab"})
	if a.Active != 1 {
		t.Fatal("Tab must still switch between two visible non-file panels")
	}
}

func TestInfoPanelIgnoresEnterAndTransfers(t *testing.T) {
	a, left := visibilityFixture(t)
	a.Active = 1
	a.key(Message{Key: "l", Ctrl: true})
	a.Active = 0
	p := &a.Panels[0]
	if p.View != "Info panel" {
		t.Fatal("info panel not shown", p.View)
	}
	selectEntry(t, p, "dir")
	for _, m := range []Message{{Key: "Enter"}, {Key: "F5"}, {Key: "F8"}, {Key: "F8", Shift: true}} {
		a.Status = ""
		a.key(m)
		if a.Dialog != nil || a.Form != nil || p.Path != left {
			t.Fatalf("%+v acted on the info panel", m)
		}
		if a.Status != "Switch to a file panel first (Tab)" {
			t.Fatalf("%+v status %q", m, a.Status)
		}
	}
	a.describeFiles()
	if a.Dialog != nil {
		t.Fatal("Describe files opened for the info panel")
	}
}

func TestQuickViewSingleClickActivatesPanel(t *testing.T) {
	a, _ := visibilityFixture(t)
	a.key(Message{Key: "q", Ctrl: true}) // right panel becomes Quick view
	x, w := a.panelRect(1)
	a.click(Message{X: x + w/2, Y: 5})
	if a.Active != 1 {
		t.Fatal("click did not activate quick view")
	}
}

func TestTabSkipsHiddenPanel(t *testing.T) {
	a, _ := visibilityFixture(t)
	a.key(Message{Key: "F2", Meta: true})
	if !a.Panels[1].Hidden {
		t.Fatal("right panel not hidden")
	}
	a.key(Message{Key: "Tab"})
	if a.Active != 0 || !strings.Contains(a.Status, "Other panel is hidden") {
		t.Fatal("Tab moved to hidden panel", a.Active, a.Status)
	}
	a.key(Message{Key: "F1", Meta: true})
	if !a.Panels[0].Hidden || !a.Panels[1].Hidden {
		t.Fatal("both panels should be hidden")
	}
	a.key(Message{Key: "Tab"})
	if a.Panels[a.Active].Hidden || a.Panels[1-a.Active].Hidden == false {
		t.Fatal("Tab must reveal only the active panel", a.Active, a.Panels[0].Hidden, a.Panels[1].Hidden)
	}
}

func TestVolumesUnhidesTargetPanel(t *testing.T) {
	a, _ := visibilityFixture(t)
	a.Panels[1].Hidden = true
	target := t.TempDir()
	a.openVolume(1, target)
	if a.Panels[1].Hidden || a.Active != 1 || a.Panels[1].Path != target {
		t.Fatal("volume did not show its panel", a.Panels[1].Hidden, a.Active, a.Panels[1].Path)
	}
}

func TestVolumesFromRemotePanelKeepsRemoteOnError(t *testing.T) {
	a, _ := visibilityFixture(t)
	p := &a.Panels[1]
	remote := &remotefs.Config{Protocol: "sftp", Host: "example.invalid", Port: 22}
	p.Remote, p.Path = remote, "/srv"
	a.openVolume(1, filepath.Join(t.TempDir(), "unmounted"))
	if p.Remote != remote || p.Path != "/srv" || a.Status == "" {
		t.Fatal("unreadable volume changed the remote panel", p.Remote, p.Path, a.Status)
	}
}

func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func TestMatchOtherPanelRemoteFailureRestoresLocalSource(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	left, right := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(right, "local.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := NewAsync(left, right)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	waitPanels(t, a)
	a.Panels[0].Remote = &remotefs.Config{Protocol: "sftp", Host: "127.0.0.1", Port: closedPort(t), User: "nobody"}
	a.Panels[0].Path = "/"
	a.matchOtherPanel()
	waitPanels(t, a)
	dst := &a.Panels[1]
	if dst.Remote != nil || dst.Path != right {
		t.Fatal("failed remote load left a half-switched panel", dst.Remote, dst.Path)
	}
	found := false
	for _, e := range dst.Entries {
		found = found || e.Name == "local.txt"
	}
	if !found || !strings.HasPrefix(a.Status, "Other panel kept: ") {
		t.Fatal("local listing or status lost", dst.Entries, a.Status)
	}
}

func TestDisconnectWithMissingLocalPathFallsBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	a, _ := visibilityFixture(t)
	p := &a.Panels[1]
	parent := t.TempDir()
	p.Remote = &remotefs.Config{Protocol: "sftp", Host: "example.invalid", Port: 22}
	p.RemoteLocalPath = filepath.Join(parent, "gone", "deeper")
	p.Path = "/srv"
	a.disconnectRemote(1)
	if p.Remote != nil || p.Path != parent || a.Status != "Disconnected" {
		t.Fatal("disconnect did not fall back to a readable folder", p.Remote, p.Path, a.Status)
	}
}

func TestCopyCommandMatchesControlInsert(t *testing.T) {
	a, left := visibilityFixture(t)
	p := &a.Panels[0]
	clip := func(m func()) string {
		a.Clipboard = nil
		m()
		if a.Clipboard == nil {
			t.Fatal("nothing copied")
		}
		return *a.Clipboard
	}
	cmdC := func() { a.copyPanelCommand(false) }
	ctrlInsert := func() { a.key(Message{Key: "Insert", Ctrl: true}) }
	selectEntry(t, p, "..")
	if got := clip(cmdC); got != left || got != clip(ctrlInsert) || a.Status != "Copied panel path" {
		t.Fatal("cursor on .. should copy the panel path", got, a.Status)
	}
	selectEntry(t, p, "a.txt")
	if got := clip(cmdC); got != "a.txt" || got != clip(ctrlInsert) {
		t.Fatal("current name", got)
	}
	p.Marked = map[string]bool{"a.txt": true, "b.txt": true}
	if got := clip(cmdC); got != "a.txt\nb.txt" || got != clip(ctrlInsert) || a.Status != "Copied 2 names" {
		t.Fatal("marked names", got, a.Status)
	}
	a.Command = "echo"
	if got := clip(cmdC); got != "echo" {
		t.Fatal("command line must win", got)
	}
}

func TestFolderShortcutKeepsRememberedPositions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, left := visibilityFixture(t)
	p := &a.Panels[0]
	selectEntry(t, p, "b.txt")
	p.Marked = map[string]bool{"a.txt": true}
	a.Settings.Shortcuts[5] = a.Panels[1].Path
	a.goFolderShortcut(5)
	if p.Path != a.Panels[1].Path {
		t.Fatal("shortcut not followed", p.Path)
	}
	a.error(p.Load(left))
	if p.Entries[p.Cursor].Name != "b.txt" || !p.Marked["a.txt"] {
		t.Fatal("bookmark jump forgot cursor/marks", p.Entries[p.Cursor].Name, p.Marked)
	}
}
