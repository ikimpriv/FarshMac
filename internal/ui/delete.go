package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"farshmac/internal/fsops"
)

func (a *App) deleteSelected(permanent bool) {
	if !a.requireFilePanel() {
		return
	}

	if p := &a.Panels[a.Active]; p.backend().Delete(a, p, permanent) {
		return
	}
	paths := a.Panels[a.Active].Selected()
	if len(paths) == 0 {
		return
	}
	subject := deleteSubject(paths)
	title, action, progress := "Move to Trash", "Move to Trash", "Moving to Trash"
	message := "Move " + subject + " to Trash?"
	if permanent {
		title, action, progress = "Delete permanently", "Delete", "Deleting permanently"
		message = "Permanently delete " + subject + "?"
	}
	trash := a.trashItem
	if trash == nil {
		trash = fsops.Trash
	}
	remove := func() {
		a.workProgress(progress, func(o *fsops.Operation) error {
			// Enumerate as deletion proceeds so unreadable children can be skipped.
			o.Streaming()
			for _, path := range paths {
				if err := o.Check(); err != nil {
					return err
				}
				var err error
				if permanent {
					err = fsops.Remove(path, o)
				} else {
					o.Add(path, 0)
					err = fsops.IgnoreSkipped(o.Attempt("move to Trash", path, "", func() error {
						info, e := os.Lstat(path)
						if e != nil {
							return e
						}
						if o.BeforeDelete != nil {
							switch o.BeforeDelete(path, info) {
							case fsops.Skip:
								o.SkipItem(path)
								return fsops.ErrSkipped
							case fsops.Cancel:
								return fsops.ErrCancelled
							}
						}
						return trash(path, o)
					}))
				}
				if err != nil {
					return err // Never fall back to permanent deletion if Trash fails.
				}
			}
			return nil
		})
	}
	// "Confirm deletion" covers only Trash; irreversible deletion always asks,
	// as remote deletion does.
	if a.Settings.ConfirmDelete || permanent {
		a.confirm(title, message, []string{action, "Cancel"}, func(i int) {
			if i == 0 {
				remove()
			}
		})
	} else {
		remove()
	}
}

// deleteSubject names one item, or counts several like Far ("3 items"),
// so a large selection cannot stretch the confirmation over pages.
func deleteSubject(paths []string) string {
	if len(paths) == 1 {
		return strings.NewReplacer("\r", "\\r", "\n", "\\n", "\t", "\\t").Replace(filepath.Base(paths[0]))
	}
	return groupDigits(len(paths)) + " items"
}

func groupDigits(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
