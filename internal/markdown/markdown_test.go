package markdown

import (
	"strings"
	"testing"
)

func TestFormattingAndIsolation(t *testing.T) {
	out := Render("# Heading\n\n| Name | Value |\n| --- | ---: |\n| CPU | 1 |\n\n- [x] ready\n\n```go\nfmt.Println(1)\n```\n\n[heading](#heading) [danger](javascript:alert(1)) ![alt](https://example.org/image.png)\n\n<script>alert(1)</script>\n")
	for _, want := range []string{"<table>", "<th", "<code>", "checkbox", `id="md-content-heading"`, `href="#md-content-heading"`, "alt"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	for _, bad := range []string{"<script", "javascript:", "<img", "https://", "style="} {
		if strings.Contains(out, bad) {
			t.Fatalf("unsafe %q", out)
		}
	}
}
func TestBoundedMarkdown(t *testing.T) {
	out := Render(strings.Repeat("text\n", Limit))
	if !strings.Contains(out, "256 KiB") || len(out) > Limit*2 {
		t.Fatal("unbounded/missing truncation", len(out))
	}
}

func TestTrustedRasterImages(t *testing.T) {
	out := RenderImages(`![a "quoted" caption](images/chart.png)`, func(src string) string {
		if src != "images/chart.png" {
			t.Fatal(src)
		}
		return "data:image/png;base64,YWJj"
	})
	if !strings.Contains(out, `<img src="data:image/png;base64,YWJj"`) || !strings.Contains(out, "quoted") {
		t.Fatal(out)
	}
	out = RenderImages("![bad](image.png)", func(string) string { return "data:image/svg+xml,<svg onload='alert(1)'/>" })
	if strings.Contains(out, "<img") || !strings.Contains(out, "image unavailable") {
		t.Fatal(out)
	}
	calls := 0
	out = RenderImages(strings.Repeat("![image](a.png)\n", 30), func(string) string { calls++; return "data:image/png;base64,YWJj" })
	if calls != 8 || strings.Count(out, "<img") != 8 {
		t.Fatal(calls)
	}
}
