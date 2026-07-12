package app

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
	"github.com/andyrewlee/amux/internal/update"
)

// TestShowQuitDialog_ShowsConfirmDialog asserts that showQuitDialog creates a
// sized, visible confirm dialog rendering the quit prompt and that confirming
// it (after moving off the default "No") emits a DialogQuit result.
func TestShowQuitDialog_ShowsConfirmDialog(t *testing.T) {
	h := newDialogHarness(t)
	if h.app.dialog != nil {
		t.Fatal("expected no dialog before showQuitDialog")
	}

	h.app.showQuitDialog()

	if h.app.dialog == nil || !h.app.dialog.Visible() {
		t.Fatal("expected quit dialog to be visible")
	}

	view := dialogView(t, h.app.dialog)
	if !strings.Contains(view, "Quit AMUX") {
		t.Fatalf("expected quit title in view, got %q", view)
	}
	if !strings.Contains(view, "Are you sure you want to quit?") {
		t.Fatalf("expected quit message in view, got %q", view)
	}

	// A confirm dialog defaults to the "No" option, so a bare Enter is the
	// cancel path; it still carries the DialogQuit ID.
	res := confirmResult(t, h.app.dialog)
	if res.ID != DialogQuit {
		t.Fatalf("expected result ID %q, got %q", DialogQuit, res.ID)
	}
	if res.Confirmed {
		t.Fatal("expected quit dialog to default to the No option")
	}
}

// TestShowQuitDialog_ConfirmYes drives the dialog to the Yes option and asserts
// the confirmed quit result flows through.
func TestShowQuitDialog_ConfirmYes(t *testing.T) {
	h := newDialogHarness(t)
	h.app.showQuitDialog()

	// Move left ("h") to select Yes, then Enter to confirm.
	yes, _ := h.app.dialog.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	res := confirmResult(t, yes)
	if res.ID != DialogQuit {
		t.Fatalf("expected result ID %q, got %q", DialogQuit, res.ID)
	}
	if !res.Confirmed {
		t.Fatal("expected moving to the Yes option to confirm the quit dialog")
	}
}

// TestShowQuitDialog_GuardsReentry verifies a second call while the dialog is
// already visible keeps the existing instance rather than replacing it.
func TestShowQuitDialog_GuardsReentry(t *testing.T) {
	h := newDialogHarness(t)

	h.app.showQuitDialog()
	first := h.app.dialog
	if first == nil {
		t.Fatal("expected first quit dialog")
	}

	h.app.showQuitDialog()
	if h.app.dialog != first {
		t.Fatal("expected re-entrant show to keep the existing visible dialog")
	}
}

// TestShowQuitDialog_ReshowsAfterDismiss verifies that once the dialog is
// hidden the guard no longer blocks constructing a fresh dialog.
func TestShowQuitDialog_ReshowsAfterDismiss(t *testing.T) {
	h := newDialogHarness(t)

	h.app.showQuitDialog()
	first := h.app.dialog
	if first == nil {
		t.Fatal("expected first quit dialog")
	}

	first.Hide()
	h.app.showQuitDialog()
	if h.app.dialog == nil || !h.app.dialog.Visible() {
		t.Fatal("expected a fresh quit dialog after the previous one was dismissed")
	}
	if h.app.dialog == first {
		t.Fatal("expected a newly constructed dialog after dismissal")
	}
}

func TestHandleUpdateCheckComplete(t *testing.T) {
	tests := []struct {
		name            string
		msg             messages.UpdateCheckComplete
		wantStored      bool
		wantCurrent     string
		wantLatest      string
		wantReleaseNote string
	}{
		{
			name: "error result is ignored",
			msg: messages.UpdateCheckComplete{
				Err:             errors.New("network down"),
				UpdateAvailable: true,
				CurrentVersion:  "1.0.0",
				LatestVersion:   "1.1.0",
			},
			wantStored: false,
		},
		{
			name: "no update available is ignored",
			msg: messages.UpdateCheckComplete{
				UpdateAvailable: false,
				CurrentVersion:  "1.0.0",
				LatestVersion:   "1.0.0",
			},
			wantStored: false,
		},
		{
			name: "update available is stored",
			msg: messages.UpdateCheckComplete{
				UpdateAvailable: true,
				CurrentVersion:  "1.0.0",
				LatestVersion:   "1.2.3",
				ReleaseNotes:    "fixes",
			},
			wantStored:      true,
			wantCurrent:     "1.0.0",
			wantLatest:      "1.2.3",
			wantReleaseNote: "fixes",
		},
		{
			name: "error takes precedence over update available",
			msg: messages.UpdateCheckComplete{
				Err:             errors.New("boom"),
				UpdateAvailable: true,
				CurrentVersion:  "1.0.0",
				LatestVersion:   "2.0.0",
			},
			wantStored: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newDialogHarness(t)
			h.app.updateAvailable = nil

			cmd := h.app.handleUpdateCheckComplete(tc.msg)
			if cmd != nil {
				t.Fatalf("expected nil cmd, got %T", cmd)
			}

			if !tc.wantStored {
				if h.app.updateAvailable != nil {
					t.Fatalf("expected no stored update, got %+v", h.app.updateAvailable)
				}
				return
			}

			if h.app.updateAvailable == nil {
				t.Fatal("expected update info to be stored")
			}
			got := h.app.updateAvailable
			if got.CurrentVersion != tc.wantCurrent {
				t.Fatalf("CurrentVersion: want %q, got %q", tc.wantCurrent, got.CurrentVersion)
			}
			if got.LatestVersion != tc.wantLatest {
				t.Fatalf("LatestVersion: want %q, got %q", tc.wantLatest, got.LatestVersion)
			}
			if !got.UpdateAvailable {
				t.Fatal("expected stored UpdateAvailable to be true")
			}
			if got.ReleaseNotes != tc.wantReleaseNote {
				t.Fatalf("ReleaseNotes: want %q, got %q", tc.wantReleaseNote, got.ReleaseNotes)
			}
		})
	}
}

// TestHandleUpdateCheckComplete_UpdatesVisibleSettingsDialog verifies the
// settings dialog version line is refreshed only when the dialog is visible.
func TestHandleUpdateCheckComplete_UpdatesVisibleSettingsDialog(t *testing.T) {
	tests := []struct {
		name     string
		visible  bool
		wantInfo bool
	}{
		{name: "visible dialog receives update info", visible: true, wantInfo: true},
		{name: "hidden dialog is left untouched", visible: false, wantInfo: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newDialogHarness(t)
			h.app.settingsDialog = common.NewSettingsDialog(common.ThemeID(h.app.config.UI.Theme), "", "", "")
			h.app.settingsDialog.SetSize(h.app.width, h.app.height)
			if tc.visible {
				h.app.settingsDialog.Show()
			}

			cmd := h.app.handleUpdateCheckComplete(messages.UpdateCheckComplete{
				UpdateAvailable: true,
				CurrentVersion:  "9.9.9",
				LatestVersion:   "10.0.0",
			})
			if cmd != nil {
				t.Fatalf("expected nil cmd, got %T", cmd)
			}

			if !tc.visible {
				// View is empty when hidden, so re-show to inspect the lines and
				// confirm the version line was NOT updated.
				h.app.settingsDialog.Show()
				view := ansi.Strip(h.app.settingsDialog.View())
				if strings.Contains(view, "10.0.0") {
					t.Fatalf("expected hidden settings dialog to retain stale version, got %q", view)
				}
				return
			}

			view := ansi.Strip(h.app.settingsDialog.View())
			if !tc.wantInfo {
				return
			}
			if !strings.Contains(view, "9.9.9") {
				t.Fatalf("expected current version in settings view, got %q", view)
			}
			if !strings.Contains(view, "10.0.0") {
				t.Fatalf("expected latest version in settings view, got %q", view)
			}
		})
	}
}

func TestHandleUpgradeComplete(t *testing.T) {
	tests := []struct {
		name            string
		msg             messages.UpgradeComplete
		wantToastText   string
		wantClearUpdate bool
	}{
		{
			name:            "success clears update and toasts new version",
			msg:             messages.UpgradeComplete{NewVersion: "2.0.0"},
			wantToastText:   "Upgraded to 2.0.0",
			wantClearUpdate: true,
		},
		{
			name:            "failure toasts the error and keeps update info",
			msg:             messages.UpgradeComplete{Err: errors.New("disk full")},
			wantToastText:   "Upgrade failed: disk full",
			wantClearUpdate: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newDialogHarness(t)
			// Seed prior state to confirm the handler mutates it correctly.
			h.app.upgradeRunning = true
			h.app.updateAvailable = commonUpdateResult()

			cmd := h.app.handleUpgradeComplete(tc.msg)
			if cmd == nil {
				t.Fatal("expected a toast command from handleUpgradeComplete")
			}

			if h.app.upgradeRunning {
				t.Fatal("expected upgradeRunning to be cleared")
			}

			if tc.wantClearUpdate {
				if h.app.updateAvailable != nil {
					t.Fatalf("expected updateAvailable cleared on success, got %+v", h.app.updateAvailable)
				}
			} else if h.app.updateAvailable == nil {
				t.Fatal("expected updateAvailable retained on failure")
			}

			// ShowError/ShowSuccess display the toast synchronously inside the
			// handler; the returned cmd is only the deferred dismissal tick. So
			// the toast is already visible without running cmd.
			if !h.app.toast.Visible() {
				t.Fatal("expected toast to be visible after handleUpgradeComplete")
			}
			toastView := ansi.Strip(h.app.toast.View())
			if !strings.Contains(toastView, tc.wantToastText) {
				t.Fatalf("expected toast %q, got %q", tc.wantToastText, toastView)
			}
		})
	}
}

// TestHandleUpgradeComplete_UpdatesVisibleSettingsDialog verifies the settings
// dialog version line is refreshed on a successful upgrade only when visible.
func TestHandleUpgradeComplete_UpdatesVisibleSettingsDialog(t *testing.T) {
	h := newDialogHarness(t)
	h.app.settingsDialog = common.NewSettingsDialog(common.ThemeID(h.app.config.UI.Theme), "", "", "")
	h.app.settingsDialog.SetSize(h.app.width, h.app.height)
	h.app.settingsDialog.SetUpdateInfo("1.0.0", "2.0.0", true)
	h.app.settingsDialog.Show()
	h.app.upgradeRunning = true

	cmd := h.app.handleUpgradeComplete(messages.UpgradeComplete{NewVersion: "2.0.0"})
	if cmd == nil {
		t.Fatal("expected a toast command")
	}

	view := ansi.Strip(h.app.settingsDialog.View())
	if !strings.Contains(view, "2.0.0") {
		t.Fatalf("expected upgraded version in settings view, got %q", view)
	}
	// updateAvailable was set to true by SetUpdateInfo; SetUpdateInfo(false)
	// during the handler removes the "[Update to ...]" affordance.
	if strings.Contains(view, "Update to") {
		t.Fatalf("expected update affordance to be cleared after upgrade, got %q", view)
	}
}

func TestHandleOpenFileInEditor(t *testing.T) {
	ws := harnessWorkspace()

	tests := []struct {
		name    string
		msg     sidebar.OpenFileInEditor
		wantCmd bool
	}{
		{
			name:    "nil workspace is a noop",
			msg:     sidebar.OpenFileInEditor{Workspace: nil, Path: "/repo/file.go"},
			wantCmd: false,
		},
		{
			name:    "empty path is a noop",
			msg:     sidebar.OpenFileInEditor{Workspace: ws, Path: ""},
			wantCmd: false,
		},
		{
			name:    "nil workspace and empty path is a noop",
			msg:     sidebar.OpenFileInEditor{Workspace: nil, Path: ""},
			wantCmd: false,
		},
		{
			name:    "valid workspace and path returns a command",
			msg:     sidebar.OpenFileInEditor{Workspace: ws, Path: "/repo/primary/ws/main.go"},
			wantCmd: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newDialogHarness(t)
			beforeCenter := h.app.center

			cmd := h.app.handleOpenFileInEditor(tc.msg)

			if tc.wantCmd {
				if cmd == nil {
					t.Fatal("expected a command for a valid open-file request")
				}
				// The center model is reassigned even when the work is deferred
				// into the returned closure. We deliberately do NOT execute the
				// returned cmd because it would exec tmux/vim.
				if h.app.center == nil {
					t.Fatal("expected center to remain set after dispatch")
				}
				return
			}

			if cmd != nil {
				t.Fatalf("expected nil command for noop request, got %T", cmd)
			}
			// A noop must not mutate the center model.
			if h.app.center != beforeCenter {
				t.Fatal("expected center to be untouched for a noop request")
			}
		})
	}
}

// commonUpdateResult is a small fixture for seeding App.updateAvailable.
func commonUpdateResult() *update.CheckResult {
	return &update.CheckResult{
		CurrentVersion:  "1.0.0",
		LatestVersion:   "2.0.0",
		UpdateAvailable: true,
	}
}
