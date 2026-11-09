package ui

import "farshmac/internal/fsops"

type panelLoad struct {
	o        *fsops.Operation
	state    panelState
	previous Panel
	done     chan panelResult
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
	previous := *p
	previous.load, previous.tree = nil, nil
	state := p.beginLoad(path)
	p.cancelPanelLoad()
	p.cancelTree()
	o := fsops.NewOperation("Reading folder")
	job := &panelLoad{o: o, state: state, previous: previous, done: make(chan panelResult, 1)}
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
			p.Err = result.err.Error()
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
