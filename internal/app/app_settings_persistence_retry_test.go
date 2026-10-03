package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// newRetryHarness builds a harness whose config path holds a real, valid
// config file carrying an unrelated synthetic section. config.UI is pinned
// to deterministic values (the harness otherwise reads the machine's real
// ~/.amux/config.json) so every dialog edit below is a guaranteed change.
// The returned bytes let blockConfigPath/repairConfigPath force a real save
// refusal ("is a directory") and later restore the exact same path.
func newRetryHarness(t *testing.T) (*Harness, string, []byte) {
	t.Helper()
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "amux-config.json")
	initial := []byte(`{"ui": {"theme": "gruvbox"}, "unrelated": {"keep": "me"}}`)
	if err := os.WriteFile(configPath, initial, 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	h.app.config.Paths.ConfigPath = configPath
	// Pin the theme to the persisted value so the dialogs below are dirty
	// only for the section under test — a theme save must not mask a retry.
	h.app.config.UI = config.UISettings{
		Theme:         string(common.ThemeGruvbox),
		ViewerCommand: "vim",
	}
	return h, configPath, initial
}

// blockConfigPath makes saves at configPath fail with a real filesystem
// error until repairConfigPath runs.
func blockConfigPath(t *testing.T, configPath string) {
	t.Helper()
	if err := os.Remove(configPath); err != nil {
		t.Fatalf("remove config for blocking: %v", err)
	}
	if err := os.Mkdir(configPath, 0o700); err != nil {
		t.Fatalf("block config path: %v", err)
	}
}

// repairConfigPath removes the blocking directory and restores original
// bytes so the same path can accept the retried save.
func repairConfigPath(t *testing.T, configPath string, original []byte) {
	t.Helper()
	if err := os.Remove(configPath); err != nil {
		t.Fatalf("unblock config path: %v", err)
	}
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatalf("restore config: %v", err)
	}
}

func readConfigBytes(t *testing.T, configPath string) string {
	t.Helper()
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return string(data)
}

// TestSettingsRetry_UnchangedUI: a confirmed interface/tmux edit whose save
// fails stays active in memory and is retried by a later confirmation that
// changed nothing — the failed save must not look like success.
func TestSettingsRetry_UnchangedUI(t *testing.T) {
	prevTheme := common.GetCurrentTheme().ID
	defer common.SetCurrentTheme(prevTheme)

	cases := []struct {
		name  string
		edit  func(d *common.SettingsDialog)
		check func(t *testing.T, h *Harness)
	}{
		{
			name: "interface",
			edit: func(d *common.SettingsDialog) {
				d.SetUIOptions(true, true, "vim -n")
			},
			check: func(t *testing.T, h *Harness) {
				persisted := h.app.config.PersistedUISettings()
				if !persisted.ShowKeymapHints || !persisted.NotifyOnDone {
					t.Errorf("persisted interface settings = %+v, want hints+notify on", persisted)
				}
				if persisted.ViewerCommand != "vim -n" {
					t.Errorf("persisted viewer_command = %q, want vim -n", persisted.ViewerCommand)
				}
			},
		},
		{
			name: "tmux",
			edit: func(d *common.SettingsDialog) {
				d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
				d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
				for _, r := range "/tmp/retry-tmux.conf" {
					d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
				}
			},
			check: func(t *testing.T, h *Harness) {
				persisted := h.app.config.PersistedUISettings()
				if persisted.TmuxConfigPath != "/tmp/retry-tmux.conf" {
					t.Errorf("persisted tmux_config = %q, want /tmp/retry-tmux.conf", persisted.TmuxConfigPath)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, configPath, initial := newRetryHarness(t)
			blockConfigPath(t, configPath)

			h.app.handleShowSettingsDialog()
			tc.edit(h.app.overlays.settings)
			cmd := h.app.handleSettingsResult(common.SettingsResult{})
			if cmd == nil {
				t.Fatal("expected an error-report cmd when the save fails")
			}
			assertReportErrorMessages(t, cmd, "Failed to save settings")
			if !h.app.overlays.settingsUIPersistPending {
				t.Fatal("failed UI save must leave a pending obligation")
			}

			repairConfigPath(t, configPath, initial)

			// Reopen and confirm without editing: the pending obligation is
			// retried and the accepted values reach disk.
			h.app.handleShowSettingsDialog()
			cmd = h.app.handleSettingsResult(common.SettingsResult{})
			if cmd != nil {
				t.Fatalf("unchanged retry confirm should not report errors, got %T", cmd)
			}
			if h.app.overlays.settingsUIPersistPending {
				t.Fatal("successful retry must clear the UI obligation")
			}
			tc.check(t, h)
			if got := readConfigBytes(t, configPath); !strings.Contains(got, `"keep": "me"`) {
				t.Fatalf("unrelated section dropped from config: %s", got)
			}
		})
	}
}

// TestSettingsRetry_UnchangedAssistants: same contract for the independent
// assistants section — a failed SaveAssistants retries on an unchanged
// confirmation and preserves interrupt tuning in the written section.
func TestSettingsRetry_UnchangedAssistants(t *testing.T) {
	h, configPath, initial := newRetryHarness(t)
	h.app.config.Assistants = map[string]config.AssistantConfig{
		"claude": {Command: "claude", InterruptCount: 3, InterruptDelayMs: 0},
	}
	blockConfigPath(t, configPath)

	h.app.handleShowSettingsDialog()
	h.app.overlays.settings.AssistantCommands()["claude"] = "claude --resume"
	cmd := h.app.handleSettingsResult(common.SettingsResult{})
	if cmd == nil {
		t.Fatal("expected an error-report cmd when the save fails")
	}
	assertReportErrorMessages(t, cmd, "Failed to save assistant settings")
	if got := h.app.config.Assistants["claude"].Command; got != "claude --resume" {
		t.Fatalf("in-memory command = %q, want applied value retained", got)
	}
	if !h.app.overlays.settingsAssistantsPersistPending {
		t.Fatal("failed assistants save must leave a pending obligation")
	}

	repairConfigPath(t, configPath, initial)
	h.app.handleShowSettingsDialog()
	cmd = h.app.handleSettingsResult(common.SettingsResult{})
	if cmd != nil {
		t.Fatalf("unchanged retry confirm should not report errors, got %T", cmd)
	}
	if h.app.overlays.settingsAssistantsPersistPending {
		t.Fatal("successful retry must clear the assistants obligation")
	}

	var saved struct {
		Assistants map[string]struct {
			Command          string `json:"command"`
			InterruptCount   int    `json:"interrupt_count"`
			InterruptDelayMs int    `json:"interrupt_delay_ms"`
		} `json:"assistants"`
		Unrelated map[string]any `json:"unrelated"`
	}
	if err := json.Unmarshal([]byte(readConfigBytes(t, configPath)), &saved); err != nil {
		t.Fatalf("decode saved config: %v", err)
	}
	claude, ok := saved.Assistants["claude"]
	if !ok {
		t.Fatal("saved config missing assistants.claude")
	}
	if claude.Command != "claude --resume" {
		t.Errorf("saved command = %q, want claude --resume", claude.Command)
	}
	if claude.InterruptCount != 3 || claude.InterruptDelayMs != 0 {
		t.Errorf("interrupt tuning = %d/%d, want 3/0 preserved", claude.InterruptCount, claude.InterruptDelayMs)
	}
	if len(saved.Unrelated) == 0 {
		t.Error("unrelated section dropped from config")
	}
}

// TestSettingsRetry_IndependentSections injects counting saves into both
// helpers: each section attempts and acknowledges on its own — one failing
// does not skip or clear the other.
func TestSettingsRetry_IndependentSections(t *testing.T) {
	fail := errors.New("disk full")

	t.Run("UI fails, assistants succeed", func(t *testing.T) {
		h, _, _ := newRetryHarness(t)
		h.app.overlays.settingsUIPersistPending = true
		h.app.overlays.settingsAssistantsPersistPending = true

		var uiN, asN int
		uiCmd := h.app.persistSettingsUIIfDirty(func() error { uiN++; return fail })
		asCmd := h.app.persistSettingsAssistantsIfDirty(func() error { asN++; return nil })
		if uiN != 1 || asN != 1 {
			t.Fatalf("attempts = ui:%d assist:%d, want 1/1", uiN, asN)
		}
		assertReportErrorMessages(t, uiCmd, "Failed to save settings")
		if asCmd != nil {
			t.Fatal("successful assistants save must not report")
		}
		if !h.app.overlays.settingsUIPersistPending || h.app.overlays.settingsAssistantsPersistPending {
			t.Fatal("failure must retain only the UI obligation")
		}

		// Second attempt retries only the still-pending section.
		uiCmd = h.app.persistSettingsUIIfDirty(func() error { uiN++; return nil })
		asCmd = h.app.persistSettingsAssistantsIfDirty(func() error { asN++; return nil })
		if uiN != 2 || asN != 1 {
			t.Fatalf("retry attempts = ui:%d assist:%d, want 2/1", uiN, asN)
		}
		if uiCmd != nil || asCmd != nil {
			t.Fatal("clean retry must not report")
		}
		if h.app.overlays.settingsUIPersistPending || h.app.overlays.settingsAssistantsPersistPending {
			t.Fatal("successful retries must clear both obligations")
		}
	})

	t.Run("assistants fail, UI succeeds", func(t *testing.T) {
		h, _, _ := newRetryHarness(t)
		h.app.overlays.settingsUIPersistPending = true
		h.app.overlays.settingsAssistantsPersistPending = true

		var uiN, asN int
		uiCmd := h.app.persistSettingsUIIfDirty(func() error { uiN++; return nil })
		asCmd := h.app.persistSettingsAssistantsIfDirty(func() error { asN++; return fail })
		if uiN != 1 || asN != 1 {
			t.Fatalf("attempts = ui:%d assist:%d, want 1/1", uiN, asN)
		}
		assertReportErrorMessages(t, asCmd, "Failed to save assistant settings")
		if uiCmd != nil {
			t.Fatal("successful UI save must not report")
		}
		if h.app.overlays.settingsUIPersistPending || !h.app.overlays.settingsAssistantsPersistPending {
			t.Fatal("failure must retain only the assistants obligation")
		}

		uiCmd = h.app.persistSettingsUIIfDirty(func() error { uiN++; return nil })
		asCmd = h.app.persistSettingsAssistantsIfDirty(func() error { asN++; return nil })
		if uiN != 1 || asN != 2 {
			t.Fatalf("retry attempts = ui:%d assist:%d, want 1/2", uiN, asN)
		}
		if uiCmd != nil || asCmd != nil {
			t.Fatal("clean retry must not report")
		}
		if h.app.overlays.settingsUIPersistPending || h.app.overlays.settingsAssistantsPersistPending {
			t.Fatal("successful retries must clear both obligations")
		}
	})
}

// TestSettingsRetry_CancelKeepsPriorObligation: Esc drops the current
// dialog's unconfirmed edits but must not discard an obligation a prior
// confirmation already accepted.
func TestSettingsRetry_CancelKeepsPriorObligation(t *testing.T) {
	h, configPath, initial := newRetryHarness(t)
	blockConfigPath(t, configPath)

	h.app.handleShowSettingsDialog()
	h.app.overlays.settings.SetUIOptions(true, true, "vim")
	if cmd := h.app.handleSettingsResult(common.SettingsResult{}); cmd == nil {
		t.Fatal("expected an error-report cmd when the save fails")
	}
	if !h.app.overlays.settingsUIPersistPending {
		t.Fatal("failed save must leave a pending obligation")
	}
	if !h.app.config.UI.NotifyOnDone {
		t.Fatal("confirmed values stay applied in memory after a failed save")
	}

	// Reopen, make a different unconfirmed edit, then Esc — no write, no
	// new accepted change, prior obligation retained.
	h.app.handleShowSettingsDialog()
	h.app.overlays.settings.SetUIOptions(true, false, "nano")
	if cmd := h.app.handleSettingsResult(common.SettingsResult{Canceled: true}); cmd != nil {
		t.Fatalf("cancel must not save or report, got %T", cmd)
	}
	if got := h.app.config.UI.ViewerCommand; got != "vim" {
		t.Fatalf("canceled edit leaked into config: viewer = %q", got)
	}
	if !h.app.overlays.settingsUIPersistPending {
		t.Fatal("Esc must not discard the prior confirmed obligation")
	}

	repairConfigPath(t, configPath, initial)
	h.app.handleShowSettingsDialog()
	if cmd := h.app.handleSettingsResult(common.SettingsResult{}); cmd != nil {
		t.Fatalf("unchanged retry confirm should not report errors, got %T", cmd)
	}
	persisted := h.app.config.PersistedUISettings()
	if !persisted.NotifyOnDone || persisted.ViewerCommand != "vim" {
		t.Fatalf("persisted settings = %+v, want notify on + vim", persisted)
	}
}

// TestSettingsRetry_ThemeSaveAcknowledgesUI: a successful theme save writes
// the whole UI section, so it also acknowledges an earlier failed interface
// save. A canceled preview with no prior obligation creates none.
func TestSettingsRetry_ThemeSaveAcknowledgesUI(t *testing.T) {
	prevTheme := common.GetCurrentTheme().ID
	defer common.SetCurrentTheme(prevTheme)

	h, configPath, initial := newRetryHarness(t)
	blockConfigPath(t, configPath)

	h.app.handleShowSettingsDialog()
	h.app.overlays.settings.SetUIOptions(true, true, "vim")
	if cmd := h.app.handleSettingsResult(common.SettingsResult{}); cmd == nil {
		t.Fatal("expected an error-report cmd when the save fails")
	}
	if !h.app.overlays.settingsUIPersistPending {
		t.Fatal("failed interface save must leave a pending obligation")
	}

	repairConfigPath(t, configPath, initial)

	// Accept a changed theme: the one UI save persists theme and interface
	// together and clears the pending obligation.
	h.app.handleShowSettingsDialog()
	h.app.handleThemePreview(common.ThemePreview{
		Theme:   common.ThemeTokyoNight,
		Session: h.app.overlays.settingsSession,
	})
	if cmd := h.app.handleSettingsResult(common.SettingsResult{}); cmd != nil {
		t.Fatalf("theme save after repair should succeed, got %T", cmd)
	}
	if h.app.overlays.settingsUIPersistPending || h.app.overlays.settingsThemeDirty {
		t.Fatal("successful UI save must clear pending and theme-dirty")
	}
	persisted := h.app.config.PersistedUISettings()
	if persisted.Theme != string(common.ThemeTokyoNight) || !persisted.NotifyOnDone {
		t.Fatalf("persisted = %+v, want tokyo-night theme + notify on", persisted)
	}

	// An unchanged confirm must not attempt another save — sentinel bytes
	// holding the same effective settings survive byte-for-byte only if no
	// write happened.
	sentinel := []byte(`{"ui": {"theme": "tokyo-night", "notify_on_done": true, "show_keymap_hints": true, "viewer_command": "vim", "tmux_server": "", "tmux_config": "", "tmux_sync_interval": ""}}`)
	if err := os.WriteFile(configPath, sentinel, 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	h.app.handleShowSettingsDialog()
	if cmd := h.app.handleSettingsResult(common.SettingsResult{}); cmd != nil {
		t.Fatalf("clean confirm must be a no-op, got %T", cmd)
	}
	if got := readConfigBytes(t, configPath); got != string(sentinel) {
		t.Fatalf("unchanged confirm rewrote the file:\n got: %s\nwant: %s", got, sentinel)
	}
}

// TestSettingsRetry_PreviewCancelCreatesNoObligation: previewing a theme and
// canceling with no prior failure must not create a save obligation.
func TestSettingsRetry_PreviewCancelCreatesNoObligation(t *testing.T) {
	prevTheme := common.GetCurrentTheme().ID
	defer common.SetCurrentTheme(prevTheme)

	h, configPath, _ := newRetryHarness(t)
	blockConfigPath(t, configPath)

	h.app.handleShowSettingsDialog()
	h.app.handleThemePreview(common.ThemePreview{
		Theme:   common.ThemeTokyoNight,
		Session: h.app.overlays.settingsSession,
	})
	if cmd := h.app.handleSettingsResult(common.SettingsResult{Canceled: true}); cmd != nil {
		t.Fatalf("cancel must not save or report, got %T", cmd)
	}
	if h.app.overlays.settingsUIPersistPending {
		t.Fatal("a canceled preview must not create a UI save obligation")
	}
	if got := common.GetCurrentTheme().ID; got != common.ThemeGruvbox {
		t.Fatalf("canceled preview must revert the live theme, got %v", got)
	}
}

// TestSettingsRetry_SuccessBecomesNoop: after a failed save and successful
// retry, an unchanged confirmation is a true no-op — valid sentinel bytes
// representing the same settings survive byte-for-byte.
func TestSettingsRetry_SuccessBecomesNoop(t *testing.T) {
	h, configPath, initial := newRetryHarness(t)
	blockConfigPath(t, configPath)

	h.app.handleShowSettingsDialog()
	h.app.overlays.settings.SetUIOptions(true, true, "vim")
	if cmd := h.app.handleSettingsResult(common.SettingsResult{}); cmd == nil {
		t.Fatal("expected an error-report cmd when the save fails")
	}
	repairConfigPath(t, configPath, initial)
	h.app.handleShowSettingsDialog()
	if cmd := h.app.handleSettingsResult(common.SettingsResult{}); cmd != nil {
		t.Fatalf("retry confirm should succeed, got %T", cmd)
	}
	if h.app.overlays.settingsUIPersistPending {
		t.Fatal("successful retry must clear the obligation")
	}

	// Hand-crafted sentinel holding the same effective settings; any write
	// would reformat/extend it, so identical bytes prove no write occurred.
	sentinel := []byte(`{"ui": {"show_keymap_hints": true, "theme": "gruvbox", "notify_on_done": true, "viewer_command": "vim", "tmux_server": "", "tmux_config": "", "tmux_sync_interval": ""}}`)
	if err := os.WriteFile(configPath, sentinel, 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	h.app.handleShowSettingsDialog()
	if cmd := h.app.handleSettingsResult(common.SettingsResult{}); cmd != nil {
		t.Fatalf("no-op confirm must not report or save, got %T", cmd)
	}
	if got := readConfigBytes(t, configPath); got != string(sentinel) {
		t.Fatalf("unchanged confirm rewrote the file:\n got: %s\nwant: %s", got, sentinel)
	}
}
