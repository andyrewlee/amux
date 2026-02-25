package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/messages"
)

// handleSandboxRulesEditorResult processes the sandbox rules editor result.
func (a *App) handleSandboxRulesEditorResult(msg messages.SandboxRulesEditorResult) tea.Cmd {
	a.sandboxRulesEditor = nil

	if !msg.Confirmed {
		a.handleShowSettingsDialog()
		return nil
	}

	rules := &config.SandboxRules{Rules: msg.Rules}
	if err := config.SaveSandboxRules(a.config.Paths.SandboxRulesPath, rules); err != nil {
		a.handleShowSettingsDialog()
		return a.toast.ShowError("Failed to save sandbox rules")
	}

	a.handleShowSettingsDialog()
	return a.toast.ShowSuccess("Sandbox rules saved")
}
