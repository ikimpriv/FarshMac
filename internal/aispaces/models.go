package aispaces

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ModelChoice comes from the CLI's local catalog, not a guessed model name.
// A catalog describes supported choices; availability is still checked by the CLI.
type ModelChoice struct {
	ID      string
	Efforts []string
}

func ModelCatalog(agent string) []ModelChoice {
	if agent == "claude" {
		return []ModelChoice{{"haiku", claudeEfforts}, {"sonnet", claudeEfforts}, {"opus", claudeEfforts}}
	}
	if agent != "codex" {
		return nil
	}
	home, _ := os.UserHomeDir()
	root := os.Getenv("CODEX_HOME")
	if root == "" {
		root = filepath.Join(home, ".codex")
	}
	f, err := os.Open(filepath.Join(root, "models_cache.json"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var cache struct {
		Models []struct {
			Slug, Visibility string
			Reasoning        []struct{ Effort string } `json:"supported_reasoning_levels"`
		}
	}
	if json.NewDecoder(io.LimitReader(f, 4<<20)).Decode(&cache) != nil {
		return nil
	}
	var out []ModelChoice
	for _, m := range cache.Models {
		if m.Visibility == "hide" || !modelName.MatchString(m.Slug) {
			continue
		}
		c := ModelChoice{ID: m.Slug}
		for _, e := range m.Reasoning {
			if modelName.MatchString(e.Effort) {
				c.Efforts = append(c.Efforts, e.Effort)
			}
		}
		out = append(out, c)
	}
	return out
}
func EffortChoices(agent string) []string {
	out := append([]string(nil), adapters[agent].efforts...)
	for _, m := range ModelCatalog(agent) {
		for _, e := range m.Efforts {
			if !oneOf(e, out) {
				out = append(out, e)
			}
		}
	}
	return out
}
func validateProfile(agent string, p Profile) error {
	if _, ok := adapters[agent]; !ok {
		return fmt.Errorf("unsupported agent %q", agent)
	}
	if p.Model != "" && !modelName.MatchString(p.Model) {
		return fmt.Errorf("model must be a plain model ID")
	}
	efforts := EffortChoices(agent)
	for _, m := range ModelCatalog(agent) {
		if m.ID == p.Model && len(m.Efforts) > 0 {
			efforts = m.Efforts
		}
	}
	if p.Effort != "" && !oneOf(p.Effort, efforts) {
		return fmt.Errorf("effort for %s must be one of: %s", agent, strings.Join(efforts, ", "))
	}
	return nil
}
func validateSettings(agent string, settings AgentSettings) error {
	if err := validateProfile(agent, Profile{strings.TrimSpace(settings.Model), strings.TrimSpace(settings.Effort)}); err != nil {
		return err
	}
	for level, p := range settings.Levels {
		if !oneOf(level, Levels) {
			return fmt.Errorf("unknown complexity %q", level)
		}
		if err := validateProfile(agent, p); err != nil {
			return fmt.Errorf("%s: %w", level, err)
		}
	}
	return nil
}
func (a *Agent) knownModels() []string {
	var models []string
	for _, m := range ModelCatalog(a.Name) {
		models = append(models, m.ID)
	}
	for _, p := range append([]Profile{{Model: a.Model}}, a.level("simple"), a.level("moderate"), a.level("complex")) {
		if p.Model != "" && !oneOf(p.Model, models) {
			models = append(models, p.Model)
		}
	}
	return models
}

func DefaultProfile(agent, level string) Profile { return adapters[agent].levels[level] }
