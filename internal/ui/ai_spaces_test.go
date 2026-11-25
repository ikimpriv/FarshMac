package ui

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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

func TestAIContextGoToRealLocation(t *testing.T) {
	local := t.TempDir()
	project := filepath.Join(local, "project")
	os.MkdirAll(project, 0755)
	os.WriteFile(filepath.Join(project, "a.txt"), []byte("alpha"), 0644)
	os.WriteFile(filepath.Join(project, "b.txt"), []byte("beta"), 0644)
	store, err := aispaces.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.CreateSpace("Space")
	store.CreateEnv("Space", "Dev", []aispaces.Agent{{Name: "codex", Path: "/bin/echo"}})
	if err = store.Attach("Space", "Dev", "/", []string{project}, true); err != nil {
		t.Fatal(err)
	}
	store.Attach("Space", "Dev", "/synthetic/nested", []string{project}, true)
	a, err := newApp(local, local, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	p := &a.Panels[1]
	find := func() func() {
		for _, item := range a.aiContextItems(p) {
			if item.Label == "Go to real location" {
				return item.Run
			}
		}
		return nil
	}
	load := func(dir, name string) {
		p.Async = false
		p.AI = &aiPanel{store}
		if err := p.Load(dir); err != nil {
			t.Fatal(err)
		}
		p.Marked = map[string]bool{}
		p.placeCursor(name)
	}
	for _, async := range []bool{false, true} {
		for _, name := range []string{"project", "a.txt"} {
			dir := "/Space/Dev"
			if name == "a.txt" {
				dir += "/project"
			}
			load(dir, name)
			a.Active = 0 // The action must navigate the menu's panel.
			p.Async = async
			run := find()
			if run == nil {
				t.Fatal("missing real-location action", name)
			}
			run()
			deadline := time.Now().Add(3 * time.Second)
			for p.load != nil && time.Now().Before(deadline) {
				p.pollPanelLoad()
				time.Sleep(time.Millisecond)
			}
			if p.load != nil || p.AI != nil || p.Path != project || a.Active != 1 || a.Panels[0].Path != local {
				t.Fatalf("wrong navigation: path=%s AI=%v async=%v", p.Path, p.AI, async)
			}
			if name == "a.txt" && filepath.Base(p.Current()) != name {
				t.Fatal("real file was not selected", p.Current())
			}
		}
	}
	for _, name := range []string{"codex", "synthetic", ".."} {
		load("/Space/Dev", name)
		if find() != nil {
			t.Fatal("action offered for virtual entry", name)
		}
	}
	load("/Space/Dev/project", "a.txt")
	p.Marked = map[string]bool{"a.txt": true, "b.txt": true}
	if find() != nil {
		t.Fatal("ambiguous multi-selection navigation")
	}
	p.Marked = map[string]bool{}
	run := find()
	os.Remove(filepath.Join(project, "a.txt"))
	run()
	if p.AI == nil || p.Path != "/Space/Dev/project" {
		t.Fatal("vanished file discarded the AI panel")
	}
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

func TestAINewEnvChoosesSingleAgentAndRole(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"codex", "claude"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	store, err := aispaces.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.CreateSpace("Space")
	local := t.TempDir()
	a, err := newApp(local, local, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	p := &a.Panels[0]
	p.AI = &aiPanel{store}
	if err = p.Load("/Space"); err != nil {
		t.Fatal(err)
	}
	a.key(Message{Key: "F7"})
	f := a.Form
	if f == nil || len(f.Fields) != 4 || !f.Fields[1].Combo || !f.Fields[2].Combo {
		t.Fatalf("creation form %+v", f)
	}
	f.Fields[0].Value = "Build"
	f.Fields[1].Value = "codex"
	f.Focus = 2
	a.formKey(Message{Key: "Enter"})
	for i, label := range a.Form.Choice {
		if label == "Coder" {
			a.Form.Focus = i
		}
	}
	a.formKey(Message{Key: "Enter"})
	f.Focus = len(f.Fields)
	a.formKey(Message{Key: "Enter"})
	agents := store.Agents("Space", "Build")
	if len(agents) != 1 || agents[0].Name != "codex" {
		t.Fatalf("agents: %+v", agents)
	}
	fs, err := store.FS("Space", "Build")
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if _, err = fs.Lstat("/AGENTS.md"); err != nil {
		t.Fatal("missing role file", err)
	}
	file, err := fs.Open("/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	file.Close()
	if err != nil || !strings.Contains(string(data), "# Coder") {
		t.Fatalf("wrong role prompt: %q, %v", data, err)
	}
	found := false
	for _, section := range a.menuSections() {
		if section.Name == "Options" {
			for _, item := range section.Items {
				if item.Label == "AI Spaces…" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("AI settings not in Options")
	}
}

func TestAICustomRoleCreationAndCancellation(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	store, err := aispaces.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.CreateSpace("Space")
	local := t.TempDir()
	a, err := newApp(local, local, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Panels[0].AI = &aiPanel{store}
	if err = a.Panels[0].Load("/Space"); err != nil {
		t.Fatal(err)
	}
	a.createAIEnv(store, "Space", 0)
	parent := a.Form
	parent.Fields[0].Value = "Security"
	parent.Fields[1].Value = "codex"
	chooseNew := func() {
		t.Helper()
		parent.Focus = 2
		a.formKey(Message{Key: "Enter"})
		if a.Form == parent || len(a.Form.Choice) != len(parent.Fields[2].Options) {
			t.Fatal("role list did not open")
		}
		a.Form.Focus = len(a.Form.Choice) - 1
		a.formKey(Message{Key: "Enter"})
		if a.Form.Title != "New role" {
			t.Fatal("custom role form did not open")
		}
	}
	chooseNew()
	a.formKey(Message{Key: "Escape"})
	if a.Form != parent || parent.Fields[0].Value != "Security" || parent.Fields[1].Value != "codex" {
		t.Fatal("cancelling lost Env settings")
	}
	chooseNew()
	a.Form.Fields[0].Value = "Security reviewer"
	a.formKey(Message{Key: "Enter"})
	if a.Doc == nil || !a.Doc.Edit || a.Form != nil {
		t.Fatal("prompt editor did not open")
	}
	draftDir := a.Doc.TempDir
	a.closeDocument() // Escape without saving must not create a role.
	roles, _ := store.Roles()
	if a.Form != parent || len(roles) != len(aispaces.DefaultRoles()) {
		t.Fatal("cancelled prompt created a role")
	}
	if _, err = os.Stat(draftDir); !os.IsNotExist(err) {
		t.Fatal("draft was not cleaned up")
	}
	chooseNew()
	a.Form.Fields[0].Value = "Discarded role"
	a.formKey(Message{Key: "Enter"})
	a.Doc.Insert("Do not save this draft")
	a.Doc.modified()
	a.closeDocument()
	if a.Form == nil || a.Form.Title != "Save changes" {
		t.Fatal("dirty prompt did not use the editor's save/discard dialog")
	}
	a.Form.Focus = 1 // Discard.
	a.formKey(Message{Key: "Enter"})
	roles, _ = store.Roles()
	if a.Form != parent || len(roles) != len(aispaces.DefaultRoles()) {
		t.Fatal("discarded draft created a role or lost the Env form")
	}
	chooseNew()
	a.Form.Fields[0].Value = "Security reviewer"
	a.formKey(Message{Key: "Enter"})
	prompt := "# Security reviewer\n\nReview permissions.\nReport findings with evidence.\n"
	a.Doc.Insert(prompt)
	a.Doc.modified()
	a.key(Message{Key: "F2"})
	if a.Doc.Dirty || a.Form != nil {
		t.Fatal("saving prompt failed", a.Status)
	}
	a.closeDocument()
	if a.Form != parent || parent.Fields[2].Value != "Security reviewer" {
		t.Fatal("new role was not selected in the Env form")
	}
	// Choice-only fields cannot be replaced by a pasted or typed role name.
	parent.Focus = 2
	a.formPaste("invented role")
	a.formKey(Message{Key: "x"})
	if parent.Fields[2].Value != "Security reviewer" {
		t.Fatal("role field accepted free text")
	}
	parent.Focus = len(parent.Fields)
	a.formKey(Message{Key: "Enter"})
	fs, err := store.FS("Space", "Security")
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	file, err := fs.Open("/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	file.Close()
	if err != nil || string(data) != prompt {
		t.Fatalf("custom prompt not copied: %q, %v", data, err)
	}
	a.createAIEnv(store, "Space", 0)
	if !slices.Contains(a.Form.Fields[2].Options, "Security reviewer") {
		t.Fatal("custom role absent from future creation")
	}
}
