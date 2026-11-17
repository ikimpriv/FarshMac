package ui

import (
	"context"
	"farshmac/internal/remotefs"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type markdownLocalOpener struct{}

func (markdownLocalOpener) Identity() string                          { return "markdown-test" }
func (markdownLocalOpener) Open(context.Context) (remotefs.FS, error) { return remotefs.Local{}, nil }
func TestMarkdownImagesLocalAndRemote(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "chart space.png")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	picture := image.NewRGBA(image.Rect(0, 0, 20, 12))
	picture.Set(1, 1, color.RGBA{R: 255, A: 255})
	if err = png.Encode(f, picture); err != nil {
		t.Fatal(err)
	}
	f.Close()
	for _, opener := range []remotefs.Opener{nil, markdownLocalOpener{}} {
		resolve := markdownImages(context.Background(), filepath.Join(dir, "README.md"), opener)
		if got := resolve("chart%20space.png"); !strings.HasPrefix(got, "data:image/png;base64,") {
			t.Fatalf("image not loaded: %.80s", got)
		}
		if got := resolve("https://example.com/image.png"); got != "" {
			t.Fatal("external image loaded")
		}
		if got := resolve("missing.png"); got != "" {
			t.Fatal("missing image loaded")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := markdownImages(ctx, filepath.Join(dir, "README.md"), nil)("chart%20space.png"); got != "" {
		t.Fatal("cancel ignored")
	}
}
func TestMarkdownImagePaths(t *testing.T) {
	for _, bad := range []string{"https://example.com/x.png", "//example.com/x.png", "data:image/png;base64,abc", "file:///etc/x.png", "a%00.png", ""} {
		if _, ok := markdownImagePath("/docs/README.md", bad, false); ok {
			t.Fatal(bad)
		}
	}
	if got, ok := markdownImagePath("/containers/app/docs/README.md", "../assets/a%20b.png", true); !ok || got != "/containers/app/assets/a b.png" {
		t.Fatal(got, ok)
	}
}
