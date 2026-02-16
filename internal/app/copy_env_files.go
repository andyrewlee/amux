package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/medusa/internal/logging"
)

// copyEnvFiles copies .env* files from src to dst, one level deep.
// It copies top-level .env* files and .env* files inside immediate subdirectories
// (e.g. frontend/.env.local), preserving directory structure.
func copyEnvFiles(src, dst string) {
	entries, err := os.ReadDir(src)
	if err != nil {
		logging.Warn("Failed to read directory for env file copy: %v", err)
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			if strings.HasPrefix(name, ".env") {
				copyFile(filepath.Join(src, name), filepath.Join(dst, name))
			}
			continue
		}

		// One level deep: look inside immediate subdirectories
		subEntries, err := os.ReadDir(filepath.Join(src, name))
		if err != nil {
			continue
		}
		for _, subEntry := range subEntries {
			subName := subEntry.Name()
			if subEntry.IsDir() || !strings.HasPrefix(subName, ".env") {
				continue
			}
			dstDir := filepath.Join(dst, name)
			if err := os.MkdirAll(dstDir, 0o755); err != nil {
				logging.Warn("Failed to create directory %s for env file copy: %v", dstDir, err)
				continue
			}
			copyFile(filepath.Join(src, name, subName), filepath.Join(dstDir, subName))
		}
	}
}

func copyFile(src, dst string) {
	content, err := os.ReadFile(src)
	if err != nil {
		logging.Warn("Failed to read %s: %v", src, err)
		return
	}
	info, err := os.Stat(src)
	if err != nil {
		logging.Warn("Failed to stat %s: %v", src, err)
		return
	}
	if err := os.WriteFile(dst, content, info.Mode()); err != nil {
		logging.Warn("Failed to write %s: %v", dst, err)
	}
}
