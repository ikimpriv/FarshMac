package ui

// Historical directory snapshots are bounded independently of the current panel.
// Maps are immutable after publication, including when shared with a read worker.
const folderSizeCacheDirectories = 128
const folderSizeCacheEntries = 16384
const folderSizeCacheBytes = 2 << 20

type folderSizeKey struct {
	Path, Source  string
	IgnoreDSStore bool
}
type folderSizeSnapshot struct {
	Key   folderSizeKey
	Sizes map[string]folderSizeResult
	Bytes int
}

func (p *Panel) sizeKey(path string) folderSizeKey {
	if p.sizeArchive != p.Archive {
		p.sizeArchive = p.Archive
		p.sizeArchiveGeneration++
	}
	source := p.backend().SizeSource(p)
	return folderSizeKey{path, source, p.IgnoreDSStore}
}

func (p *Panel) loadFolderSizes(path string) {
	key := p.sizeKey(path)
	if key != p.folderSizeKey {
		// Copy the slice so async rollback snapshots never share mutable storage.
		history := make([]folderSizeSnapshot, 0, len(p.folderSizeHistory)+1)
		var restored map[string]folderSizeResult
		for _, item := range p.folderSizeHistory {
			if item.Key == key {
				restored = item.Sizes
				continue
			}
			if item.Key != p.folderSizeKey {
				history = append(history, item)
			}
		}
		bytes := len(p.folderSizeKey.Path) + len(p.folderSizeKey.Source)
		for name := range p.folderSizes {
			bytes += len(name) + 64
		}
		if p.folderSizeKey.Path != "" && len(p.folderSizes) > 0 && len(p.folderSizes) <= folderSizeCacheEntries && bytes <= folderSizeCacheBytes {
			history = append(history, folderSizeSnapshot{p.folderSizeKey, p.folderSizes, bytes})
		}
		entries, totalBytes := 0, 0
		for _, item := range history {
			entries += len(item.Sizes)
			totalBytes += item.Bytes
		}
		for len(history) > 0 && (len(history) > folderSizeCacheDirectories || entries > folderSizeCacheEntries || totalBytes > folderSizeCacheBytes) {
			entries -= len(history[0].Sizes)
			totalBytes -= history[0].Bytes
			history[0] = folderSizeSnapshot{}
			history = history[1:]
		}
		p.folderSizeHistory, p.folderSizes, p.folderSizeKey = history, restored, key
	}
	if len(p.folderSizes) > 0 {
		stale := make(map[string]folderSizeResult, len(p.folderSizes))
		for name, r := range p.folderSizes {
			r.Stale = true
			stale[name] = r
		}
		p.folderSizes = stale
	}
}

func (p *Panel) pruneFolderSizes() {
	if len(p.folderSizes) == 0 {
		return
	}
	kept := make(map[string]folderSizeResult)
	for _, e := range p.Entries {
		if r, ok := p.folderSizes[e.Name]; e.Dir && ok {
			kept[e.Name] = r
		}
	}
	p.folderSizes = kept
}
