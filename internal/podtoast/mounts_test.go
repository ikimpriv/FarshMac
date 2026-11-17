package podtoast

import (
	"strings"
	"testing"
)

func TestMountLocations(t *testing.T) {
	mounts := readMountLocations(strings.NewReader("1 0 0:1 / / rw - overlay overlay rw\n2 1 0:2 / /data rw - ext4 /dev/a rw\n3 2 0:3 / /data/cache\\040files rw - tmpfs tmpfs rw\n4 1 0:4 / /etc/hosts ro - ext4 /dev/a ro\n"))
	for filename, want := range map[string]string{"/data/x": "/data", "/database/x": "/", "/data/cache files/x": "/data/cache files", "/etc/hosts": "/etc/hosts"} {
		m := mountAt(mounts, filename)
		if m == nil || m.Path != want {
			t.Fatalf("%s: %+v", filename, m)
		}
	}
	if got := readMountLocations(strings.NewReader(strings.Repeat("x", (1<<20)+1))); got != nil {
		t.Fatal("truncated mount table accepted")
	}
}
