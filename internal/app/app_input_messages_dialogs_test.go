package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/update"
)

func TestHandleThemePreview_PersistsOnCloseOnly(t *testing.T) {
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
	h.app.handleShowSettingsDialog()
	session := h.app.settingsDialogSession

	cmd := h.app.handleThemePreview(common.ThemePreview{Theme: common.ThemeTokyoNight, Session: session})
	if cmd != nil {
		t.Fatal("expected no warning cmd during preview")
	}

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected no config write during preview, got err=%v", err)
	}

	cmd = h.app.handleSettingsResult(common.SettingsResult{})
	if cmd != nil {
		t.Fatal("expected no warning cmd when save on close succeeds")
	}
	if view := h.app.toast.View(); view != "" {
		t.Fatalf("expected no toast on successful close save, got %q", view)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config file to be written: %v", err)
	}
	if !strings.Contains(string(data), `"theme": "tokyo-night"`) {
		t.Fatalf("expected persisted theme in config, got %q", string(data))
	}
}

func TestHandleSettingsResult_SaveFailureShowsWarningToast(t *testing.T) {
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
	h.app.handleShowSettingsDialog()
	session := h.app.settingsDialogSession
	_ = h.app.handleThemePreview(common.ThemePreview{Theme: common.ThemeTokyoNight, Session: session})

	cmd := h.app.handleSettingsResult(common.SettingsResult{})
	if cmd == nil {
		t.Fatal("expected warning toast cmd when close save fails")
	}

	if view := h.app.toast.View(); !strings.Contains(view, "Failed to save theme setting") {
		t.Fatalf("expected warning toast for save failure, got %q", view)
	}
}

func TestHandleSettingsResult_UnchangedThemeSkipsSave(t *testing.T) {
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
	if err := h.app.config.SaveUISettings(); err != nil {
		t.Fatalf("SaveUISettings returned error: %v", err)
	}
	h.app.handleShowSettingsDialog()

	cmd := h.app.handleSettingsResult(common.SettingsResult{})
	if cmd != nil {
		t.Fatal("expected no cmd when closing settings without theme change")
	}
	if h.app.settingsThemeDirty {
		t.Fatal("expected dirty flag to remain false when theme unchanged")
	}
}

func TestHandleTriggerUpgrade_PersistsThemeChange(t *testing.T) {
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
	h.app.updateAvailable = &update.CheckResult{
		CurrentVersion:  "v0.0.1",
		LatestVersion:   "v0.0.2",
		UpdateAvailable: true,
	}
	h.app.handleShowSettingsDialog()
	session := h.app.settingsDialogSession
	_ = h.app.handleThemePreview(common.ThemePreview{Theme: common.ThemeTokyoNight, Session: session})

	cmd := h.app.handleTriggerUpgrade()
	if cmd == nil {
		t.Fatal("expected upgrade command when update is available")
	}
	if h.app.settingsThemeDirty {
		t.Fatal("expected theme dirty flag to clear after successful save on upgrade trigger")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config file to be written: %v", err)
	}
	if !strings.Contains(string(data), `"theme": "tokyo-night"`) {
		t.Fatalf("expected persisted theme in config, got %q", string(data))
	}
}

func TestHandleTriggerUpgrade_SaveFailureShowsWarningToast(t *testing.T) {
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
	h.app.updateAvailable = &update.CheckResult{
		CurrentVersion:  "v0.0.1",
		LatestVersion:   "v0.0.2",
		UpdateAvailable: true,
	}
	h.app.handleShowSettingsDialog()
	session := h.app.settingsDialogSession
	_ = h.app.handleThemePreview(common.ThemePreview{Theme: common.ThemeTokyoNight, Session: session})

	cmd := h.app.handleTriggerUpgrade()
	if cmd == nil {
		t.Fatal("expected command batch for upgrade trigger")
	}
	if !h.app.settingsThemeDirty {
		t.Fatal("expected dirty flag to remain set after failed save")
	}
	if view := h.app.toast.View(); !strings.Contains(view, "Failed to save theme setting") {
		t.Fatalf("expected warning toast for save failure, got %q", view)
	}
}

func TestHandleShowSettingsDialog_RefreshesPersistedThemeBaseline(t *testing.T) {
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

	// First close fails to persist, leaving dirty state true.
	h.app.config.Paths.ConfigPath = t.TempDir()
	h.app.handleShowSettingsDialog()
	session := h.app.settingsDialogSession
	_ = h.app.handleThemePreview(common.ThemePreview{Theme: common.ThemeTokyoNight, Session: session})
	_ = h.app.handleSettingsResult(common.SettingsResult{})
	if !h.app.settingsThemeDirty {
		t.Fatal("expected dirty state after failed close save")
	}

	// Persist the same in-memory theme via another save path.
	configPath := filepath.Join(t.TempDir(), "amux-config.json")
	h.app.config.Paths.ConfigPath = configPath
	if err := h.app.config.SaveUISettings(); err != nil {
		t.Fatalf("SaveUISettings returned error: %v", err)
	}

	// Re-open settings should refresh baseline from disk (tokyo-night).
	h.app.handleShowSettingsDialog()
	if h.app.settingsThemeDirty {
		t.Fatal("expected dirty state to reset after baseline refresh")
	}

	// Switching to gruvbox must be treated as dirty and persisted on close.
	_ = h.app.handleThemePreview(common.ThemePreview{Theme: common.ThemeGruvbox, Session: h.app.settingsDialogSession})
	if !h.app.settingsThemeDirty {
		t.Fatal("expected dirty state for theme change away from persisted value")
	}
	_ = h.app.handleSettingsResult(common.SettingsResult{})

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config file to be written: %v", err)
	}
	if !strings.Contains(string(data), `"theme": "gruvbox"`) {
		t.Fatalf("expected persisted gruvbox theme in config, got %q", string(data))
	}
}

func TestHandleThemePreview_DropsStaleSessionAfterClose(t *testing.T) {
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
	startTheme := common.ThemeID(h.app.config.UI.Theme)
	h.app.handleShowSettingsDialog()
	session := h.app.settingsDialogSession

	// Close immediately without preview.
	_ = h.app.handleSettingsResult(common.SettingsResult{})
	if h.app.settingsDialogSession == session {
		t.Fatal("expected settings session to advance on close")
	}

	// Late preview from old session must be ignored.
	_ = h.app.handleThemePreview(common.ThemePreview{Theme: common.ThemeTokyoNight, Session: session})
	if common.ThemeID(h.app.config.UI.Theme) != startTheme {
		t.Fatalf("expected stale preview to be ignored, got %q", h.app.config.UI.Theme)
	}
	if h.app.settingsThemeDirty {
		t.Fatal("expected stale preview to not dirty settings theme state")
	}
}
