package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/config"
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

func TestHandleSettingsResult_CancelRevertsPreviewWithoutSave(t *testing.T) {
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
	originalTheme := common.ThemeID(h.app.config.UI.Theme)
	previewTheme := common.ThemeTokyoNight
	if previewTheme == originalTheme {
		previewTheme = common.ThemeGruvbox
	}
	h.app.handleShowSettingsDialog()
	session := h.app.settingsDialogSession

	_ = h.app.handleThemePreview(common.ThemePreview{Theme: previewTheme, Session: session})
	if common.ThemeID(h.app.config.UI.Theme) != previewTheme {
		t.Fatalf("expected preview theme to apply, got %q", h.app.config.UI.Theme)
	}

	cmd := h.app.handleSettingsResult(common.SettingsResult{Canceled: true})
	if cmd != nil {
		t.Fatal("expected canceled settings close to skip persistence")
	}
	if got := common.ThemeID(h.app.config.UI.Theme); got != originalTheme {
		t.Fatalf("canceled settings close theme = %q, want original %q", got, originalTheme)
	}
	if h.app.settingsThemeDirty {
		t.Fatal("expected canceled settings close to clear dirty flag")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected canceled settings close not to write config, stat err=%v", err)
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
		t.Fatal("expected an error-report cmd when close save fails")
	}
	assertReportErrorMessages(t, cmd, "Failed to save theme setting")
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
	assertReportErrorMessages(t, cmd, "Failed to save theme setting")
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

// TestHandleShowSettingsDialog_SeedsAssistantRoster confirms the dialog is
// handed the live config's roster (names in AssistantNames order, current
// commands), not an empty one, so the Assistants section has something to
// show and edit as soon as the dialog opens.
func TestHandleShowSettingsDialog_SeedsAssistantRoster(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	h.app.config.Assistants = map[string]config.AssistantConfig{
		"claude": {Command: "claude"},
		"mytool": {Command: "mytool --serve"},
	}

	h.app.handleShowSettingsDialog()

	commands := h.app.settingsDialog.AssistantCommands()
	if got := commands["claude"]; got != "claude" {
		t.Errorf("seeded claude command = %q, want %q", got, "claude")
	}
	if got := commands["mytool"]; got != "mytool --serve" {
		t.Errorf("seeded mytool command = %q, want %q", got, "mytool --serve")
	}
}

// TestHandleSettingsResult_PersistsAssistantCommandEdit confirms an edited
// assistant command is written to config.Assistants and persisted to disk
// via SaveAssistants on a non-canceled close.
func TestHandleSettingsResult_PersistsAssistantCommandEdit(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "amux-config.json")
	h.app.config.Paths.ConfigPath = configPath
	// Pin the theme to the same value PersistedUISettings resolves to for a
	// not-yet-existing config file (the default, "gruvbox"), so this test's
	// close is dirty for assistants only -- independent of whatever theme the
	// machine running the test happens to have in its real ~/.amux/config.json.
	h.app.config.UI.Theme = string(common.ThemeGruvbox)
	h.app.config.Assistants = map[string]config.AssistantConfig{
		"claude": {Command: "claude", InterruptCount: 2, InterruptDelayMs: 200},
	}
	h.app.handleShowSettingsDialog()

	h.app.settingsDialog.AssistantCommands()["claude"] = "claude --resume"

	cmd := h.app.handleSettingsResult(common.SettingsResult{})
	if cmd != nil {
		t.Fatal("expected no warning cmd when assistant save succeeds")
	}

	if got := h.app.config.Assistants["claude"].Command; got != "claude --resume" {
		t.Errorf("in-memory claude command = %q, want %q", got, "claude --resume")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config file to be written: %v", err)
	}
	if !strings.Contains(string(data), `"command": "claude --resume"`) {
		t.Fatalf("expected persisted assistant command in config, got %q", string(data))
	}
}

// TestHandleSettingsResult_CancelDiscardsAssistantEdit confirms Esc drops an
// in-dialog assistant edit: the in-memory config and disk are both
// untouched, matching the tmux fields' cancel contract.
func TestHandleSettingsResult_CancelDiscardsAssistantEdit(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "amux-config.json")
	h.app.config.Paths.ConfigPath = configPath
	h.app.config.UI.Theme = string(common.ThemeGruvbox)
	h.app.config.Assistants = map[string]config.AssistantConfig{
		"claude": {Command: "claude"},
	}
	h.app.handleShowSettingsDialog()

	h.app.settingsDialog.AssistantCommands()["claude"] = "claude --resume"

	cmd := h.app.handleSettingsResult(common.SettingsResult{Canceled: true})
	if cmd != nil {
		t.Fatal("expected canceled settings close to skip persistence")
	}
	if got := h.app.config.Assistants["claude"].Command; got != "claude" {
		t.Errorf("in-memory claude command = %q, want unchanged %q", got, "claude")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected canceled settings close not to write config, stat err=%v", err)
	}
}

// TestHandleSettingsResult_UnchangedAssistantSkipsSave confirms closing the
// dialog without editing any assistant command does not touch config.json
// (a bare theme/tmux-unchanged close should stay a no-op, per
// TestHandleSettingsResult_UnchangedThemeSkipsSave's contract).
func TestHandleSettingsResult_UnchangedAssistantSkipsSave(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "amux-config.json")
	h.app.config.Paths.ConfigPath = configPath
	h.app.config.UI.Theme = string(common.ThemeGruvbox)
	h.app.config.Assistants = map[string]config.AssistantConfig{
		"claude": {Command: "claude"},
	}
	h.app.handleShowSettingsDialog()

	cmd := h.app.handleSettingsResult(common.SettingsResult{})
	if cmd != nil {
		t.Fatal("expected no cmd when closing settings without any edits")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected unchanged settings close not to write config, stat err=%v", err)
	}
}

// TestHandleSettingsResult_AssistantSaveFailureShowsWarningToast confirms a
// SaveAssistants failure is reported via common.ReportError, matching the
// tmux/theme save-failure contract.
func TestHandleSettingsResult_AssistantSaveFailureShowsWarningToast(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}

	// Point to a directory path so the write fails with "is a directory".
	h.app.config.Paths.ConfigPath = t.TempDir()
	h.app.config.UI.Theme = string(common.ThemeGruvbox)
	h.app.config.Assistants = map[string]config.AssistantConfig{
		"claude": {Command: "claude"},
	}
	h.app.handleShowSettingsDialog()
	h.app.settingsDialog.AssistantCommands()["claude"] = "claude --resume"

	cmd := h.app.handleSettingsResult(common.SettingsResult{})
	if cmd == nil {
		t.Fatal("expected an error-report cmd when assistant save fails")
	}
	assertReportErrorMessages(t, cmd, "Failed to save assistant settings")
}

// TestHandleSettingsResult_BlankAssistantEditIsNotPersisted confirms a
// command edited down to blank/whitespace-only never overwrites the
// existing (launchable) command -- an assistant must never be left with an
// empty command via the Settings dialog.
func TestHandleSettingsResult_BlankAssistantEditIsNotPersisted(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "amux-config.json")
	h.app.config.Paths.ConfigPath = configPath
	h.app.config.UI.Theme = string(common.ThemeGruvbox)
	h.app.config.Assistants = map[string]config.AssistantConfig{
		"claude": {Command: "claude"},
	}
	h.app.handleShowSettingsDialog()
	h.app.settingsDialog.AssistantCommands()["claude"] = "   "

	cmd := h.app.handleSettingsResult(common.SettingsResult{})
	if cmd != nil {
		t.Fatal("expected no cmd: a blank edit changes nothing to persist")
	}
	if got := h.app.config.Assistants["claude"].Command; got != "claude" {
		t.Errorf("claude command = %q, want unchanged %q (blank edit ignored)", got, "claude")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected no config write for a no-op blank edit, stat err=%v", err)
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
