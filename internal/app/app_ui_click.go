package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func (a *App) handleCenterPaneClick(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft {
		return nil
	}
	if a.layout == nil || !a.layout.ShowCenter() || a.center.HasTabs() {
		return nil
	}
	dashWidth := a.layout.DashboardWidth()
	centerWidth := a.layout.CenterWidth()
	gapX := a.layout.GapX()
	if centerWidth <= 0 {
		return nil
	}
	centerStart := a.layout.LeftGutter() + dashWidth + gapX
	centerEnd := centerStart + centerWidth
	if msg.X < centerStart || msg.X >= centerEnd {
		return nil
	}
	contentX, contentY := a.centerPaneContentOrigin()
	localX := msg.X - contentX
	localY := msg.Y - contentY
	if localX < 0 || localY < 0 {
		return nil
	}

	if a.showWelcome {
		return a.handleWelcomeClick(localX, localY)
	}
	if a.activeWorkspace != nil {
		return a.handleWorkspaceInfoClick(localX, localY)
	}
	return nil
}

func (a *App) handleWelcomeClick(localX, localY int) tea.Cmd {
	content := a.welcomeContent()
	lines := strings.Split(content, "\n")
	_, contentHeight := viewDimensions(content)

	placeWidth := a.layout.CenterWidth() - 4
	placeHeight := a.layout.Height() - 2
	if placeWidth <= 0 || placeHeight <= 0 {
		return nil
	}

	offsetY := centerOffset(placeHeight, contentHeight)

	for i, line := range lines {
		strippedLine := ansi.Strip(line)
		lineWidth := lipgloss.Width(line)
		lineOffsetX := centerOffset(placeWidth, lineWidth)

		settingsText := "[Settings]"
		if idx := strings.Index(strippedLine, settingsText); idx >= 0 {
			region := common.HitRegion{
				X:      idx + lineOffsetX,
				Y:      i + offsetY,
				Width:  len(settingsText),
				Height: 1,
			}
			if region.Contains(localX, localY) {
				return func() tea.Msg { return messages.ShowSettingsDialog{} }
			}
		}

		addProjectText := "[Add project]"
		if idx := strings.Index(strippedLine, addProjectText); idx >= 0 {
			region := common.HitRegion{
				X:      idx + lineOffsetX,
				Y:      i + offsetY,
				Width:  len(addProjectText),
				Height: 1,
			}
			if region.Contains(localX, localY) {
				return func() tea.Msg { return messages.ShowAddProjectDialog{} }
			}
		}
	}

	return nil
}

func (a *App) handleWorkspaceInfoClick(localX, localY int) tea.Cmd {
	if a.activeWorkspace == nil {
		return nil
	}
	content := a.renderWorkspaceInfo()
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		strippedLine := ansi.Strip(line)
		agentText := "[New agent]"
		if idx := strings.Index(strippedLine, agentText); idx >= 0 {
			region := common.HitRegion{
				X:      idx,
				Y:      i,
				Width:  len(agentText),
				Height: 1,
			}
			if region.Contains(localX, localY) {
				return func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
			}
		}
	}

	return nil
}
