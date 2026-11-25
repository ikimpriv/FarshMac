package aispaces

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// A content fingerprint for review evidence, not a Git revision or a lock.
// Dependencies/caches are omitted; dependency lockfiles are included.
func workspaceSnapshot(ctx context.Context, dir string) string {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "unavailable"
	}
	defer root.Close()
	sum := sha256.New()
	count := 0
	left := int64(256 << 20)
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if p != "." && (skipped(d.Name()) || d.Name() == ".cache" || d.Name() == ".tmp" || d.Name() == ".runs" || d.Name() == ".runtime-home") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if count++; count > workspaceLimit {
			return fmt.Errorf("too many files")
		}
		file, err := root.Open(p)
		if err != nil {
			return err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if info.Size() > left {
			return fmt.Errorf("snapshot too large")
		}
		fmt.Fprintf(sum, "%s\x00%d\x00%d\x00", p, info.Mode().Perm(), info.Size())
		n, err := io.Copy(sum, io.LimitReader(file, left+1))
		left -= n
		if left < 0 {
			return fmt.Errorf("snapshot too large")
		}
		return err
	})
	if err != nil {
		return "unavailable (" + err.Error() + ")"
	}
	return fmt.Sprintf("sha256:%x (%d files, excluding dependencies/caches)", sum.Sum(nil), count)
}
