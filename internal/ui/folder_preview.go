package ui

import (
	"errors"
	"farshmac/internal/podtoast"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"time"

	"farshmac/internal/fsops"
)

// directoryPreview scans metadata only, does not follow symlinks, and retains
// aggregate counters instead of collecting directory entries in memory.
func directoryPreview(root string, o *fsops.Operation, walk sizeWalker, local bool, update func(string)) string {
	var files, dirs, links, unreadable, bytes, allocated int64
	allocatedKnown := local
	format := func(state string) string {
		text := fmt.Sprintf("%s\nDirectory contents (recursive)\n\nFolders:        %d\nFiles:          %d\nTotal size:     %s (%d bytes)", root, dirs, files, humanSize(bytes), bytes)
		if allocatedKnown {
			text += fmt.Sprintf("\nSize on disk:   %s (%d bytes)", humanSize(allocated), allocated)
		}
		if links > 0 {
			text += fmt.Sprintf("\nSymbolic links: %d (not followed)", links)
		}
		if unreadable > 0 {
			text += fmt.Sprintf("\nUnreadable:     %d", unreadable)
		}
		return text + "\n\n" + state
	}
	last := time.Now()
	err := walk(root, o, func(path string, d fs.DirEntry, err error) error {
		if e := o.Check(); e != nil {
			return e
		}
		if err != nil {
			unreadable++
			return nil
		}
		if path != root {
			if d.IsDir() {
				dirs++
			} else {
				info, e := d.Info()
				if e != nil {
					unreadable++
				} else {
					files++
					bytes += info.Size()
					if info.Mode()&os.ModeSymlink != 0 {
						links++
					}
					if st, ok := info.Sys().(*syscall.Stat_t); ok && local {
						allocated += st.Blocks * 512
					} else {
						allocatedKnown = false
					}
				}
			}
		}
		if update != nil && time.Since(last) > 200*time.Millisecond {
			update(format("Calculating… Esc cancels"))
			last = time.Now()
		}
		return o.Check()
	})
	state := "Complete"
	if err != nil {
		state = "Incomplete: " + err.Error()
	} else if unreadable > 0 {
		state = "Incomplete: some entries could not be read"
	}
	return format(state)
}

// updateFolderPreview returns false for object catalogs, which retain their
// Kubernetes/Docker summaries rather than treating objects as directories.
func (a *App) updateFolderPreview(p *Panel) bool {
	if len(p.Entries) == 0 || !p.Entries[p.Cursor].Dir {
		return false
	}
	endpoint := p.endpoint()
	if (p.Kube != nil || p.Docker != nil || p.AI != nil) && endpoint.Opener == nil {
		return false
	}
	root := p.Current()
	if p.Entries[p.Cursor].Name == ".." {
		root = p.Path
	}
	key := p.label() + "/folder-preview/" + root
	if a.previewPending {
		if key != a.previewKey && a.previewOperation != nil {
			a.previewOperation.Cancel()
			a.previewKey = ""
		}
		return true
	}
	if key == a.previewKey {
		return true
	}
	a.previewKey = key
	a.previewPath = root
	a.previewImage = ""
	a.previewOffset = 0
	a.previewPending = true
	a.previewText = root + "\nCalculating directory contents… Esc cancels"
	o := fsops.NewOperation("Directory Quick view")
	o.IgnoreDSStore = a.Settings.IgnoreDSStore
	a.previewOperation = o
	archive := p.Archive
	go func() {
		walk := fsops.WalkDir
		local := endpoint.Opener == nil && archive == nil
		var result string
		if archive != nil {
			walk = func(root string, o *fsops.Operation, visit fs.WalkDirFunc) error {
				return walkArchiveSizes(archive, root, o, visit)
			}
		} else if endpoint.Opener != nil {
			client, err := endpoint.Opener.Open(o.Context())
			if err != nil {
				result = root + "\n" + err.Error()
			} else {
				defer client.Close()
				if scanner, ok := client.(directorySummarizer); ok {
					summary, e := scanner.DirectorySummary(root, o.IgnoreDSStore, func(s podtoast.DirectorySummary) error {
						partial := agentDirectoryText(root, s, nil)
						select {
						case a.jobs <- func() {
							if a.previewOperation == o && a.previewKey == key && o.Check() == nil {
								a.previewText = partial
								a.dirty = true
							}
						}:
						default:
						}
						return o.Check()
					})
					if !errors.Is(e, podtoast.ErrDirectorySummaryUnsupported) {
						result = agentDirectoryText(root, summary, e)
					}
				}

				walk = func(root string, o *fsops.Operation, visit fs.WalkDirFunc) error {
					return walkRemoteSizes(client, root, o, visit, 0, nil)
				}
			}
		}
		if result == "" {
			result = directoryPreview(root, o, walk, local, func(partial string) {
				select {
				case a.jobs <- func() {
					if a.previewOperation == o && a.previewKey == key && o.Check() == nil {
						a.previewText = partial
						a.dirty = true
					}
				}:
				default:
				}
			})
		}
		a.jobs <- func() {
			if a.previewOperation != o {
				return
			}
			if a.previewOperation == o && a.previewKey == key && o.Check() == nil {
				a.previewText = result
			}
			a.previewPending = false
			a.previewOperation = nil
			a.dirty = true
		}
	}()
	return true
}
