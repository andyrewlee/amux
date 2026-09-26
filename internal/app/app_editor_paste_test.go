package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// These tests pin the overlay-chain routing for bracketed paste: a visible
// editor's text field receives the pasted line AND the message is consumed,
// so the same bytes never fall through to the focused terminal's PTY.

func overlayChainConsumes(t *testing.T, a *App, msg tea.Msg) bool {
	t.Helper()
	var cmds []tea.Cmd
	for _, slot := range a.overlayChain() {
		if slot(msg, &cmds) {
			return true
		}
	}
	return false
}

func TestEditorPasteOverlayEnvDialog(t *testing.T) {
	ws := &data.Workspace{
		Name: "feature",
		Repo: "/repo/primary",
		Root: "/repo/primary/ws",
		Env:  map[string]string{"NODE_ENV": "dev"},
	}
	h, store, wsID := newEnvTestHarness(t, ws)
	h.app.handleShowWorkspaceEnvDialog(messages.ShowWorkspaceEnvDialog{Workspace: ws})
	if h.app.overlays.env == nil || !h.app.overlays.env.Visible() {
		t.Fatal("expected env dialog to be shown")
	}

	// Paste lands in the focused row's value and the chain consumes it —
	// forwarded paste would also write these bytes to the focused terminal.
	paste := tea.PasteMsg{Content: "-pasted\nSECOND LINE"}
	if !overlayChainConsumes(t, h.app, paste) {
		t.Fatal("paste was not consumed by the overlay chain")
	}
	if got := h.app.overlays.env.Env()["NODE_ENV"]; got != "dev-pasted" {
		t.Fatalf("NODE_ENV = %q, want %q", got, "dev-pasted")
	}

	// Save path: Enter closes with a non-canceled result the app persists.
	_, cmd := h.app.overlays.env.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected Enter to produce a dialog result command")
	}
	res, ok := cmd().(common.EnvDialogResult)
	if !ok || res.Canceled {
		t.Fatal("expected a non-canceled EnvDialogResult")
	}
	h.app.handleEnvDialogResult(res)
	stored, err := store.Load(wsID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := stored.Env["NODE_ENV"]; got != "dev-pasted" {
		t.Fatalf("persisted NODE_ENV = %q, want %q", got, "dev-pasted")
	}
}

func TestEditorPasteOverlayScriptsDialogCancelKeepsValues(t *testing.T) {
	ws := &data.Workspace{
		Name:       "feature",
		Repo:       "/repo/primary",
		Root:       "/repo/primary/ws",
		Scripts:    data.ScriptsConfig{Setup: "make deps"},
		ScriptMode: "concurrent",
	}
	h, store, wsID := newScriptsTestHarness(t, ws)
	h.app.handleShowWorkspaceScriptsDialog(messages.ShowWorkspaceScriptsDialog{Workspace: ws})
	if h.app.overlays.scripts == nil || !h.app.overlays.scripts.Visible() {
		t.Fatal("expected scripts dialog to be shown")
	}

	if !overlayChainConsumes(t, h.app, tea.PasteMsg{Content: " && extra\nMULTILINE"}) {
		t.Fatal("paste was not consumed by the overlay chain")
	}
	setup, _, _, _, _ := h.app.overlays.scripts.Values()
	if setup != "make deps && extra" {
		t.Fatalf("setup field = %q, want %q", setup, "make deps && extra")
	}

	// Cancel: Esc closes with Canceled and the store keeps the old scripts.
	_, cmd := h.app.overlays.scripts.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	res, ok := cmd().(common.ScriptsDialogResult)
	if !ok || !res.Canceled {
		t.Fatal("expected Esc to produce a canceled ScriptsDialogResult")
	}
	h.app.handleScriptsDialogResult(res)
	stored, err := store.Load(wsID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if stored.Scripts.Setup != "make deps" {
		t.Fatalf("cancel mutated scripts: setup = %q", stored.Scripts.Setup)
	}
}

func TestEditorPasteOverlaySettingsTmuxField(t *testing.T) {
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "amux-config.json")
	h.app.config.Paths.ConfigPath = configPath
	// Pin the theme to the persisted default so this close is dirty for tmux
	// only, matching the settings tests in app_input_messages_dialogs_*_test.go.
	h.app.config.UI.Theme = string(common.ThemeGruvbox)

	h.app.handleShowSettingsDialog()
	if h.app.overlays.settings == nil || !h.app.overlays.settings.Visible() {
		t.Fatal("expected settings dialog to be shown")
	}

	// Focus the tmux server field the way a user would: Tab from theme.
	dlg, _ := h.app.overlays.settings.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	h.app.overlays.settings = dlg

	if !overlayChainConsumes(t, h.app, tea.PasteMsg{Content: "pasted-srv\nextra"}) {
		t.Fatal("paste was not consumed by the overlay chain")
	}
	if got := h.app.overlays.settings.TmuxServer(); got != "pasted-srv" {
		t.Fatalf("TmuxServer = %q, want %q", got, "pasted-srv")
	}

	// Confirm close persists the pasted value to in-memory config and disk.
	h.app.handleSettingsResult(common.SettingsResult{})
	if got := h.app.config.UI.TmuxServer; got != "pasted-srv" {
		t.Fatalf("config.UI.TmuxServer = %q, want %q", got, "pasted-srv")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config file to be written: %v", err)
	}
	if !strings.Contains(string(data), `"tmux_server": "pasted-srv"`) {
		t.Fatalf("persisted tmux_server missing from config: %q", string(data))
	}
}
