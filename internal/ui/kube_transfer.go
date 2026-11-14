package ui

import (
	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"fmt"
	"strings"
)

func (a *App) kubeResourceTransferDialog(source, target *Panel, move, local bool) {
	if !local && !move {
		a.kubeApplyDialog(source, target)
		return
	}
	ns, ok := kubeApplyTarget(target)
	if !ok {
		a.Status = "Choose a namespace (or a folder inside one) in the cluster panel"
		return
	}
	var refs []kube.Ref
	selected := map[string]bool{}
	for _, p := range source.Selected() {
		selected[p] = true
	}
	for _, e := range source.Entries {
		if !selected[source.Path+"/"+e.Name] {
			continue
		}
		if entry, ok := e.Info.(*kubeEntry); ok && entry.object != nil {
			refs = append(refs, entry.object.Ref())
		}
	}
	if len(refs) == 0 {
		a.Status = "Select resources inside a resource type folder"
		return
	}
	name := ""
	if len(refs) == 1 {
		name = refs[0].Name
	}
	label := "Copy resource"
	if move {
		label = "Move / rename resource"
	}
	fields := []Field{{Label: "Namespace", Value: ns}}
	if len(refs) == 1 {
		fields = append(fields, Field{Label: "Name", Value: name})
	}
	if local {
		fields = []Field{{Label: "New name", Value: name}}
	}
	from, to := source.Kube, target.Kube
	a.form(label, fields, func(fields []Field) {
		namespace := ns
		if !local {
			namespace = strings.TrimSpace(fields[0].Value)
		}
		newName := ""
		if len(refs) == 1 {
			index := 1
			if local {
				index = 0
			}
			newName = strings.TrimSpace(fields[index].Value)
			if newName == "" {
				a.error(fmt.Errorf("enter a resource name"))
				return
			}
		}
		a.workProgress(label, func(o *fsops.Operation) error {
			defer from.invalidate("")
			defer to.invalidate("")
			for _, ref := range refs {
				if err := o.Check(); err != nil {
					return err
				}
				o.Add(ref.String(), 0)
				err := o.Attempt("transfer resource", ref.String(), namespace+"/"+newName, func() error {
					return kube.TransferResource(o.Context(), from.client, to.client, ref, kube.ResourceTransferOptions{Namespace: namespace, Name: newName, Move: move})
				})
				if err = fsops.IgnoreSkipped(err); err != nil {
					return err
				}
				o.Complete(ref.String())
			}
			return nil
		})
	})
	if move {
		a.Form.Primary = "Move"
		a.Form.Message = "Creates the destination, then deletes the source. Owned resources remain orphaned."
	} else {
		a.Form.Primary = "Copy"
	}
	if local && len(refs) == 1 {
		a.Form.Focus = 0
		a.Form.Selected = true
		a.Form.Cursor = len(name)
	}
}
