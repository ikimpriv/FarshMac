package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const HelperFlag = "--preview-helper"

type Result struct {
	Data                 string
	Width, Height, Pages int
	Format               string
}

func Supported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".tif", ".tiff", ".bmp", ".heic", ".heif", ".webp", ".pdf":
		return true
	}
	return false
}

// DefaultPixels is the thumbnail bound of the quick view.
const DefaultPixels = 768

// HelperMain decodes path in the helper process; maxPixels bounds the longer
// side of the picture (DefaultPixels when 0).
func HelperMain(path string, maxPixels int) int {
	if maxPixels <= 0 {
		maxPixels = DefaultPixels
	}
	r, err := decode(path, maxPixels)
	if err != nil {
		return 1
	}
	if json.NewEncoder(os.Stdout).Encode(r) != nil {
		return 1
	}
	return 0
}

// Run decodes a quick-view thumbnail (DefaultPixels) in a helper process.
func Run(ctx context.Context, path string) (Result, error) {
	return RunSized(ctx, path, DefaultPixels)
}

// RunSized decodes path with the longer side bounded by maxPixels. Larger
// pictures are allowed more bytes and time.
func RunSized(ctx context.Context, path string, maxPixels int) (Result, error) {
	limit := int64(5 * 1024 * 1024)
	timeout := 4 * time.Second
	if maxPixels > DefaultPixels {
		limit = 20 * 1024 * 1024
		timeout = 8 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, exe, HelperFlag, path, strconv.Itoa(maxPixels))
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	if err = cmd.Start(); err != nil {
		return Result{}, err
	}
	data, readErr := io.ReadAll(io.LimitReader(pipe, limit+1))
	if readErr != nil || int64(len(data)) > limit {
		cancel()
	}
	err = cmd.Wait()
	if err != nil {
		return Result{}, err
	}
	if readErr != nil {
		return Result{}, readErr
	}
	if int64(len(data)) > limit {
		return Result{}, fmt.Errorf("preview exceeds limit")
	}
	var r Result
	err = json.Unmarshal(data, &r)
	return r, err
}
