package ui

import (
	"errors"
	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (a *App) transferDialog(move bool) {
	if !a.requireFilePanel() {
		return
	}
	p := &a.Panels[a.Active]
	other := &a.Panels[1-a.Active]
	if move && p.backend().Move(a, p) {
		return
	}
	if move && other.kind() != backendLocal {
		if other.kind() == backendRemote {
			a.remoteMoveDialog()
		} else {
			a.Status = "Moving into this panel is not supported. Use F5 Copy, then Delete."
		}
		return
	}
	if !other.can(capCopyIn) {
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
	dest := other.Path
	sourceEndpoint := p.endpoint()
	targetEndpoint := other.endpoint()
	if sourceEndpoint.Opener != nil || targetEndpoint.Opener != nil {
		base = dest
	}
	if len(paths) == 1 {
		dest = filepath.Join(dest, filepath.Base(paths[0]))
	}
	label := "Copy"
	if move {
		label = "Move / rename"
	}
	// Symbolic-link handling applies only to local-to-local transfers; archive
	// contents are extracted to a temporary folder and keep their links as is.
	links := catalog == nil && sourceEndpoint.Opener == nil && targetEndpoint.Opener == nil
	symlinkField := Field{Label: "Symbolic links", Toggle: true, Value: fsops.ParseSymlinkMode(a.Settings.CopySymlinks).String(), Options: []string{fsops.SymlinksSmart.String(), fsops.SymlinksAsIs.String(), fsops.SymlinksContents.String()}}
	symlinks := func(fields []Field) fsops.SymlinkMode {
		if !links {
			return fsops.SymlinksAsIs
		}
		mode := fsops.ParseSymlinkMode(fields[len(fields)-1].Value)
		a.Settings.CopySymlinks = mode.String()
		return mode
	}
	start := func(s string, metadata fsops.MetadataOptions, symlinks fsops.SymlinkMode) {
		if targetEndpoint.Opener != nil && remoteTilde(s) {
			a.Status = remoteTildeStatus
			return
		}
		workers := max(1, min(8, a.Settings.CopyWorkers))
		dst := expand(s, base)
		if !move && catalog == nil && len(paths) == 1 && dst == filepath.Clean(paths[0]) && sourceEndpoint.Same(targetEndpoint) && !selectedDir(p, paths[0]) {
			a.Status = "cannot copy a file onto itself: " + dst // As in Far: no operation, no dialog.
			return
		}
		ask := a.Settings.ConfirmOverwrite
		a.workProgress(label, func(o *fsops.Operation) error {
			o.Symlinks = symlinks
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
			err := o.AttemptRequired("open destination", dst, "", func() error { var e error; destination, e = targetEndpoint.Open(o.Context()); return e })
			if err != nil {
				return err
			}
			var info os.FileInfo
			err = o.AttemptRequired("read destination", dst, "", func() error {
				var e error
				info, e = destination.Lstat(dst)
				if os.IsNotExist(e) {
					return nil
				}
				info = remotefs.FollowDir(destination, dst, info)
				return e
			})
			exists := info != nil
			created := false
			if err == nil && strings.HasSuffix(s, "/") {
				// A trailing slash names a folder, as in Far: create it and copy inside.
				if exists && !info.IsDir() {
					err = fmt.Errorf("destination is not a directory: %s", dst)
				} else if !exists {
					err = o.AttemptRequired("create folder", dst, "", func() error {
						if targetEndpoint.Opener == nil {
							return os.MkdirAll(dst, 0777)
						}
						return destination.Mkdir(dst)
					})
					created = err == nil
				}
			}
			destination.Close()
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			isDir := created || err == nil && exists && info.IsDir() && !(len(paths) == 1 && caseRename(paths[0], dst))
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
			o.TransferRoots(paths)
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
		a.form(label, []Field{{Label: fmt.Sprintf("Move %d item(s) to", len(paths)), Value: dest}, symlinkField}, func(fields []Field) {
			start(fields[0].Value, fsops.MetadataOptions{}, symlinks(fields))
		})
		a.Form.Primary = "Move"
		a.Form.Selected = a.Settings.SelectDialogInput
		return
	}
	fields := []Field{
		{Label: fmt.Sprintf("Copy %d item(s) to", len(paths)), Value: dest},
		{Label: "Preserve permissions (mode, owner, group, ACL)", Toggle: true, Checked: a.Settings.CopyPermissions},
		{Label: "Preserve attributes (xattrs, flags, timestamps)", Toggle: true, Checked: a.Settings.CopyAttributes},
		{Label: "Parallel transfers (1–8)", Value: strconv.Itoa(max(1, min(8, a.Settings.CopyWorkers)))},
	}
	if links {
		fields = append(fields, symlinkField)
	}
	a.form(label, fields, func(fields []Field) {
		workers, err := strconv.Atoi(fields[3].Value)
		if err != nil || workers < 1 || workers > 8 {
			a.error(fmt.Errorf("parallel transfers must be between 1 and 8"))
			return
		}
		a.Settings.CopyWorkers = workers
		a.Settings.CopyPermissions, a.Settings.CopyAttributes = fields[1].Checked, fields[2].Checked
		start(fields[0].Value, fsops.MetadataOptions{Permissions: fields[1].Checked, Attributes: fields[2].Checked}, symlinks(fields))
	})
	a.Form.Primary = "Copy"
	a.Form.Selected = a.Settings.SelectDialogInput
}

const remoteTildeStatus = "~ is not available on remote destinations; enter an absolute server path"

// remoteTilde: "~" and "~user" would expand to a local home folder.
func remoteTilde(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "~") }

const archiveRemoteStatus = "Archives are extracted and created on local panels. Use F5 to transfer the result afterwards"

// archiveRemoteTarget refuses archive output into a remote opposite panel; its
// filesystemPath is the hidden local folder from before connecting.
func (a *App) archiveRemoteTarget() bool {
	if a.Panels[1-a.Active].can(capLocalOutput) {
		return false
	}
	a.Status = archiveRemoteStatus
	return true
}

func (a *App) archiveCreate() {
	if a.remoteUnsupported() || a.archiveRemoteTarget() {
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
	a.archiveCreatePrompt(paths, base, filepath.Join(a.Panels[1-a.Active].Path, filepath.Base(paths[0])+".zip"))
}

// The destination is checked before starting: Retry cannot fix an existing
// name, and Skip must not report "Done" for an archive that was never created.
func (a *App) archiveCreatePrompt(paths []string, base, suggested string) {
	a.prompt("Create archive", "New filename: .zip / .tar.gz / .tar.bz2 / .tar.xz / .7z", suggested, func(s string) {
		dst := expand(s, base)
		if !fsops.CreatableArchive(dst) {
			a.Status = "Archive creation supports .zip, .tar, .tar.gz, .tar.bz2, .tar.xz and .7z"
			return
		}
		if _, err := os.Lstat(dst); err == nil {
			a.confirm("Warning", "The archive already exists\n"+dst, []string{"Rename", "Cancel"}, func(i int) {
				if i == 0 {
					a.archiveCreatePrompt(paths, base, archiveCandidate(dst))
				}
			})
			return
		}
		source := paths[0]
		if len(paths) > 1 {
			source = fmt.Sprintf("%s (+%d more)", paths[0], len(paths)-1)
		}
		a.workProgress("Creating archive", func(o *fsops.Operation) error {
			return o.AttemptRequired("create archive", source, dst, func() error { return fsops.CreateArchive(paths, dst, o) })
		})
	})
}

// archiveCandidate suggests "<stem> (1).zip", keeping compound extensions.
func archiveCandidate(dst string) string {
	name := filepath.Base(dst)
	stem := fsops.ArchiveStem(name)
	ext := strings.TrimPrefix(name, stem)
	for n := 1; ; n++ {
		candidate := filepath.Join(filepath.Dir(dst), fmt.Sprintf("%s (%d)%s", stem, n, ext))
		if _, err := os.Lstat(candidate); os.IsNotExist(err) || n >= 1000 {
			return candidate
		}
	}
}
func (a *App) archiveExtract() {
	if a.remoteUnsupported() || a.archiveRemoteTarget() {
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
	if !a.Panels[1-a.Active].can(capLocalOutput) {
		base = a.Panels[a.Active].filesystemPath() // Never the remote panel's hidden local folder.
	}
	dst := filepath.Join(base, fsops.ArchiveStem(filepath.Base(src)))
	a.prompt("Extract archive", "New destination folder", dst, func(s string) {
		a.workProgress("Extracting archive", func(o *fsops.Operation) error {
			return o.Attempt("extract archive", src, expand(s, base), func() error {
				err := fsops.ExtractArchive(src, expand(s, base), o)
				if err != nil && !errors.Is(err, fsops.ErrCancelled) && fsops.ArchiveFormat(src) == "" {
					// libarchive is still tried for .iso, .xar, .deb, single .gz and the like.
					err = fmt.Errorf("%w (%s has no known archive extension)", err, filepath.Base(src))
				}
				return err
			})
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
			return func() { a.report("Archive: "+filepath.Base(path), text) }, archiveFailure(err)
		})
		if a.task != nil {
			a.task.reportError = true
		}
	})
}

func selectedDir(p *Panel, path string) bool {
	for _, e := range p.Entries {
		if e.Name == filepath.Base(path) {
			return e.Dir
		}
	}
	return false
}

// caseRename: on a case-insensitive volume "Folder" → "folder" finds the source
// itself, which must be renamed rather than treated as a destination folder.
func caseRename(src, dst string) bool {
	src, dst = filepath.Clean(src), filepath.Clean(dst)
	return src != dst && filepath.Dir(src) == filepath.Dir(dst) && strings.EqualFold(filepath.Base(src), filepath.Base(dst))
}

// The prefilled single-item path names the item itself, even when it exists.
// A different existing directory or a trailing slash explicitly selects a container.
func transferIntoDirectory(input, destination, suggested string, count int, isDir bool) bool {
	return isDir && (count > 1 || destination != suggested || strings.HasSuffix(input, "/"))
}
