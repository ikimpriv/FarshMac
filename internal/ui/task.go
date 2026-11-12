package ui

import (
	"context"
	"errors"
	"farshmac/internal/fsops"
)

type taskResult struct {
	apply, cleanup func()
	err            error
}
type uiTask struct {
	done           chan taskResult
	operation      *fsops.Operation
	applyCompleted bool
	reportError    bool
	cancelStatus   string // replaces the generic cancellation error, if set
}

func (a *App) backgroundTask(label string, fn func(*fsops.Operation) (func(), func(), error)) {
	if a.Busy || a.closed {
		return
	}
	o := fsops.NewOperation(label)
	o.IgnoreDSStore = a.Settings.IgnoreDSStore
	task := &uiTask{done: make(chan taskResult, 1), operation: o}
	a.task, a.Operation, a.Busy = task, o, true
	a.Status, a.dirty = label, true
	go func() { apply, cleanup, err := fn(o); task.done <- taskResult{apply, cleanup, err} }()
}
func (a *App) pollTask() {
	task := a.task
	if task == nil {
		return
	}
	select {
	case result := <-task.done:
		a.task, a.Operation, a.Busy = nil, nil, false
		a.LastProgress = task.operation.Snapshot()
		if result.cleanup != nil {
			defer result.cleanup()
		}
		if err := task.operation.Check(); err != nil && !task.applyCompleted {
			result.err = err
		}
		a.Status = ""
		if result.err != nil {
			a.error(result.err)
			cancelled := errors.Is(result.err, fsops.ErrCancelled) || errors.Is(result.err, context.Canceled)
			if cancelled && task.cancelStatus != "" {
				a.Status = task.cancelStatus
			}
			if task.reportError && !errors.Is(result.err, fsops.ErrCancelled) && !errors.Is(result.err, context.Canceled) {
				a.confirm(a.LastProgress.Label+" failed", result.err.Error(), []string{"OK"}, func(int) {})
			}
		} else if result.apply != nil {
			result.apply()
		}
		a.dirty = true
	default:
	}
}
