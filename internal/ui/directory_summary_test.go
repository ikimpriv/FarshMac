package ui

import (
	"farshmac/internal/fsops"
	"farshmac/internal/podtoast"
	"farshmac/internal/remotefs"
	"testing"
)

type summaryFixture struct {
	remotefs.FS
	path  string
	calls int
}

func (f *summaryFixture) DirectorySummary(p string, ignore bool, fn func(podtoast.DirectorySummary) error) (podtoast.DirectorySummary, error) {
	f.path = p
	f.calls++
	s := podtoast.DirectorySummary{Files: 3, Directories: 2, Bytes: 4096, Done: true}
	if fn != nil {
		if err := fn(s); err != nil {
			return s, err
		}
	}
	return s, nil
}
func TestContainerSummaryPathAndSize(t *testing.T) {
	fixture := &summaryFixture{}
	prefixed := prefixFS{FS: fixture, prefix: "/ns/pods/p/app"}
	sizes, err := measureRemoteFolders("/ns/pods/p/app", []string{"data"}, fsops.NewOperation("test"), prefixed, nil)
	if err != nil || fixture.path != "/data" || fixture.calls != 1 || sizes["data"].Bytes != 4096 {
		t.Fatalf("%+v path=%s err=%v", sizes, fixture.path, err)
	}
	// Docker's ownership wrapper must preserve the optional capability.
	docker := dockerAgentFS{FS: fixture}
	if _, err = docker.DirectorySummary("/docker", false, nil); err != nil || fixture.path != "/docker" {
		t.Fatal(fixture.path, err)
	}
}
