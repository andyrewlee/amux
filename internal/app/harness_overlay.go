package app

import (
	"fmt"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// Overlay values accepted by HarnessOptions.Overlay. The empty value renders the
// base pane with no overlay (the historical harness behavior).
const (
	HarnessOverlayNone     = ""
	HarnessOverlayDialog   = "dialog"
	HarnessOverlaySettings = "settings"
	HarnessOverlayPrefix   = "prefix"
)

// applyHarnessOverlay puts the App into the overlay state named by overlay so a
// subsequent Render exercises composeOverlays (app_view_overlays.go) instead of
// only the base-pane chrome. Adding or altering a dialog/overlay is the single
// most common UI change an agent makes, yet the streaming harness had no way to
// render one headlessly; this wiring closes that gap.
//
// Only deterministic, filesystem-independent overlays are supported so the
// rendered frame stays byte-stable for goldens: the confirm dialog, the settings
// dialog, and the prefix command palette. (The file picker reads the real
// filesystem and the toast's visibility is wall-clock gated, so neither yields a
// reproducible golden; they are intentionally excluded.)
func applyHarnessOverlay(app *App, overlay string) {
	switch overlay {
	case HarnessOverlayNone:
		// Base pane only; nothing to show.
	case HarnessOverlayDialog:
		app.dialog = common.NewConfirmDialog(
			DialogDeleteWorkspace,
			"Delete Workspace",
			"Delete workspace 'primary' and its branch?",
		)
		app.presentDialog(app.dialog)
	case HarnessOverlaySettings:
		// Use a fixed theme (not app.config.UI.Theme) so the golden frame is
		// deterministic across machines: DefaultConfig() reads the developer's
		// real ~/.amux/config.json, which would otherwise bake a machine-specific
		// theme selection into testdata/golden and fail CI on a clean checkout.
		// ThemeGruvbox matches the chrome theme.Init() installs in the harness.
		app.settingsDialog = common.NewSettingsDialog(common.ThemeGruvbox)
		app.settingsDialog.SetSize(app.width, app.height)
		app.settingsDialog.SetUpdateInfo(app.version, "", false)
		app.settingsDialog.Show()
	case HarnessOverlayPrefix:
		app.prefixActive = true
	}
}

func validateHarnessOverlay(overlay string) error {
	switch overlay {
	case HarnessOverlayNone, HarnessOverlayDialog, HarnessOverlaySettings, HarnessOverlayPrefix:
		return nil
	default:
		return fmt.Errorf("unknown overlay %q", overlay)
	}
}
