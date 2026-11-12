package ui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
)

func TestQuickViewOfArchiveMemberAndRemotePanelMenu(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source")
	if err := os.MkdirAll(src, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "member.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	arc := filepath.Join(root, "test.zip")
	if err := fsops.CreateArchive([]string{src}, arc); err != nil {
		t.Fatal(err)
	}
	a, err := NewAsync(root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	settlePanels(t, a)
	p := &a.Panels[0]
	selectEntry(t, p, "test.zip")
	a.key(Message{Key: "Enter"})
	settlePanels(t, a)
	selectEntry(t, p, "source")
	a.setView(1, "Quick view")
	settlePanels(t, a)
	waitPreview(t, a)
	if a.previewText != "Archive member: press F3 to extract and view" {
		t.Fatalf("archive member preview %q", a.previewText)
	}
	a.Panels[1].View = "Full"
	a.Panels[1].Remote = &remotefs.Config{Protocol: "sftp", Host: "127.0.0.1", Port: closedPort(t)}
	a.setView(1, "Quick view")
	if a.Panels[1].View != "Quick view" || a.Panels[1].load != nil {
		t.Fatal("menu refused Quick view for a remote panel", a.Status)
	}
	a.setView(1, "Info panel")
	if a.Panels[1].View != "Quick view" || !strings.Contains(a.Status, "remote") {
		t.Fatal("Info panel must stay unavailable for remote panels", a.Panels[1].View, a.Status)
	}
}

func TestDescribeReadErrorMapsEPERMToDiskAccessHint(t *testing.T) {
	eperm := &fs.PathError{Op: "open", Path: "/Users/x/Library/Mail", Err: syscall.EPERM}
	if footer, hint := describeReadError("", eperm); footer != privacyDeniedFooter || hint != diskAccessHint {
		t.Fatal(footer, hint)
	}
	eacces := &fs.PathError{Op: "open", Path: "/private/secret", Err: syscall.EACCES}
	if footer, hint := describeReadError("", eacces); footer != "Permission denied: /private/secret" || hint != "" {
		t.Fatal(footer, hint)
	}
	if footer, hint := describeReadError("/x", errors.New("dial tcp: i/o timeout")); footer != "dial tcp: i/o timeout" || hint != "" {
		t.Fatal("remote errors must pass through", footer, hint)
	}
}

func TestPanelLoadPermissionDeniedShowsHint(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	left := t.TempDir()
	locked := filepath.Join(left, "locked")
	if err := os.Mkdir(locked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0700) })
	a, err := NewAsync(left, left)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	waitPanels(t, a)
	p := &a.Panels[0]
	a.error(p.Load(locked))
	waitPanels(t, a)
	if p.Path != left || p.Err != "Permission denied: "+locked {
		t.Fatal("permission error", p.Path, p.Err)
	}
}

func TestTreeRootErrorRestoresPreviousRoot(t *testing.T) {
	root := treeFixture(t)
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.setView(0, "Tree panel")
	p := &a.Panels[0]
	waitTree(t, p)
	entries := len(p.Entries)
	missing := filepath.Join(root, "vanished")
	a.error(p.Load(missing))
	waitTree(t, p)
	a.Poll()
	if p.Path != root || len(p.Entries) != entries || !strings.Contains(p.Err, "no such file") {
		t.Fatal("tree root error not rolled back", p.Path, len(p.Entries), p.Err)
	}
	if !strings.HasPrefix(a.Status, "Cannot read "+missing+": ") {
		t.Fatal("status", a.Status)
	}
}

func waitPreview(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	a.Poll()
	for a.previewPending {
		if time.Now().After(deadline) {
			t.Fatal("preview did not finish")
		}
		time.Sleep(time.Millisecond)
		a.Poll()
	}
}

func TestQuickViewEmptySourceAndSpecialFiles(t *testing.T) {
	left := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(left, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(left, "nowhere"), filepath.Join(left, "dangling")); err != nil {
		t.Fatal(err)
	}
	a, err := New(left, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.key(Message{Key: "q", Ctrl: true})
	p := &a.Panels[0]
	selectEntry(t, p, "pipe")
	waitPreview(t, a)
	if a.previewText != "pipe\nNamed pipe (no preview)" {
		t.Fatalf("fifo preview %q", a.previewText)
	}
	selectEntry(t, p, "dangling")
	waitPreview(t, a)
	if a.previewText != "dangling\nBroken symbolic link → "+filepath.Join(left, "nowhere") {
		t.Fatalf("symlink preview %q", a.previewText)
	}
	p.Entries, p.Cursor = nil, 0
	waitPreview(t, a)
	if a.previewText != "No file selected" {
		t.Fatalf("empty preview %q", a.previewText)
	}
}
