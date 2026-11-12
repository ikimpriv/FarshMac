package ui

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"farshmac/internal/preview"
)

// TestMain lets the test binary serve as the sandboxed preview helper, which
// the image view starts through os.Executable().
func TestMain(m *testing.M) {
	if (len(os.Args) == 3 || len(os.Args) == 4) && os.Args[1] == preview.HelperFlag {
		pixels := 0
		if len(os.Args) == 4 {
			pixels, _ = strconv.Atoi(os.Args[3])
		}
		os.Exit(preview.HelperMain(os.Args[2], pixels))
	}
	os.Exit(m.Run())
}

func TestEnterOnAppBundleEntersIt(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "Foo.app")
	if err := os.MkdirAll(filepath.Join(bundle, "Contents"), 0700); err != nil {
		t.Fatal(err)
	}
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	launched := false
	a.runExternal = func(context.Context, []string) ([]byte, error) {
		launched = true
		return nil, nil
	}
	p := &a.Panels[0]
	selectEntry(t, p, "Foo.app")
	a.key(Message{Key: "Enter"})
	if launched || p.Path != bundle {
		t.Fatal("Enter must enter the bundle, not launch it", launched, p.Path)
	}
	a.error(p.Load(root))
	selectEntry(t, p, "Foo.app")
	a.key(Message{Key: "Enter", Shift: true})
	waitWork(t, a)
	if !launched || p.Path != root {
		t.Fatal("Shift+Enter must launch the bundle", launched, p.Path)
	}
}

func writeTestPNG(t *testing.T, path string) {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 40, 30))
	im.Set(1, 1, color.White)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	png.Encode(f, im)
	f.Close()
}

func TestF3OnImageOpensGraphicalView(t *testing.T) {
	root := t.TempDir()
	writeTestPNG(t, filepath.Join(root, "photo.png"))
	a, err := New(root, root)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Cols, a.Rows = 100, 40
	selectEntry(t, &a.Panels[0], "photo.png")
	a.key(Message{Key: "F3"})
	if a.Image == nil || a.Doc != nil || a.Image.Path != filepath.Join(root, "photo.png") {
		t.Fatal("F3 on an image must open the image view", a.Image, a.Doc, a.Status)
	}
	// The backdrop is black; keys are not panel keys while the view is open.
	g := a.Draw()
	if cell := g.Cells[(g.H/4)*g.W+g.W/4]; cell.BG != Black || strings.TrimSpace(cell.Text) != "" {
		t.Fatalf("backdrop %+v", cell)
	}
	if u := a.Poll(); u.PanelKeys || !u.Document || u.Title != "photo.png" {
		t.Fatal("update flags", u.PanelKeys, u.Document, u.Title)
	}
	deadline := time.Now().Add(10 * time.Second)
	for a.Image != nil && a.Image.Data == "" && time.Now().Before(deadline) {
		a.Poll()
		time.Sleep(20 * time.Millisecond)
	}
	if a.Image == nil || a.Image.Data == "" {
		t.Fatal("image not decoded", a.Status)
	}
	if !strings.Contains(a.Image.Caption, "40 × 30") {
		t.Fatal("caption", a.Image.Caption)
	}
	a.previewSent = "" // The polls above already carried the payload once.
	a.dirty = true
	u := a.Poll()
	if u.Preview == nil || !u.Preview.Full || u.Preview.W != 100 || u.Preview.H != 40 || !strings.HasPrefix(u.Preview.Data, "data:image/") {
		t.Fatalf("overlay %+v", u.Preview)
	}
	a.dirty = true
	if u = a.Poll(); u.Preview == nil || u.Preview.Data != "" {
		t.Fatal("payload must be sent once")
	}
	a.key(Message{Key: "ArrowDown"}) // ignored
	a.key(Message{Key: "Escape"})
	if a.Image != nil {
		t.Fatal("Esc did not close the image view")
	}
	a.dirty = true
	if u = a.Poll(); u.Preview != nil || !u.PanelKeys {
		t.Fatal("overlay must disappear with the view")
	}
	// A text file still opens the viewer; a missing image reports and closes.
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hi"), 0644)
	a.error(a.Panels[0].Load(root))
	selectEntry(t, &a.Panels[0], "notes.txt")
	a.key(Message{Key: "F3"})
	if a.Doc == nil || a.Image != nil {
		t.Fatal("text file must open the text viewer")
	}
	a.key(Message{Key: "Escape"})
	os.WriteFile(filepath.Join(root, "broken.jpg"), []byte("not a jpeg"), 0644)
	a.error(a.Panels[0].Load(root))
	selectEntry(t, &a.Panels[0], "broken.jpg")
	a.key(Message{Key: "F3"})
	deadline = time.Now().Add(10 * time.Second)
	for a.Image != nil && !a.Image.Failed && time.Now().Before(deadline) {
		a.Poll()
		time.Sleep(20 * time.Millisecond)
	}
	if a.Image == nil || !a.Image.Failed || !strings.Contains(a.Status, "broken.jpg") {
		t.Fatal("damaged image must be reported", a.Status)
	}
	a.key(Message{Key: "F3"})
	if a.Image != nil {
		t.Fatal("F3 did not close the failed view")
	}
}
