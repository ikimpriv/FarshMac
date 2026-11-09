package ui

import "testing"

func TestImagePreviewVisibilityAndPayload(t *testing.T) {
	a := &App{Cols: 100, Rows: 32, previewImage: "data:image/png;base64,abc", previewPath: "/tmp/photo.png"}
	a.Panels[0] = Panel{Path: "/tmp", Entries: []Entry{{Name: "photo.png"}}}
	a.Panels[1].View = "Quick view"
	p := a.imagePreview()
	if p == nil || p.Data == "" || p.X != 52 || p.W != 46 {
		t.Fatal("preview placement", p)
	}
	if p = a.imagePreview(); p == nil || p.Data != "" {
		t.Fatal("thumbnail retransmitted")
	}
	a.Menu = &MenuState{}
	if a.imagePreview() != nil {
		t.Fatal("preview covers menu")
	}
	a.Menu = nil
	if p = a.imagePreview(); p == nil || p.Data == "" {
		t.Fatal("preview not restored")
	}
	a.Panels[0].Entries[0].Name = "other.png"
	if a.imagePreview() != nil {
		t.Fatal("stale preview")
	}
	a.TermVisible = true
	if a.imagePreview() != nil {
		t.Fatal("preview covers terminal")
	}
}
