package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
func HelperMain(path string) int {
	r, err := decode(path)
	if err != nil {
		return 1
	}
	if json.NewEncoder(os.Stdout).Encode(r) != nil {
		return 1
	}
	return 0
}
func Run(ctx context.Context, path string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, exe, HelperFlag, path)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	if err = cmd.Start(); err != nil {
		return Result{}, err
	}
	data, readErr := io.ReadAll(io.LimitReader(pipe, 5*1024*1024+1))
	if readErr != nil || len(data) > 5*1024*1024 {
		cancel()
	}
	err = cmd.Wait()
	if err != nil {
		return Result{}, err
	}
	if readErr != nil {
		return Result{}, readErr
	}
	if len(data) > 5*1024*1024 {
		return Result{}, fmt.Errorf("preview exceeds limit")
	}
	var r Result
	err = json.Unmarshal(data, &r)
	return r, err
}
