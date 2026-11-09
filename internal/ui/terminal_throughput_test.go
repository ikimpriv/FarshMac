package ui

import (
	"encoding/json"
	"farshmac/internal/terminal"
	"testing"
)

func TestTerminalOutputOverlapsBoundedHistoryWrite(t *testing.T) {
	a := &App{Cols: 80, Rows: 24, Settings: defaultSettings(), TermVisible: true}
	a.historyWrite = make(chan terminal.HistoryStatus)
	a.pending = []byte("output while disk write is in flight")
	if u := a.Poll(); u.Terminal == "" {
		t.Fatal("disk writer stalls renderer")
	}
	a.pending = []byte("bounded")
	a.historyQueue = [][]json.RawMessage{{json.RawMessage(`{}`)}, {json.RawMessage(`{}`)}}
	if u := a.Poll(); u.Terminal != "" || len(a.pending) == 0 {
		t.Fatal("missing backpressure at queue limit")
	}
	a.historyQueue = nil
	a.historyQueuedBytes = terminal.MaxHistoryBatch / 2
	if a.terminalCanDrain() {
		t.Fatal("missing byte budget")
	}
	a.historyStatus.Error = "disk full"
	if !a.terminalCanDrain() {
		t.Fatal("history failure froze live terminal")
	}
}
