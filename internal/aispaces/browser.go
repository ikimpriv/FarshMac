package aispaces

// The browser is a managed sibling of the isolated test process. Chrome keeps
// its own renderer sandbox. Agents never receive CDP, a host execution API,
// arbitrary evaluation, a personal profile, or a path to host files.
import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

type BrowserStep struct {
	Use             string `json:"use,omitempty"`
	Match           string `json:"match,omitempty"`
	ValueFixture    string `json:"value_fixture,omitempty"`
	ExpectedFixture string `json:"expected_fixture,omitempty"`
	Action          string `json:"action"`
	Selector        string `json:"selector,omitempty"`
	Value           string `json:"value,omitempty"`
	File            string `json:"file,omitempty"`
	Key             string `json:"key,omitempty"`
	Expected        any    `json:"expected,omitempty"`
}
type BrowserCase struct {
	Name  string        `json:"name"`
	Steps []BrowserStep `json:"steps"`
}
type BrowserSuite struct {
	Spec     string                   `json:"spec,omitempty"`
	Only     []string                 `json:"only,omitempty"`
	Fixtures map[string]any           `json:"fixtures,omitempty"`
	Blocks   map[string][]BrowserStep `json:"blocks,omitempty"`
	Path     string                   `json:"path"`
	Artifact string                   `json:"artifact,omitempty"`
	Page     string                   `json:"page"`
	Cases    []BrowserCase            `json:"cases"`
}
type BrowserResult struct {
	Name   string
	Passed bool
	Error  string `json:",omitempty"`
}

var browserTools = []tool{{"browser_check", "Test a static build with native Chrome. Prefer spec: a VFS JSON file written once. Fresh context per case. Limits: 20 cases, 80 expanded steps/case, 1200 total; automatic batches of 240, 120s total. Evidence automatically attaches to artifact, including failures. Share that same ID; do not republish.", object(nil,
	prop{"artifact", "string", "Received candidate ID (or provide path)"}, prop{"path", "string", pathHelp}, prop{"spec", "string", "VFS JSON file: {page?,fixtures?:{name:JSON},fixture_files?:{name:relative/VFS file},blocks?:{name:[steps]},cases:[{name,steps}]}. Steps may use {use:block}; value_fixture and expected_fixture reuse named fixtures. No JavaScript. Relative fixture files resolve beside the spec."},
	prop{"page", "string", "Relative entry point, default index.html"}, prop{"cases", "array", "Inline cases for small focused checks; otherwise use spec"}, prop{"only", "array", "Exact case names to rerun from the same spec; omit for all cases"}, prop{"fixtures", "object", "Named JSON values for inline cases"}, prop{"blocks", "object", "Named reusable step arrays for inline cases"})}}

func init() {
	step := object(nil, prop{"action", "string", "goto, click, fill, select, key, reload, text, value, count, storage, upload, download, dialog, focus"}, prop{"selector", "string", "CSS selector; key with selector explicitly focuses it; omit to test natural keyboard traversal"}, prop{"value", "string", "Input text/option value, relative URL, upload text, or dialog accept/dismiss"}, prop{"key", "string", "Storage key or Enter, Tab, Shift+Tab, Escape, Space, ArrowDown, ArrowUp, ArrowLeft, ArrowRight, Home, End, Backspace"}, prop{"file", "string", "Relative candidate file for upload, or filename for upload value"}, prop{"match", "string", "text only: exact (default, full textContent) or contains. No implicit trimming"}, prop{"use", "string", "Expand a named block instead of an action"}, prop{"value_fixture", "string", "Named fixture: string as text, JSON serialized for upload"}, prop{"expected_fixture", "string", "Named fixture as exact expected JSON/text"})
	step["properties"].(map[string]any)["expected"] = map[string]any{"description": "Exact text/value/count or complete storage/download JSON. focus checks document.activeElement matches selector (no expected needed). Assertions retry up to 5s. select sets value via DOM; key sends real browser keys, not a substitute for native keyboard evidence."}
	c := object([]string{"name", "steps"}, prop{"name", "string", "Unique case name"})
	c["properties"].(map[string]any)["steps"] = map[string]any{"type": "array", "minItems": 1, "maxItems": 80, "items": step}
	browserTools[0].InputSchema["properties"].(map[string]any)["cases"] = map[string]any{"type": "array", "minItems": 1, "maxItems": 20, "items": c}
	browserTools[0].InputSchema["properties"].(map[string]any)["only"] = stringList("Exact case names for a focused rerun; reuse the same spec")
	step["properties"].(map[string]any)["selector"] = map[string]any{"type": "string", "description": "Required for click/fill/select/text/value/count/focus/upload/download. For key it explicitly focuses the element; omit to test natural traversal. download clicks this element, or activates it with optional key:Enter/Space, then verifies that download (do not trigger a separate click first)."}
}

var browserKeys = map[string]int{"Enter": 13, "Tab": 9, "Shift+Tab": 9, "Escape": 27, "Space": 32, "ArrowDown": 40, "ArrowUp": 38, "ArrowLeft": 37, "ArrowRight": 39, "Home": 36, "End": 35, "Backspace": 8}

func readEvidenceLines(r io.Reader, offset, limit int) (string, error) {
	scan := bufio.NewScanner(io.LimitReader(r, 4<<20))
	scan.Buffer(make([]byte, 4096), 64<<10)
	var out strings.Builder
	n := 0
	for scan.Scan() {
		n++
		if n < offset {
			continue
		}
		if n >= offset+limit || out.Len()+len(scan.Bytes()) > 12<<10 {
			out.WriteString("\n[more; use offset/limit]")
			break
		}
		out.WriteString(scan.Text() + "\n")
	}
	return out.String(), scan.Err()
}

type cdpMessage struct {
	ID      int             `json:"id"`
	Session string          `json:"sessionId"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}
type nativeBrowser struct {
	conn        *websocket.Conn
	mu, write   sync.Mutex
	next        int
	wait        map[int]chan cdpMessage
	events      chan cdpMessage
	done        chan struct{}
	origin      string
	auth        string
	close       func()
	overflow    atomic.Bool
	diagnostics []string
	dialogs     map[string][]bool
}

func chromePath() string {
	for _, p := range []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
func browserURLAllowed(raw, origin string) bool {
	if raw == "about:blank" {
		return true
	}
	if strings.HasPrefix(raw, "blob:") {
		return strings.HasPrefix(raw, "blob:"+origin+"/")
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return false
	}
	return u.Scheme == "http" && u.Scheme+"://"+u.Host == origin
}
func newNativeBrowser(ctx context.Context, origin, auth, dir string) (*nativeBrowser, error) {
	chrome := chromePath()
	if chrome == "" {
		return nil, fmt.Errorf("Chrome or Chromium is not installed")
	}
	profile := filepath.Join(dir, "profile")
	cmd := exec.CommandContext(ctx, chrome, "--headless=new", "--no-first-run", "--no-default-browser-check", "--disable-extensions", "--disable-background-networking", "--disable-sync", "--remote-debugging-port=0", "--user-data-dir="+profile, "about:blank")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// macOS Chrome stalls during native initialization with a synthetic HOME.
	// All browsing state is isolated with --user-data-dir and fresh contexts.
	home, _ := os.UserHomeDir()
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home, "TMPDIR=" + dir}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = time.Second
	log := &boundedLog{file: io.Discard}
	cmd.Stderr = log
	cmd.Stdout = log
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	stop := func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }
	var endpoint string
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			stop()
			return nil, ctx.Err()
		}
		data, _ := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
		parts := strings.Fields(string(data))
		if len(parts) == 2 {
			endpoint = "ws://127.0.0.1:" + parts[0] + parts[1]
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if endpoint == "" {
		stop()
		return nil, fmt.Errorf("Chrome did not expose its managed endpoint: %s", shorten(string(log.tail), 1500))
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, endpoint, nil)
	if err != nil {
		stop()
		return nil, err
	}
	b := &nativeBrowser{conn: conn, wait: map[int]chan cdpMessage{}, events: make(chan cdpMessage, 256), done: make(chan struct{}), origin: origin, auth: auth, dialogs: map[string][]bool{}}
	b.close = func() { conn.Close(); stop() }
	conn.SetReadLimit(4 << 20)
	go b.receive()
	return b, nil
}
func (b *nativeBrowser) send(session, method string, params any, ch chan cdpMessage) error {
	b.mu.Lock()
	b.next++
	id := b.next
	if ch != nil {
		b.wait[id] = ch
	}
	b.mu.Unlock()
	b.write.Lock()
	_ = b.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	message := map[string]any{"id": id, "method": method, "params": params}
	if session != "" {
		message["sessionId"] = session
	}
	err := b.conn.WriteJSON(message)
	b.write.Unlock()
	if err != nil && ch != nil {
		b.mu.Lock()
		delete(b.wait, id)
		b.mu.Unlock()
	}
	return err
}
func (b *nativeBrowser) receive() {
	defer close(b.done)
	for {
		var m cdpMessage
		if err := b.conn.ReadJSON(&m); err != nil {
			return
		}
		if m.ID != 0 {
			b.mu.Lock()
			ch := b.wait[m.ID]
			delete(b.wait, m.ID)
			b.mu.Unlock()
			if ch != nil {
				ch <- m
			}
			continue
		}
		if m.Method == "Fetch.requestPaused" {
			var p struct {
				RequestID string `json:"requestId"`
				Request   struct {
					URL string `json:"url"`
				}
			}
			json.Unmarshal(m.Params, &p)
			method := "Fetch.continueRequest"
			args := map[string]any{"requestId": p.RequestID}
			if !browserURLAllowed(p.Request.URL, b.origin) {
				b.diagnostic("Blocked URL: " + p.Request.URL)
				method = "Fetch.failRequest"
				args["errorReason"] = "BlockedByClient"
			}
			_ = b.send(m.Session, method, args, nil)
			continue
		}
		if m.Method == "Page.javascriptDialogOpening" {
			b.mu.Lock()
			choices := b.dialogs[m.Session]
			accept := false
			if len(choices) > 0 {
				accept = choices[0]
				b.dialogs[m.Session] = choices[1:]
			} else {
				b.diagnostics = append(b.diagnostics, "Unexpected JavaScript dialog dismissed")
			}
			b.mu.Unlock()
			_ = b.send(m.Session, "Page.handleJavaScriptDialog", map[string]any{"accept": accept}, nil)
			continue
		}
		if m.Method == "Runtime.exceptionThrown" || m.Method == "Log.entryAdded" {
			b.diagnostic(m.Method + ": " + string(m.Params))
		}
		if m.Method == "Target.targetCreated" {
			var p struct {
				TargetInfo struct {
					TargetID string `json:"targetId"`
					OpenerID string `json:"openerId"`
				}
			}
			json.Unmarshal(m.Params, &p)
			if p.TargetInfo.OpenerID != "" {
				_ = b.send("", "Target.closeTarget", map[string]any{"targetId": p.TargetInfo.TargetID}, nil)
			}
			continue
		}
		if strings.HasPrefix(m.Method, "Browser.download") {
			select {
			case b.events <- m:
			default:
				b.overflow.Store(true)
			}
		}
	}
}
func (b *nativeBrowser) call(ctx context.Context, session, method string, params any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	ch := make(chan cdpMessage, 1)
	defer func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for id, pending := range b.wait {
			if pending == ch {
				delete(b.wait, id)
			}
		}
	}()
	if err := b.send(session, method, params, ch); err != nil {
		return err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return fmt.Errorf("%s: %s", method, m.Error.Message)
		}
		if out != nil {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", method, ctx.Err())
	case <-b.done:
		return fmt.Errorf("Chrome connection closed")
	}
}
func (b *nativeBrowser) evaluate(ctx context.Context, session, expression string) (any, error) {
	var out struct {
		Result struct {
			Value any `json:"value"`
		}
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	err := b.call(ctx, session, "Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true, "awaitPromise": true, "userGesture": true}, &out)
	if err == nil && len(out.ExceptionDetails) > 0 {
		err = fmt.Errorf("page expression failed")
	}
	return out.Result.Value, err
}
func (b *nativeBrowser) newCase(ctx context.Context, dir string) (session, contextID string, err error) {
	var c struct {
		BrowserContextID string `json:"browserContextId"`
	}
	err = b.call(ctx, "", "Target.createBrowserContext", map[string]any{}, &c)
	if err != nil {
		return
	}
	contextID = c.BrowserContextID
	var t struct {
		TargetID string `json:"targetId"`
	}
	err = b.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank", "browserContextId": contextID}, &t)
	if err != nil {
		return
	}
	var a struct {
		SessionID string `json:"sessionId"`
	}
	err = b.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": t.TargetID, "flatten": true}, &a)
	if err != nil {
		return
	}
	session = a.SessionID
	for _, method := range []string{"Page.enable", "Runtime.enable", "DOM.enable", "Log.enable", "Network.enable"} {
		if err = b.call(ctx, session, method, map[string]any{}, nil); err != nil {
			return
		}
	}
	if err = b.call(ctx, session, "Emulation.setFocusEmulationEnabled", map[string]any{"enabled": true}, nil); err != nil {
		return
	}
	if err = b.call(ctx, session, "Page.bringToFront", map[string]any{}, nil); err != nil {
		return
	}
	err = b.call(ctx, session, "Network.setExtraHTTPHeaders", map[string]any{"headers": map[string]string{"X-FarshMac-Browser-Key": b.auth}}, nil)
	if err != nil {
		return
	}
	err = b.call(ctx, session, "Fetch.enable", map[string]any{"patterns": []any{map[string]any{"urlPattern": "*", "requestStage": "Request"}}}, nil)
	if err != nil {
		return
	}
	err = b.call(ctx, "", "Browser.setDownloadBehavior", map[string]any{"behavior": "allowAndName", "browserContextId": contextID, "downloadPath": dir, "eventsEnabled": true}, nil)
	return
}
func waitBrowser(ctx context.Context, fn func() (bool, error)) error {
	for {
		ok, err := fn()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Millisecond):
		}
	}
}
func (b *nativeBrowser) navigate(ctx context.Context, session, relative string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	u, err := url.Parse(relative)
	if err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(relative, "//") {
		return fmt.Errorf("navigation must be relative to the test origin")
	}
	// net/http's static server canonicalizes index.html to its directory.
	if strings.HasSuffix(u.Path, "/index.html") || u.Path == "index.html" {
		u.Path = strings.TrimSuffix(u.Path, "index.html")
	}
	target := b.origin + "/" + strings.TrimPrefix(u.String(), "/")
	if !browserURLAllowed(target, b.origin) {
		return fmt.Errorf("navigation outside test origin")
	}
	if err = b.call(ctx, session, "Page.navigate", map[string]any{"url": target}, nil); err != nil {
		return err
	}
	var last any
	err = waitBrowser(ctx, func() (bool, error) {
		v, e := b.evaluate(ctx, session, `({url:location.href,state:document.readyState})`)
		last = v
		state, _ := v.(map[string]any)
		return state["url"] == target && state["state"] == "complete", e
	})
	if err != nil {
		return fmt.Errorf("navigation %s (%s): %w", target, jsonText(last), err)
	}
	return nil
}
func (b *nativeBrowser) step(ctx context.Context, session, root, downloads string, step BrowserStep) error {
	sel := `document.querySelector(` + jsonText(step.Selector) + `)`
	switch step.Action {
	case "dialog":
		if step.Value != "accept" && step.Value != "dismiss" {
			return fmt.Errorf("dialog requires accept or dismiss")
		}
		b.mu.Lock()
		b.dialogs[session] = append(b.dialogs[session], step.Value == "accept")
		b.mu.Unlock()
		return nil
	case "goto":
		return b.navigate(ctx, session, step.Value)
	case "reload":
		marker := "__farshmac_reload_" + newID()
		if _, err := b.evaluate(ctx, session, `window[`+jsonText(marker)+`]=true`); err != nil {
			return err
		}
		if err := b.call(ctx, session, "Page.reload", map[string]any{"ignoreCache": true}, nil); err != nil {
			return err
		}
		return waitBrowser(ctx, func() (bool, error) {
			v, e := b.evaluate(ctx, session, `!window[`+jsonText(marker)+`] && document.readyState === 'complete'`)
			if e != nil && strings.Contains(e.Error(), "context") {
				return false, nil
			}
			return v == true, e
		})
	case "click", "fill", "select":
		return waitBrowser(ctx, func() (bool, error) {
			body := `e.click();`
			if step.Action != "click" {
				body = `e.focus(); e.value=` + jsonText(step.Value) + `;e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));`
			}
			v, e := b.evaluate(ctx, session, `(()=>{const e=`+sel+`;if(!e||e.disabled||!e.getClientRects().length)return false;`+body+`return true})()`)
			return v == true, e
		})
	case "key":
		code, ok := browserKeys[step.Key]
		if !ok {
			return fmt.Errorf("unsupported key")
		}
		if step.Selector != "" {
			if _, err := b.evaluate(ctx, session, sel+`.focus()`); err != nil {
				return err
			}
		}
		key := step.Key
		modifiers := 0
		if key == "Shift+Tab" {
			key = "Tab"
			modifiers = 8
		}
		text := ""
		if key == "Space" {
			key = " "
			text = " "
		}
		if key == "Enter" {
			text = "\r"
		}
		for _, typ := range []string{"keyDown", "keyUp"} {
			nativeCodes := map[string]int{"Enter": 36, "Tab": 48, "Escape": 53, " ": 49, "ArrowDown": 125, "ArrowUp": 126, "ArrowLeft": 123, "ArrowRight": 124, "Home": 115, "End": 119, "Backspace": 51}
			physical := key
			if key == " " {
				physical = "Space"
			}
			p := map[string]any{"type": typ, "key": key, "code": physical, "windowsVirtualKeyCode": code, "nativeVirtualKeyCode": nativeCodes[key], "modifiers": modifiers}
			if typ == "keyDown" && text != "" {
				p["text"] = text
			}
			if err := b.call(ctx, session, "Input.dispatchKeyEvent", p, nil); err != nil {
				return err
			}
		}
		return nil
	case "focus":
		return waitBrowser(ctx, func() (bool, error) {
			v, e := b.evaluate(ctx, session, `!!document.activeElement && document.activeElement.matches(`+jsonText(step.Selector)+`)`)
			return v == true, e
		})
	case "text", "value", "count", "storage":
		expression := `(()=>{const e=` + sel + `;return e?e.textContent:null})()`
		if step.Action == "value" {
			expression = `(()=>{const e=` + sel + `;return e?e.value:null})()`
		}
		if step.Action == "count" {
			expression = `document.querySelectorAll(` + jsonText(step.Selector) + `).length`
		}
		if step.Action == "storage" {
			expression = `JSON.parse(localStorage.getItem(` + jsonText(step.Key) + `))`
		}
		var last any
		err := waitBrowser(ctx, func() (bool, error) {
			var e error
			last, e = b.evaluate(ctx, session, expression)
			if step.Match == "contains" {
				actual, ok := last.(string)
				expected, _ := step.Expected.(string)
				return ok && strings.Contains(actual, expected), e
			}
			return reflect.DeepEqual(last, step.Expected), e
		})
		if err != nil {
			return fmt.Errorf("%s mismatch: expected %s, got %s: %w", step.Action, shorten(jsonText(step.Expected), 256), shorten(jsonText(last), 256), err)
		}
		return nil
	case "upload":
		file := step.File
		if file == "" || !filepath.IsLocal(file) {
			return fmt.Errorf("upload needs a relative filename")
		}
		if step.Value != "" {
			file = filepath.Join(downloads, "upload-"+filepath.Base(file))
			if err := os.WriteFile(file, []byte(step.Value), 0600); err != nil {
				return err
			}
		} else {
			file = filepath.Join(root, file)
			resolved, err := filepath.EvalSymlinks(file)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(resolved, realPath(root)+string(filepath.Separator)) {
				return fmt.Errorf("upload escapes snapshot")
			}
			file = resolved
		}
		var doc struct {
			Root struct {
				NodeID int `json:"nodeId"`
			}
		}
		if err := b.call(ctx, session, "DOM.getDocument", map[string]any{}, &doc); err != nil {
			return err
		}
		var n struct {
			NodeID int `json:"nodeId"`
		}
		if err := b.call(ctx, session, "DOM.querySelector", map[string]any{"nodeId": doc.Root.NodeID, "selector": step.Selector}, &n); err != nil {
			return err
		}
		return b.call(ctx, session, "DOM.setFileInputFiles", map[string]any{"nodeId": n.NodeID, "files": []string{file}}, nil)
	case "download":
		for len(b.events) > 0 {
			<-b.events
		}
		trigger := BrowserStep{Action: "click", Selector: step.Selector}
		if step.Key != "" {
			trigger.Action, trigger.Key = "key", step.Key
		}
		if err := b.step(ctx, session, root, downloads, trigger); err != nil {
			return err
		}
		for {
			select {
			case m := <-b.events:
				var p struct {
					GUID  string `json:"guid"`
					State string `json:"state"`
				}
				json.Unmarshal(m.Params, &p)
				if p.State == "canceled" {
					return fmt.Errorf("download canceled")
				}
				if p.State != "completed" {
					continue
				}
				if !filepath.IsLocal(p.GUID) || strings.ContainsAny(p.GUID, "/\\") {
					return fmt.Errorf("invalid download name")
				}
				f, err := os.Open(filepath.Join(downloads, p.GUID))
				if err != nil {
					return err
				}
				data, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
				f.Close()
				if err != nil {
					return err
				}
				if len(data) > 1<<20 {
					return fmt.Errorf("download exceeds 1 MiB")
				}
				var actual any
				if err = json.Unmarshal(data, &actual); err != nil {
					actual = string(data)
				}
				if !reflect.DeepEqual(actual, step.Expected) {
					return fmt.Errorf("download payload mismatch")
				}
				return nil
			case <-ctx.Done():
				return fmt.Errorf("download did not complete: %w", ctx.Err())
			case <-b.done:
				return fmt.Errorf("Chrome closed")
			}
		}
	}
	return fmt.Errorf("unsupported browser action %q", step.Action)
}

var browserSlots = make(chan struct{}, 2)

func runBrowserSuite(ctx context.Context, root string, suite BrowserSuite) ([]BrowserResult, error) {
	select {
	case browserSlots <- struct{}{}:
		defer func() { <-browserSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := validateBrowserSuite(suite, 240); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	files, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer files.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	origin := "http://" + ln.Addr().String()
	auth := newID() + newID()
	server := &http.Server{ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192, Handler: browserFileHandler(files, auth)}
	go server.Serve(ln)
	defer server.Close()
	dir, err := os.MkdirTemp(filepath.Dir(root), ".browser-runtime-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	b, err := newNativeBrowser(ctx, origin, auth, dir)
	if err != nil {
		return nil, err
	}
	defer b.close()
	_ = b.call(ctx, "", "Target.setDiscoverTargets", map[string]any{"discover": true}, nil)
	if suite.Page == "" {
		suite.Page = "index.html"
	}
	out := []BrowserResult{}
	for i, c := range suite.Cases {
		downloads := filepath.Join(dir, fmt.Sprint(i))
		if err = os.Mkdir(downloads, 0700); err != nil {
			return out, err
		}
		session, id, e := b.newCase(ctx, downloads)
		if e == nil {
			e = b.navigate(ctx, session, suite.Page)
		}
		for j, step := range c.Steps {
			if e != nil {
				break
			}
			stepCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			e = b.step(stepCtx, session, root, downloads, step)
			stop()
			if e != nil {
				e = fmt.Errorf("step %d: %w", j+1, e)
			}
		}
		b.mu.Lock()
		if e == nil && len(b.dialogs[session]) > 0 {
			e = fmt.Errorf("expected dialog did not open")
		}
		delete(b.dialogs, session)
		b.mu.Unlock()
		if id != "" {
			_ = b.call(ctx, "", "Target.disposeBrowserContext", map[string]any{"browserContextId": id}, nil)
		}
		result := BrowserResult{Name: c.Name, Passed: e == nil}
		if e != nil {
			result.Error = shorten(e.Error(), 700)
			b.mu.Lock()
			result.Error += "\n" + shorten(strings.Join(b.diagnostics, "\n"), 1800)
			b.diagnostics = nil
			b.mu.Unlock()
		}
		out = append(out, result)
		if ctx.Err() != nil || b.overflow.Load() {
			return out, fmt.Errorf("browser suite interrupted or event limit exceeded")
		}
	}
	return out, nil
}

func browserCheck(s *Store, session *mcpSession, fs *FS, raw map[string]any) (string, error) {
	suite, specHash, err := resolveBrowserSuite(s, session, fs, raw)
	if err != nil {
		return "", err
	}
	base := suite.Path
	if strings.HasPrefix(base, "@") {
		base, err = s.resolveRef(session.env, base)
		if err != nil {
			return "", err
		}
	}
	if err = os.MkdirAll(s.envDir(session.env), 0700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(s.envDir(session.env), ".browser-input-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if suite.Artifact != "" {
		if suite.Path != "" {
			return "", fmt.Errorf("provide artifact or path, not both")
		}
		artifacts, e := s.artifactDir(session.env)
		if e != nil {
			return "", e
		}
		s.mu.Lock()
		a, e := loadArtifact(artifacts, suite.Artifact, session.env.ID)
		s.mu.Unlock()
		if e != nil {
			return "", e
		}
		// Artifact files are immutable and never writable by an Env process.
		dir = filepath.Join(artifacts, a.ID, "files")
	} else if err = stageFiles(sessionContext(session), fs, base, dir); err != nil {
		return "", err
	}
	snapshot := workspaceSnapshot(sessionContext(session), dir)
	started := time.Now()
	ctx, cancel := context.WithTimeout(sessionContext(session), 120*time.Second)
	defer cancel()
	var results []BrowserResult
	var runErr error
	batches := browserBatches(suite)
	for _, batch := range batches {
		var more []BrowserResult
		more, runErr = runBrowserSuite(ctx, dir, batch)
		results = append(results, more...)
		if runErr != nil {
			break
		}
	}
	exit := 0
	if runErr != nil {
		exit = 1
	}
	for _, r := range results {
		if !r.Passed {
			exit = 1
		}
	}
	out := map[string]any{"cases": results}
	if suite.Spec != "" {
		out["spec"] = suite.Spec
		out["spec_sha256"] = specHash
	}
	if runErr != nil {
		out["environment_error"] = runErr.Error()
	}
	// Evidence contains replayable, resolved input even if the VFS spec changes.
	resolved := suite
	resolved.Spec, resolved.Only = "", nil
	r, err := s.recordRun(session, RunEvidence{Origin: "managed-chrome", Command: jsonText(resolved), Before: snapshot, After: workspaceSnapshot(sessionContext(session), dir), Exit: exit, DurationMS: time.Since(started).Milliseconds(), Output: jsonText(out)})
	if err != nil {
		return "", err
	}
	out["run"] = r.ID
	out["exit"] = exit
	out["snapshot"] = snapshot
	out["batches"] = len(batches)
	if specHash != "" {
		out["spec_sha256"] = specHash
	}
	if suite.Artifact != "" {
		_, attachErr := artifactCall(s, session, fs, "artifact_verify", map[string]any{"id": suite.Artifact, "runs": []string{r.ID}})
		out["artifact"] = suite.Artifact
		if attachErr != nil {
			out["evidence_error"] = attachErr.Error()
		} else {
			out["evidence_attached"] = true
		}
	}
	return jsonText(out), nil
}

func browserPreflight(ctx context.Context) any {
	dir, err := os.MkdirTemp("", "farshmac-browser-probe-*")
	if err != nil {
		return map[string]any{"ready": false, "error": err.Error()}
	}
	defer os.RemoveAll(dir)
	page := `<!doctype html><input id="field"><button id="save" onclick="localStorage.setItem('probe',JSON.stringify(document.querySelector('#field').value))">Save</button><span id="state"></span><input type="file" id="upload" onchange="this.files[0].text().then(t=>document.querySelector('#state').textContent=t)"><button id="download" onclick="let a=document.createElement('a');a.download='probe.json';a.href=window.URL.createObjectURL(new Blob([JSON.stringify({ok:true})]));a.click()">Download</button><select id="choice" onchange="document.querySelector('#chosen').textContent=this.value"><option value="one">One</option><option value="two">Two</option></select><span id="chosen">one</span><script>document.querySelector('#state').textContent=JSON.parse(localStorage.getItem('probe'))||'empty'</script>`
	if err = os.WriteFile(filepath.Join(dir, "index.html"), []byte(page), 0600); err != nil {
		return map[string]any{"ready": false, "error": err.Error()}
	}
	suite := BrowserSuite{Cases: []BrowserCase{{Name: "navigation, JavaScript, persistence, upload and download", Steps: []BrowserStep{
		{Action: "fill", Selector: "#field", Value: "persisted"}, {Action: "click", Selector: "#save"}, {Action: "reload"}, {Action: "text", Selector: "#state", Expected: "persisted"},
		{Action: "upload", Selector: "#upload", File: "probe.txt", Value: "uploaded"}, {Action: "text", Selector: "#state", Expected: "uploaded"}, {Action: "download", Selector: "#download", Expected: map[string]any{"ok": true}},
	}}, {Name: "native keyboard focus and select", Steps: []BrowserStep{{Action: "key", Selector: "#choice", Key: "ArrowDown"}, {Action: "key", Key: "Enter"}, {Action: "focus", Selector: "#choice"}, {Action: "value", Selector: "#choice", Expected: "two"}, {Action: "text", Selector: "#chosen", Expected: "two"}}}, {Name: "fresh context", Steps: []BrowserStep{{Action: "text", Selector: "#state", Expected: "empty"}}}}}
	results, err := runBrowserSuite(ctx, dir, suite)
	ready := err == nil
	nativeSelect := false
	for i, r := range results {
		if r.Name == "native keyboard focus and select" {
			nativeSelect = r.Passed
			if !r.Passed {
				results[i].Error = "Unavailable in this Chrome/macOS runtime. DOM select is supported but does not prove native popup keyboard behavior. Do not repeat native popup probes; report the gap or test supported keyboard controls."
			}
		} else {
			ready = ready && r.Passed
		}
	}
	out := map[string]any{"ready": ready, "engine": chromePath(), "cases": results, "capabilities": map[string]any{"native_select_keyboard": nativeSelect, "text_match": []string{"exact", "contains"}, "focus_emulation": true}}
	if err != nil {
		out["error"] = err.Error()
	}
	return out
}

func (b *nativeBrowser) diagnostic(text string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.diagnostics = append(b.diagnostics, shorten(text, 700))
	if len(b.diagnostics) > 8 {
		b.diagnostics = b.diagnostics[len(b.diagnostics)-8:]
	}
}

func browserFileHandler(files *os.Root, auth string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-FarshMac-Browser-Key") != auth {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "method not allowed", 405)
			return
		}
		w.Header().Set("Content-Security-Policy", "sandbox allow-scripts allow-same-origin allow-forms allow-downloads allow-modals; default-src 'self' data: blob:; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-src 'none'; worker-src 'none'; object-src 'none'; base-uri 'self'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.FileServerFS(files.FS()).ServeHTTP(w, r)
	})
}
