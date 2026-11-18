package ui

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"farshmac/internal/kube"
)

const pastedConfigFixture = `apiVersion: v1
kind: Config
clusters:
- name: fixture
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: first
  context:
    cluster: fixture
    user: user
- name: second
  context:
    cluster: fixture
    user: user
current-context: second
users:
- name: user
  user:
    token: synthetic-secret-value
`

func TestPastedKubeconfigPrivateLibraryAndMultipleContexts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	entry, e := savePastedKubeconfig("Lab clusters", pastedConfigFixture, "")
	if e != nil {
		t.Fatal(e)
	}
	st, e := os.Stat(entry.path())
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal(st, e)
	}
	catalog, e := os.ReadFile(kubeConfigLibraryPath())
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(catalog), "synthetic-secret-value") || !strings.Contains(string(catalog), "Lab clusters") {
		t.Fatal("catalog exposed credentials or lost name")
	}
	p := kube.Profile{Kubeconfig: entry.path(), StoredConfig: true}
	source, e := kube.ReadConfigSource(context.Background(), p)
	if e != nil || len(source.Contexts) != 2 || source.Current != "second" {
		t.Fatal(source, e)
	}
	a, e := New(t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	p.Context = "first"
	if e = a.saveKubeProfile(p); e != nil {
		t.Fatal(e)
	}
	p.Context = "second"
	p.SSHHost = "fixture-ssh"
	if e = a.saveKubeProfile(p); e != nil {
		t.Fatal(e)
	}
	if len(a.Settings.KubeProfiles) != 2 {
		t.Fatal("contexts replaced each other")
	}
	profiles, _ := os.ReadFile(kubeProfilesPath())
	if strings.Contains(string(profiles), "synthetic-secret-value") {
		t.Fatal("credentials in profile list")
	}
	if e = deleteStoredKubeconfig(entry); e == nil {
		t.Fatal("deleted config still used by profiles")
	}
	if _, e = savePastedKubeconfig("Lab clusters", pastedConfigFixture, ""); e == nil {
		t.Fatal("duplicate name accepted")
	}
	renamed, e := savePastedKubeconfig("Renamed lab", pastedConfigFixture, entry.ID)
	if e != nil || renamed.ID != entry.ID {
		t.Fatal(renamed, e)
	}
	var got []storedKubeconfig
	b, _ := os.ReadFile(kubeConfigLibraryPath())
	json.Unmarshal(b, &got)
	if len(got) != 1 || got[0].Name != "Renamed lab" {
		t.Fatal(got)
	}
}
func TestKubeconfigPastePreservesYAMLAndHidesCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, e := New(t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	a.pasteKubeconfigForm(0, kube.Profile{}, storedKubeconfig{})
	a.Form.Fields[0].Value = "Pasted lab"
	a.Form.Focus = 1
	a.formPaste(strings.ReplaceAll(pastedConfigFixture, "\n", "\r\n"))
	if a.Form.Fields[1].Value != pastedConfigFixture {
		t.Fatal("paste flattened YAML")
	}
	if strings.Contains(a.Draw().ANSI(), "synthetic-secret-value") {
		t.Fatal("raw credential rendered")
	}
	a.formKey(Message{Key: "Enter"})
	if a.Form == nil || a.Form.Title != "API access" || a.Form.Choice[0] != "Direct" {
		t.Fatal(a.Form)
	}
	a.formKey(Message{Key: "Enter"})
	waitNetwork(t, a)
	if a.Form == nil || a.Form.ConfigBrowser == nil || len(a.Form.ConfigBrowser.contexts()) != 2 {
		t.Fatal(a.Form)
	}
	if a.Busy {
		t.Fatal("choosing config connected without choosing context")
	}
}
func TestKubernetesConnectionSourceChoices(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, e := New(t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	a.kubeConnectionForm(0, kube.Profile{})
	if len(a.Form.Choice) != 3 {
		t.Fatal(a.Form)
	}
	a.Form.Select(1)
	for _, f := range a.Form.Fields {
		if f.Label == "Kubeconfig location" {
			t.Fatal("removed local-file-through-SSH option still present")
		}
	}
	if len(a.Form.Fields) != 2 {
		t.Fatal(a.Form.Fields)
	}
}
