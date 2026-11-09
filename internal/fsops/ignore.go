package fsops

import (
	"path/filepath"
	"strings"
)

// Ignored checks a single filesystem entry, without following symlinks.
func (o *Operation) Ignored(path string) bool {
	return o != nil && o.IgnoreDSStore && !o.moving && filepath.Base(path) == ".DS_Store"
}

// Archive paths are relative and may include explicitly stored directory trees.
func (o *Operation) ignoredArchivePath(path string) bool {
	if o == nil || !o.IgnoreDSStore {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(filepath.Clean(path)), "/") {
		if part == ".DS_Store" {
			return true
		}
	}
	return false
}
