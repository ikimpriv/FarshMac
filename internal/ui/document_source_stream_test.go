package ui

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
)

type streamDocumentFS struct {
	remotefs.FS
	size    int64
	readErr error
}

func (f streamDocumentFS) Lstat(string) (os.FileInfo, error) {
	return streamDocumentInfo{FileInfo: mustStreamInfo(), size: f.size}, nil
}
func mustStreamInfo() os.FileInfo {
	i, _ := (fstest.MapFS{"cpuinfo": &fstest.MapFile{Data: nil, Mode: 0444}}).Stat("cpuinfo")
	return i
}

type streamDocumentInfo struct {
	os.FileInfo
	size int64
}

func (i streamDocumentInfo) Size() int64 { return i.size }
func (f streamDocumentFS) Open(string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("processor: 0\n")), f.readErr
}
func (f streamDocumentFS) Close() error { return nil }

type streamDocumentOpener struct{ fs streamDocumentFS }

func (o streamDocumentOpener) Open(context.Context) (remotefs.FS, error) { return o.fs, nil }

func TestRemoteDocumentGeneratedZeroSize(t *testing.T) {
	for _, size := range []int64{0, 13, 42} {
		o := fsops.NewOperation("View")
		source := &remoteSource{Opener: streamDocumentOpener{streamDocumentFS{size: size}}, Path: "/proc/cpuinfo"}
		local, _, err := source.Fetch(o, t.TempDir())
		o.Cancel()
		if size == 42 {
			if err == nil || !strings.Contains(err.Error(), "changed during download") {
				t.Fatalf("sized file mismatch: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(local)
		if err != nil || string(data) != "processor: 0\n" {
			t.Fatalf("content %q: %v", data, err)
		}
	}
}

func (o streamDocumentOpener) Identity() string { return "generated-test" }
