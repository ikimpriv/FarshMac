package ui

import (
	"farshmac/internal/fsops"
	"fmt"
	"path/filepath"
)

// Called by the transfer worker; only queued closures touch the UI.
func (a *App) moveMetadataQuestions(o *fsops.Operation) {
	a.metadataQuestions(o, true)
}

func (a *App) metadataQuestions(o *fsops.Operation, move bool) {
	allowAll := false
	o.MetadataFailure = func(src, dst string, err error) error {
		o.Questions.Lock()
		defer o.Questions.Unlock()
		if cancelled := o.Check(); cancelled != nil {
			return cancelled
		}
		if allowAll {
			return nil
		}
		answer := make(chan int, 1)
		select {
		case <-o.Context().Done():
			return fsops.ErrCancelled
		case a.jobs <- func() {
			if o.Check() != nil {
				return
			}
			verb, question := "Copy", "with changed metadata?"
			if move {
				verb, question = "Move", "with changed metadata and remove the source?"
			}
			a.confirm("Cannot preserve metadata", fmt.Sprintf("%s %s %s %s", verb, tail(filepath.Base(src), 32), question, tail(err.Error(), 100)), []string{verb + " anyway", "For all", "Cancel"}, func(i int) { answer <- i })
		}:
		}
		select {
		case <-o.Context().Done():
			return fsops.ErrCancelled
		case choice := <-answer:
			if choice == 0 || choice == 1 {
				allowAll = choice == 1
				return o.Check()
			}
			return fsops.ErrCancelled
		}
	}
}
