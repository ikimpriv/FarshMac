package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"farshmac/internal/fsops"
)

// All decisions belong to one operation. Parallel workers serialize questions;
// cancellation wakes both a displayed dialog and workers waiting for their turn.
func (a *App) operationQuestions(o *fsops.Operation) {
	readOnlyDelete := -1
	o.BeforeDelete = func(path string, info os.FileInfo) int {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0222 != 0 {
			return fsops.Replace
		}
		o.Questions.Lock()
		defer o.Questions.Unlock()
		if o.Check() != nil {
			return fsops.Cancel
		}
		choice := readOnlyDelete
		if choice < 0 {
			choice = a.operationChoice(o, "Warning", "The file is read only\n"+path+"\nDelete it?", []string{"Delete", "All", "Skip", "Skip all", "Cancel"})
			if choice == 1 {
				readOnlyDelete = 0
			}
			if choice == 3 {
				readOnlyDelete = 2
			}
		}
		switch choice {
		case 0, 1:
			return fsops.Replace
		case 2, 3:
			return fsops.Skip
		default:
			return fsops.Cancel
		}
	}
	skipErrors := false
	o.Failure = func(f fsops.Failure) fsops.ErrorAction {
		o.Questions.Lock()
		defer o.Questions.Unlock()
		if o.Check() != nil {
			return fsops.ErrorCancel
		}
		if skipErrors {
			return fsops.ErrorSkip
		}
		message := "Cannot " + f.Operation + "\n" + f.Source
		if f.Destination != "" {
			message += "\nTo: " + f.Destination
		}
		message += "\n\n" + f.Err.Error()
		choice := a.operationChoice(o, "Error", message, []string{"Retry", "Skip", "Skip all", "Cancel"})
		switch choice {
		case 0:
			return fsops.ErrorRetry
		case 1:
			return fsops.ErrorSkip
		case 2:
			skipErrors = true
			return fsops.ErrorSkip
		default:
			return fsops.ErrorCancel
		}
	}
}

func (a *App) operationChoice(o *fsops.Operation, title, message string, buttons []string) int {
	answer := make(chan int, 1)
	select {
	case <-o.Context().Done():
		return len(buttons) - 1
	case a.jobs <- func() {
		if o.Check() != nil {
			return
		}
		a.confirm(title, message, buttons, func(i int) { answer <- i })
		a.Form.OperationQuestion = true
	}:
	}
	select {
	case <-o.Context().Done():
		return len(buttons) - 1
	case i := <-answer:
		return i
	}
}

func fileComparison(label string, info os.FileInfo) string {
	if info == nil {
		return label + ": unavailable"
	}
	kind := fmt.Sprintf("%d bytes", info.Size())
	if info.IsDir() {
		kind = "Folder"
	}
	return fmt.Sprintf("%s: %s  %s", label, kind, info.ModTime().Format("2006-01-02 15:04:05"))
}
func (a *App) collisionQuestions(o *fsops.Operation, ask bool) {
	remembered := -1
	readOnly := -1
	o.Collision = func(c fsops.Collision) fsops.Resolution {
		o.Questions.Lock()
		defer o.Questions.Unlock()
		if o.Check() != nil {
			return fsops.Resolution{Action: fsops.Cancel}
		}
		action := remembered
		canAppend := c.SourceInfo != nil && c.DestinationInfo != nil && c.SourceInfo.Mode().IsRegular() && c.DestinationInfo.Mode().IsRegular()
		if !ask {
			action = fsops.Replace
		}
		if action == fsops.Append && !canAppend {
			action = -1
		}
		destination := c.Destination
		if action < 0 {
			buttons := []string{"Overwrite", "Skip", "Rename"}
			actions := []int{fsops.Replace, fsops.Skip, fsops.Rename}
			if canAppend {
				buttons = append(buttons, "Append")
				actions = append(actions, fsops.Append)
			}
			buttons = append(buttons, "Cancel")
			actions = append(actions, fsops.Cancel)
			type decision struct {
				index    int
				remember bool
			}
			answer := make(chan decision, 1)
			select {
			case <-o.Context().Done():
				return fsops.Resolution{Action: fsops.Cancel}
			case a.jobs <- func() {
				if o.Check() != nil {
					return
				}
				message := "The file already exists\n" + c.Destination + "\n\n" + fileComparison("New", c.SourceInfo) + "\n" + fileComparison("Existing", c.DestinationInfo)
				a.confirm("Warning", message, buttons, func(i int) {})
				form := a.Form
				form.Remember = true
				form.OperationQuestion = true
				form.Select = func(i int) { answer <- decision{i, form.RememberChecked} }
				form.Cancel = func() { answer <- decision{len(buttons) - 1, false} }
			}:
			}
			select {
			case <-o.Context().Done():
				return fsops.Resolution{Action: fsops.Cancel}
			case d := <-answer:
				action = actions[d.index]
				if d.remember && action != fsops.Cancel {
					remembered = action
				}
			}
		}
		if action == fsops.Rename {
			// Remembered rename generates a fresh candidate for every collision. The
			// transfer checks it again and never overwrites an existing candidate blindly.
			ext := filepath.Ext(destination)
			stem := strings.TrimSuffix(filepath.Base(destination), ext)
			candidate := filepath.Join(filepath.Dir(destination), stem+" (copy)"+ext)
			if remembered == fsops.Rename {
				destination = candidate
			} else {
				answer := make(chan string, 1)
				select {
				case <-o.Context().Done():
					return fsops.Resolution{Action: fsops.Cancel}
				case a.jobs <- func() {
					if o.Check() != nil {
						return
					}
					a.form("Rename", []Field{{Label: "New name", Value: candidate}}, func(f []Field) { answer <- f[0].Value })
					a.Form.OperationQuestion = true
					a.Form.Cancel = func() { answer <- "" }
					a.Form.Selected = true
				}:
				}
				select {
				case <-o.Context().Done():
					return fsops.Resolution{Action: fsops.Cancel}
				case name := <-answer:
					if name == "" {
						return fsops.Resolution{Action: fsops.Cancel}
					}
					destination = expand(name, filepath.Dir(c.Destination))
				}
			}
		}
		if (action == fsops.Replace || action == fsops.Append) && c.DestinationInfo != nil && c.DestinationInfo.Mode().IsRegular() && c.DestinationInfo.Mode().Perm()&0222 == 0 {
			choice := readOnly
			if choice < 0 {
				choice = a.operationChoice(o, "Warning", "The file is read only\n"+c.Destination+"\nOverwrite it?", []string{"Overwrite", "All", "Skip", "Skip all", "Cancel"})
				if choice == 1 {
					readOnly = 0
				}
				if choice == 3 {
					readOnly = 2
				}
			}
			switch choice {
			case 0, 1:
			case 2, 3:
				action = fsops.Skip
			default:
				action = fsops.Cancel
			}
		}
		return fsops.Resolution{Action: action, Destination: destination}
	}
}
