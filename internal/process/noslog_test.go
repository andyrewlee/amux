package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoDirectSlogUsage pins the package to internal/logging: log/slog's
// default handler writes to stderr at Info+, so slog.Debug lines were
// silently dropped everywhere and slog.Warn painted over the TUI. The
// package's log sites must go through internal/logging (Printf style).
func TestNoDirectSlogUsage(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(raw), "slog.") || strings.Contains(string(raw), `"log/slog"`) {
			t.Errorf("%s uses log/slog directly — route through internal/logging (see AGENTS.md log-level contract)", name)
		}
	}
}
