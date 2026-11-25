package aispaces

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCustomRolePromptPersistsAndBecomesEnvInstructions(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	prompt := "# Security reviewer\n\nReview authentication and authorization.\nReport reproducible findings with evidence.\n"
	role, err := s.CreateRole("Security reviewer", prompt)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][2]string{{"security REVIEWER", "another prompt"}, {"Empty", " \n "}, {"", prompt}} {
		if _, err = s.CreateRole(bad[0], bad[1]); err == nil {
			t.Fatalf("accepted invalid role %q", bad[0])
		}
	}
	s, err = Open(s.root)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := s.Roles()
	if err != nil || len(roles) != len(DefaultRoles())+1 || roles[len(roles)-1] != role {
		t.Fatalf("custom role not preserved: %+v, %v", roles, err)
	}
	s.CreateSpace("Space")
	for _, cli := range []string{"codex", "claude"} {
		if err := s.CreateRoleEnv("Space", cli, Agent{Name: cli, Path: "/bin/true"}, role.ID, false); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(s.envDir(s.Spaces[0].env(cli)), "overlay", InstructionFile(cli)))
		if err != nil || string(data) != prompt {
			t.Fatalf("%s instructions = %q, %v", cli, data, err)
		}
	}
}

func TestRolesSnapshotAndPermissions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CreateSpace("Space"); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveRole("reviewer", "Reviewer / Tester"); err != nil {
		t.Fatal(err)
	}
	settings := AgentSettings{Model: "user-model", Effort: "medium", Levels: map[string]Profile{"complex": {"user-model", "high"}}}
	if err = s.SaveAgentDefaults("codex", settings); err != nil {
		t.Fatal(err)
	}
	agent := Agent{Name: "codex", Path: "/bin/true"}
	if err = s.CreateRoleEnv("Space", "Product", agent, "product", false); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateRoleEnv("Space", "Architect", agent, "architect", true); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateRoleEnv("Space", "Coder", agent, "coder", true); err != nil {
		t.Fatal(err)
	}
	sp := s.Spaces[0]
	coder := sp.env("Coder")
	architect := sp.env("Architect")
	if len(coder.Agents) != 1 || coder.Agents[0].Model != "user-model" || !mayDirect(architect, coder) || mayDirect(coder, architect) {
		t.Fatal("role defaults or directing rights incorrect")
	}
	if !mayContact(sp, sp.env("Product"), architect) {
		t.Fatal("role contacts missing")
	}
	_, err = s.FS("Space", "Coder")
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(s.envDir(coder), "overlay", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(original), "workspace_run") {
		t.Fatal("role instructions missing")
	}
	rolePath, _ := s.RolePath("coder")
	if err = os.WriteFile(rolePath, []byte("edited template"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateRoleEnv("Space", "Coder2", agent, "coder", false); err != nil {
		t.Fatal(err)
	}
	existing, _ := os.ReadFile(filepath.Join(s.envDir(coder), "overlay", "AGENTS.md"))
	if string(existing) != string(original) {
		t.Fatal("existing instructions overwritten")
	}
	next, _ := os.ReadFile(filepath.Join(s.envDir(sp.env("Coder2")), "overlay", "AGENTS.md"))
	if string(next) != "edited template" {
		t.Fatal("edited template not used")
	}
	if err = s.CreateRoleEnv("Space", "Blank", Agent{Name: "claude", Path: "/bin/true"}, "", false); err != nil {
		t.Fatal(err)
	}
	blank := sp.env("Blank")
	s.ensureInstructions(blank)
	data, _ := os.ReadFile(filepath.Join(s.envDir(blank), "overlay", "CLAUDE.md"))
	if len(data) != 0 {
		t.Fatal("blank role got instructions")
	}
	if err = s.DeleteRole("coder"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RolePath("coder"); err == nil {
		t.Fatal("deleted role listed")
	}
	reopened, err := Open(s.root)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Spaces[0].env("Coder").Role != "coder" {
		t.Fatal("Env role not persisted")
	}
	if err = s.CreateRoleEnv("Space", "Bad", agent, "missing", false); err == nil {
		t.Fatal("unknown role accepted")
	}
}
func TestModelCatalogAndSettingsValidation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	os.WriteFile(filepath.Join(root, "models_cache.json"), []byte(`{"models":[{"slug":"known-model","visibility":"list","supported_reasoning_levels":[{"effort":"low"},{"effort":"xhigh"}]},{"slug":"hidden","visibility":"hide"}]}`), 0600)
	a := Agent{Name: "codex", Model: "known-model"}
	if err := a.accepts(directive{model: "invented-model"}); err == nil {
		t.Fatal("invented model accepted")
	}
	if err := a.accepts(directive{model: "known-model", effort: "xhigh"}); err != nil {
		t.Fatal(err)
	}
	if err := a.accepts(directive{effort: "medium"}); err == nil {
		t.Fatal("unsupported effort for model accepted")
	}
	a.Levels = map[string]Profile{"simple": {"explicit-custom", "low"}}
	if err := a.accepts(directive{model: "explicit-custom", effort: "low"}); err != nil {
		t.Fatal(err)
	}
	if err := validateSettings("codex", AgentSettings{Effort: "nonsense"}); err == nil {
		t.Fatal("invalid effort saved")
	}
	if len(ModelCatalog("codex")) != 1 {
		t.Fatal("hidden model listed")
	}
}
