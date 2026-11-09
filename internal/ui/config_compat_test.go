package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenamedConfigFallsBackWithoutOverwriting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, "Far Go")
	current := filepath.Join(root, "FarshMac")
	if err = os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(current, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"setup.json", "session.json", "connections.json"} {
		if err = os.WriteFile(filepath.Join(legacy, name), []byte("legacy"), 0600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(current, name)
		if data, err := readConfigFile(path); err != nil || string(data) != "legacy" {
			t.Fatalf("fallback %s: %q %v", name, data, err)
		}
		if err = os.WriteFile(path, []byte("new"), 0600); err != nil {
			t.Fatal(err)
		}
		if data, err := readConfigFile(path); err != nil || string(data) != "new" {
			t.Fatalf("priority %s: %q %v", name, data, err)
		}
		if data, _ := os.ReadFile(filepath.Join(legacy, name)); string(data) != "legacy" {
			t.Fatal("modified legacy settings")
		}
	}
}

// Explicit session paths must not accidentally import the user's old session.
func TestRenamedConfigDoesNotFallbackForCustomPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, "Far Go")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "session.json"), []byte("legacy"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfigFile(filepath.Join(t.TempDir(), "session.json")); !os.IsNotExist(err) {
		t.Fatalf("custom path unexpectedly fell back: %v", err)
	}
}
