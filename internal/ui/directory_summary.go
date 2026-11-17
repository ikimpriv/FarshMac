package ui

import (
	"errors"
	"farshmac/internal/fsops"
	"farshmac/internal/podtoast"
	"farshmac/internal/remotefs"
	"fmt"
	"path/filepath"
)

type directorySummarizer interface {
	DirectorySummary(string, bool, func(podtoast.DirectorySummary) error) (podtoast.DirectorySummary, error)
}

func (f dockerAgentFS) DirectorySummary(p string, ignore bool, update func(podtoast.DirectorySummary) error) (podtoast.DirectorySummary, error) {
	if scanner, ok := f.FS.(directorySummarizer); ok {
		return scanner.DirectorySummary(p, ignore, update)
	}
	return podtoast.DirectorySummary{}, podtoast.ErrDirectorySummaryUnsupported
}

func agentDirectoryText(root string, s podtoast.DirectorySummary, err error) string {
	state := "Calculating… Esc cancels"
	if s.Done {
		state = "Complete"
		if s.Unreadable > 0 {
			state = "Incomplete: some entries could not be read"
		}
	}
	if err != nil {
		state = "Incomplete: " + err.Error()
	}
	text := fmt.Sprintf("%s\nDirectory contents (recursive)\n\nFolders:        %d\nFiles:          %d\nTotal size:     %s (%d bytes)\nSize on disk:   %s (%d bytes)", root, s.Directories, s.Files, humanSize(s.Bytes), s.Bytes, humanSize(s.Allocated), s.Allocated)
	if s.Links > 0 {
		text += fmt.Sprintf("\nSymbolic links: %d (not followed)", s.Links)
	}
	if s.Unreadable > 0 {
		text += fmt.Sprintf("\nUnreadable:     %d", s.Unreadable)
	}
	return text + "\n\n" + state
}

func measureRemoteFolders(base string, names []string, o *fsops.Operation, f remotefs.FS, walk sizeWalker) (map[string]folderSizeResult, error) {
	results := map[string]folderSizeResult{}
	for _, name := range names {
		if err := o.Check(); err != nil {
			return results, err
		}
		root := filepath.Join(base, name)
		if name == ".." {
			root = base
		}
		if scanner, ok := f.(directorySummarizer); ok {
			var previous podtoast.DirectorySummary
			summary, err := scanner.DirectorySummary(root, o.IgnoreDSStore, func(s podtoast.DirectorySummary) error {
				o.ScanProgress(root, s.Files+s.Directories-previous.Files-previous.Directories, s.Bytes-previous.Bytes)
				previous = s
				return o.Check()
			})
			if !errors.Is(err, podtoast.ErrDirectorySummaryUnsupported) {
				if err != nil {
					return results, err
				}
				results[name] = folderSizeResult{Bytes: summary.Bytes, Files: int(summary.Files), Dirs: int(summary.Directories), Unreadable: int(summary.Unreadable)}
				continue
			}
		}
		size, err := measureFolder(root, o, walk)
		if err != nil {
			return results, err
		}
		results[name] = size
	}
	return results, nil
}
