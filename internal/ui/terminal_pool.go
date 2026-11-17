package ui

import (
	"encoding/base64"
	"encoding/json"
	"farshmac/internal/terminal"
)

type terminalHistoryState struct {
	terminalHistory    *terminal.History
	historyStatus      terminal.HistoryStatus
	historyWrite       chan terminal.HistoryStatus
	historyRead        chan terminal.HistoryPage
	historyPage        *terminal.HistoryPage
	historyQueue       [][]json.RawMessage
	historyQueuedBytes int
}
type parkedTerminal struct {
	terminalHistoryState
	pending      []byte
	stream       foreignStream
	label        string
	done         func()
	ended        bool
	commandPanel *Panel
}
type terminalPacket struct {
	Context string
	Data    string
}

func (a *App) currentTerminalKey() string {
	if a.terminalKey == "" {
		return "local"
	}
	return a.terminalKey
}
func (a *App) activateTerminal(key string) bool {
	if a.currentTerminalKey() == key {
		return true
	}
	if a.parkedTerminals == nil {
		a.parkedTerminals = map[string]*parkedTerminal{}
	}
	a.parkedTerminals[a.currentTerminalKey()] = &parkedTerminal{a.terminalHistoryState, a.pending, a.Foreign, a.foreignLabel, a.foreignDone, a.foreignEnded, a.shellCommandPanel}
	a.Shells.Suspend()
	old, found := a.parkedTerminals[key]
	delete(a.parkedTerminals, key)
	if !found {
		old = &parkedTerminal{}
	}
	a.terminalHistoryState = old.terminalHistoryState
	a.pending, a.Foreign, a.foreignLabel, a.foreignDone, a.foreignEnded, a.shellCommandPanel = old.pending, old.stream, old.label, old.done, old.ended, old.commandPanel
	a.terminalKey = key
	a.shellContextReturn = ""
	a.Shells.Select(key)
	if a.Foreign != nil {
		a.error(a.Foreign.Resize(a.Cols, max(1, a.Rows-1)))
	}
	a.dirty = true
	return found
}
func (a *App) withTerminalHistory(key string, fn func()) {
	if key == "" || key == a.currentTerminalKey() {
		fn()
		return
	}
	p := a.parkedTerminals[key]
	if p == nil {
		return
	}
	active := a.terminalHistoryState
	a.terminalHistoryState = p.terminalHistoryState
	fn()
	p.terminalHistoryState = a.terminalHistoryState
	a.terminalHistoryState = active
}
func (a *App) pollAllTerminals() {
	a.pollTerminalHistory()
	for key, p := range a.parkedTerminals {
		a.withTerminalHistory(key, a.pollTerminalHistory)
		if p.stream == nil || len(p.pending) >= 256*1024 || !p.terminalHistoryState.canDrain() {
			continue
		}
		for i := 0; i < 8 && len(p.pending) < 256*1024; i++ {
			select {
			case data, ok := <-p.stream.Output():
				managed := a.Shells.Lookup(key)
				if !ok {
					if managed != nil {
						r := managed.End()
						p.pending = append(p.pending, r.Output...)
					}
					p.stream = nil
					p.ended = true
					if p.done != nil {
						p.done()
						p.done = nil
					}
					goto next
				}
				if managed != nil {
					result := managed.Feed(data)
					data = result.Output
					if result.Completed && p.commandPanel != nil {
						a.error(p.commandPanel.Load(p.commandPanel.Path))
					}
				}
				p.pending = append(p.pending, data...)
			default:
				goto next
			}
		}
	next:
	}
}
func (a *App) backgroundTerminalPackets() []terminalPacket {
	var packets []terminalPacket
	budget := 32 * 1024
	for key, p := range a.parkedTerminals {
		if budget <= 0 {
			break
		}
		if len(p.pending) == 0 || !p.terminalHistoryState.canDrain() {
			continue
		}
		n := min(len(p.pending), 4096, budget)
		packets = append(packets, terminalPacket{key, base64.StdEncoding.EncodeToString(p.pending[:n])})
		p.pending = p.pending[n:]
		budget -= n
	}
	return packets
}

func (a *App) localTerminalCanDrain() bool {
	if a.currentTerminalKey() == "local" {
		return a.terminalCanDrain() && len(a.pending) < 256*1024
	}
	p := a.parkedTerminals["local"]
	return p != nil && len(p.pending) < 256*1024 && p.terminalHistoryState.canDrain()
}

// Terminal-generated replies (DSR, mouse protocol, etc.) belong to the screen
// that parsed the request, including when that screen is in the background.
func (a *App) routeBackgroundTerminalInput(m Message) bool {
	if m.Context == "" || m.Context == a.currentTerminalKey() || (m.Type != "input" && m.Type != "inputBinary") {
		return false
	}
	data := []byte(m.Text)
	if m.Type == "inputBinary" {
		var err error
		data, err = base64.StdEncoding.DecodeString(m.Text)
		if err != nil {
			return true
		}
	}
	if m.Context == "local" {
		if a.Term != nil {
			a.error(a.Term.Write(string(data)))
		}
		return true
	}
	if p := a.parkedTerminals[m.Context]; p != nil && p.stream != nil {
		a.error(p.stream.Write(data))
	}
	return true
}
