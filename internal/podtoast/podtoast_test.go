package podtoast

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func testClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	a, b := net.Pipe()
	go Serve(b, h)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, e := Connect(ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func TestFileOperationsAndBinaryNames(t *testing.T) {
	root := t.TempDir()
	c := testClient(t, &Server{Root: root})
	f := c.FS(context.Background())
	if e := f.Mkdir("/folder"); e != nil {
		t.Fatal(e)
	}
	name := "/folder/spaces ' \n unicode-λ"
	if runtime.GOOS == "linux" {
		name += "-\xff"
	}
	data := bytes.Repeat([]byte{0, 1, 2, 255, '\n'}, 100000)
	w, e := f.Create(name)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = w.Write(data); e != nil {
		t.Fatal(e)
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	entries, e := f.ReadDir("/folder")
	if e != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(name) {
		t.Fatalf("listing %+v %v", entries, e)
	}
	r, e := f.Open(name)
	if e != nil {
		t.Fatal(e)
	}
	got, e := io.ReadAll(r)
	r.Close()
	if e != nil || !bytes.Equal(got, data) {
		t.Fatalf("binary transfer: %v", e)
	}
	w, e = f.Create(name)
	if e != nil {
		t.Fatal(e)
	}
	_ = w.Close()
	if !errors.Is(w.Close(), os.ErrExist) {
		t.Fatal("exclusive create must refuse overwrite")
	}
	if e = f.Chmod(name, 0640); e != nil {
		t.Fatal(e)
	}
	stamp := time.Unix(1234567890, 0)
	if e = f.Chtimes(name, stamp); e != nil {
		t.Fatal(e)
	}
	st, e := f.Stat(name)
	if e != nil || st.Mode().Perm() != 0640 || !st.ModTime().Equal(stamp) {
		t.Fatalf("stat %v %v", st, e)
	}
	if e = f.CopyFile(name, "/folder/copy", ""); e != nil {
		t.Fatal(e)
	}
	if e = f.Rename("/folder/copy", "/folder/renamed"); e != nil {
		t.Fatal(e)
	}
	if e = f.Symlink("renamed", "/folder/link"); e != nil {
		t.Fatal(e)
	}
	link, e := f.Readlink("/folder/link")
	if e != nil || link != "renamed" {
		t.Fatal(link, e)
	}
	st, e = f.Stat("/folder/link")
	if e != nil || st.Size() != int64(len(data)) {
		t.Fatal(st, e)
	}
	if e = f.Remove("/folder", true); e == nil {
		t.Fatal("nonempty directory removed")
	}
	if e = f.Remove("/folder/link", false); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Lstat("/missing"); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
}
func TestCancelOneStreamDoesNotBlockBrowsing(t *testing.T) {
	entered := make(chan struct{})
	stopped := make(chan struct{})
	server := &Server{Root: t.TempDir()}
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			close(entered)
			<-r.Context().Done()
			close(stopped)
			return
		}
		server.ServeHTTP(w, r)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.JSON(ctx, "GET", "/slow", nil, nil) }()
	<-entered
	browseCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if _, e := c.FS(browseCtx).ReadDir("/"); e != nil {
		t.Fatal(e)
	}
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation blocked")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("server did not receive cancellation")
	}
	if _, e := c.FS(browseCtx).ReadDir("/"); e != nil {
		t.Fatal(e)
	}
}
func TestPagedListingConcurrentOperations(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 2000; i++ {
		if e := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%04d", i)), nil, 0600); e != nil {
			t.Fatal(e)
		}
	}
	c := testClient(t, &Server{Root: root})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entries, e := c.FS(ctx).ReadDir("/")
			if e != nil || len(entries) != 2000 {
				t.Errorf("entries %d: %v", len(entries), e)
			}
		}()
	}
	wg.Wait()
}
func TestDirectoryStreamMustFinish(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/hello" {
			reply(w, Capability{Version: Version, Files: true})
			return
		}
		fmt.Fprintln(w, `{"entry":{"name":"partial"}}`)
	}))
	if _, e := c.FS(context.Background()).ReadDir("/"); e == nil {
		t.Fatal("truncated listing reported success")
	}
}
func TestProbeLimitsAndHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201); fmt.Fprintln(w, "hello") }))
	defer server.Close()
	result, e := runProbe(context.Background(), Probe{Kind: "load", Target: server.URL, Requests: 6, Concurrency: 2, Rate: 1000})
	if e != nil || result.Completed != 6 || result.Status[201] != 6 {
		t.Fatalf("%+v %v", result, e)
	}
	if _, e = runProbe(context.Background(), Probe{Kind: "load", Target: server.URL, Requests: 10001, Concurrency: 1, Rate: 1}); e == nil {
		t.Fatal("unbounded load accepted")
	}
	if _, e = runProbe(context.Background(), Probe{Kind: "http", Target: "file:///etc/passwd"}); e == nil {
		t.Fatal("non-HTTP probe accepted")
	}
}
func TestReadTrailerError(t *testing.T) {
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/hello" {
			reply(w, Capability{Version: Version, Files: true})
			return
		}
		w.Header().Set("Trailer", "Podtoast-Error")
		fmt.Fprint(w, "partial")
		w.Header().Set("Podtoast-Error", "disk read failed")
	}))
	r, e := c.FS(context.Background()).Open("/file")
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	_, e = io.ReadAll(r)
	if e == nil || !strings.Contains(e.Error(), "disk read failed") {
		t.Fatal(e)
	}
}

func TestListingIncludesLinkTypesInOneRequest(t *testing.T) {
	root := t.TempDir()
	if e := os.Mkdir(filepath.Join(root, "folder"), 0700); e != nil {
		t.Fatal(e)
	}
	for name, target := range map[string]string{"directory-link": "folder", "broken-link": "missing"} {
		if e := os.Symlink(target, filepath.Join(root, name)); e != nil {
			t.Fatal(e)
		}
	}
	c := testClient(t, &Server{Root: root})
	entries, e := c.FS(context.Background()).ReadDir("/")
	if e != nil {
		t.Fatal(e)
	}
	for _, info := range entries {
		entry := info.(Entry)
		if entry.RawName != nil {
			t.Fatal("ordinary name duplicated in binary field")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			dir, known := entry.LinkIsDir()
			if !known || dir != (entry.Name() == "directory-link") {
				t.Fatalf("missing/wrong link metadata: %+v", entry)
			}
		}
	}
	if len(entries) != 3 {
		t.Fatalf("truncated listing: %d", len(entries))
	}
}
