package ui

import (
	"context"
	"farshmac/internal/testnet"
	"github.com/pkg/sftp"
	"io"
	"testing"
	"time"
)

// Unlike waitNetwork, this helper must not acknowledge any metadata prompt.
func waitWithoutMetadataNotice(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for a.Busy && time.Now().Before(deadline) {
		a.Poll()
		if a.Form != nil && a.Form.Title == "Cannot preserve metadata" {
			t.Fatal("unexpected metadata warning", a.Form.Message)
		}
		time.Sleep(time.Millisecond)
	}
	if a.Busy {
		t.Fatal("operation stalled", a.Status)
	}
	waitNetwork(t, a)
}

func TestSameRemoteCopyAndEditorSaveDoNotWarnAboutProtocolLimits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := testnet.SFTP(t)
	seedRemote(t, c)
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.connectRemote(0, c)
	waitNetwork(t, a)
	a.error(a.Panels[0].Load("/folder"))
	waitNetwork(t, a)
	selectEntry(t, &a.Panels[0], "file.txt")
	a.key(Message{Key: "F5", Shift: true})
	a.Form.Fields[0].Value = "copy.txt"
	a.Form.Fields[1].Checked = true
	a.Form.Fields[2].Checked = true
	a.key(Message{Key: "Enter"})
	waitWithoutMetadataNotice(t, a)
	selectEntry(t, &a.Panels[0], "copy.txt")
	a.key(Message{Key: "F4"})
	waitNetwork(t, a)
	a.Doc.Insert("edited ")
	a.Doc.modified()
	want := a.Doc.Text
	a.save()
	waitWithoutMetadataNotice(t, a)
	if a.Doc.Dirty {
		t.Fatal("save failed", a.Status)
	}
	f, err := c.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := f.Open("/folder/copy.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil || string(got) != want {
		t.Fatal("wrong saved content", err, string(got))
	}
}

func TestSameRemoteCopyStillReportsPermissionFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := testnet.SFTP(t, func(h *sftp.Handlers) { h.FileCmd = failSetstat{h.FileCmd} })
	seedRemote(t, c)
	a, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.connectRemote(0, c)
	waitNetwork(t, a)
	a.error(a.Panels[0].Load("/folder"))
	waitNetwork(t, a)
	selectEntry(t, &a.Panels[0], "file.txt")
	a.key(Message{Key: "F5", Shift: true})
	a.Form.Fields[0].Value = "copy.txt"
	a.Form.Fields[1].Checked = true
	a.key(Message{Key: "Enter"})
	deadline := time.Now().Add(5 * time.Second)
	for a.Busy && time.Now().Before(deadline) {
		a.Poll()
		if a.Form != nil && a.Form.Title == "Cannot preserve metadata" {
			if a.Form.Choice[0] == "Continue" {
				t.Fatal("generic protocol notice returned", a.Form.Message)
			}
			chooseOperation(t, a, "Cancel")
			waitNetwork(t, a)
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("permission failure not reported", a.Status)
}
