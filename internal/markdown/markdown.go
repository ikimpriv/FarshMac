// Package markdown renders bounded, offline Markdown fragments for the viewer.
package markdown

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"golang.org/x/net/html"
)

const Limit = 256 << 10

func Supported(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown", ".mdown", ".mkd":
		return true
	}
	return false
}
func Render(source string) string { return RenderImages(source, nil) }

// RenderImages only embeds trusted, rasterized data URLs from the resolver.
func RenderImages(source string, resolve func(string) string) string {
	imageCount, imageBytes := 0, 0
	truncated := len(source) > Limit
	if truncated {
		source = source[:Limit]
		if i := strings.LastIndexByte(source, '\n'); i >= 0 {
			source = source[:i]
		}
	}
	var b bytes.Buffer
	md := goldmark.New(goldmark.WithExtensions(extension.GFM, extension.Footnote), goldmark.WithParserOptions(parser.WithAutoHeadingID()))
	if err := md.Convert([]byte(source), &b); err != nil {
		return "<p>Markdown preview unavailable. Press Cmd+Shift+M to view source.</p>"
	}
	// Allow only document formatting. No resource URLs, inline styles, executable
	// HTML, or navigation can escape into the privileged application document.
	allowed := map[string]bool{}
	for _, tag := range strings.Fields("p h1 h2 h3 h4 h5 h6 ul ol li blockquote pre code em strong del hr br table thead tbody tr th td a sup section div span input") {
		allowed[tag] = true
	}
	z := html.NewTokenizer(bytes.NewReader(b.Bytes()))
	b.Reset()
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() == io.EOF {
				break
			}
			break
		}
		switch tt {
		case html.TextToken:
			b.WriteString(html.EscapeString(string(z.Text())))
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			t := z.Token()
			if t.Data == "img" && tt != html.EndTagToken {
				src, alt := "", ""
				for _, a := range t.Attr {
					if a.Key == "src" {
						src = a.Val
					}
					if a.Key == "alt" {
						alt = a.Val
					}
				}
				data := ""
				if resolve != nil && imageCount < 8 && imageBytes < 8<<20 {
					imageCount++
					data = resolve(src)
				}
				if (strings.HasPrefix(data, "data:image/png;base64,") || strings.HasPrefix(data, "data:image/jpeg;base64,")) && imageBytes+len(data) <= 8<<20 {
					imageBytes += len(data)
					b.WriteString(`<img src="` + html.EscapeString(data) + `" alt="` + html.EscapeString(alt) + `" loading="lazy" decoding="async">`)
				} else {
					b.WriteString(html.EscapeString(alt))
					if resolve != nil {
						b.WriteString(" (image unavailable)")
					}
				}
				continue
			}
			if !allowed[t.Data] {
				continue
			}
			attrs := t.Attr[:0]
			for _, a := range t.Attr {
				if a.Key == "id" || a.Key == "title" || a.Key == "align" || (a.Key == "href" && strings.HasPrefix(a.Val, "#")) || (t.Data == "input" && (a.Key == "type" && a.Val == "checkbox" || a.Key == "checked" || a.Key == "disabled")) {
					if a.Key == "id" {
						a.Val = "md-content-" + a.Val
					}
					if a.Key == "href" {
						a.Val = "#md-content-" + strings.TrimPrefix(a.Val, "#")
					}
					attrs = append(attrs, a)
				}
			}
			t.Attr = attrs
			b.WriteString(t.String())
		}
	}
	if truncated {
		b.WriteString("<p><em>Preview limited to 256 KiB. Press Cmd+Shift+M in the viewer for the complete source.</em></p>")
	}
	return b.String()
}
