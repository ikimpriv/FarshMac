package ui

import (
	"os"
	"path/filepath"
)

// Read pre-rename preferences only when the corresponding new file is absent.
// Writes always use FarshMac. Never overwrite or remove the previous settings.
func readConfigFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if !os.IsNotExist(err) {
		return data, err
	}
	root, rootErr := os.UserConfigDir()
	if rootErr != nil || filepath.Dir(path) != filepath.Join(root, "FarshMac") {
		return data, err
	}
	return os.ReadFile(filepath.Join(root, "Far Go", filepath.Base(path)))
}
