package app

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func (a *App) centerPaneStyle() lipgloss.Style {
	width := a.layout.CenterWidth()
	height := a.layout.Height()

	style := lipgloss.NewStyle().
		Width(width-2).
		Height(height-2).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(common.ColorBorder).
		Padding(0, 1)

	if a.focusedPane == messages.PaneCenter {
		style = style.
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(common.ColorBorderFocused)
	}
	return style
}

// renderCenterPaneContent renders the center pane content when no tabs (raw content, no borders)
func (a *App) renderCenterPaneContent() string {
	if a.showWelcome {
		return a.renderWelcome()
	}

	if a.activeWorktree != nil {
		return a.renderWorktreeInfo()
	}

	return "Select a worktree from the dashboard"
}

func (a *App) centerPaneContentOrigin() (x, y int) {
	if a.layout == nil {
		return 0, 0
	}
	frameX, frameY := a.centerPaneStyle().GetFrameSize()
	gapX := 0
	if a.layout.ShowCenter() {
		gapX = a.layout.GapX()
	}
	return a.layout.LeftGutter() + a.layout.DashboardWidth() + gapX + frameX/2, a.layout.TopGutter() + frameY/2
}

func (a *App) goHome() {
	a.showWelcome = true
	a.activeWorktree = nil
	a.center.SetWorktree(nil)
	a.sidebar.SetWorktree(nil)
	a.sidebar.SetGitStatus(nil)
	_ = a.sidebarTerminal.SetWorktree(nil)
	a.dashboard.ClearActiveRoot()
	a.centerBtnFocused = false
	a.centerBtnIndex = 0
}

// renderWorktreeInfo renders information about the active worktree
func (a *App) renderWorktreeInfo() string {
	wt := a.activeWorktree

	title := a.styles.Title.Render(wt.Name)
	content := title + "\n\n"
	content += fmt.Sprintf("Branch: %s\n", wt.Branch)
	content += fmt.Sprintf("Path: %s\n", wt.Root)

	if a.activeProject != nil {
		content += fmt.Sprintf("Project: %s\n", a.activeProject.Name)
	}

	activeStyle := lipgloss.NewStyle().Foreground(common.ColorForeground).Bold(true)
	inactiveStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)

	btnStyle := inactiveStyle
	if a.centerBtnFocused && a.centerBtnIndex == 0 {
		btnStyle = activeStyle
	}
	agentBtn := btnStyle.Render("[New agent]")
	content += "\n" + agentBtn
	if a.config.UI.ShowKeymapHints {
		content += "\n" + a.styles.Help.Render("C-Spc a:new agent")
	}

	return content
}

// renderWelcome renders the welcome screen
func (a *App) renderWelcome() string {
	content := a.welcomeContent()

	// Center the content in the pane
	width := a.layout.CenterWidth() - 4 // Account for borders/padding
	height := a.layout.Height() - 4

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func (a *App) welcomeContent() string {
	logo, logoStyle := a.welcomeLogo()
	var b strings.Builder
	b.WriteString(logoStyle.Render(logo))
	b.WriteString("\n\n")

	activeStyle := lipgloss.NewStyle().Foreground(common.ColorForeground).Bold(true)
	inactiveStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)

	newProjectStyle := inactiveStyle
	settingsStyle := inactiveStyle
	if a.centerBtnFocused {
		if a.centerBtnIndex == 0 {
			newProjectStyle = activeStyle
		} else if a.centerBtnIndex == 1 {
			settingsStyle = activeStyle
		}
	}
	newProject := newProjectStyle.Render("[New project]")
	settingsBtn := settingsStyle.Render("[Settings]")
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Left, newProject, "  ", settingsBtn))
	b.WriteString("\n")
	if a.config.UI.ShowKeymapHints {
		b.WriteString(a.styles.Help.Render("Dashboard: j/k to move • Enter to select"))
	}
	return b.String()
}

func (a *App) welcomeLogo() (string, lipgloss.Style) {
	logo := `
 8888b.  88888b.d88b.  888  888 888  888
    "88b 888 "888 "88b 888  888  Y8bd8P
.d888888 888  888  888 888  888   X88K
888  888 888  888  888 Y88b 888 .d8""8b.
"Y888888 888  888  888  "Y88888 888  888`

	logoStyle := lipgloss.NewStyle().
		Foreground(common.ColorPrimary).
		Bold(true)
	return logo, logoStyle
}
