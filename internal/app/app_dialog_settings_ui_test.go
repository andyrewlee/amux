package app

import (
	"path/filepath"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// The Interface section round-trips: toggling notify_on_done in the dialog
// flips config + the live dashboard flag, and closing persists the whole
// UISettings block (verified by reading it back off disk).
func TestSettingsInterfaceRoundTrip(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 60})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	h.app.config.Paths.ConfigPath = filepath.Join(t.TempDir(), "amux-config.json")
	h.app.config.UI.ViewerCommand = "vim"

	h.app.handleShowSettingsDialog()
	d := h.app.overlays.settings
	if d == nil {
		t.Fatal("settings dialog should be open")
	}

	// Tab Theme→Tmux×3 → KeymapHints(4) → NotifyOnDone(5) → ViewerCmd(6).
	for range 5 {
		d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // notify_on_done on
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})   // → viewer command
	for _, r := range " -n" {
		d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	if cmd := h.app.handleSettingsResult(common.SettingsResult{}); cmd != nil {
		t.Fatalf("clean close should not report errors, got %T", cmd)
	}
	if !h.app.config.UI.NotifyOnDone {
		t.Fatal("config.UI.NotifyOnDone should be flipped on")
	}
	if !h.app.dashboard.NotifyOnDone() {
		t.Fatal("dashboard.SetNotifyOnDone should have applied the toggle")
	}
	if got := h.app.config.UI.ViewerCommand; got != "vim -n" {
		t.Fatalf("viewer command = %q, want vim -n", got)
	}

	persisted := h.app.config.PersistedUISettings()
	if !persisted.NotifyOnDone || persisted.ViewerCommand != "vim -n" {
		t.Fatalf("persisted settings = %+v, want notify on + vim -n", persisted)
	}
}

// Esc-cancel drops Interface edits — nothing reaches config or the
// dashboard, and nothing is written to disk.
func TestSettingsInterfaceCancelDropsEdits(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 60})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	h.app.config.Paths.ConfigPath = filepath.Join(t.TempDir(), "amux-config.json")

	h.app.handleShowSettingsDialog()
	d := h.app.overlays.settings
	for range 4 {
		d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // toggle keymap hints on (dialog-local)
	if !d.ShowKeymapHints() {
		t.Fatal("dialog-local toggle should register")
	}

	h.app.handleSettingsResult(common.SettingsResult{Canceled: true})
	if h.app.config.UI.ShowKeymapHints {
		t.Fatal("canceled edit must not reach config")
	}
	if persisted := h.app.config.PersistedUISettings(); persisted.ShowKeymapHints {
		t.Fatal("canceled edit must not persist")
	}
}

// Every UISettings field is either dialog-exposed or explicitly marked
// JSON-only — a new ui.* key fails here until its surface is decided, the
// same tripwire plan 078 put on the dialog registry.
func TestSettingsDialogCoversUISettings(t *testing.T) {
	dialogExposed := map[string]bool{
		"ShowKeymapHints":  true,
		"NotifyOnDone":     true,
		"ViewerCommand":    true,
		"Theme":            true, // the theme section
		"TmuxServer":       true,
		"TmuxConfigPath":   true,
		"TmuxSyncInterval": true,
	}
	jsonOnly := map[string]bool{} // deliberately empty today
	for _, f := range reflect.VisibleFields(reflect.TypeOf(config.UISettings{})) {
		if !dialogExposed[f.Name] && !jsonOnly[f.Name] {
			t.Errorf("UISettings.%s has no settings-dialog row and is not marked JSON-only — decide its surface", f.Name)
		}
	}
}
