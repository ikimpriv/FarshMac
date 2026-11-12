package ui

import (
	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (a *App) transferDialog(move bool) {
	p := &a.Panels[a.Active]
	if move && (p.Remote != nil || a.Panels[1-a.Active].Remote != nil) {
		a.remoteMoveDialog()
		return
	}
	if move && a.archiveReadOnly() {
		return
	}
	if a.Panels[1-a.Active].Archive != nil {
		a.Status = "Choose a regular destination panel; writing into archives is not supported"
		return
	}
	catalog := p.Archive
	paths := p.Selected()
	if len(paths) == 0 {
		return
	}
	p.RestoreMarked = cloneMarks(p.Marked)
	base := p.filesystemPath()
	dest := a.Panels[1-a.Active].Path
	sourceEndpoint := remotefs.Endpoint{Remote: p.Remote}
	targetEndpoint := remotefs.Endpoint{Remote: a.Panels[1-a.Active].Remote}
	if p.Remote != nil || targetEndpoint.Remote != nil {
		base = dest
	}
	if len(paths) == 1 {
		dest = filepath.Join(dest, filepath.Base(paths[0]))
	}
	label := "Copy"
	if move {
		label = "Move / rename"
	}
	start := func(s string, metadata fsops.MetadataOptions) {
		workers := max(1, min(8, a.Settings.CopyWorkers))
		dst := expand(s, base)
		ask := a.Settings.ConfirmOverwrite
		a.workProgress(label, func(o *fsops.Operation) error {
			if move {
				o.IgnoreDSStore = false
				a.moveMetadataQuestions(o)
			} else {
				o.CopyMetadata = &metadata
				a.metadataQuestions(o, false)
			}
			if catalog != nil {
				names := make([]string, len(paths))
				for i, path := range paths {
					name, ok := catalog.Relative(path)
					if !ok {
						return fmt.Errorf("outside archive")
					}
					names[i] = name
				}
				temp, err := os.MkdirTemp("", "farshmac-archive-copy-*")
				if err != nil {
					return err
				}
				defer os.RemoveAll(temp)
				content := filepath.Join(temp, "content")
				if err = catalog.ExtractSelection(names, content, o); err != nil {
					return err
				}
				paths = make([]string, len(names))
				for i, name := range names {
					paths[i] = filepath.Join(content, name)
				}
			}
			if move {
				o.Streaming() // Same-volume rename must not enumerate the tree first.
			}
			var destination remotefs.FS
			err := o.Attempt("open destination", dst, "", func() error { var e error; destination, e = targetEndpoint.Open(o.Context()); return e })
			if err != nil {
				return err
			}
			var info os.FileInfo
			err = o.Attempt("read destination", dst, "", func() error {
				var e error
				info, e = destination.Lstat(dst)
				if os.IsNotExist(e) {
					return nil
				}
				info = remotefs.FollowDir(destination, dst, info)
				return e
			})
			exists := info != nil
			destination.Close()
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			isDir := err == nil && exists && info.IsDir()
			if len(paths) > 1 && !isDir {
				return fmt.Errorf("destination must be an existing directory")
			}
			a.collisionQuestions(o, ask)
			var conflict fsops.Conflict
			if !move {
				pairs := make([]remotefs.Pair, 0, len(paths))
				for _, src := range paths {
					target := dst
					if transferIntoDirectory(s, dst, dest, len(paths), isDir) {
						target = filepath.Join(dst, filepath.Base(src))
					}
					pairs = append(pairs, remotefs.Pair{Source: src, Destination: target})
				}
				return remotefs.Copy(sourceEndpoint, targetEndpoint, pairs, remotefs.CopyOptions{Workers: workers, Metadata: metadata, Conflict: conflict}, o)
			}
			for _, src := range paths {
				target := dst
				if transferIntoDirectory(s, dst, dest, len(paths), isDir) {
					target = filepath.Join(dst, filepath.Base(src))
				}
				if err := fsops.Transfer(src, target, move, conflict, o); err != nil {
					return err
				}
			}
			return nil
		})
	}
	if move {
		a.prompt(label, fmt.Sprintf("%d item(s) to", len(paths)), dest, func(s string) { start(s, fsops.MetadataOptions{}) })
		return
	}
	a.form(label, []Field{
		{Label: fmt.Sprintf("Copy %d item(s) to", len(paths)), Value: dest},
		{Label: "Preserve permissions (mode, owner, group, ACL)", Toggle: true, Checked: a.Settings.CopyPermissions},
		{Label: "Preserve attributes (xattrs, flags, timestamps)", Toggle: true, Checked: a.Settings.CopyAttributes},
		{Label: "Parallel transfers (1–8)", Value: strconv.Itoa(max(1, min(8, a.Settings.CopyWorkers)))},
	}, func(fields []Field) {
		workers, err := strconv.Atoi(fields[3].Value)
		if err != nil || workers < 1 || workers > 8 {
			a.error(fmt.Errorf("parallel transfers must be between 1 and 8"))
			return
		}
		a.Settings.CopyWorkers = workers
		a.Settings.CopyPermissions, a.Settings.CopyAttributes = fields[1].Checked, fields[2].Checked
		start(fields[0].Value, fsops.MetadataOptions{Permissions: fields[1].Checked, Attributes: fields[2].Checked})
	})
	a.Form.Primary = "Copy"
	a.Form.Selected = a.Settings.SelectDialogInput
}
func (a *App) archiveCreate() {
	if a.remoteUnsupported() || a.Panels[1-a.Active].Remote != nil {
		return
	}
	if a.archiveReadOnly() {
		return
	}
	paths := a.Panels[a.Active].Selected()
	if len(paths) == 0 {
		return
	}
	base := a.Panels[a.Active].Path
	dst := filepath.Join(a.Panels[1-a.Active].Path, filepath.Base(paths[0])+".zip")
	a.prompt("Create archive", "New filename: .zip / .tar.gz / .tar.bz2 / .tar.xz / .7z", dst, func(s string) {
		a.workProgress("Creating archive", func(o *fsops.Operation) error {
			return o.Attempt("create archive", strings.Join(paths, "\n"), expand(s, base), func() error { return fsops.CreateArchive(paths, expand(s, base), o) })
		})
	})
}
func (a *App) archiveExtract() {
	if a.remoteUnsupported() || a.Panels[1-a.Active].Remote != nil {
		return
	}
	p := &a.Panels[a.Active]
	path := p.Current()
	if p.Archive != nil {
		path = p.Archive.Path
	}
	a.archiveExtractPath(path)
}
func (a *App) archiveExtractPath(src string) {
	if src == "" {
		return
	}
	base := a.Panels[1-a.Active].filesystemPath()
	dst := filepath.Join(base, fsops.ArchiveStem(filepath.Base(src)))
	a.prompt("Extract archive", "New destination folder", dst, func(s string) {
		a.workProgress("Extracting archive", func(o *fsops.Operation) error {
			return o.Attempt("extract archive", src, expand(s, base), func() error { return fsops.ExtractArchive(src, expand(s, base), o) })
		})
	})
}
func (a *App) archiveCommands() {
	if a.remoteUnsupported() {
		return
	}
	p := &a.Panels[a.Active]
	path := p.Current()
	if p.Archive != nil {
		path = p.Archive.Path
	}
	if path == "" {
		return
	}
	a.choose("Archive: "+filepath.Base(path), []string{"List contents", "Test archive", "Extract to folder…"}, func(i int) {
		if i == 2 {
			a.archiveExtractPath(path)
			return
		}
		a.backgroundProgress("Reading archive", func(o *fsops.Operation) (func(), error) {
			text, err := fsops.ArchiveContents(path, i == 1, o)
			return func() { a.report("Archive: "+filepath.Base(path), text) }, err
		})
	})
}

// The prefilled single-item path names the item itself, even when it exists.
// A different existing directory or a trailing slash explicitly selects a container.
func transferIntoDirectory(input, destination, suggested string, count int, isDir bool) bool {
	return isDir && (count > 1 || destination != suggested || strings.HasSuffix(input, "/"))
}
