package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/ui/common"
)

func TestHandleThemePreview_SaveFailureShowsWarningToast(t *testing.T) {
	prevTheme := common.GetCurrentTheme().ID
	defer common.SetCurrentTheme(prevTheme)

	h, err := NewHarness(HarnessOptions{
		Mode:   HarnessCenter,
		Width:  120,
		Height: 40,
	})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}

	// Point to a directory path so os.WriteFile fails with "is a directory".
	h.app.config.Paths.ConfigPath = t.TempDir()

	cmd := h.app.handleThemePreview(common.ThemePreview{Theme: common.ThemeTokyoNight})
	if cmd == nil {
		t.Fatal("expected warning toast cmd when SaveUISettings fails")
	}

	if view := h.app.toast.View(); !strings.Contains(view, "Failed to save theme setting") {
		t.Fatalf("expected warning toast for save failure, got %q", view)
	}
}

func TestHandleThemePreview_SaveSuccessPersistsTheme(t *testing.T) {
	prevTheme := common.GetCurrentTheme().ID
	defer common.SetCurrentTheme(prevTheme)

	h, err := NewHarness(HarnessOptions{
		Mode:   HarnessCenter,
		Width:  120,
		Height: 40,
	})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "amux-config.json")
	h.app.config.Paths.ConfigPath = configPath

	cmd := h.app.handleThemePreview(common.ThemePreview{Theme: common.ThemeTokyoNight})
	if cmd != nil {
		t.Fatal("expected no warning toast cmd when SaveUISettings succeeds")
	}
	if view := h.app.toast.View(); view != "" {
		t.Fatalf("expected no toast when save succeeds, got %q", view)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config file to be written: %v", err)
	}
	if !strings.Contains(string(data), `"theme": "tokyo-night"`) {
		t.Fatalf("expected persisted theme in config, got %q", string(data))
	}
}
