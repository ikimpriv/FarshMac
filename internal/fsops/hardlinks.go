package fsops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type fileIdentity struct {
	device int32
	inode  uint64
}
type copiedLink struct {
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

func (o *Operation) copyHardlink(src, dst string, info os.FileInfo) (bool, error) {
	if o == nil || !o.moving {
		return false, nil
	}
	key, ok := hardlinkIdentity(info)
	if !ok {
		return false, nil
	}
	previous, ok := o.hardlinks[key]
	if !ok {
		return false, nil
	}
	current, err := os.Lstat(previous.path)
	if err != nil || !os.SameFile(current, previous.info) {
		return false, nil
	}
	if err = os.Link(previous.path, dst); err != nil {
		problem := fmt.Errorf("cannot preserve hard-link relationship: %w", err)
		if o.MetadataFailure == nil {
			return false, problem
		}
		return false, o.MetadataFailure(src, dst, problem)
	}
	o.Begin(src, info.Size())
	o.Advance(info.Size())
	o.Complete(src)
	return true, nil
}

func (o *Operation) rememberHardlink(dst string, source os.FileInfo) {
	if o == nil || !o.moving {
		return
	}
	key, ok := hardlinkIdentity(source)
	if !ok {
		return
	}
	if source.Sys().(*syscall.Stat_t).Nlink < 2 {
		if _, known := o.hardlinks[key]; !known {
			return
		}
	}
	info, err := os.Lstat(dst)
	if err != nil {
		return
	}
	if o.hardlinks == nil {
		o.hardlinks = make(map[fileIdentity]copiedLink)
	}
	o.hardlinks[key] = copiedLink{dst, info}
}

func (o *Operation) relocateHardlinks(old, path string) {
	if o == nil {
		return
	}
	for key, link := range o.hardlinks {
		if link.path == old || strings.HasPrefix(link.path, old+string(filepath.Separator)) {
			link.path = path + strings.TrimPrefix(link.path, old)
			o.hardlinks[key] = link
		}
	}
}
