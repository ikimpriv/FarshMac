//go:build linux

package podtoast

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrafficOverviewParsing(t *testing.T) {
	root := t.TempDir()
	for _, proto := range []string{"tcp", "tcp6", "udp", "udp6"} {
		local, remote, state := "0100007F:216A", "0200007F:C350", "01"
		if strings.HasSuffix(proto, "6") {
			local = "00000000000000000000000001000000:216A"
			remote = "00000000000000000000000000000000:0000"
		}
		if strings.HasPrefix(proto, "udp") {
			state = "07"
		}
		text := "header\n 0: " + local + " " + remote + " " + state + " 00000000:00000000 00:00000000 00000000 0 0 12345\n"
		if e := os.WriteFile(filepath.Join(root, proto), []byte(text), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.WriteFile(filepath.Join(root, "dev"), []byte("header\n lo: 9000 0 0 0 0 0 0 0 9000 0 0 0 0 0 0 0\n eth0: 1000 1 0 0 0 0 0 0 2000 1 0 0 0 0 0 0\n"), 0600); e != nil {
		t.Fatal(e)
	}
	got := readTrafficOverview(context.Background(), root)
	if len(got.Sockets) != 4 || got.Sockets[0].Local != "127.0.0.1:8554" || got.Sockets[1].Local != "[::1]:8554" || got.Sockets[2].State != "UNCONNECTED" {
		t.Fatalf("socket parsing: %+v", got)
	}
	if !got.CountersValid || got.Sent != 2000 || got.Received != 1000 {
		t.Fatalf("interface scope: %+v", got)
	}
	missing := readTrafficOverview(context.Background(), t.TempDir())
	if missing.CountersValid || len(missing.Warnings) == 0 {
		t.Fatal("missing data represented as valid zero")
	}
}
