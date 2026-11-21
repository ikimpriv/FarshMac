package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"farshmac/internal/aispaces"
	"farshmac/internal/fsops"
)

func aiEntryNames(p *Panel) string {
	var names []string
	for _, e := range p.Entries {
		if e.Name != ".." {
			names = append(names, e.Name)
		}
	}
	return strings.Join(names, ",")
}

func TestAISpacesPanelAttachEditAndCatalog(t *testing.T) {
	local := t.TempDir()
	project := filepath.Join(local, "project")
	os.MkdirAll(project, 0755)
	os.WriteFile(filepath.Join(project, "a.txt"), []byte("alpha\n"), 0644)
	store, err := aispaces.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CreateSpace("Space"); err != nil {
		t.Fatal(err)
	}
	if err = store.CreateEnv("Space", "Dev", []aispaces.Agent{{Name: "claude", Path: "/bin/echo"}}); err != nil {
		t.Fatal(err)
	}
	a, err := newApp(local, local, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ai := &a.Panels[1]
	ai.AI = &aiPanel{store}
	if err = ai.Load("/"); err != nil || aiEntryNames(ai) != "Space" {
		t.Fatalf("catalog root = %q, %v", aiEntryNames(ai), err)
	}
	if ai.can(capCopyIn) || ai.can(capEdit) || ai.endpoint().Opener != nil {
		t.Fatal("catalog offered filesystem operations")
	}
	if err = ai.Load("/Space"); err != nil || aiEntryNames(ai) != "Dev" {
		t.Fatalf("space = %q, %v", aiEntryNames(ai), err)
	}
	if err = ai.Load("/Space/Dev"); err != nil || aiEntryNames(ai) != "claude,CLAUDE.md" || !ai.aiAgent("claude") {
		t.Fatalf("env root = %q, %v", aiEntryNames(ai), err)
	}

	// F5 from the local panel attaches a reference instead of copying.
	a.Active = 0
	for i, e := range a.Panels[0].Entries {
		if e.Name == "project" {
			a.Panels[0].Cursor = i
		}
	}
	a.transferDialogTo(false, false)
	if a.Form == nil || a.Form.Title != "Attach to AI Env" {
		t.Fatalf("F5 into an Env did not offer to attach: %+v, %q", a.Form, a.Status)
	}
	a.Form.Select(0)
	if err = ai.Load("/Space/Dev"); err != nil || aiEntryNames(ai) != "project,claude,CLAUDE.md" {
		t.Fatalf("after attach = %q, %v", aiEntryNames(ai), err)
	}
	// F6 into an Env is refused: it would contradict reference semantics.
	a.Form = nil
	a.transferDialogTo(true, false)
	if a.Form != nil || !strings.Contains(a.Status, "disabled") {
		t.Fatalf("move into an Env was not refused: %q", a.Status)
	}

	if err = ai.Load("/Space/Dev/project"); err != nil || aiEntryNames(ai) != "a.txt" {
		t.Fatalf("reference content = %q, %v", aiEntryNames(ai), err)
	}
	source := ai.backend().DocumentSource(ai, "/Space/Dev/project/a.txt")
	o := fsops.NewOperation("test")
	defer o.Cancel()
	copy, info, err := source.Fetch(o, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(copy, []byte("edited\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = source.Store(o, copy, info, false); err != nil {
		t.Fatal(err)
	}
	// The editor saves straight into the referenced folder.
	if saved, _ := os.ReadFile(filepath.Join(project, "a.txt")); string(saved) != "edited\n" {
		t.Fatalf("real file = %q", saved)
	}
}
