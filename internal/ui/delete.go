package ui

import (
	"path/filepath"
	"strings"

	"farshmac/internal/fsops"
)

func (a *App) deleteSelected(permanent bool) {
	if a.Panels[a.Active].Remote != nil {
		a.remoteDelete()
		return
	}
	if a.archiveReadOnly() {
		return
	}
	paths := a.Panels[a.Active].Selected()
	if len(paths) == 0 {
		return
	}
	names := make([]string, len(paths))
	for i, path := range paths {
		names[i] = filepath.Base(path)
	}
	title, action, progress := "Move to Trash", "Move to Trash", "Moving to Trash"
	message := "Move " + strings.Join(names, ", ") + " to Trash?"
	if permanent {
		title, action, progress = "Delete permanently", "Delete", "Deleting permanently"
		message = "Permanently delete " + strings.Join(names, ", ") + "?"
	}
	trash := a.trashItem
	if trash == nil {
		trash = fsops.Trash
	}
	remove := func() {
		a.workProgress(progress, func(o *fsops.Operation) error {
			if permanent {
				if err := o.Scan(paths); err != nil {
					return err
				}
			} else {
				// Recycling a folder is one native operation; never scan its contents.
				for _, path := range paths {
					o.Add(path, 0)
				}
				o.Ready()
			}
			for _, path := range paths {
				if err := o.Check(); err != nil {
					return err
				}
				var err error
				if permanent {
					err = fsops.Remove(path, o)
				} else {
					err = trash(path, o)
				}
				if err != nil {
					return err // Never fall back to permanent deletion if Trash fails.
				}
			}
			return nil
		})
	}
	if a.Settings.ConfirmDelete {
		a.confirm(title, message, []string{action, "Cancel"}, func(i int) {
			if i == 0 {
				remove()
			}
		})
	} else {
		remove()
	}
}
