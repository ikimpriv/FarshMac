package remotefs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func validateLocalPair(pair Pair) error {
	i, err := os.Lstat(pair.Source)
	if err != nil {
		return err
	}
	if !i.IsDir() {
		return nil
	}
	a, err := filepath.EvalSymlinks(pair.Source)
	if err != nil {
		return err
	}
	b, err := filepath.EvalSymlinks(filepath.Dir(pair.Destination))
	if err != nil {
		return err
	}
	b = filepath.Join(b, filepath.Base(pair.Destination))
	if a == b || strings.HasPrefix(b, a+string(filepath.Separator)) {
		return fmt.Errorf("destination is inside source")
	}
	return nil
}
