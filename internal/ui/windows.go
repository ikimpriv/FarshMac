package ui

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"time"
)

// WindowGroup is owned by the native serial engine queue; each window owns its
// panels, documents, terminal and operation. No App is shared between windows.
type WindowGroup struct {
	historyCleanups  []<-chan struct{}
	frames           map[int]WindowFrame
	focused          int
	lastWindow       *SessionWindow
	sessionPath      string
	lastSessionBytes []byte
	lastSessionWrite time.Time
	Apps             map[int]*App
	Order            []int
	next             int
	Factory          func(string, string) (*App, error)
	StartShell       bool
	quitting         bool
	waiting          int
	approved         map[int]bool
	attention        int
	sessionError     string
	sessionErrorAt   time.Time
	Terminating      bool
}

func NewWindowGroup(first *App) *WindowGroup {
	return &WindowGroup{frames: map[int]WindowFrame{}, focused: 1, Apps: map[int]*App{1: first}, Order: []int{1}, next: 1, StartShell: true, Factory: func(l, r string) (*App, error) {
		return newApp(l, r, first.Async)
	}}
}
func (g *WindowGroup) NewWindow(parent int) (int, error) {
	if g.quitting || g.Terminating {
		return 0, fmt.Errorf("finish quitting before opening a new window")
	}
	home, _ := os.UserHomeDir()
	l, r := home, home
	p := g.Apps[parent]
	if p != nil {
		l, r = p.Panels[0].filesystemPath(), p.Panels[1].filesystemPath()
	}
	a, e := g.Factory(l, r)
	if e != nil {
		if p != nil {
			p.error(e)
		}
		return 0, e
	}
	if p != nil {
		b, _ := json.Marshal(p.Settings)
		_ = json.Unmarshal(b, &a.Settings)
		for i := range a.Panels {
			a.Panels[i].panelOptions = p.Panels[i].panelOptions
		}
		// Histories are per window but start from the parent's, as after LoadSetup.
		a.history = slices.Clone(p.history)
		a.historyAt = len(a.history)
		a.folderHistory = slices.Clone(p.folderHistory)
		a.viewHistory = slices.Clone(p.viewHistory)
		a.dialogValues = maps.Clone(p.dialogValues)
		a.refresh()
	} else {
		a.LoadSetup()
	}
	if g.StartShell && a.Term == nil {
		a.StartTerminalOrRetry()
	}
	g.next++
	g.Apps[g.next] = a
	g.Order = append(g.Order, g.next)
	return g.next, nil
}
func (g *WindowGroup) BeginQuit() {
	if g.quitting {
		return
	}
	g.quitting = true
	g.approved = map[int]bool{}
	g.waiting = 0
	g.advanceQuit()
}
func (g *WindowGroup) advanceQuit() {
	if !g.quitting {
		return
	}
	if g.waiting != 0 {
		a := g.Apps[g.waiting]
		if a != nil {
			if a.Quit {
				a.Quit = false
				g.approved[g.waiting] = true
			} else if a.Form != nil || a.Dialog != nil || a.Busy {
				return
			} else {
				g.quitting = false
				g.waiting = 0
				return
			}
		}
		g.waiting = 0
	}
	for _, id := range g.Order {
		if g.approved[id] {
			continue
		}
		a := g.Apps[id]
		if a == nil {
			continue
		}
		g.waiting = id
		a.RequestQuit()
		if a.Quit {
			a.Quit = false
			g.approved[id] = true
			g.waiting = 0
			continue
		}
		if a.Form == nil && a.Dialog == nil {
			g.abortQuit(id)
		}
		return
	}
	g.quitting = false
	g.Terminating = true
	if e := g.SaveSession(true); e != nil {
		for _, a := range g.Apps {
			a.error(e)
		}
	}
}

// abortQuit reports a quit refused without a dialog (e.g. a running file
// operation): the refusing window is raised and the other windows explain why.
func (g *WindowGroup) abortQuit(id int) {
	g.quitting = false
	g.waiting = 0
	g.attention = id
	reason := fmt.Sprintf("Quit cancelled in window %d", id)
	if a := g.Apps[id]; a != nil && a.Busy {
		reason = fmt.Sprintf("Quit cancelled: window %d is running a file operation", id)
	}
	for other, a := range g.Apps {
		if other != id {
			a.Status = reason
			a.dirty = true
		}
	}
}
func (g *WindowGroup) Handle(id int, raw string) {
	a := g.Apps[id]
	if a == nil {
		return
	}
	var m Message
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	m = latinShortcut(m)
	if m.Type == "quit" || (m.Type == "key" && m.Meta && (m.Key == "q" || m.Key == "Q")) {
		g.BeginQuit()
		return
	}
	if g.quitting && id != g.waiting && m.Type != "resize" {
		a.Status = "Finish the quit dialog in the other window"
		a.dirty = true
		return
	}
	if m.Type == "close" {
		a.RequestQuit()
	} else {
		a.Handle(raw)
	}
	g.advanceQuit()
}
func (g *WindowGroup) Poll(id int) Update {
	a := g.Apps[id]
	if a == nil {
		return Update{Quit: true}
	}
	g.saveSessionPeriodically(a)
	u := a.Poll()
	g.advanceQuit()
	u.QuitApplication = g.Terminating
	u.AttentionWindow = g.waiting
	if u.AttentionWindow == 0 && g.attention != 0 {
		u.AttentionWindow = g.attention
		g.attention = 0
	}
	return u
}

// sessionRetryInterval spaces out periodic session writes after a failure.
var sessionRetryInterval = 30 * time.Second

// saveSessionPeriodically reports a failing periodic session save once per
// distinct error instead of overwriting the status line every frame.
func (g *WindowGroup) saveSessionPeriodically(a *App) {
	if g.Terminating || g.sessionError != "" && time.Since(g.sessionErrorAt) < sessionRetryInterval {
		return
	}
	e := g.SaveSession(false)
	if e == nil {
		g.sessionError = ""
		return
	}
	message := "Cannot save window session: " + e.Error()
	if message != g.sessionError && a.Status != message {
		a.Status = message
		a.dirty = true
	}
	g.sessionError = message
	g.sessionErrorAt = time.Now()
}
func (g *WindowGroup) CloseWindow(id int) {
	if a := g.Apps[id]; a != nil {
		s := g.windowSnapshot(id)
		g.lastWindow = &s
		a.Close()
		if a.historyCleanup != nil {
			g.historyCleanups = append(g.historyCleanups, a.historyCleanup)
		}
		delete(g.Apps, id)
	}
	for i, v := range g.Order {
		if v == id {
			g.Order = append(g.Order[:i], g.Order[i+1:]...)
			break
		}
	}
	delete(g.frames, id)
	if g.focused == id && len(g.Order) > 0 {
		g.focused = g.Order[len(g.Order)-1]
	}
	if !g.Terminating {
		_ = g.SaveSession(true)
	}
}
func (g *WindowGroup) CloseAll() {
	// Teardown must not replace the saved multi-window session with one/zero windows.
	if !g.Terminating {
		_ = g.SaveSession(true)
	}
	g.Terminating = true
	for _, id := range append([]int{}, g.Order...) {
		g.CloseWindow(id)
	}
	for _, done := range g.historyCleanups {
		<-done
	}
	g.historyCleanups = nil
}
