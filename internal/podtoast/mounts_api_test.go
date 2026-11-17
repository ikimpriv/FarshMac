package podtoast

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestListingIncludesMountIdentity(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"proc/42", "data"} {
		if err := os.MkdirAll(filepath.Join(root, p), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "proc/42/mountinfo"), []byte("1 0 0:1 / / rw - overlay overlay rw\n2 1 0:2 / /data rw - ext4 /dev/a rw\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data/file"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	s := &Server{Root: root, TargetPID: 42}
	for _, dir := range []string{"/", "/data"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", "/v1/list?path="+dir, nil))
		d := json.NewDecoder(w.Body)
		found := false
		for {
			var row Result
			if err := d.Decode(&row); err != nil {
				t.Fatal(err)
			}
			if row.Text == "done" {
				break
			}
			if row.Entry == nil {
				t.Fatal("missing entry")
			}
			e := row.Entry
			if dir == "/" && e.Name() == "data" || dir == "/data" && e.Name() == "file" {
				found = true
				if e.Mount == nil || e.Mount.Path != "/data" || e.Mount.Filesystem != "ext4" || e.MountPoint != (dir == "/") {
					t.Fatalf("%s: %+v", dir, e)
				}
			}
		}
		if !found {
			t.Fatal("missing fixture")
		}
	}
}
