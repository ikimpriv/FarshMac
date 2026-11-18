package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"farshmac/internal/kube"
)

func TestKubeMasterDetailsSelectionAndLargeLists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, e := New(t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	var sources []kube.ConfigSource
	for i := 0; i < 200; i++ {
		s := kube.ConfigSource{Path: fmt.Sprintf("/configs/cluster-%03d.yaml", i), Current: "context-050"}
		for j := 0; j < 200; j++ {
			s.Contexts = append(s.Contexts, fmt.Sprintf("context-%03d", j))
		}
		sources = append(sources, s)
	}
	a.chooseKubeContext(0, kube.Profile{}, sources, nil)
	b := a.Form.ConfigBrowser
	if b.detail != 50 || b.master != 0 {
		t.Fatal(b.master, b.detail)
	}
	selectedFile, selectedContext := -1, -1
	b.selectContext = func(f, c int) { selectedFile, selectedContext = f, c }
	for _, size := range [][2]int{{120, 40}, {80, 24}, {48, 16}} {
		a.Cols, a.Rows = size[0], size[1]
		a.formKey(Message{Key: "End"})
		frame := a.Draw().ANSI()
		if len(frame) > 100000 {
			t.Fatal("unbounded modal frame")
		}
		if b.master != 199 {
			t.Fatal(b.master)
		}
	}
	a.formKey(Message{Key: "Tab"})
	a.formKey(Message{Key: "End"})
	a.Draw()
	if b.detail != 199 || b.scroll[1] == 0 {
		t.Fatal("details did not scroll", b.detail, b.scroll)
	}
	a.formKey(Message{Key: "Enter"})
	if a.Form != nil || selectedFile != 199 || selectedContext != 199 {
		t.Fatal("wrong context selected", selectedFile, selectedContext)
	}
}
func TestKubeMasterDetailsMouseAndCancel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, e := New(t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	a.Cols, a.Rows = 120, 40
	sources := []kube.ConfigSource{{Path: "/a", Contexts: []string{"one"}}, {Path: "/b", Contexts: []string{"two", "three"}, Current: "three"}}
	a.chooseKubeContext(0, kube.Profile{}, sources, nil)
	r := a.kubeBrowserRect()
	a.formClick(Message{X: r.X + 5, Y: r.Y + 5})
	b := a.Form.ConfigBrowser
	if b.master != 1 || b.detail != 1 {
		t.Fatal("master did not update details")
	}
	a.formClick(Message{X: r.X + r.W/2 + 3, Y: r.Y + 4})
	if b.column != 1 || b.detail != 0 {
		t.Fatal("detail click")
	}
	a.scrollMouse(Message{X: r.X + r.W/2 + 3, Y: r.Y + 4, Delta: 1})
	if b.column != 1 || b.detail != 1 {
		t.Fatal("wheel did not scroll hovered details")
	}

	if text := a.Draw().ANSI(); !strings.Contains(text, "Contexts") {
		t.Fatal("missing details heading")
	}
	a.formKey(Message{Key: "Escape"})
	if a.Form != nil {
		t.Fatal("Esc did not close")
	}
}
func TestSSHWizardPreferencesExcludeSecrets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := kube.Profile{SSHHost: "server", SSHUser: "user", SSHAuth: "password", SSHSecret: "never-save-this"}
	if err := saveSSHPreference(p); err != nil {
		t.Fatal(err)
	}
	b, e := os.ReadFile(kubeSSHPreferencesPath())
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), p.SSHSecret) {
		t.Fatal("secret persisted")
	}
	got := loadSSHPreference(kube.Profile{SSHHost: "server", SSHUser: "user"})
	if got.SSHAuth != "password" || got.SSHSecret != "" {
		t.Fatal("wrong saved method")
	}
	a, e := New(t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	a.kubeSSHServerForm(0, p)
	if len(a.Form.Fields) != 2 {
		t.Fatal("too many initial fields")
	}
	a.kubeSSHAuthFailed(0, p, fmt.Errorf("Permission denied (publickey,password)"))
	if a.Form == nil || len(a.Form.Choice) == 0 || a.Form.Choice[1] != "Password…" {
		t.Fatal("no authentication fallback")
	}
	a.kubeSSHAuthFailed(0, p, fmt.Errorf("REMOTE HOST IDENTIFICATION HAS CHANGED; Host key verification failed"))
	if a.Form.Title != "SSH host key changed" {
		t.Fatal("changed key offered trust")
	}
	a.kubeSSHAuthFailed(0, p, fmt.Errorf("REVOKED HOST KEY DETECTED; Host key verification failed"))
	if a.Form.Title != "SSH host key changed" {
		t.Fatal("revoked key offered trust")
	}
}
