package ui

import (
	"context"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"farshmac/internal/cloudfiles"
	"farshmac/internal/preview"
	"farshmac/internal/remotefs"
)

const markdownImageBytes = 12 << 20

// URLs in the document never reach WebKit. Relative images are read from the
// same filesystem as the source, then rasterized in the existing helper.
func markdownImagePath(document, reference string, remote bool) (string, bool) {
	u, err := url.Parse(reference)
	if err != nil || u.Scheme != "" || u.Host != "" || u.RawQuery != "" || strings.ContainsAny(u.Path, "\x00\\") || u.Path == "" {
		return "", false
	}
	if remote {
		if path.IsAbs(u.Path) {
			return path.Clean(u.Path), true
		}
		return path.Join(path.Dir(document), u.Path), true
	}
	if filepath.IsAbs(u.Path) {
		return filepath.Clean(u.Path), true
	}
	return filepath.Join(filepath.Dir(document), filepath.FromSlash(u.Path)), true
}
func markdownImages(ctx context.Context, document string, opener remotefs.Opener) func(string) string {
	cache := map[string]string{}
	return func(reference string) string {
		if ctx.Err() != nil {
			return ""
		}
		if data, ok := cache[reference]; ok {
			return data
		}
		cache[reference] = ""
		filename, ok := markdownImagePath(document, reference, opener != nil)
		if !ok || !preview.Supported(filename) || strings.EqualFold(filepath.Ext(filename), ".pdf") {
			return ""
		}
		local := filename
		if opener == nil {
			var err error
			local, err = cloudfiles.Prepare(ctx, local)
			if err != nil {
				return ""
			}
			info, err := os.Stat(local)
			if err != nil || !info.Mode().IsRegular() || info.Size() > markdownImageBytes {
				return ""
			}
		} else {
			fs, err := opener.Open(ctx)
			if err != nil {
				return ""
			}
			defer fs.Close()
			info, err := fs.Stat(filename)
			if err != nil || !info.Mode().IsRegular() || info.Size() > markdownImageBytes {
				return ""
			}
			input, err := fs.Open(filename)
			if err != nil {
				return ""
			}
			defer input.Close()
			dir, err := os.MkdirTemp("", "farshmac-markdown-image-*")
			if err != nil {
				return ""
			}
			defer os.RemoveAll(dir)
			local = filepath.Join(dir, "image"+filepath.Ext(filename))
			out, err := os.OpenFile(local, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return ""
			}
			n, err := io.Copy(out, io.LimitReader(input, markdownImageBytes+1))
			closeErr := out.Close()
			if err != nil || closeErr != nil || n > markdownImageBytes || ctx.Err() != nil {
				return ""
			}
		}
		r, err := preview.Run(ctx, local)
		if err != nil {
			return ""
		}
		cache[reference] = r.Data
		return r.Data
	}
}
