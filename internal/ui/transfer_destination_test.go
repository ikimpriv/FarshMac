package ui

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"farshmac/internal/remotefs"
	"farshmac/internal/testnet"
)

func transferTo(t *testing.T, a *App, key, destination string) {
	t.Helper()
	a.key(Message{Key: key})
	switch {
	case a.Form != nil:
		a.Form.Fields[0].Value = destination
	case a.Dialog != nil:
		a.Dialog.Input = destination
	default:
		t.Fatal("no transfer dialog")
	}
	a.key(Message{Key: "Enter"})
}

func TestRemoteDestinationRejectsTilde(t *testing.T) {
	left := t.TempDir()
	if err := os.WriteFile(filepath.Join(left, "file"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(left, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Panels[1].Remote = &remotefs.Config{Protocol: "sftp", Host: "example.invalid", Port: 22, Directory: "/"}
	a.Panels[1].Path = "/"
	for _, input := range []string{"~/dir", "~", "~user/dir"} {
		a.Status = ""
		selectEntry(t, &a.Panels[0], "file")
		transferTo(t, a, "F5", input)
		if a.Busy || a.Status != remoteTildeStatus {
			t.Fatalf("%q: busy=%v status=%q", input, a.Busy, a.Status)
		}
	}
}

func TestTrailingSlashCreatesContainer(t *testing.T) {
	for _, key := range []string{"F5", "F6"} {
		t.Run(key, func(t *testing.T) {
			left, right := t.TempDir(), t.TempDir()
			for _, name := range []string{"one", "two", "three"} {
				if err := os.WriteFile(filepath.Join(left, name), []byte(name), 0600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := New(left, right)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			selectEntry(t, &a.Panels[0], "one")
			transferTo(t, a, key, filepath.Join(right, "single")+"/")
			waitWork(t, a)
			a.Panels[0].Marked = map[string]bool{"two": true, "three": true}
			transferTo(t, a, key, filepath.Join(right, "nested", "many")+"/")
			waitWork(t, a)
			for _, path := range []string{"single/one", "nested/many/two", "nested/many/three"} {
				if b, err := os.ReadFile(filepath.Join(right, path)); err != nil || string(b) != filepath.Base(path) {
					t.Fatalf("%s: %q %v status %s form %+v", path, b, err, a.Status, a.Form)
				}
			}
			// A trailing slash on an existing file is an error, never an overwrite.
			if err := os.WriteFile(filepath.Join(left, "four"), []byte("four"), 0600); err != nil {
				t.Fatal(err)
			}
			a.refresh()
			selectEntry(t, &a.Panels[0], "four")
			transferTo(t, a, key, filepath.Join(right, "single", "one")+"/")
			waitWork(t, a)
			if b, err := os.ReadFile(filepath.Join(right, "single", "one")); err != nil || string(b) != "one" {
				t.Fatal("existing file changed", err)
			}
		})
	}
}

func TestTrailingSlashCreatesRemoteContainer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := testnet.SFTP(t)
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "one"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := New(local, local)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.connectRemote(1, c)
	waitNetwork(t, a)
	selectEntry(t, &a.Panels[0], "one")
	transferTo(t, a, "F5", "/backup/")
	waitNetwork(t, a)
	f, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := f.Open("/backup/one")
	if err != nil {
		t.Fatal(err, a.Status)
	}
	defer r.Close()
	if b, err := io.ReadAll(r); err != nil || string(b) != "one" {
		t.Fatalf("%q %v", b, err)
	}
}
