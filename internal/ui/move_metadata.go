package ui

import (
	"farshmac/internal/fsops"
	"fmt"
)

// Called by the transfer worker; only queued closures touch the UI.
func (a *App) moveMetadataQuestions(o *fsops.Operation) {
	a.metadataQuestions(o, true)
}

func (a *App) metadataQuestions(o *fsops.Operation, move bool) {
	allowAll := false
	skipAll := false
	noticed := false
	o.MetadataLimitation = func(notice string) error {
		o.Questions.Lock()
		defer o.Questions.Unlock()
		if cancelled := o.Check(); cancelled != nil {
			return cancelled
		}
		if noticed || allowAll {
			return nil
		}
		// A protocol limitation applies to every file alike: ask once per operation.
		if a.operationChoice(o, "Cannot preserve metadata", notice+"\nContinue for all files?", []string{"Continue", "Cancel"}) != 0 {
			return fsops.ErrCancelled
		}
		noticed = true
		return o.Check()
	}
	o.MetadataFailure = func(src, dst string, err error) error {
		o.Questions.Lock()
		defer o.Questions.Unlock()
		if cancelled := o.Check(); cancelled != nil {
			return cancelled
		}
		if skipAll {
			o.SkipItem(src)
			return fsops.ErrSkipped
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
			a.confirm("Cannot preserve metadata", fmt.Sprintf("%s %s\n%s\nTo: %s\n\n%s", verb, question, src, dst, err.Error()), []string{verb + " anyway", "For all", "Retry", "Skip", "Skip all", "Cancel"}, func(i int) { answer <- i })
			a.Form.OperationQuestion = true
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
			if choice == 2 {
				return fsops.ErrRetryMetadata
			}
			if choice == 3 || choice == 4 {
				skipAll = choice == 4
				o.SkipItem(src)
				return fsops.ErrSkipped
			}
			return fsops.ErrCancelled
		}
	}
}
