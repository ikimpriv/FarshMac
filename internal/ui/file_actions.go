package ui

import (
	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
	"fmt"
	"os"
	"path/filepath"
)

type selectionReceipt struct {
	panel        *Panel
	source, path string
	marks        map[string]bool
}

func (p *Panel) selectionReceipt() selectionReceipt {
	return selectionReceipt{p, p.backend().SizeSource(p), p.Path, cloneMarks(p.Marked)}
}
func (r selectionReceipt) complete() {
	if r.panel == nil || r.panel.Path != r.path || r.panel.backend().SizeSource(r.panel) != r.source {
		return
	}
	r.panel.RestoreMarked = cloneMarks(r.marks)
	for name := range r.marks {
		delete(r.panel.Marked, name)
	}
}

type nativeFileAction struct {
	Kind  string
	Paths []string
	ID    int64
}
type pendingFileAction struct {
	receipt selectionReceipt
	temp    string
}

func (p *Panel) fileContextActions() bool {
	if p.AI != nil {
		return p.can(capEdit)
	}
	if p.Docker != nil {
		return p.can(capEdit)
	}
	return p.Kube == nil || p.kubeLevel().Level == kubeContainerFS
}
func materializeSelection(p Panel, paths []string, o *fsops.Operation) ([]string, string, error) {
	if p.endpoint().Opener == nil && p.Archive == nil {
		return paths, "", nil
	}
	dir, err := os.MkdirTemp("", "farshmac-file-action-*")
	if err != nil {
		return nil, "", err
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(dir)
		}
	}()
	local := make([]string, len(paths))
	if p.Archive != nil {
		names := make([]string, len(paths))
		for i, item := range paths {
			var ok bool
			names[i], ok = p.Archive.Relative(item)
			if !ok {
				return nil, "", fmt.Errorf("outside archive")
			}
			local[i] = filepath.Join(dir, names[i])
		}
		if err = p.Archive.ExtractSelection(names, dir, o); err != nil {
			return nil, "", err
		}
	} else {
		pairs := make([]remotefs.Pair, len(paths))
		for i, item := range paths {
			local[i] = filepath.Join(dir, filepath.Base(item))
			pairs[i] = remotefs.Pair{Source: item, Destination: local[i]}
		}
		err = remotefs.Copy(p.endpoint(), remotefs.Endpoint{}, pairs, remotefs.CopyOptions{Workers: 4}, o)
		if err != nil {
			return nil, "", err
		}
	}
	success = true
	return local, dir, nil
}
func (a *App) nativeSelectedFiles(kind string) {
	p := &a.Panels[a.Active]
	paths := p.Selected()
	if len(paths) == 0 {
		return
	}
	snapshot, receipt := *p, p.selectionReceipt()
	a.backgroundTask("Preparing files", func(o *fsops.Operation) (func(), func(), error) {
		files, temp, err := materializeSelection(snapshot, paths, o)
		if err != nil {
			return nil, nil, err
		}
		handedOff := false
		return func() {
				handedOff = true
				a.fileActionID++
				if a.fileActions == nil {
					a.fileActions = map[int64]pendingFileAction{}
				}
				a.fileActions[a.fileActionID] = pendingFileAction{receipt: receipt, temp: temp}
				a.nativeFileAction = &nativeFileAction{Kind: kind, Paths: files, ID: a.fileActionID}
			}, func() {
				if !handedOff && temp != "" {
					os.RemoveAll(temp)
				}
			}, nil
	})
}
func (a *App) finishNativeFileAction(m Message) {
	action, ok := a.fileActions[m.Request]
	if !ok {
		return
	}
	if action.temp == "" || m.Count != 1 {
		delete(a.fileActions, m.Request)
		if action.temp != "" {
			os.RemoveAll(action.temp)
		}
	}
	if m.Text != "" {
		a.error(fmt.Errorf("%s", m.Text))
		return
	}
	if m.Count == 1 {
		action.receipt.complete()
		a.dirty = true
	}
}
func (a *App) archiveSelectionMenu() {
	p := &a.Panels[a.Active]
	paths := p.Selected()
	if len(paths) == 0 {
		return
	}
	a.choose("Archive format", []string{"ZIP (.zip)", "Tar + gzip (.tar.gz)", "Tar + bzip2 (.tar.bz2)"}, func(i int) {
		ext := []string{".zip", ".tar.gz", ".tar.bz2"}[i]
		name := filepath.Base(paths[0]) + ext
		if len(paths) > 1 {
			name = "Archive" + ext
		}
		a.prompt("Create archive", "Archive filename", name, func(value string) {
			if !validLocalTransferName(value) || !fsops.CreatableArchive(value) {
				a.error(fmt.Errorf("enter an archive filename with a supported extension"))
				return
			}
			target := filepath.Join(p.Path, value)
			if p.Archive != nil {
				a.error(fmt.Errorf("choose a writable panel for the new archive"))
				return
			}
			snapshot, receipt := *p, p.selectionReceipt()
			a.backgroundTask("Creating archive", func(o *fsops.Operation) (func(), func(), error) {
				a.operationQuestions(o)
				a.collisionQuestions(o, true)
				files, temp, err := materializeSelection(snapshot, paths, o)
				if err != nil {
					return nil, nil, err
				}
				if temp != "" {
					defer os.RemoveAll(temp)
				}
				dst := target
				if snapshot.endpoint().Opener != nil {
					dst = filepath.Join(temp, value)
				}
				err = fsops.CreateArchive(files, dst, o)
				if err == nil && snapshot.endpoint().Opener != nil {
					err = remotefs.Copy(remotefs.Endpoint{}, snapshot.endpoint(), []remotefs.Pair{{Source: dst, Destination: target}}, remotefs.CopyOptions{Workers: 1}, o)
				}
				return func() {
					if o.Snapshot().Skipped == 0 {
						receipt.complete()
					}
					a.Status = "Archive created"
				}, func() { a.refresh() }, err
			})
		})
	})
}
