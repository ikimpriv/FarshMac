package fsops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

type fileIdentity struct {
	device int32
	inode  uint64
}

// hardlinkTable is shared by all workers of one operation, so names of one
// file copied by different workers still become hard links of one copy.
type hardlinkTable struct {
	mu      sync.Mutex
	entries map[fileIdentity]*copiedLink
}
type copiedLink struct {
	done chan struct{} // closed once the first copy is published or failed
	path string
	info os.FileInfo
}

func hardlinkIdentity(info os.FileInfo) (fileIdentity, bool) {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() {
		return fileIdentity{}, false
	}
	return fileIdentity{s.Dev, s.Ino}, true
}

func (o *Operation) hardlinkTable() *hardlinkTable {
	root := o
	for root.parent != nil {
		root = root.parent
	}
	root.mu.Lock()
	defer root.mu.Unlock()
	if root.links == nil {
		root.links = &hardlinkTable{entries: map[fileIdentity]*copiedLink{}}
	}
	return root.links
}

// copyHardlink creates dst as a hard link to an earlier copy of the same
// source file in this operation. Otherwise, if the file has other names, it
// returns release, which the caller invokes once its own copy is final.
// A copy falls back to independent data when linking fails; a move asks first.
func (o *Operation) copyHardlink(src, dst string, info os.FileInfo) (linked bool, release func(bool), err error) {
	if o == nil {
		return false, nil, nil
	}
	key, ok := hardlinkIdentity(info)
	if !ok {
		return false, nil, nil
	}
	table := o.hardlinkTable()
	var path string
	var copied os.FileInfo
	for path == "" {
		table.mu.Lock()
		previous, known := table.entries[key]
		if !known {
			if info.Sys().(*syscall.Stat_t).Nlink < 2 {
				table.mu.Unlock()
				return false, nil, nil
			}
			entry := &copiedLink{done: make(chan struct{})}
			table.entries[key] = entry
			table.mu.Unlock()
			return false, func(ok bool) {
				table.mu.Lock()
				// A staged copy is recorded under its staging name and moved
				// along by relocateHardlinks once it is published.
				if published, err := os.Lstat(dst); ok && err == nil {
					entry.path, entry.info = dst, published
				} else {
					delete(table.entries, key) // Let the next name make the copy.
				}
				table.mu.Unlock()
				close(entry.done)
			}, nil
		}
		table.mu.Unlock()
		select {
		case <-previous.done:
		case <-o.Context().Done():
			return false, nil, ErrCancelled
		}
		if err = o.Check(); err != nil {
			return false, nil, err
		}
		table.mu.Lock()
		if table.entries[key] == previous {
			path, copied = previous.path, previous.info
		}
		table.mu.Unlock()
		// Otherwise the first copy failed: try to become the copy.
	}
	if current, err := os.Lstat(path); err != nil || !os.SameFile(current, copied) {
		return false, nil, nil
	}
	if err = os.Link(path, dst); err != nil {
		if !o.moving {
			return false, nil, nil // e.g. a file system without hard links.
		}
		problem := fmt.Errorf("cannot preserve hard-link relationship: %w", err)
		if o.MetadataFailure == nil {
			return false, nil, problem
		}
		return false, nil, o.MetadataFailure(src, dst, problem)
	}
	o.Begin(src, info.Size())
	o.Advance(info.Size())
	o.Complete(src)
	return true, nil, nil
}

// relocateHardlinks follows a staged copy renamed from old to path.
func (o *Operation) relocateHardlinks(old, path string) {
	if o == nil {
		return
	}
	table := o.hardlinkTable()
	table.mu.Lock()
	defer table.mu.Unlock()
	for _, link := range table.entries {
		if link.path == old || strings.HasPrefix(link.path, old+string(filepath.Separator)) {
			link.path = path + strings.TrimPrefix(link.path, old)
		}
	}
}
