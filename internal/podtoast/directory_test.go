package podtoast

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDirectorySummaryStream(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"a": "123", "sub/b": "4567", ".DS_Store": "ignored"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(".", filepath.Join(root, "loop")); err != nil {
		t.Fatal(err)
	}
	c := testClient(t, &Server{Root: root})
	frames := 0
	result, err := c.FS(context.Background()).DirectorySummary("/", true, func(s DirectorySummary) error { frames++; return nil })
	if err != nil || !result.Done || result.Files != 3 || result.Directories != 1 || result.Links != 1 || result.Bytes != 8 || frames < 2 {
		t.Fatalf("%+v frames=%d: %v", result, frames, err)
	}
}

func TestDirectoryScanCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := scanDirectory(ctx, t.TempDir(), false, func(DirectorySummary) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) || s.Files != 0 {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestDirectoryRequestCancellationAndTruncation(t *testing.T) {
	done := make(chan struct{})
	server := &Server{Root: t.TempDir()}
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/directory-summary" {
			server.ServeHTTP(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(DirectorySummary{})
		_ = http.NewResponseController(w).Flush()
		<-r.Context().Done()
		close(done)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	_, err := c.FS(ctx).DirectorySummary("/", false, func(DirectorySummary) error { cancel(); return context.Canceled })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("agent request not cancelled")
	}
	truncated := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/directory-summary" {
			server.ServeHTTP(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(DirectorySummary{Files: 2})
	}))
	_, err = truncated.FS(context.Background()).DirectorySummary("/", false, nil)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
}

func TestOldAgentDirectoryFallback(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/hello" {
			t.Error("unsupported endpoint was requested")
		}
		reply(w, Capability{Version: Version, Files: true})
	}))
	_, err := c.FS(context.Background()).DirectorySummary("/", false, nil)
	if !errors.Is(err, ErrDirectorySummaryUnsupported) {
		t.Fatal(err)
	}
}

func TestDrainKeepsBorrowedFilesystemAlive(t *testing.T) {
	c := testClient(t, &Server{Root: t.TempDir()})
	f := c.FS(context.Background())
	c.Drain()
	if c.Healthy() {
		t.Fatal("draining agent advertised for new operations")
	}
	if _, err := f.Lstat("/"); err != nil {
		t.Fatal("borrowed filesystem aborted", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	var cap Capability
	if err := c.JSON(context.Background(), "GET", "/v1/hello", nil, &cap); err == nil {
		t.Fatal("idle drained connection remained open")
	}
}
