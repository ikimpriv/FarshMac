package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"
)

// Frame uses Cocoa screen coordinates in points (not backing pixels).
type WindowFrame struct{ X, Y, Width, Height float64 }

func (f WindowFrame) Valid() bool {
	for _, v := range []float64{f.X, f.Y, f.Width, f.Height} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return f.Width >= 100 && f.Height >= 100 && f.Width <= 20000 && f.Height <= 20000 && math.Abs(f.X) < 100000 && math.Abs(f.Y) < 100000
}

type SessionPanel struct {
	Path        string
	Options     panelOptions
	Cursor, Top string
}
type SessionWindow struct {
	Frame  WindowFrame
	Panels [2]SessionPanel
	Active int
}
type SavedSession struct {
	Version int
	Windows []SessionWindow
	Focused int
}
type StartupWindow struct {
	ID      int         `json:"id"`
	Frame   WindowFrame `json:"frame"`
	Focused bool        `json:"focused"`
}

func SessionPath() string {
	root, _ := os.UserConfigDir()
	return filepath.Join(root, "FarshMac", "session.json")
}
func accessibleFolder(path, fallback string) string {
	// Probe readability without enumerating a potentially enormous saved folder.
	readable := func(path string) bool {
		f, err := os.Open(path)
		if err != nil {
			return false
		}
		defer f.Close()
		_, err = f.ReadDir(1)
		return err == nil || err == io.EOF
	}
	if filepath.IsAbs(path) {
		for {
			if readable(path) {
				return path
			}
			parent := filepath.Dir(path)
			if parent == path {
				break
			}
			path = parent
		}
	}
	if readable(fallback) {
		return fallback
	}
	home, _ := os.UserHomeDir()
	if readable(home) {
		return home
	}
	return "/"
}
func (g *WindowGroup) windowSnapshot(id int) SessionWindow {
	a := g.Apps[id]
	s := SessionWindow{Frame: g.frames[id], Active: a.Active}
	for i, p := range a.Panels {
		saved := SessionPanel{Path: p.filesystemPath(), Options: p.panelOptions}
		if p.Cursor >= 0 && p.Cursor < len(p.Entries) {
			saved.Cursor = p.Entries[p.Cursor].Name
		}
		if p.Offset >= 0 && p.Offset < len(p.Entries) {
			saved.Top = p.Entries[p.Offset].Name
		}
		if p.tree != nil || p.load != nil {
			state := p.snapshot()
			saved.Cursor, saved.Top = state.CursorName, state.TopName
		}
		if p.Archive != nil {
			saved.Cursor = filepath.Base(p.Archive.Path)
			saved.Top = ""
		}
		s.Panels[i] = saved
	}
	return s
}
func (g *WindowGroup) sessionSnapshot() SavedSession {
	s := SavedSession{Version: 1, Windows: []SessionWindow{}}
	for _, id := range g.Order {
		if g.Apps[id] == nil {
			continue
		}
		if id == g.focused {
			s.Focused = len(s.Windows)
		}
		s.Windows = append(s.Windows, g.windowSnapshot(id))
	}
	// Remember the last closed window even when the app is left running without windows.
	if len(s.Windows) == 0 && g.lastWindow != nil {
		s.Windows = append(s.Windows, *g.lastWindow)
	}
	return s
}
func (g *WindowGroup) EnableSession(path string, restore bool) error {
	g.sessionPath = path
	if !restore {
		return nil
	}
	b, e := readConfigFile(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	var s SavedSession
	if e = json.Unmarshal(b, &s); e != nil {
		return fmt.Errorf("cannot read window session: %w", e)
	}
	if s.Version != 1 {
		return fmt.Errorf("unsupported window session version %d", s.Version)
	}
	if len(s.Windows) == 0 {
		return nil
	}
	if len(s.Windows) > 100 {
		return fmt.Errorf("too many saved windows")
	}
	first := g.Apps[g.Order[0]]
	for index, w := range s.Windows {
		left := accessibleFolder(w.Panels[0].Path, first.Panels[0].Path)
		right := accessibleFolder(w.Panels[1].Path, first.Panels[1].Path)
		id := g.Order[0]
		a := first
		if index > 0 {
			a, e = g.Factory(left, right)
			if e != nil {
				continue
			}
			b, _ := json.Marshal(first.Settings)
			_ = json.Unmarshal(b, &a.Settings)
			g.next++
			id = g.next
			g.Apps[id] = a
			g.Order = append(g.Order, id)
		}
		for i, path := range []string{left, right} {
			a.Panels[i].panelOptions = w.Panels[i].Options
			a.configurePanel(i)
			if e = a.Panels[i].Load(path); e != nil {
				a.error(e)
				continue
			}
			p := &a.Panels[i]
			p.pendingPosition(w.Panels[i].Cursor, w.Panels[i].Top)
			for j, entry := range p.Entries {
				if entry.Name == w.Panels[i].Cursor {
					p.Cursor = j
				}
				if entry.Name == w.Panels[i].Top {
					p.Offset = j
				}
			}
			p.Offset = min(p.Offset, p.Cursor)
		}
		a.Active = max(0, min(1, w.Active))
		if g.StartShell && a.Term == nil {
			a.StartTerminalOrRetry()
		}
		a.dirty = true
		if w.Frame.Valid() {
			g.frames[id] = w.Frame
		}
		if index == s.Focused {
			g.focused = id
		}
	}
	return nil
}
func (g *WindowGroup) SetFrame(id int, f WindowFrame) {
	if g.Apps[id] != nil && f.Valid() {
		g.frames[id] = f
	}
}
func (g *WindowGroup) Focus(id int) {
	if g.Apps[id] != nil {
		g.focused = id
	}
}
func (g *WindowGroup) StartupWindows() []StartupWindow {
	var out []StartupWindow
	for _, id := range g.Order {
		out = append(out, StartupWindow{id, g.frames[id], id == g.focused})
	}
	return out
}
func (g *WindowGroup) SaveSession(force bool) error {
	if g.sessionPath == "" {
		return nil
	}
	now := time.Now()
	if !force && now.Sub(g.lastSessionWrite) < 500*time.Millisecond {
		return nil
	}
	b, e := json.MarshalIndent(g.sessionSnapshot(), "", "  ")
	if e != nil {
		return e
	}
	if bytes.Equal(b, g.lastSessionBytes) {
		return nil
	}
	// Throttle failed writes too, so a read-only config folder does not stall every frame.
	g.lastSessionWrite = now
	if e = os.MkdirAll(filepath.Dir(g.sessionPath), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(g.sessionPath), ".session-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, e = f.Write(b); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(f.Name(), g.sessionPath); e != nil {
		return e
	}
	g.lastSessionBytes = b
	return nil
}
