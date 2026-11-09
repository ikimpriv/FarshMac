package ui

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Cross the real Objective-C JSON -> Go boundary. Constructing Message directly
// cannot catch NSNumber(int) flags that json.Unmarshal rejects as non-booleans.
func TestNativeOptionJSONStartsFastFind(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "panel-keys")
	build := exec.Command("clang", "-framework", "Cocoa", "../native/tests/panel_keys.m", "-o", binary)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("native test build: %v\n%s", err, out)
	}
	out, err := exec.Command(binary, "--json").Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected five key events, got %q", out)
	}
	root := t.TempDir()
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, line := range lines {
		var m Message
		if err = json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("native event rejected: %s: %v", line, err)
		}
		a.FastFind = nil
		a.Panels[a.Active].Entries = []Entry{{Name: "unrelated"}, {Name: m.Key + "target"}}
		a.Panels[a.Active].Cursor = 0
		a.Handle(line)
		if a.FastFind == nil || a.FastFind.Query != m.Key || a.Panels[a.Active].Cursor != 1 {
			t.Fatalf("native event did not start search: %s, query %#v", line, a.FastFind)
		}
		if a.Poll().Grid == "" {
			t.Fatal("search did not redraw the panel")
		}
	}
}
