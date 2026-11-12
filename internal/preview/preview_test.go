package preview

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if (len(os.Args) == 3 || len(os.Args) == 4) && os.Args[1] == HelperFlag {
		pixels := 0
		if len(os.Args) == 4 {
			pixels, _ = strconv.Atoi(os.Args[3])
		}
		os.Exit(HelperMain(os.Args[2], pixels))
	}
	os.Exit(m.Run())
}
func TestThumbnailBoundedAndCancelled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "photo.png")
	im := image.NewRGBA(image.Rect(0, 0, 2000, 1000))
	im.Set(0, 0, color.White)
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	png.Encode(f, im)
	f.Close()
	r, e := Run(context.Background(), path)
	if e != nil {
		t.Fatal(e)
	}
	if r.Width != 2000 || r.Height != 1000 {
		t.Fatal(r.Width, r.Height)
	}
	raw, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(r.Data, "data:image/png;base64,"))
	if e != nil {
		t.Fatal(e)
	}
	small, e := png.DecodeConfig(bytes.NewReader(raw))
	if e != nil || small.Width > 768 || small.Height > 768 {
		t.Fatal("unbounded thumbnail", small, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = Run(ctx, path); e == nil {
		t.Fatal("cancel ignored")
	}
	os.WriteFile(path, []byte("invalid image"), 0600)
	if _, e = Run(context.Background(), path); e == nil {
		t.Fatal("corrupt image accepted")
	}
}

func TestPDFPreview(t *testing.T) {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> /Contents 4 0 R >>", "<< /Length 0 >>\nstream\n\nendstream"}
	offsets := []int{0}
	for i, o := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 5\n0000000000 65535 f \n")
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	path := filepath.Join(t.TempDir(), "document.pdf")
	if e := os.WriteFile(path, b.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	r, e := Run(context.Background(), path)
	if e != nil || r.Pages != 1 || r.Data == "" {
		t.Fatal("PDF preview failed", r.Pages, e)
	}
}
