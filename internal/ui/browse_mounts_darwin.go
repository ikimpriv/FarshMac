package ui

import (
	"farshmac/internal/fsops"
	"golang.org/x/sys/unix"
	"path/filepath"
)

func newLocalBrowse(root string, o *fsops.Operation) (*localBrowse, error) {
	// NOWAIT uses the kernel's mount table without contacting remote servers.
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}
	mounts := make([]unix.Statfs_t, n+32)
	n, err = unix.Getfsstat(mounts, unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}
	b := &localBrowse{o: o, boundaries: map[string]bool{}}
	root = filepath.Clean(root)
	for _, m := range mounts[:min(n, len(mounts))] {
		if m.Flags&unix.MNT_LOCAL != 0 {
			continue
		}
		mount := unix.ByteSliceToString(m.Mntonname[:])
		// An explicitly selected remote mount is a valid search root. Nested mounts
		// are boundaries; never cross them automatically from a parent search.
		if mount != "" && filepath.Clean(mount) != root {
			b.boundaries[filepath.Clean(mount)] = true
		}
	}
	return b, nil
}
