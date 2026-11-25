package aispaces

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrowserOriginBoundary(t *testing.T) {
	const origin = "http://127.0.0.1:12345"
	for _, u := range []string{"https://example.com", "file:///etc/passwd", "http://127.0.0.1:1234/a", "http://evil@127.0.0.1:12345", "http://127.0.0.1:12345.evil/a", "javascript:alert(1)"} {
		if browserURLAllowed(u, origin) {
			t.Fatalf("accepted %s", u)
		}
	}
	if !browserURLAllowed(origin+"/index.html", origin) {
		t.Fatal("rejected test origin")
	}
}

func TestNativeBrowserPreflight(t *testing.T) {
	if os.Getenv("FARSHMAC_NATIVE_BROWSER_TEST") != "1" {
		t.Skip("opt in to native Chrome with its renderer sandbox")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	result := browserPreflight(ctx)
	data, _ := json.Marshal(result)
	var out struct{ Ready bool }
	json.Unmarshal(data, &out)
	if !out.Ready {
		t.Fatalf("preflight: %s", data)
	}
	t.Logf("preflight: %s", data)
}

func TestNativeBrowserHorizontalArrowKeys(t *testing.T) {
	if os.Getenv("FARSHMAC_NATIVE_BROWSER_TEST") != "1" {
		t.Skip("native browser opt-in")
	}
	root := t.TempDir()
	page := `<input id="text" value="abc"><span id="caret"></span><span id="event"></span>
<label><input id="first" type="radio" name="choice" checked>First</label>
<label><input id="second" type="radio" name="choice">Second</label>
<script>document.addEventListener('keyup', e => {
document.querySelector('#event').textContent = e.key+'/'+e.code+'/'+e.keyCode;
document.querySelector('#caret').textContent = document.querySelector('#text').selectionStart;
});</script>`
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(page), 0600); err != nil {
		t.Fatal(err)
	}
	suite := BrowserSuite{Cases: []BrowserCase{
		{Name: "native caret movement", Steps: []BrowserStep{
			{Action: "key", Selector: "#text", Key: "End"},
			{Action: "key", Key: "ArrowLeft"},
			{Action: "text", Selector: "#caret", Expected: "2"},
			{Action: "text", Selector: "#event", Expected: "ArrowLeft/ArrowLeft/37"},
			{Action: "key", Key: "ArrowRight"},
			{Action: "text", Selector: "#caret", Expected: "3"},
			{Action: "text", Selector: "#event", Expected: "ArrowRight/ArrowRight/39"},
		}},
		{Name: "native radio movement", Steps: []BrowserStep{
			{Action: "key", Selector: "#first", Key: "ArrowRight"},
			{Action: "focus", Selector: "#second"},
			{Action: "count", Selector: "#second:checked", Expected: float64(1)},
			{Action: "key", Key: "ArrowLeft"},
			{Action: "focus", Selector: "#first"},
			{Action: "count", Selector: "#first:checked", Expected: float64(1)},
		}},
	}}
	if err := validateBrowserSuite(suite, 1200); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	results, err := runBrowserSuite(ctx, root, suite)
	if err != nil || len(results) != 2 {
		t.Fatalf("arrow checks: %+v, %v", results, err)
	}
	for _, result := range results {
		if !result.Passed {
			t.Fatalf("arrow check: %+v", result)
		}
	}
}

func TestNativeBrowserIndependentDownloadsAndBoundaries(t *testing.T) {
	if os.Getenv("FARSHMAC_NATIVE_BROWSER_TEST") != "1" {
		t.Skip("native browser opt-in")
	}
	s, fs, source := testEnv(t)
	session := agentOf(s, "Dev")
	var external atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { external.Add(1); w.Write([]byte("external")) }))
	defer other.Close()
	page := `<!doctype html><button id="out" onclick="const a=document.createElement('a');a.href=window.URL.createObjectURL(new Blob([JSON.stringify({value:42})]));a.download='data.json';a.click()">Export</button><button id="confirm" onclick="document.querySelector('#state').textContent=confirm('Proceed?')?'yes':'no'">Confirm</button><span id="state">empty</span><span id="network">pending</span><script>fetch('` + other.URL + `').then(()=>document.querySelector('#network').textContent='leaked').catch(()=>document.querySelector('#network').textContent='blocked');</script>`
	os.WriteFile(filepath.Join(source, "index.html"), []byte(page), 0644)
	out, err := artifactCall(s, session, fs, "artifact_publish", map[string]any{"path": "/project", "name": "browser candidate"})
	if err != nil {
		t.Fatal(err)
	}
	var artifact Artifact
	json.Unmarshal([]byte(out), &artifact)
	suite := BrowserSuite{Artifact: artifact.ID}
	for i := 0; i < 12; i++ {
		key := ""
		if i%2 == 1 {
			key = "Enter"
		}
		suite.Cases = append(suite.Cases, BrowserCase{Name: fmt.Sprintf("independent export %d", i), Steps: []BrowserStep{{Action: "text", Selector: "#state", Expected: "empty"}, {Action: "text", Selector: "#network", Expected: "blocked"}, {Action: "download", Selector: "#out", Key: key, Expected: map[string]any{"value": float64(42)}}}})
	}
	suite.Cases = append(suite.Cases, BrowserCase{Name: "native confirmation", Steps: []BrowserStep{{Action: "dialog", Value: "dismiss"}, {Action: "click", Selector: "#confirm"}, {Action: "text", Selector: "#state", Expected: "no"}, {Action: "dialog", Value: "accept"}, {Action: "click", Selector: "#confirm"}, {Action: "text", Selector: "#state", Expected: "yes"}}})
	raw := map[string]any{}
	data, _ := json.Marshal(suite)
	json.Unmarshal(data, &raw)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	session.ctx = ctx
	out, err = browserCheck(s, session, fs, raw)
	var result struct {
		Exit  int
		Run   string
		Cases []BrowserResult
	}
	json.Unmarshal([]byte(out), &result)
	if err != nil || result.Exit != 0 || len(result.Cases) != 13 || result.Run == "" {
		t.Fatalf("suite: %s %v", out, err)
	}
	if external.Load() != 0 {
		t.Fatal("browser accessed a foreign origin")
	}
	if _, err = artifactCall(s, session, fs, "artifact_verify", map[string]any{"id": artifact.ID, "runs": []string{result.Run}}); err != nil {
		t.Fatal(err)
	}
	t.Log("13 native cases passed; source and test evidence match; external origin untouched")
	// A failed expectation must produce a nonzero recorded exit, not a tool transport success.
	raw["cases"] = []BrowserCase{{Name: "expected failure", Steps: []BrowserStep{{Action: "text", Selector: "#state", Expected: "wrong"}}}}
	out, err = browserCheck(s, session, fs, raw)
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(out), &result)
	if result.Exit == 0 {
		t.Fatal("failed assertion recorded as a pass")
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	start := time.Now()
	if _, err = runBrowserSuite(canceled, source, suite); err == nil || time.Since(start) > time.Second {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestNativeBrowserCancelsRunningSuite(t *testing.T) {
	if os.Getenv("FARSHMAC_NATIVE_BROWSER_TEST") != "1" {
		t.Skip("native browser opt-in")
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "index.html"), []byte("<p id=state>waiting</p>"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(1500*time.Millisecond, cancel)
	defer timer.Stop()
	start := time.Now()
	_, err := runBrowserSuite(ctx, root, BrowserSuite{Cases: []BrowserCase{{Name: "canceled wait", Steps: []BrowserStep{{Action: "text", Selector: "#state", Expected: "never"}}}}})
	if err == nil || time.Since(start) > 4*time.Second {
		t.Fatalf("running cancellation %v after %s", err, time.Since(start))
	}
}

func TestBrowserStaticServerRequiresRunSecret(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("private candidate"), 0600)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	handler := browserFileHandler(root, "run-secret")
	for _, tc := range []struct {
		key  string
		want int
	}{{"", 403}, {"wrong", 403}, {"run-secret", 200}} {
		req := httptest.NewRequest("GET", "http://localhost/", nil)
		req.Header.Set("X-FarshMac-Browser-Key", tc.key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("unauthorized client: status %d, want %d", w.Code, tc.want)
		}
		if tc.want != 200 && strings.Contains(w.Body.String(), "private candidate") {
			t.Fatal("candidate exposed")
		}
	}
}
