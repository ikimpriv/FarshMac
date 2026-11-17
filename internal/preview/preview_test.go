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
	if (len(os.Args) >= 3 && len(os.Args) <= 5) && os.Args[1] == HelperFlag {
		pixels := 0
		if len(os.Args) >= 4 {
			pixels, _ = strconv.Atoi(os.Args[3])
		}
		page := 1
		if len(os.Args) == 5 {
			page, _ = strconv.Atoi(os.Args[4])
		}
		os.Exit(HelperMain(os.Args[2], pixels, page))
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
	testPDFPreview(t, 0, 768)
}

func TestPDFContentScale(t *testing.T) {
	for _, rotation := range []int{0, 90, 180, 270} {
		for _, pixels := range []int{384, 2048} {
			t.Run(fmt.Sprintf("rotation%d-pixels%d", rotation, pixels), func(t *testing.T) { testPDFPreview(t, rotation, pixels) })
		}
	}
}

func testPDFPreview(t *testing.T, rotation, pixels int) {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	content := "0 0 0 rg 61.2 79.2 489.6 633.6 re f\n"
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Rotate %d /Resources << >> /Contents 4 0 R >>", rotation), fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content)}
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
	r, e := decode(path, pixels)
	width, height := 612, 792
	if rotation%180 != 0 {
		width, height = height, width
	}
	if e != nil || r.Pages != 1 || r.Data == "" || r.Width != width || r.Height != height {
		t.Fatal("PDF preview failed", r.Pages, e)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(r.Data, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, fraction := range []float64{0.2, 0.5, 0.8} {
		x, y := int(float64(im.Bounds().Dx())*fraction), int(float64(im.Bounds().Dy())*fraction)
		red, _, _, _ := im.At(x, y).RGBA()
		if red > 0x1000 {
			t.Fatalf("PDF content shrunk within page: pixel at %.0f%% is white", fraction*100)
		}
	}
}

func TestPDFPageSelection(t *testing.T) {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	content := "0 0 0 rg 0 0 612 792 re f\n"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> /Contents 5 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> /Contents 6 0 R >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
	}
	offsets := []int{0}
	for i, o := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 7\n0000000000 65535 f \n")
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 7 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	path := filepath.Join(t.TempDir(), "two-pages.pdf")
	if err := os.WriteFile(path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for page := 1; page <= 2; page++ {
		r, err := RunPage(context.Background(), path, 384, page)
		if err != nil {
			t.Fatal(err)
		}
		if r.Pages != 2 {
			t.Fatal(r.Pages)
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(r.Data, "data:image/png;base64,"))
		if err != nil {
			t.Fatal(err)
		}
		im, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		red, _, _, _ := im.At(im.Bounds().Dx()/2, im.Bounds().Dy()/2).RGBA()
		if (page == 1 && red < 0xf000) || (page == 2 && red > 0x1000) {
			t.Fatalf("wrong content for page %d: %d", page, red)
		}
	}
	if _, err := RunPage(context.Background(), path, 384, 3); err == nil {
		t.Fatal("out-of-range page accepted")
	}
}
