package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// persistSettingsUIIfDirty saves the UI settings section when it owes a
// write — either an earlier confirmed save failed (settingsUIPersistPending)
// or the live theme diverges from the persisted one (settingsThemeDirty).
// The obligation is recorded before attempting so a failure is retained for
// the next confirmation; only a successful write acknowledges it. One
// successful UI save acknowledges the whole section: theme, tmux, and
// interface fields all travel in the same write.
//
// save is a small local seam — production passes a.config.SaveUISettings.
func (a *App) persistSettingsUIIfDirty(save func() error) tea.Cmd {
	themeSave := a.overlays.settingsThemeDirty
	if !a.overlays.settingsUIPersistPending && !themeSave {
		return nil
	}
	a.overlays.settingsUIPersistPending = true
	if err := save(); err != nil {
		if themeSave {
			return common.ReportError("saving theme setting", err, "Failed to save theme setting")
		}
		return common.ReportError("saving UI settings", err, "Failed to save settings")
	}
	a.overlays.settingsUIPersistPending = false
	a.overlays.settingsThemeDirty = false
	a.overlays.settingsPersistedTheme = common.ThemeID(a.config.UI.Theme)
	return nil
}

// persistSettingsAssistantsIfDirty saves the assistants section when a
// confirmed save is still owed. Like the UI obligation, a failure stays
// pending for the next confirmation; success clears only this section.
//
// save is a small local seam — production passes a.config.SaveAssistants.
func (a *App) persistSettingsAssistantsIfDirty(save func() error) tea.Cmd {
	if !a.overlays.settingsAssistantsPersistPending {
		return nil
	}
	if err := save(); err != nil {
		return common.ReportError("saving assistants", err, "Failed to save assistant settings")
	}
	a.overlays.settingsAssistantsPersistPending = false
	return nil
}
