package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/ui/common"
)

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

	commands := h.app.overlays.settings.AssistantCommands()
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

	h.app.overlays.settings.AssistantCommands()["claude"] = "claude --resume"

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

	h.app.overlays.settings.AssistantCommands()["claude"] = "claude --resume"

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
	h.app.overlays.settings.AssistantCommands()["claude"] = "claude --resume"

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
	h.app.overlays.settings.AssistantCommands()["claude"] = "   "

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

// TestHandleSettingsResult_PersistsNewAssistant drives the Assistants
// section's ctrl+a add input end to end through the dialog's exported Update
// (Tab navigates sections; add mode types name -> command), then confirms the
// new assistant reaches config.Assistants and disk via SaveAssistants —
// while an existing assistant's interrupt tuning survives untouched.
func TestHandleSettingsResult_PersistsNewAssistant(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "amux-config.json")
	h.app.config.Paths.ConfigPath = configPath
	h.app.config.UI.Theme = string(common.ThemeGruvbox)
	h.app.config.Assistants = map[string]config.AssistantConfig{
		"claude": {Command: "claude", InterruptCount: 2, InterruptDelayMs: 200},
	}
	h.app.handleShowSettingsDialog()

	d := h.app.overlays.settings
	// Tab from Theme through the three tmux fields lands on Assistants.
	for range 4 {
		d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	for _, r := range "MyBot" {
		d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	for _, r := range "mybot --serve" {
		d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	cmd := h.app.handleSettingsResult(common.SettingsResult{})
	if cmd != nil {
		t.Fatal("expected no warning cmd when assistant save succeeds")
	}

	// New assistant persisted in memory (normalized name) with the command;
	// a zero-value config is fine — interruptSettings floors the count to 1.
	got, ok := h.app.config.Assistants["mybot"]
	if !ok || got.Command != "mybot --serve" {
		t.Fatalf("Assistants[mybot] = %+v (ok=%v), want command %q", got, ok, "mybot --serve")
	}
	// The existing assistant's non-command fields are preserved.
	if claude := h.app.config.Assistants["claude"]; claude.InterruptCount != 2 || claude.InterruptDelayMs != 200 {
		t.Fatalf("claude interrupt tuning lost: %+v", claude)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config file to be written: %v", err)
	}
	if !strings.Contains(string(data), `"command": "mybot --serve"`) {
		t.Fatalf("expected persisted new assistant in config, got %q", string(data))
	}
}

// TestHandleShowSettingsDialog_RejectsInvalidAssistantName pins the
// ValidateAssistant wiring: a name the config loader would drop on the next
// start is rejected inside the dialog instead of being persisted then lost.
func TestHandleShowSettingsDialog_RejectsInvalidAssistantName(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	h.app.config.Paths.ConfigPath = filepath.Join(t.TempDir(), "amux-config.json")
	h.app.config.UI.Theme = string(common.ThemeGruvbox)
	h.app.config.Assistants = map[string]config.AssistantConfig{
		"claude": {Command: "claude"},
	}
	h.app.handleShowSettingsDialog()

	d := h.app.overlays.settings
	for range 4 {
		d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	for _, r := range "bad!name" {
		d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	// Tab straight to the command field and commit — the commit path
	// re-validates the name, so this exercises the real gate (not just the
	// field-advance check).
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	for _, r := range "some-cmd" {
		d.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if view := d.View(); !strings.Contains(view, "letter/number") {
		t.Fatalf("expected a visible identifier rejection in the dialog, got:\n%s", view)
	}
	// Cancel out of the still-open add, then close — nothing may persist.
	d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	_ = h.app.handleSettingsResult(common.SettingsResult{})
	if _, ok := h.app.config.Assistants["bad!name"]; ok {
		t.Fatal("invalid name must never reach config.Assistants")
	}
}
