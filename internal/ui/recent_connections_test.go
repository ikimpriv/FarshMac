package ui

import (
	"farshmac/internal/remotefs"
	"farshmac/internal/testnet"
	"fmt"
	"strings"
	"testing"
)

func TestRecentConnectionsKeepAllLocationsButShowFive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for i := 0; i < 8; i++ {
		c := remotefs.Config{Protocol: "sftp", Host: fmt.Sprintf("server-%d.invalid", i), Port: 22, User: "test", Password: "secret", Directory: "/"}
		p := Panel{Remote: &c, Path: fmt.Sprintf("/location/%d", i), Marked: map[string]bool{}}
		r, ok := panelConnection(&p)
		if !ok {
			t.Fatal("missing connection")
		}
		if err := saveRecentConnection(r, true); err != nil {
			t.Fatal(err)
		}
	}
	records := readRecentConnections()
	if len(records) != 8 {
		t.Fatalf("locations lost: %d", len(records))
	}
	b, err := readConfigFile(recentConnectionsPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret") {
		t.Fatal("credentials persisted")
	}
	a := &App{}
	items := a.connectionMenuItems(0)
	found := 0
	for _, item := range items {
		if strings.Contains(item.Label, ".invalid") {
			found++
		}
	}
	if found != 5 {
		t.Fatalf("recent menu has %d connections", found)
	}
	r := records[0]
	r.Path = "/changed"
	used := r.Used
	if err := saveRecentConnection(r, false); err != nil {
		t.Fatal(err)
	}
	if got := readRecentConnections()[0]; got.Path != "/changed" || got.Used != used {
		t.Fatal("location save changed recency")
	}
}

func TestRecentRemoteConnectionStartsWithoutSettingsForm(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	c := testnet.SFTP(t)
	r := recentConnection{Kind: "remote", Key: c.Label(), Label: c.Label(), Path: "/", Remote: &c}
	a.openRecentConnection(0, r)
	if a.Form != nil {
		t.Fatal("recent connection opened settings instead of connecting")
	}
	if a.task == nil {
		t.Fatal("connection did not start")
	}
	waitNetwork(t, a)
	if a.Panels[0].Remote == nil {
		t.Fatal("connection not restored", a.Status)
	}
}
