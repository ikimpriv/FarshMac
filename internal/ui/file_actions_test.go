package ui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"farshmac/internal/fsops"
)

func TestSelectionReceiptPreservesNewSelectionAndOtherLocation(t *testing.T) {
	p := Panel{Path: "/first", Marked: map[string]bool{"one": true}}
	receipt := p.selectionReceipt()
	p.Marked["two"] = true
	receipt.complete()
	if p.Marked["one"] || !p.Marked["two"] || !p.RestoreMarked["one"] {
		t.Fatal("wrong selection after successful action")
	}
	p.Marked = map[string]bool{"one": true}
	receipt = p.selectionReceipt()
	p.Path = "/other"
	receipt.complete()
	if !p.Marked["one"] {
		t.Fatal("completion changed another folder")
	}
}

func TestWorkProgressClearsSelectionOnlyOnSuccess(t *testing.T) {
	for _, fail := range []bool{false, true} {
		a, err := New(t.TempDir(), t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		p := &a.Panels[a.Active]
		if err := os.WriteFile(filepath.Join(p.Path, "selected"), []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := p.Load(p.Path); err != nil {
			t.Fatal(err)
		}
		p.Marked = map[string]bool{"selected": true}
		a.workProgress("Test", func(o *fsops.Operation) error {
			if fail {
				return errors.New("failed")
			}
			return nil
		})
		waitWork(t, a)
		if p.Marked["selected"] != fail {
			t.Fatalf("fail=%v: selection=%v", fail, p.Marked)
		}
		a.Close()
	}
}

func TestNativeFileActionKeepsStagedFilesUntilWindowClose(t *testing.T) {
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	a.fileActions = map[int64]pendingFileAction{1: {temp: dir}}
	a.finishNativeFileAction(Message{Request: 1, Count: 1})
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("external app lost its staged files")
	}
	a.Close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("staged files not cleaned at close")
	}
}
