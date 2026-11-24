package aispaces

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
)

// Specifications are data, never scripts. References are resolved through the
// caller's VFS once, before Chrome starts; the resolved input is kept in evidence.
type browserSpec struct {
	Page         string                   `json:"page,omitempty"`
	Fixtures     map[string]any           `json:"fixtures,omitempty"`
	FixtureFiles map[string]string        `json:"fixture_files,omitempty"`
	Blocks       map[string][]BrowserStep `json:"blocks,omitempty"`
	Cases        []BrowserCase            `json:"cases"`
}

func resolveBrowserSuite(s *Store, session *mcpSession, fs *FS, raw map[string]any) (BrowserSuite, string, error) {
	var suite BrowserSuite
	if err := decodeArgs(raw, &suite); err != nil {
		return suite, "", err
	}
	var spec browserSpec
	digest := ""
	if suite.Spec != "" {
		if len(suite.Cases) > 0 || len(suite.Fixtures) > 0 || len(suite.Blocks) > 0 {
			return suite, "", fmt.Errorf("use spec or inline cases/fixtures/blocks, not both")
		}
		file := suite.Spec
		if strings.HasPrefix(file, "@") {
			var err error
			file, err = s.resolveRef(session.env, file)
			if err != nil {
				return suite, "", err
			}
		}
		data, truncated, err := fs.ReadFile(file, 1<<20)
		if err != nil {
			return suite, "", err
		}
		if truncated {
			return suite, "", fmt.Errorf("browser spec exceeds 1 MiB")
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&spec); err != nil {
			return suite, "", fmt.Errorf("browser spec: %w", err)
		}
		if dec.Decode(new(any)) != io.EOF {
			return suite, "", fmt.Errorf("browser spec must contain one JSON object")
		}
		digest = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
		if spec.Fixtures == nil {
			spec.Fixtures = map[string]any{}
		}
		if len(spec.FixtureFiles) > 64 {
			return suite, "", fmt.Errorf("at most 64 fixture files")
		}
		remaining := int64(1 << 20)
		for name, ref := range spec.FixtureFiles {
			if _, ok := spec.Fixtures[name]; ok {
				return suite, "", fmt.Errorf("duplicate fixture %q", name)
			}
			if !strings.HasPrefix(ref, "/") && !strings.HasPrefix(ref, "@") {
				if strings.Contains(ref, "..") {
					return suite, "", fmt.Errorf("fixture path must not contain ..")
				}
				ref = path.Join(path.Dir(file), ref)
			}
			if strings.HasPrefix(ref, "@") {
				ref, err = s.resolveRef(session.env, ref)
				if err != nil {
					return suite, "", err
				}
			}
			if remaining <= 0 {
				return suite, "", fmt.Errorf("fixtures exceed 1 MiB")
			}
			b, cut, e := fs.ReadFile(ref, remaining)
			if e != nil {
				return suite, "", e
			}
			if cut {
				return suite, "", fmt.Errorf("fixtures exceed 1 MiB")
			}
			remaining -= int64(len(b))
			var value any
			if json.Unmarshal(b, &value) != nil {
				value = string(b)
			}
			spec.Fixtures[name] = value
		}
		suite.Cases, suite.Fixtures, suite.Blocks = spec.Cases, spec.Fixtures, spec.Blocks
		if suite.Page == "" {
			suite.Page = spec.Page
		}
	}
	expand := func(steps []BrowserStep) ([]BrowserStep, error) {
		return expandBrowserSteps(steps, suite.Fixtures, suite.Blocks, nil)
	}
	for i := range suite.Cases {
		steps, err := expand(suite.Cases[i].Steps)
		if err != nil {
			return suite, "", fmt.Errorf("case %q: %w", suite.Cases[i].Name, err)
		}
		suite.Cases[i].Steps = steps
	}
	suite.Fixtures = nil
	suite.Blocks = nil
	if err := validateBrowserSuite(suite, 1200); err != nil {
		return suite, "", err
	}
	if len(suite.Only) > 0 {
		wanted := map[string]bool{}
		for _, name := range suite.Only {
			if name == "" || wanted[name] {
				return suite, "", fmt.Errorf("only requires unique, nonempty case names")
			}
			wanted[name] = true
		}
		var selected []BrowserCase
		for _, c := range suite.Cases {
			if wanted[c.Name] {
				selected = append(selected, c)
				delete(wanted, c.Name)
			}
		}
		if len(wanted) > 0 {
			return suite, "", fmt.Errorf("only names must match existing cases exactly")
		}
		suite.Cases = selected
	}
	budget := 4 << 20
	for _, c := range suite.Cases {
		for _, step := range c.Steps {
			budget -= len(jsonText(step))
			if budget < 0 {
				return suite, "", fmt.Errorf("expanded browser suite exceeds 4 MiB")
			}
		}
	}
	return suite, digest, nil
}

func expandBrowserSteps(steps []BrowserStep, fixtures map[string]any, blocks map[string][]BrowserStep, stack []string) ([]BrowserStep, error) {
	var out []BrowserStep
	for _, step := range steps {
		if step.Use != "" {
			if step.Action != "" {
				return nil, fmt.Errorf("use cannot have an action")
			}
			if len(stack) >= 8 || oneOf(step.Use, stack) {
				return nil, fmt.Errorf("recursive block %q", step.Use)
			}
			block, ok := blocks[step.Use]
			if !ok {
				return nil, fmt.Errorf("unknown block %q", step.Use)
			}
			more, err := expandBrowserSteps(block, fixtures, blocks, append(stack, step.Use))
			if err != nil {
				return nil, err
			}
			out = append(out, more...)
		} else {
			if step.ValueFixture != "" {
				value, ok := fixtures[step.ValueFixture]
				if !ok {
					return nil, fmt.Errorf("unknown fixture %q", step.ValueFixture)
				}
				if step.Value != "" {
					return nil, fmt.Errorf("use value or value_fixture")
				}
				if str, ok := value.(string); ok {
					step.Value = str
				} else {
					step.Value = jsonText(value)
				}
			}
			if step.ExpectedFixture != "" {
				value, ok := fixtures[step.ExpectedFixture]
				if !ok {
					return nil, fmt.Errorf("unknown fixture %q", step.ExpectedFixture)
				}
				if step.Expected != nil {
					return nil, fmt.Errorf("use expected or expected_fixture")
				}
				step.Expected = value
			}
			step.ValueFixture = ""
			step.ExpectedFixture = ""
			out = append(out, step)
		}
		if len(out) > 80 {
			return nil, fmt.Errorf("expanded case exceeds 80 steps; split into independent cases")
		}
	}
	return out, nil
}

func validateBrowserSuite(suite BrowserSuite, maxSteps int) error {
	if len(suite.Cases) < 1 || len(suite.Cases) > 20 {
		return fmt.Errorf("provide 1–20 cases")
	}
	count := 0
	names := map[string]bool{}
	for _, c := range suite.Cases {
		if c.Name == "" || len(c.Name) > 160 || names[c.Name] {
			return fmt.Errorf("case names must be unique, nonempty and at most 160 bytes")
		}
		names[c.Name] = true
		if len(c.Steps) < 1 || len(c.Steps) > 80 {
			return fmt.Errorf("provide 1–80 expanded steps per case")
		}
		count += len(c.Steps)
		for index, step := range c.Steps {
			if !oneOf(step.Action, []string{"goto", "click", "fill", "select", "key", "reload", "text", "value", "count", "storage", "upload", "download", "dialog", "focus"}) {
				return fmt.Errorf("case %q: unsupported action %q", c.Name, step.Action)
			}
			if oneOf(step.Action, []string{"click", "fill", "select", "text", "value", "count", "focus", "upload", "download"}) && strings.TrimSpace(step.Selector) == "" {
				return fmt.Errorf("case %q step %d: %s requires selector", c.Name, index+1, step.Action)
			}
			if step.Action == "upload" && !filepath.IsLocal(step.File) {
				return fmt.Errorf("case %q step %d: upload requires a relative file name", c.Name, index+1)
			}
			if step.Action == "storage" && step.Key == "" {
				return fmt.Errorf("case %q step %d: storage requires key", c.Name, index+1)
			}
			if step.Action == "dialog" && !oneOf(step.Value, []string{"accept", "dismiss"}) {
				return fmt.Errorf("case %q step %d: dialog requires value accept or dismiss", c.Name, index+1)
			}
			if step.Action == "download" && step.Key != "" && !oneOf(step.Key, []string{"Enter", "Space"}) {
				return fmt.Errorf("download key must be Enter or Space")
			}
			if step.Match != "" && (step.Action != "text" || !oneOf(step.Match, []string{"exact", "contains"})) {
				return fmt.Errorf("match applies only to text: exact (default) or contains")
			}
			if step.Match == "contains" {
				if _, ok := step.Expected.(string); !ok {
					return fmt.Errorf("contains requires a string expected value")
				}
			}
			if step.Action == "key" {
				if _, ok := browserKeys[step.Key]; !ok {
					return fmt.Errorf("unsupported key %q", step.Key)
				}
			}
		}
	}
	if count > maxSteps {
		return fmt.Errorf("suite exceeds %d expanded steps", maxSteps)
	}
	return nil
}

// Cases have isolated storage, so batches can use separate browser processes.
// A single request is bounded overall and produces one durable result.
func browserBatches(suite BrowserSuite) []BrowserSuite {
	var batches []BrowserSuite
	batch := suite
	batch.Cases = nil
	count := 0
	for _, c := range suite.Cases {
		if count+len(c.Steps) > 240 {
			batches = append(batches, batch)
			batch = suite
			batch.Cases = nil
			count = 0
		}
		batch.Cases = append(batch.Cases, c)
		count += len(c.Steps)
	}
	if len(batch.Cases) > 0 {
		batches = append(batches, batch)
	}
	return batches
}
