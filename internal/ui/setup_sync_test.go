package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	os.MkdirAll(filepath.Dir(setupPath()), 0700)
}
func readSetup(t *testing.T) savedSetup {
	t.Helper()
	var s savedSetup
	b, err := os.ReadFile(setupPath())
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}
func groupWithTwoWindows(t *testing.T) (*WindowGroup, *App, *App) {
	t.Helper()
	root := t.TempDir()
	g := sessionGroup(t, root, root)
	g.Apps[1].LoadSetup()
	id, err := g.NewWindow(1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.CloseAll)
	return g, g.Apps[1], g.Apps[id]
}
func TestCorruptSetupIsPreservedAndReported(t *testing.T) {
	setupHome(t)
	os.WriteFile(setupPath(), []byte("{not json"), 0600)
	a := formTestApp(t)
	a.LoadSetup()
	if !strings.HasPrefix(a.Status, "setup.json is damaged; saved as setup.json.broken-") {
		t.Fatal(a.Status)
	}
	matches, _ := filepath.Glob(setupPath() + ".broken-*")
	if len(matches) != 1 {
		t.Fatal("damaged file not preserved")
	}
	if b, _ := os.ReadFile(matches[0]); string(b) != "{not json" {
		t.Fatal("damaged content changed")
	}
	if err := a.saveSetup(); err != nil || a.Form != nil {
		t.Fatal("save after preserving failed", err)
	}
	readSetup(t)
}
func TestEmptySetupIsTreatedAsMissing(t *testing.T) {
	setupHome(t)
	os.WriteFile(setupPath(), nil, 0600)
	a := formTestApp(t)
	a.Status = ""
	a.LoadSetup()
	matches, _ := filepath.Glob(setupPath() + ".broken-*")
	if a.Status != "" || len(matches) != 0 {
		t.Fatal("empty setup reported as damaged", a.Status)
	}
}
func TestSetupTypeErrorKeepsPartialSettings(t *testing.T) {
	setupHome(t)
	os.WriteFile(setupPath(), []byte(`{"Settings":{"FontSize":"x","ShowKeys":false,"SortGroups":"*.go"}}`), 0600)
	a := formTestApp(t)
	a.LoadSetup()
	if a.Settings.FontSize != 14 || a.Settings.ShowKeys || a.Settings.SortGroups != "*.go" {
		t.Fatal("partial settings lost", a.Settings.FontSize, a.Settings.ShowKeys)
	}
	if a.Status != "setup.json: Settings.FontSize has wrong type; default used" {
		t.Fatal(a.Status)
	}
}
func TestUnreadableSetupBlocksSilentOverwrite(t *testing.T) {
	setupHome(t)
	os.WriteFile(setupPath(), []byte(`{"Settings":{"FontSize":20}}`), 0600)
	os.Chmod(setupPath(), 0)
	t.Cleanup(func() { os.Chmod(setupPath(), 0600) })
	a := formTestApp(t)
	a.LoadSetup()
	if !strings.Contains(a.Status, "Save setup is disabled") {
		t.Fatal(a.Status)
	}
	a.saveSetup()
	if a.Form == nil || a.Form.Title != "Save setup" || !strings.Contains(a.Form.Message, "could not be read") {
		t.Fatal("no overwrite question")
	}
	chooseButton(t, a, "Cancel")
	os.Chmod(setupPath(), 0600)
	if s := readSetup(t); s.Settings.FontSize != 20 {
		t.Fatal("unreadable setup overwritten")
	}
	a.Settings.AutoSave = true
	if err := a.autoSaveSetup(); err != errSetupUnread {
		t.Fatal("auto-save overwrote unreadable setup", err)
	}
	a.saveSetup()
	chooseButton(t, a, "Overwrite")
	if s := readSetup(t); s.Settings.FontSize != 14 || a.Status != "Setup saved" {
		t.Fatal("overwrite did not save", a.Status)
	}
}
func TestSaveSetupDetectsExternalChange(t *testing.T) {
	setupHome(t)
	a := formTestApp(t)
	a.LoadSetup()
	if err := a.saveSetup(); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	os.WriteFile(setupPath(), []byte(`{"Settings":{"FontSize":22}}`), 0600)
	os.Chtimes(setupPath(), future, future)
	a.saveSetup()
	if a.Form == nil || !strings.Contains(a.Form.Message, "changed by another FarshMac instance") {
		t.Fatal("external change not detected")
	}
	chooseButton(t, a, "Overwrite")
	if s := readSetup(t); s.Settings.FontSize != 14 {
		t.Fatal("overwrite ignored")
	}
	if err := a.saveSetup(); err != nil || a.Form != nil {
		t.Fatal("own write treated as external", err)
	}
}
func TestBookmarksStayInTheirWindow(t *testing.T) {
	setupHome(t)
	g, a, b := groupWithTwoWindows(t)
	a.Panels[a.Active].Load(t.TempDir())
	g.Handle(1, `{"Type":"key","Key":"3","Code":"Digit3","Ctrl":true,"Shift":true}`)
	if a.Bookmarks[3].Path != a.Panels[a.Active].Path {
		t.Fatal("bookmark not saved", a.Status)
	}
	if _, ok := b.Bookmarks[3]; ok {
		t.Fatal("bookmark leaked into another window")
	}
	if _, err := os.Stat(setupPath()); err == nil {
		t.Fatal("bookmark written to setup")
	}
}
func TestSettingsPropagateAcrossWindows(t *testing.T) {
	setupHome(t)
	g, a, b := groupWithTwoWindows(t)
	a.settingsMenu("General settings")
	a.Form.Fields[1].Checked = !a.Settings.ShowKeys
	want := a.Form.Fields[1].Checked
	g.Handle(1, `{"Type":"key","Key":"Enter"}`)
	if a.Settings.ShowKeys != want || b.Settings.ShowKeys != want {
		t.Fatal("ShowKeys not propagated")
	}
	os.WriteFile(filepath.Join(b.Panels[0].Path, ".hidden-file"), nil, 0600)
	a.Settings.ShowHidden = !a.Settings.ShowHidden
	g.Poll(1)
	if b.Panels[0].HideHidden == !a.Settings.ShowHidden {
		// configurePanel ran
	} else {
		t.Fatal("panel settings not applied in other window")
	}
	g.Handle(g.Order[1], `{"Type":"key","Key":"F9","Shift":true}`)
	if s := readSetup(t); s.Settings.ShowKeys != want {
		t.Fatal("other window saved stale settings")
	}
}
func TestNewWindowHistoryIsNotShared(t *testing.T) {
	setupHome(t)
	g := sessionGroup(t, t.TempDir(), t.TempDir())
	t.Cleanup(g.CloseAll)
	g.Apps[1].history = []string{"make", "ls"}
	g.Apps[1].folderHistory = []string{"/tmp"}
	g.Apps[1].viewHistory = []viewRecord{{Path: "/tmp/x"}}
	id, _ := g.NewWindow(1)
	b := g.Apps[id]
	if strings.Join(b.history, ",") != "make,ls" || b.historyAt != 2 || len(b.folderHistory) != 1 || len(b.viewHistory) != 1 {
		t.Fatal("history not inherited", b.history)
	}
	b.history = append(b.history, "pwd")
	if len(g.Apps[1].history) != 2 {
		t.Fatal("history shared by reference")
	}
}
func TestSaveSetupMergesCommandHistoryAcrossWindows(t *testing.T) {
	setupHome(t)
	g, a, b := groupWithTwoWindows(t)
	g.primeSettings(a)
	a.Settings.AutoSave = true
	g.syncSettings(a)
	a.history = append(a.history, "only-in-a")
	b.history = append(b.history, "only-in-b")
	for _, id := range append([]int{}, g.Order...) {
		g.CloseWindow(id)
	}
	c := formTestApp(t)
	c.LoadSetup()
	joined := strings.Join(c.history, ",")
	if !strings.Contains(joined, "only-in-a") || !strings.Contains(joined, "only-in-b") {
		t.Fatal("history lost", joined)
	}
}
func TestAutoSaveFailureShownInRemainingWindow(t *testing.T) {
	setupHome(t)
	g, a, _ := groupWithTwoWindows(t)
	g.primeSettings(a)
	a.Settings.AutoSave = true
	g.syncSettings(a)
	dir := filepath.Dir(setupPath())
	os.RemoveAll(dir)
	os.WriteFile(dir, []byte("file in the way"), 0600)
	second := g.Order[1]
	g.CloseWindow(second)
	if !strings.HasPrefix(a.Status, "Setup not saved: ") {
		t.Fatal(a.Status)
	}
}
func TestAutoSaveFailureSurfacesOnQuit(t *testing.T) {
	setupHome(t)
	g, a, _ := groupWithTwoWindows(t)
	g.primeSettings(a)
	a.Settings.AutoSave = true
	g.syncSettings(a)
	dir := filepath.Dir(setupPath())
	os.RemoveAll(dir)
	os.WriteFile(dir, []byte("file in the way"), 0600)
	g.Focus(1)
	g.BeginQuit()
	if g.Terminating || a.Form == nil || a.Form.Title != "Settings were not saved" {
		t.Fatal("quit did not stop for setup error")
	}
	if u := g.Poll(1); u.QuitApplication {
		t.Fatal("quit before answer")
	}
	g.Handle(1, `{"Type":"key","Key":"ArrowRight"}`)
	g.Handle(1, `{"Type":"key","Key":"Enter"}`)
	if g.Terminating {
		t.Fatal("cancel did not abort quit")
	}
	g.BeginQuit()
	g.Handle(1, `{"Type":"key","Key":"Enter"}`)
	if !g.Terminating {
		t.Fatal("Quit anyway did not quit")
	}
}
func TestSecondInstanceDoesNotOverwriteSession(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config", "session.json")
	first := sessionGroup(t, root, root)
	if err := first.EnableSession(path, true); err != nil {
		t.Fatal(err)
	}
	first.SaveSession(true)
	before, _ := os.ReadFile(path)
	second := sessionGroup(t, root, root)
	err := second.EnableSession(path, true)
	if err != errSessionOwned || len(second.Order) != 1 {
		t.Fatal("second instance took the session", err)
	}
	second.NewWindow(1)
	second.SaveSession(true)
	second.CloseAll()
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("second instance wrote session")
	}
	first.CloseAll()
	third := sessionGroup(t, root, root)
	if err := third.EnableSession(path, true); err != nil {
		t.Fatal("lock not released", err)
	}
	third.CloseAll()
}
func TestCorruptSessionIsPreserved(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.json")
	os.WriteFile(path, []byte("{"), 0600)
	g := sessionGroup(t, root, root)
	t.Cleanup(g.CloseAll)
	err := g.EnableSession(path, true)
	matches, _ := filepath.Glob(path + ".broken-*")
	if err == nil || !strings.HasPrefix(err.Error(), "session.json is damaged") || len(matches) != 1 {
		t.Fatal(err)
	}
}
