package ui

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sessionGroup(t *testing.T, left, right string) *WindowGroup {
	t.Helper()
	a, e := New(left, right)
	if e != nil {
		t.Fatal(e)
	}
	g := NewWindowGroup(a)
	g.Factory = New
	g.StartShell = false
	return g
}
func readSession(t *testing.T, path string) SavedSession {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var s SavedSession
	if e = json.Unmarshal(b, &s); e != nil {
		t.Fatal(e)
	}
	return s
}
func TestWindowSessionRestoresMultipleWindows(t *testing.T) {
	root := t.TempDir()
	left, right := filepath.Join(root, "left"), filepath.Join(root, "right")
	os.Mkdir(left, 0700)
	os.Mkdir(right, 0700)
	os.WriteFile(filepath.Join(left, "selected"), nil, 0600)
	path := filepath.Join(root, "config", "session.json")
	g := sessionGroup(t, left, right)
	g.EnableSession(path, true)
	id, e := g.NewWindow(1)
	if e != nil {
		t.Fatal(e)
	}
	second := g.Apps[id]
	second.Panels[0].Load(right)
	second.Panels[1].Load(left)
	second.Active = 1
	second.Panels[0].View = "Brief"
	selectEntry(t, &g.Apps[1].Panels[0], "selected")
	g.SetFrame(1, WindowFrame{10, 20, 1100, 700})
	g.SetFrame(id, WindowFrame{-1400, 80, 900, 600})
	g.Focus(id)
	g.BeginQuit()
	if !g.Terminating {
		t.Fatal("quit failed")
	}
	g.CloseAll()
	saved := readSession(t, path)
	if len(saved.Windows) != 2 {
		t.Fatal("quit teardown overwrote session", saved)
	}
	restored := sessionGroup(t, root, root)
	if e = restored.EnableSession(path, true); e != nil {
		t.Fatal(e)
	}
	defer restored.CloseAll()
	if len(restored.Order) != 2 {
		t.Fatal("window count")
	}
	a, b := restored.Apps[1], restored.Apps[2]
	if a.Panels[0].Path != left || a.Panels[1].Path != right || filepath.Base(a.Panels[0].Current()) != "selected" {
		t.Fatal("first window panels")
	}
	if b.Panels[0].Path != right || b.Panels[1].Path != left || b.Active != 1 || b.Panels[0].View != "Brief" {
		t.Fatal("second window panels")
	}
	startup := restored.StartupWindows()
	if startup[1].Frame.X != -1400 || !startup[1].Focused || startup[0].Frame.Width != 1100 {
		t.Fatal(startup)
	}
	if a.Settings.AutoSave {
		t.Fatal("session restore must not require autosave setting")
	}
}
func TestSessionAutoSaveAndLastClosedWindow(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	os.Mkdir(sub, 0700)
	path := filepath.Join(root, "session.json")
	g := sessionGroup(t, root, root)
	g.EnableSession(path, false)
	g.Poll(1)
	g.Apps[1].Panels[0].Load(sub)
	g.SetFrame(1, WindowFrame{30, 40, 800, 500})
	g.lastSessionWrite = time.Now().Add(-time.Second)
	g.Poll(1)
	if readSession(t, path).Windows[0].Panels[0].Path != sub {
		t.Fatal("navigation not autosaved")
	}
	g.CloseWindow(1)
	g.CloseAll()
	s := readSession(t, path)
	if len(s.Windows) != 1 || s.Windows[0].Panels[0].Path != sub || s.Windows[0].Frame.X != 30 {
		t.Fatal("last window lost", s)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("session exposes folder paths", info.Mode())
	}
}
func TestSessionCloseOneWindowDoesNotRestoreIt(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.json")
	g := sessionGroup(t, root, root)
	g.EnableSession(path, false)
	id, _ := g.NewWindow(1)
	g.SetFrame(id, WindowFrame{50, 60, 800, 600})
	g.CloseWindow(1)
	g.BeginQuit()
	g.CloseAll()
	s := readSession(t, path)
	if len(s.Windows) != 1 || s.Windows[0].Frame.X != 50 {
		t.Fatal(s)
	}
}
func TestSessionMissingDirectoryInvalidFrameAndExplicitPaths(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.json")
	s := SavedSession{Version: 1, Windows: []SessionWindow{{Frame: WindowFrame{1, 2, -1, 100}, Panels: [2]SessionPanel{{Path: filepath.Join(root, "gone", "child")}, {Path: root}}, Active: 99}}}
	data, _ := json.Marshal(s)
	os.WriteFile(path, data, 0600)
	g := sessionGroup(t, root, root)
	if e := g.EnableSession(path, true); e != nil {
		t.Fatal(e)
	}
	if g.Apps[1].Panels[0].Path != root || g.Apps[1].Active != 1 || g.frames[1].Valid() {
		t.Fatal("invalid restore")
	}
	g.CloseAll()
	explicit := filepath.Join(root, "explicit")
	os.Mkdir(explicit, 0700)
	g = sessionGroup(t, explicit, root)
	g.EnableSession(path, false)
	if g.Apps[1].Panels[0].Path != explicit {
		t.Fatal("session overrode CLI path")
	}
	g.CloseAll()
	for _, f := range []WindowFrame{{0, 0, 0, 0}, {math.NaN(), 0, 1000, 800}, {0, 0, math.Inf(1), 800}, {1000000, 0, 1000, 800}} {
		if f.Valid() {
			t.Fatal("accepted invalid frame", f)
		}
	}
}
func TestCorruptSessionDoesNotPreventStartup(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.json")
	os.WriteFile(path, []byte("{invalid"), 0600)
	g := sessionGroup(t, root, root)
	if e := g.EnableSession(path, true); e == nil {
		t.Fatal("expected diagnostic")
	}
	if len(g.Apps) != 1 || g.Apps[1].Panels[0].Path != root {
		t.Fatal("corrupt session prevented fallback")
	}
	g.CloseAll()
}
