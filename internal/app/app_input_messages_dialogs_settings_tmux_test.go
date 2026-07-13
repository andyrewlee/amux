package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// TestHandleSettingsResult_PersistsTmuxConfigEdit confirms an edited tmux
// config-path field is written to config.UI.TmuxConfigPath and persisted to
// disk via SaveUISettings on a non-canceled close -- the tmux counterpart to
// TestHandleSettingsResult_PersistsAssistantCommandEdit in
// app_input_messages_dialogs_test.go. The dialog exposes no direct tmux
// setter (unlike AssistantCommands(), which returns a mutable map), so the
// edit is driven the same way a real user would: Tab to focus the field,
// then type into it (see settings_test.go's TestSettingsDialogEditsTmuxFields
// for the same pattern one layer down, in internal/ui/common).
func TestHandleSettingsResult_PersistsTmuxConfigEdit(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "amux-config.json")
	h.app.config.Paths.ConfigPath = configPath
	// Pin the theme to the same value PersistedUISettings resolves to for a
	// not-yet-existing config file (the default, "gruvbox"), so this test's
	// close is dirty for tmux only -- independent of whatever theme the
	// machine running the test happens to have in its real ~/.amux/config.json.
	h.app.config.UI.Theme = string(common.ThemeGruvbox)
	h.app.handleShowSettingsDialog()

	// Tab from Theme to the tmux server field, then to the config-path field,
	// and type a new value there.
	h.app.settingsDialog, _ = h.app.settingsDialog.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	h.app.settingsDialog, _ = h.app.settingsDialog.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	for _, r := range "/tmp/new-tmux.conf" {
		h.app.settingsDialog, _ = h.app.settingsDialog.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	cmd := h.app.handleSettingsResult(common.SettingsResult{})
	if cmd != nil {
		t.Fatal("expected no warning cmd when tmux save succeeds")
	}

	if got := h.app.config.UI.TmuxConfigPath; got != "/tmp/new-tmux.conf" {
		t.Errorf("in-memory TmuxConfigPath = %q, want %q", got, "/tmp/new-tmux.conf")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config file to be written: %v", err)
	}
	if !strings.Contains(string(data), `"tmux_config": "/tmp/new-tmux.conf"`) {
		t.Fatalf("expected persisted tmux config path in config, got %q", string(data))
	}
}

// TestHandleSettingsResult_UnchangedTmuxSkipsSave confirms closing the
// settings dialog without editing any tmux field does not write to disk,
// mirroring TestHandleSettingsResult_UnchangedThemeSkipsSave's contract for
// the tmux fields specifically.
func TestHandleSettingsResult_UnchangedTmuxSkipsSave(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "amux-config.json")
	h.app.config.Paths.ConfigPath = configPath
	h.app.config.UI.Theme = string(common.ThemeGruvbox)
	h.app.config.UI.TmuxServer = "existing-server"
	h.app.handleShowSettingsDialog()

	cmd := h.app.handleSettingsResult(common.SettingsResult{})
	if cmd != nil {
		t.Fatal("expected no cmd when closing settings without any tmux edit")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected unchanged tmux close not to write config, stat err=%v", err)
	}
}
