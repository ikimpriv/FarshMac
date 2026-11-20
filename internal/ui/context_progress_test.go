package ui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"farshmac/internal/fsops"
)

func TestContextMutationProgressDecisions(t *testing.T) {
	for _, action := range []string{"Retry", "Skip", "Skip all", "Cancel"} {
		t.Run(action, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			dir := t.TempDir()
			names := []string{"first", "second", "third", "fourth"}
			for _, n := range names {
				if err := os.WriteFile(filepath.Join(dir, n), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := New(dir, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			p := &a.Panels[0]
			p.Marked = map[string]bool{"first": true, "second": true, "third": true, "fourth": true}
			calls := map[string]int{}
			var jobs []contextJob
			for _, name := range names {
				jobs = append(jobs, contextJob{name, func(o *fsops.Operation) ([]byte, error) {
					calls[name]++
					if name == "second" && (action != "Retry" || calls[name] == 1) || name == "third" && action == "Skip all" {
						return nil, errors.New("permission denied")
					}
					return nil, nil
				}})
			}
			a.contextBatch(p, "Restart", jobs, true)
			chooseOperation(t, a, "Apply")
			waitOperationQuestion(t, a, "Error")
			snapshot := a.Operation.Snapshot()
			if snapshot.TotalFiles != 4 || snapshot.Files != 1 || snapshot.Scanning {
				t.Fatal(snapshot)
			}
			chooseOperation(t, a, action)
			waitWork(t, a)
			if a.Doc != nil || a.Form != nil || a.Operation != nil || a.Busy {
				t.Fatal("batch did not close cleanly")
			}
			if p.Marked["first"] {
				t.Fatal("successful item remains marked")
			}
			switch action {
			case "Retry":
				if calls["second"] != 2 || len(p.Marked) != 0 {
					t.Fatal(calls, p.Marked)
				}
			case "Skip":
				if !p.Marked["second"] || len(p.Marked) != 1 || calls["fourth"] != 1 {
					t.Fatal(calls, p.Marked)
				}
			case "Skip all":
				if !p.Marked["second"] || !p.Marked["third"] || len(p.Marked) != 2 || calls["fourth"] != 1 {
					t.Fatal(calls, p.Marked)
				}
			case "Cancel":
				if calls["third"] != 0 || calls["fourth"] != 0 || len(p.Marked) != 3 {
					t.Fatal(calls, p.Marked)
				}
			}
		})
	}
}
