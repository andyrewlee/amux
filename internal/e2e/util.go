package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func stringsContains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}
