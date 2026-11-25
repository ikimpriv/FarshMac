package aispaces

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrowserSpecReferencesAndValidation(t *testing.T) {
	s, fs, source := testEnv(t)
	session := agentOf(s, "Dev")
	os.WriteFile(filepath.Join(source, "seed.json"), []byte(`{"tasks":[{"title":"seed"}]}`), 0600)
	spec := `{"fixture_files":{"seed":"seed.json"},"fixtures":{"bad":"{broken"},"blocks":{"seed":[{"action":"upload","selector":"#file","file":"board.json","value_fixture":"seed"}]},"cases":[{"name":"import","steps":[{"use":"seed"},{"action":"storage","key":"board","expected_fixture":"seed"},{"action":"text","selector":"p","match":"contains","expected":"done"}]}]}`
	os.WriteFile(filepath.Join(source, "suite.json"), []byte(spec), 0600)
	suite, hash, err := resolveBrowserSuite(s, session, fs, map[string]any{"path": "/project", "spec": "/project/suite.json"})
	if err != nil || !strings.HasPrefix(hash, "sha256:") || len(suite.Cases) != 1 {
		t.Fatalf("%v %s", err, hash)
	}
	if suite.Cases[0].Steps[0].Value != `{"tasks":[{"title":"seed"}]}` {
		t.Fatal(suite.Cases)
	}
	if _, _, err = resolveBrowserSuite(s, session, fs, map[string]any{"path": "/project", "spec": "/project/suite.json", "only": []string{"absent"}}); err == nil {
		t.Fatal("unknown case selection accepted")
	}
	selected, selectedHash, err := resolveBrowserSuite(s, session, fs, map[string]any{"path": "/project", "spec": "/project/suite.json", "only": []string{"import"}})
	if err != nil || selectedHash != hash || len(selected.Cases) != 1 {
		t.Fatalf("focused rerun changed spec: %v", err)
	}
	for _, bad := range []string{
		strings.Replace(spec, `"use":"seed"`, `"use":"absent"`, 1),
		strings.Replace(spec, `"match":"contains"`, `"match":"partial"`, 1),
		strings.Replace(spec, `"seed.json"`, `"/unattached/secret"`, 1),
		strings.Replace(spec, `"seed":[{"action":"upload","selector":"#file","file":"board.json","value_fixture":"seed"}]`, `"seed":[{"use":"seed"}]`, 1),
		strings.Replace(spec, `"cases":`, `"unknown":true,"cases":`, 1),
		spec + ` {}`,
	} {
		os.WriteFile(filepath.Join(source, "suite.json"), []byte(bad), 0600)
		if _, _, err = resolveBrowserSuite(s, session, fs, map[string]any{"path": "/project", "spec": "/project/suite.json"}); err == nil {
			t.Fatalf("accepted invalid spec %s", bad)
		}
	}
}

func TestBrowserActionValidationBeforeExecution(t *testing.T) {
	for _, step := range []BrowserStep{
		{Action: "download"}, {Action: "upload", Selector: "#file", File: "../secret"},
		{Action: "focus"}, {Action: "storage"}, {Action: "dialog", Value: "ok"},
		{Action: "download", Selector: "#export", Key: "ArrowDown"},
	} {
		if err := validateBrowserSuite(BrowserSuite{Cases: []BrowserCase{{Name: "invalid", Steps: []BrowserStep{step}}}}, 1200); err == nil {
			t.Fatalf("accepted invalid action %+v", step)
		}
	}
}

func TestTest6RoleMigrationPreservesCustomization(t *testing.T) {
	addition := "Repair a small test error with vfs_edit and recheck affected case names using only; do not rewrite or rerun the full spec without a coverage reason. "
	for _, role := range DefaultRoles() {
		old := strings.Replace(role.Instructions, addition, "", 1)
		if old == role.Instructions || upgradedRole(role.ID, old) != role.Instructions {
			t.Fatalf("unchanged test6 role %s did not upgrade", role.ID)
		}
		custom := old + "\nCustom instruction."
		if upgradedRole(role.ID, custom) != custom {
			t.Fatalf("custom role %s overwritten", role.ID)
		}
	}
}
func TestBrowserAutomaticBatchBounds(t *testing.T) {
	suite := BrowserSuite{}
	for i := 0; i < 10; i++ {
		c := BrowserCase{Name: strings.Repeat("x", i+1)}
		for j := 0; j < 40; j++ {
			c.Steps = append(c.Steps, BrowserStep{Action: "text", Selector: "p", Expected: "ok"})
		}
		suite.Cases = append(suite.Cases, c)
	}
	if err := validateBrowserSuite(suite, 1200); err != nil {
		t.Fatal(err)
	}
	batches := browserBatches(suite)
	if len(batches) != 2 {
		t.Fatal(len(batches))
	}
	count := 0
	for _, b := range batches {
		if err := validateBrowserSuite(b, 240); err != nil {
			t.Fatal(err)
		}
		count += len(b.Cases)
	}
	if count != 10 {
		t.Fatal(count)
	}
}
func TestRuntimeCacheSharesProbeAndCancellation(t *testing.T) {
	var c runtimeCache
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	probe := func(ctx context.Context) (string, error) {
		calls.Add(1)
		close(entered)
		<-release
		return `{"browser":{"ready":true}}`, nil
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, e := c.get(context.Background(), "build1", probe); e != nil {
			t.Error(e)
		}
	}()
	<-entered
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.get(canceled, "build1", probe); err == nil {
		t.Fatal("wait ignored cancellation")
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, e := c.get(context.Background(), "build1", probe)
			if e != nil || !strings.Contains(out, `"cached":true`) {
				t.Errorf("cache %s %v", out, e)
			}
		}()
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	if _, err := c.get(context.Background(), "build2", func(context.Context) (string, error) { calls.Add(1); return `{}`, nil }); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("runtime identity did not invalidate")
	}
}
func TestNativeBrowserSpecAndAutomaticEvidence(t *testing.T) {
	if os.Getenv("FARSHMAC_NATIVE_BROWSER_TEST") != "1" {
		t.Skip("native browser opt-in")
	}
	s, fs, source := testEnv(t)
	session := agentOf(s, "Dev")
	page := `<!doctype html><button id=b onclick="document.querySelector('p').textContent='hello world'">Go</button><p>empty</p><input type=file id=f onchange="this.files[0].text().then(t=>localStorage.setItem('data',t))">`
	os.WriteFile(filepath.Join(source, "index.html"), []byte(page), 0600)
	out, err := artifactCall(s, session, fs, "artifact_publish", map[string]any{"path": "/project", "name": "spec test"})
	if err != nil {
		t.Fatal(err)
	}
	var a Artifact
	json.Unmarshal([]byte(out), &a)
	spec := browserSpec{Fixtures: map[string]any{"data": map[string]any{"n": float64(7)}}, Blocks: map[string][]BrowserStep{"setup": {{Action: "key", Selector: "#b", Key: "Enter"}, {Action: "focus", Selector: "#b"}, {Action: "text", Selector: "p", Match: "contains", Expected: "world"}, {Action: "upload", Selector: "#f", File: "input.json", ValueFixture: "data"}, {Action: "storage", Key: "data", ExpectedFixture: "data"}}}}
	for i := 0; i < 7; i++ {
		c := BrowserCase{Name: strings.Repeat("x", i+1), Steps: []BrowserStep{{Use: "setup"}}}
		for j := 0; j < 35; j++ {
			c.Steps = append(c.Steps, BrowserStep{Action: "text", Selector: "p", Expected: "hello world"})
		}
		spec.Cases = append(spec.Cases, c)
	}
	// The spec is outside the immutable candidate, but inside the caller's VFS.
	data, _ := json.Marshal(spec)
	os.WriteFile(filepath.Join(source, "checks.json"), data, 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	session.ctx = ctx
	out, err = browserCheck(s, session, fs, map[string]any{"artifact": a.ID, "spec": "/project/checks.json"})
	var result struct {
		Exit, Batches    int
		EvidenceAttached bool `json:"evidence_attached"`
		Cases            []BrowserResult
		Run              string
	}
	json.Unmarshal([]byte(out), &result)
	if err != nil || result.Exit != 0 || result.Batches != 2 || !result.EvidenceAttached || len(result.Cases) != 7 {
		t.Fatalf("%s %v", out, err)
	}
	out, err = artifactCall(s, session, fs, "artifact_read", map[string]any{"id": a.ID, "run": result.Run})
	if err != nil || !strings.Contains(out, `"Exit":0`) {
		t.Fatalf("missing evidence %s %v", out, err)
	}
	if len(out) > 4000 || strings.Contains(out, `"Command"`) || !strings.Contains(out, "spec_sha256") {
		t.Fatal("browser result must be compact and retain spec provenance")
	}
	record, err := os.ReadFile(filepath.Join(s.envDir(session.env), "evidence", result.Run+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var evidence RunEvidence
	json.Unmarshal(record, &evidence)
	var replay map[string]any
	json.Unmarshal([]byte(evidence.Command), &replay)
	// Replaying evidence does not require a surviving external specification.
	os.Remove(filepath.Join(source, "checks.json"))
	resolved, _, err := resolveBrowserSuite(s, session, fs, replay)
	if err != nil || len(resolved.Cases) != 7 {
		t.Fatalf("evidence is not replayable: %v", err)
	}
}
