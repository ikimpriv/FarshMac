package ui

import (
	"farshmac/internal/fsops"
	"time"
)

type panelLoad struct {
	o        *fsops.Operation
	state    panelState
	previous Panel
	failure  string
	done     chan panelResult
	started  time.Time
}

// panelRollback is the panel as it was before a caller switched its source
// (remote, archive, view). An asynchronous load that fails restores exactly
// this snapshot instead of a half-switched panel.
type panelRollback struct {
	previous Panel
	failure  string
}

// takeRollback returns the snapshot a failed load must restore: the caller's
// pre-switch snapshot when one was registered, otherwise the current panel.
func (p *Panel) takeRollback() (Panel, string) {
	if r := p.rollback; r != nil {
		p.rollback = nil
		return r.previous, r.failure
	}
	previous := *p
	previous.load, previous.tree = nil, nil
	return previous, ""
}

// loadSource loads path after the caller changed the panel source fields. If
// loading fails (now, or later on the engine queue), the panel returns to
// previous, which the caller captured before touching Remote/Archive/View.
// failure prefixes the status shown for an asynchronous failure.
func (p *Panel) loadSource(path string, previous Panel, failure string) error {
	previous.load, previous.tree, previous.rollback = nil, nil, nil
	p.rollback = &panelRollback{previous: previous, failure: failure}
	err := p.Load(path)
	p.rollback = nil
	if err != nil {
		*p = previous
	}
	return err
}

type panelResult struct {
	panel Panel
	err   error
}

func (p *Panel) cancelPanelLoad() {
	if p.load != nil {
		p.load.o.Cancel()
		p.load = nil
	}
}
func (p *Panel) loadPanel(path string) error {
	previous, failure := p.takeRollback()
	state := p.beginLoad(path)
	p.cancelPanelLoad()
	p.cancelTree()
	o := fsops.NewOperation("Reading folder")
	job := &panelLoad{o: o, state: state, previous: previous, failure: failure, done: make(chan panelResult, 1), started: time.Now()}
	p.load = job
	p.Err = "Reading folder… Esc stops"
	worker := *p
	worker.Async, worker.load, worker.tree = false, nil, nil
	worker.states, worker.Entries, worker.History = nil, nil, nil
	worker.folderSizeHistory = nil
	worker.readOperation = o
	worker.Marked = cloneMarks(state.Marked)
	worker.MaskGroups = copyStrings(p.MaskGroups)
	go func() {
		err := worker.Load(path)
		job.done <- panelResult{worker, err}
	}()
	return nil
}
func copyStrings(m map[string]string) map[string]string {
	c := make(map[string]string, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}
func (p *Panel) pollPanelLoad() bool {
	job := p.load
	if job == nil {
		return false
	}
	select {
	case result := <-job.done:
		p.load = nil
		if err := job.o.Check(); err != nil {
			result.err = err
		}
		if result.err != nil {
			if job.previous.Path != "" {
				*p = job.previous
			}
			footer, hint := describeReadError("", result.err)
			p.Err = footer
			if job.failure != "" {
				p.loadStatus = job.failure + footer
			} else if hint != "" {
				p.loadStatus = hint
			}
			return true

		}
		entries := result.panel.Entries
		if len(entries) > 0 && entries[0].Name == ".." {
			entries = entries[1:]
		}
		p.Archive = result.panel.Archive
		p.installEntries(entries, job.state)
		p.Descriptions, p.TotalBytes, p.ItemCount, p.InfoText = result.panel.Descriptions, result.panel.TotalBytes, result.panel.ItemCount, result.panel.InfoText
		p.Err = ""
		return true
	default:
		return false
	}
}

func (a *App) cancelReads() bool {
	cancelled := false
	for i := range a.Panels {
		p := &a.Panels[i]
		if p.tree != nil {
			p.tree.cancel()
			p.Err = "Stopping tree scan…"
			cancelled = true
		}
		if p.load != nil {
			p.load.o.Cancel()
			p.Err = "Stopping folder scan…"
			cancelled = true
		}
	}
	if a.previewOperation != nil {
		a.previewOperation.Cancel()
		a.previewText = "Preview cancelled"
		cancelled = true
	}
	return cancelled
}

func (p *Panel) pendingPosition(cursor, top string) bool {
	var state *panelState
	if p.tree != nil {
		state = &p.tree.state
	}
	if p.load != nil {
		state = &p.load.state
	}
	if state == nil {
		return false
	}
	state.CursorName = cursor
	if top != "" {
		state.TopName = top
	}
	return true
}
