package ui

import (
	"testing"

	"farshmac/internal/remotefs"
)

func TestSortMenuRefusesMetadataSortsOnRemote(t *testing.T) {
	a := formTestApp(t)
	p := &a.Panels[0]
	p.Sort = "Size"
	p.Reverse = true
	a.sortMenu(0)
	if a.Form.Choice[2] != "✓ Size" || a.Form.Choice[0] != "  Name" || a.Form.Choice[4] != "✓ Reverse order" || a.Form.Focus != 2 {
		t.Fatal("sort state not shown", a.Form.Choice)
	}
	a.Form = nil
	p.Remote = &remotefs.Config{Protocol: "sftp", Host: "example.org", Port: 22}
	a.sortMenu(0)
	a.Form.Focus = 9
	a.key(Message{Key: "Enter"})
	if p.Sort != "Size" || a.Status != "This sort mode requires local file metadata" {
		t.Fatal("metadata sort applied on remote", p.Sort)
	}
}
