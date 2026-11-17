//go:build linux

package podtoast

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestObservationTargetsCgroupMembership(t *testing.T) {
	root := t.TempDir()
	cg := filepath.Join(root, "sys/fs/cgroup")
	if e := os.MkdirAll(cg, 0755); e != nil {
		t.Fatal(e)
	}
	for name, value := range map[string]string{"cgroup.procs": "99\n", "cpu.stat": "usage_usec 400\n", "memory.current": "1024\n"} {
		if e := os.WriteFile(filepath.Join(cg, name), []byte(value), 0600); e != nil {
			t.Fatal(e)
		}
	}
	s := Server{Root: root, TargetPID: 1}
	snap := s.snapshot(context.Background())
	if snap.Resources.Unavailable == "" {
		t.Fatal("reported another container's cgroup")
	}
	if e := os.WriteFile(filepath.Join(cg, "cgroup.procs"), []byte("1\n"), 0600); e != nil {
		t.Fatal(e)
	}
	snap = s.snapshot(context.Background())
	if snap.Resources.Value == nil {
		t.Fatal(snap.Resources)
	}
}
func TestSocketAddresses(t *testing.T) {
	if got := socketAddress("0100007F:1F90"); got != "127.0.0.1:8080" {
		t.Fatal(got)
	}
	if got := socketAddress("00000000000000000000000001000000:0050"); got != "[::1]:80" {
		t.Fatal(got)
	}
}
func TestProcessStatWithSpacesAndParentheses(t *testing.T) {
	p, e := parseProcess(7, "7 (name ) with spaces) S "+strings.Repeat("0 ", 21))
	if e != nil || p.Name != "name ) with spaces" {
		t.Fatal(p, e)
	}
}
