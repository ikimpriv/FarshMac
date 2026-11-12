package ui

import (
	"encoding/json"
	"fmt"
	"os"
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
	Terminating      bool
	windowSync
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
		a.inheritWindowState(p)
		for i := range a.Panels {
			a.Panels[i].panelOptions = p.Panels[i].panelOptions
		}
		a.refresh()
	} else {
		a.LoadSetup()
	}
	if g.StartShell && a.Term == nil {
		if err := a.StartTerminal(); err != nil {
			a.Status = "Shell: " + err.Error()
		}
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
			g.quitting = false
			g.waiting = 0
		}
		return
	}
	g.quitting = false
	if err := g.quitAutoSave(); err != nil {
		g.askQuitWithoutSetup(err)
		return
	}
	g.finishQuit()
}
func (g *WindowGroup) finishQuit() {
	g.Terminating = true
	if e := g.SaveSession(true); e != nil {
		for _, a := range g.Apps {
			a.error(e)
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
	if m.Type == "quit" || (m.Type == "key" && m.Meta && (m.Key == "q" || m.Key == "Q")) {
		g.BeginQuit()
		return
	}
	if g.quitting && id != g.waiting && m.Type != "resize" {
		a.Status = "Finish the quit dialog in the other window"
		a.dirty = true
		return
	}
	g.primeSettings(a)
	if m.Type == "close" {
		a.RequestQuit()
	} else {
		a.Handle(raw)
	}
	g.syncSettings(a)
	g.advanceQuit()
}
func (g *WindowGroup) Poll(id int) Update {
	a := g.Apps[id]
	if a == nil {
		return Update{Quit: true}
	}
	if !g.Terminating {
		if e := g.SaveSession(false); e != nil {
			a.error(e)
		}
	}
	g.primeSettings(a)
	u := a.Poll()
	g.syncSettings(a)
	g.advanceQuit()
	u.QuitApplication = g.Terminating
	u.AttentionWindow = g.waiting
	return u
}
func (g *WindowGroup) CloseWindow(id int) {
	var autoSaveErr error
	if a := g.Apps[id]; a != nil {
		s := g.windowSnapshot(id)
		g.lastWindow = &s
		autoSaveErr = a.autoSaveSetup()
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
	if autoSaveErr != nil {
		if a := g.Apps[g.focused]; a != nil {
			a.Status = "Setup not saved: " + autoSaveErr.Error()
			a.dirty = true
		}
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
	g.unlockSession()
}
