package ui

import (
	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFolderSizesSortAndSelectedF3(t *testing.T) {
	root := t.TempDir()
	for name, size := range map[string]int{"large": 17000, "small": 3, "empty": 0} {
		os.Mkdir(filepath.Join(root, name), 0700)
		if size > 0 {
			os.WriteFile(filepath.Join(root, name, "file"), make([]byte, size), 0600)
		}
	}
	os.WriteFile(filepath.Join(root, "small", ".hidden"), []byte("12345"), 0600)
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	p := &a.Panels[0]
	p.Sort = "Size"
	p.HideHidden = true
	selectEntry(t, p, "large")
	p.Marked = map[string]bool{"large": true, "small": true}
	a.folderSize()
	waitWork(t, a)
	if p.folderSizes["large"].Bytes != 17000 || p.folderSizes["small"].Bytes != 8 {
		t.Fatal(p.folderSizes)
	}
	if a.Doc != nil {
		t.Fatal("F3 opened report instead of storing sizes")
	}
	if p.Entries[p.Cursor].Name != "large" {
		t.Fatal("sort lost cursor")
	}
	a.calculateFolderSizes(true)
	waitWork(t, a)
	if len(p.folderSizes) != 3 {
		t.Fatal("all folders not calculated", p.folderSizes)
	}
	var names []string
	for _, e := range p.Entries {
		if e.Name != ".." {
			names = append(names, e.Name)
		}
	}
	if len(names) != 3 || names[0] != "empty" || names[1] != "small" || names[2] != "large" {
		t.Fatal(names)
	}
	if err = p.Load(root); err != nil {
		t.Fatal(err)
	}
	if p.entrySize(p.Entries[len(p.Entries)-1]) != 17000 {
		t.Fatal("sort reload lost sizes")
	}
	a.rereadPanel(0)
	if len(p.folderSizes) != 3 || !p.folderSizes["large"].Stale {
		t.Fatal("explicit refresh must retain sizes as outdated")
	}
}
func TestCancelledFolderSizeKeepsCompletedResults(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "empty"), 0700)
	a, _ := New(root, root)
	defer a.Close()
	a.calculateFolderSizes(true)
	// Complete the worker, then cancel before the UI applies it.
	result := <-a.task.done
	a.task.done <- result
	a.task.operation.Cancel()
	a.pollTask()
	if _, ok := a.Panels[0].folderSizes["empty"]; !ok {
		t.Fatal("completed result discarded on cancellation")
	}
}

func TestParallelSizesCancellationKeepsCompletedAndNoPerFileProgressIndex(t *testing.T) {
	o := fsops.NewOperation("sizes")
	// Keep the first result, then cancel on the next directory.
	walk := func(root string, o *fsops.Operation, visit fs.WalkDirFunc) error {
		if filepath.Base(root) == "stop" {
			o.Cancel()
			return o.Check()
		}
		for i := 0; i < 1000; i++ {
			if err := visit(filepath.Join(root, fmt.Sprint(i)), fs.FileInfoToDirEntry(fsops.ArchiveMember{Path: "file", Bytes: 7}), nil); err != nil {
				return err
			}
		}
		return nil
	}
	result, err := measureFolders("/example", []string{"completed", "stop", "never"}, o, walk, 1)
	if err != fsops.ErrCancelled || len(result) != 1 || result["completed"].Bytes != 7000 {
		t.Fatal(result, err)
	}
	if p := o.Snapshot(); p.TotalFiles != 1000 || p.TotalBytes != 7000 {
		t.Fatal("wrong aggregate progress", p)
	}
}

func TestFolderSizeHotkeyAndNavigationCache(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(fmt.Sprint(async), func(t *testing.T) {
			root := t.TempDir()
			for name, size := range map[string]int{"large": 100, "small": 10} {
				if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, name, "file"), make([]byte, size), 0600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := New(root, root)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			p := &a.Panels[0]
			p.Async = async
			p.Sort = "Size"
			p.Marked = map[string]bool{"small": true}
			a.key(Message{Key: "F3", Meta: true, Alt: true})
			waitWork(t, a)
			if len(p.folderSizes) != 2 || p.folderSizes["large"].Stale {
				t.Fatal("hotkey must calculate all folders", p.folderSizes)
			}
			load := func(path string) {
				t.Helper()
				if err := p.Load(path); err != nil {
					t.Fatal(err)
				}
				for p.load != nil {
					p.pollPanelLoad()
					time.Sleep(time.Millisecond)
				}
			}
			load(filepath.Join(root, "small"))
			load(root)
			if p.folderSizes["large"].Bytes != 100 || !p.folderSizes["large"].Stale {
				t.Fatal("return lost cache", p.folderSizes)
			}
			if p.Entries[len(p.Entries)-1].Name != "large" {
				t.Fatal("cached sort order", p.Entries)
			}
			if err := os.RemoveAll(filepath.Join(root, "small")); err != nil {
				t.Fatal(err)
			}
			load(root)
			if _, ok := p.folderSizes["small"]; ok {
				t.Fatal("deleted folder kept its size")
			}
			p.IgnoreDSStore = true
			load(root)
			if len(p.folderSizes) != 0 {
				t.Fatal("different measurement policy reused sizes")
			}
		})
	}
}

func TestFolderSizeCacheBoundedAndSourceIsolated(t *testing.T) {
	p := Panel{}
	for i := 0; i < folderSizeCacheDirectories+5; i++ {
		p.loadFolderSizes(fmt.Sprintf("/folder/%d", i))
		p.folderSizes = map[string]folderSizeResult{"child": {Bytes: int64(i)}}
	}
	if len(p.folderSizeHistory) != folderSizeCacheDirectories {
		t.Fatal(len(p.folderSizeHistory))
	}
	p.loadFolderSizes("/folder/0")
	if len(p.folderSizes) != 0 {
		t.Fatal("oldest cache not evicted")
	}
	p.folderSizes = map[string]folderSizeResult{"local": {Bytes: 7}}
	p.Remote = &remotefs.Config{Protocol: "sftp", Host: "one", Port: 22, User: "user"}
	p.loadFolderSizes("/folder/0")
	if len(p.folderSizes) != 0 {
		t.Fatal("local size reused on remote server")
	}
	p.folderSizes = map[string]folderSizeResult{"remote": {Bytes: 9}}
	p.Remote = &remotefs.Config{Protocol: "sftp", Host: "two", Port: 22, User: "user"}
	p.loadFolderSizes("/folder/0")
	if len(p.folderSizes) != 0 {
		t.Fatal("size reused on another server")
	}
	p.Remote = nil
	p.loadFolderSizes("/folder/0")
	if p.folderSizes["local"].Bytes != 7 {
		t.Fatal("local cache lost")
	}
	p.folderSizes = make(map[string]folderSizeResult)
	for i := 0; i <= folderSizeCacheEntries; i++ {
		p.folderSizes[fmt.Sprint(i)] = folderSizeResult{}
	}
	p.loadFolderSizes("/other")
	p.loadFolderSizes("/folder/0")
	if len(p.folderSizes) != 0 {
		t.Fatal("oversized snapshot retained")
	}
}

func TestFolderSizeCacheDoesNotReuseReopenedArchive(t *testing.T) {
	p := Panel{Archive: &fsops.ArchiveIndex{Path: "/example.zip"}}
	p.loadFolderSizes("/example.zip")
	p.folderSizes = map[string]folderSizeResult{"dir": {Bytes: 99}}
	p.Archive = nil
	p.loadFolderSizes("/")
	p.Archive = &fsops.ArchiveIndex{Path: "/example.zip"}
	p.loadFolderSizes("/example.zip")
	if len(p.folderSizes) != 0 {
		t.Fatal("reopened archive reused old contents")
	}
}
