package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/validation"
)

// handleShowSettingsDialog shows the settings dialog.
func (a *App) handleShowSettingsDialog() {
	a.requestOverlayOpen(func() {
		persistedUI := a.config.PersistedUISettings()
		a.overlays.settingsPersistedTheme = common.ThemeID(persistedUI.Theme)
		a.overlays.settingsThemeOriginal = common.ThemeID(a.config.UI.Theme)
		a.overlays.settingsThemeDirty = common.ThemeID(a.config.UI.Theme) != a.overlays.settingsPersistedTheme
		a.overlays.settingsSession++
		a.overlays.settings = common.NewSettingsDialog(
			common.ThemeID(a.config.UI.Theme),
			a.config.UI.TmuxServer,
			a.config.UI.TmuxConfigPath,
			a.config.UI.TmuxSyncInterval,
		)
		a.overlays.settings.SetUIOptions(
			a.config.UI.ShowKeymapHints,
			a.config.UI.NotifyOnDone,
			a.config.UI.ViewerCommand,
		)
		a.overlays.settings.SetAssistants(a.config.AssistantNames(), assistantCommandMap(a.config.Assistants))
		// Apply the same name rules the config loader enforces so a name added
		// in the dialog can't be silently dropped on the next load.
		a.overlays.settings.SetAssistantNameValidator(func(name string) string {
			if err := validation.ValidateAssistant(name); err != nil {
				return err.Error()
			}
			return ""
		})
		a.overlays.settings.SetSession(a.overlays.settingsSession)
		a.overlays.settings.SetSize(a.width, a.height)

		// Set update state
		if a.updateAvailable != nil {
			a.overlays.settings.SetUpdateInfo(
				a.updateAvailable.CurrentVersion,
				a.updateAvailable.LatestVersion,
				a.updateAvailable.UpdateAvailable,
			)
		} else {
			a.overlays.settings.SetUpdateInfo(a.version, "", false)
		}
		if a.updateService != nil && a.updateService.IsHomebrewBuild() {
			a.overlays.settings.SetUpdateHint("Installed via Homebrew - update with brew upgrade amux")
		}

		a.overlays.settings.Show()
	})
}

func (a *App) applyTheme(theme common.ThemeID) {
	common.SetCurrentTheme(theme)
	a.config.UI.Theme = string(theme)
	a.overlays.settingsThemeDirty = theme != a.overlays.settingsPersistedTheme
	a.styles = common.DefaultStyles()
	// Propagate styles to all components.
	a.propagateStyles()
}

// handleThemePreview handles live theme preview.
func (a *App) handleThemePreview(msg common.ThemePreview) tea.Cmd {
	if msg.Session != a.overlays.settingsSession {
		return nil
	}
	if a.overlays.settings != nil {
		a.overlays.settings.SetSelectedTheme(msg.Theme)
	}
	a.applyTheme(msg.Theme)
	return nil
}

// persistSettingsThemeIfDirty is the upgrade entry point: it stays gated on
// the theme being dirty, then delegates to the shared UI-section save so a
// failure becomes a pending obligation like any other UI save.
func (a *App) persistSettingsThemeIfDirty() tea.Cmd {
	if !a.overlays.settingsThemeDirty {
		return nil
	}
	return a.persistSettingsUIIfDirty(a.config.SaveUISettings)
}

// applySettingsTmux copies the dialog's (possibly edited) tmux values into the
// in-memory config and reports whether any changed. The values are read as
// AMUX_TMUX_* env vars at launch, so persisting them here takes effect on the
// next start (the dialog surfaces a "restart to apply" hint).
func (a *App) applySettingsTmux(d *common.SettingsDialog) bool {
	changed := false
	if v := d.TmuxServer(); v != a.config.UI.TmuxServer {
		a.config.UI.TmuxServer = v
		changed = true
	}
	if v := d.TmuxConfigPath(); v != a.config.UI.TmuxConfigPath {
		a.config.UI.TmuxConfigPath = v
		changed = true
	}
	if v := d.TmuxSyncInterval(); v != a.config.UI.TmuxSyncInterval {
		a.config.UI.TmuxSyncInterval = v
		changed = true
	}
	return changed
}

// applySettingsUI copies the dialog's Interface-section values into config and
// pushes them through the live setters — keymap hints and the done-bell take
// effect immediately; the viewer command lands on the next viewer tab (open
// viewers keep the command they launched with). Returns whether anything
// changed so the caller knows a SaveUISettings is owed.
func (a *App) applySettingsUI(d *common.SettingsDialog) bool {
	changed := false
	if v := d.ShowKeymapHints(); v != a.config.UI.ShowKeymapHints {
		a.setKeymapHintsEnabled(v)
		changed = true
	}
	if v := d.NotifyOnDone(); v != a.config.UI.NotifyOnDone {
		a.config.UI.NotifyOnDone = v
		a.dashboard.SetNotifyOnDone(v)
		changed = true
	}
	if v := d.ViewerCommand(); v != a.config.UI.ViewerCommand {
		// Match center.SetViewerCommand's normalization so the persisted
		// value equals what the next viewer actually runs.
		v = strings.TrimSpace(v)
		if v == "" {
			v = "vim"
		}
		a.config.UI.ViewerCommand = v
		if a.center != nil {
			a.center.SetViewerCommand(v)
		}
		changed = true
	}
	return changed
}

// assistantCommandMap flattens an assistants config map to name->command, the
// shape SettingsDialog.SetAssistants wants (it only exposes command editing;
// interrupt tuning stays config.json-only in this first cut).
func assistantCommandMap(assistants map[string]config.AssistantConfig) map[string]string {
	commands := make(map[string]string, len(assistants))
	for name, cfg := range assistants {
		commands[name] = cfg.Command
	}
	return commands
}

// applySettingsAssistants copies the dialog's (possibly edited) assistant
// commands into the in-memory config and reports whether any changed. A
// blank edited command is never persisted (never leave an assistant
// unlaunchable). Names added through the dialog's add input create a
// zero-value AssistantConfig with just the command — interruptSettings
// floors the interrupt count to 1 for unconfigured agents.
func (a *App) applySettingsAssistants(d *common.SettingsDialog) bool {
	changed := false
	for name, cmd := range d.AssistantCommands() {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		if a.config.Assistants == nil {
			a.config.Assistants = make(map[string]config.AssistantConfig)
		}
		cfg, ok := a.config.Assistants[name]
		if ok && cfg.Command == cmd {
			continue
		}
		cfg.Command = cmd
		a.config.Assistants[name] = cfg
		changed = true
	}
	return changed
}

// handleSettingsResult handles settings dialog close.
func (a *App) handleSettingsResult(res common.SettingsResult) tea.Cmd {
	if res.Canceled {
		// Esc cancels: revert any live theme preview to what was active when the
		// dialog opened and do not persist. Tmux and assistant edits are dropped
		// with it (the in-memory config is only mutated below, on confirm).
		a.applyTheme(a.overlays.settingsThemeOriginal)
		a.overlays.settingsThemeDirty = false
		a.overlays.settings = nil
		a.overlays.settingsSession++
		return nil
	}
	tmuxChanged := false
	uiChanged := false
	assistantsChanged := false
	if a.overlays.settings != nil {
		a.applyTheme(a.overlays.settings.SelectedTheme())
		tmuxChanged = a.applySettingsTmux(a.overlays.settings)
		uiChanged = a.applySettingsUI(a.overlays.settings)
		assistantsChanged = a.applySettingsAssistants(a.overlays.settings)
	}
	a.overlays.settings = nil
	a.overlays.settingsSession++

	// Confirmed edits become save obligations. "Changed in this dialog" and
	// "successfully saved" are different facts: once a save is owed it stays
	// pending until that section's write succeeds, so a later confirmation
	// retries it even when that dialog changed nothing. A dirty theme save
	// already persists the whole UI struct (tmux and interface fields
	// included, since the apply wrote them), so all three share the UI
	// obligation. Assistants live in a different config-file section
	// (SaveAssistants, not SaveUISettings), so it is tracked and attempted
	// independently — one failed section must not skip the other's attempt.
	a.overlays.settingsUIPersistPending = a.overlays.settingsUIPersistPending ||
		tmuxChanged || uiChanged || a.overlays.settingsThemeDirty
	a.overlays.settingsAssistantsPersistPending = a.overlays.settingsAssistantsPersistPending || assistantsChanged
	saveCmd := a.persistSettingsUIIfDirty(a.config.SaveUISettings)
	assistantsSaveCmd := a.persistSettingsAssistantsIfDirty(a.config.SaveAssistants)
	return common.SafeBatch(saveCmd, assistantsSaveCmd)
}
