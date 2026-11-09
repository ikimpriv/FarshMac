package ui

import (
	"encoding/json"
	"farshmac/internal/terminal"
)

func (a *App) appendTerminalHistory(lines []json.RawMessage) {
	if a.historyStatus.Error != "" || len(lines) == 0 {
		return
	}
	bytes := 0
	for _, line := range lines {
		bytes += len(line)
	}
	if a.historyQueuedBytes+bytes > terminal.MaxHistoryBatch || len(a.historyQueue) >= 8 {
		a.historyStatus.Error = "History writer is overloaded; live output continues"
		return
	}
	if a.terminalHistory == nil {
		h, err := terminal.NewHistory()
		if err != nil {
			a.historyStatus.Error = err.Error()
			return
		}
		a.terminalHistory = h
	}
	a.historyQueue = append(a.historyQueue, lines)
	a.historyQueuedBytes += bytes
	a.startHistoryWrite()
}
func (a *App) startHistoryWrite() {
	if a.historyWrite != nil || len(a.historyQueue) == 0 {
		return
	}
	lines := a.historyQueue[0]
	a.historyQueue = a.historyQueue[1:]
	for _, line := range lines {
		a.historyQueuedBytes -= len(line)
	}
	h := a.terminalHistory
	done := make(chan terminal.HistoryStatus, 1)
	a.historyWrite = done
	go func() { status, _ := h.Append(lines); done <- status }()
}
func (a *App) pollTerminalHistory() {
	if a.historyWrite != nil {
		select {
		case status := <-a.historyWrite:
			if a.historyStatus.Error != "" {
				status.Error = a.historyStatus.Error
			}
			a.historyStatus = status
			a.historyWrite = nil
			a.startHistoryWrite()
		default:
		}
	}
	if a.historyRead != nil {
		select {
		case page := <-a.historyRead:
			a.historyPage = &page
			a.historyRead = nil
		default:
		}
	}
}
func (a *App) readTerminalHistory(m Message) {
	if a.historyRead != nil {
		return
	} // The bridge keeps at most one page request in flight.
	done := make(chan terminal.HistoryPage, 1)
	a.historyRead = done
	h := a.terminalHistory
	go func() {
		page := terminal.HistoryPage{Lines: []json.RawMessage{}}
		if h != nil {
			var err error
			page, err = h.Page(m.Start, m.Count)
			if err != nil {
				page.Error = err.Error()
			}
		}
		page.Request = m.Request
		done <- page
	}()
}

// Overlap rendering with asynchronous disk writes. Keep a strict queue budget
// and leave headroom for the one renderer frame currently in flight.
func (a *App) terminalCanDrain() bool {
	return a.historyStatus.Error != "" || len(a.historyQueue) < 2 && a.historyQueuedBytes < terminal.MaxHistoryBatch/2
}
