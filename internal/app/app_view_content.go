package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/ui/common"
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

	if a.activeWorkspace != nil {
		return a.renderWorkspaceInfo()
	}

	// Show group info when a group header is highlighted
	if a.activeGroup != nil && a.activeGroupWs == nil {
		return a.renderGroupInfo()
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
	a.activeWorkspace = nil
	a.activeGroup = nil
	a.activeGroupWs = nil
	a.center.SetWorkspace(nil)
	a.sidebar.SetWorkspace(nil)
	a.sidebar.SetGitStatus(nil)
	_ = a.sidebarTerminal.SetWorkspace(nil)
	a.dashboard.ClearActiveRoot()
	a.centerBtnFocused = false
	a.centerBtnIndex = 0
}

// renderGroupInfo renders information about the active group (when group header is highlighted)
func (a *App) renderGroupInfo() string {
	group := a.activeGroup
	title := a.styles.Title.Render(group.Name)
	content := title + "\n\n"

	repoLabel := lipgloss.NewStyle().Foreground(common.ColorMuted).Render("Repos:")
	content += repoLabel + "\n"
	for _, repo := range group.Repos {
		content += "    " + repo.Path + "\n"
	}

	activeStyle := lipgloss.NewStyle().Foreground(common.ColorForeground).Bold(true)
	inactiveStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)

	// Edit repos button
	editStyle := inactiveStyle
	if a.centerBtnFocused && a.centerBtnIndex == 0 {
		editStyle = activeStyle
	}
	editBtn := editStyle.Render("[Edit repos]")

	// New worktree button
	newWsStyle := inactiveStyle
	if a.centerBtnFocused && a.centerBtnIndex == 1 {
		newWsStyle = activeStyle
	}
	newWsBtn := newWsStyle.Render("[New worktree]")

	content += "\n" + lipgloss.JoinHorizontal(lipgloss.Left, editBtn, "  ", newWsBtn)

	return content
}

// renderWorkspaceInfo renders information about the active workspace (for center pane and Info tab)
func (a *App) renderWorkspaceInfo() string {
	ws := a.activeWorkspace

	var content string

	// For group workspaces, show group details
	if a.activeGroupWs != nil {
		content += fmt.Sprintf("Group: %s\n", a.activeGroupWs.GroupName)
		content += fmt.Sprintf("Branch: %s\n", ws.Branch)
		content += fmt.Sprintf("Path: %s\n", ws.Root)

		repoLabel := lipgloss.NewStyle().Foreground(common.ColorMuted).Render("Repos:")
		content += "\n" + repoLabel + "\n"
		for _, sec := range a.activeGroupWs.Secondary {
			repoName := filepath.Base(sec.Repo)
			baseInfo := ""
			if sec.Base != "" {
				baseInfo = lipgloss.NewStyle().Foreground(common.ColorMuted).Render(
					fmt.Sprintf(" [%s]", sec.Base),
				)
			}
			content += fmt.Sprintf("    %s%s\n", repoName, baseInfo)
		}

		if a.activeGroupWs.Isolated {
			content += a.renderIsolationInfo()
		}
	} else {
		content += fmt.Sprintf("Branch: %s\n", ws.Branch)
		content += fmt.Sprintf("Path: %s\n", ws.Root)

		if a.activeProject != nil {
			content += fmt.Sprintf("Project: %s\n", a.activeProject.Name)
		}

		if ws.Isolated {
			content += a.renderIsolationInfo()
		}
	}

	return content
}

// renderIsolationInfo renders the sandbox isolation details for the info tab.
func (a *App) renderIsolationInfo() string {
	sandboxLabel := lipgloss.NewStyle().Foreground(common.ColorError).Bold(true).Render("Sandboxed")
	detail := lipgloss.NewStyle().Foreground(common.ColorMuted)

	var b strings.Builder
	b.WriteString("\n" + sandboxLabel + "\n")
	b.WriteString(detail.Render("  Writes allowed:  workspace, git dir, claude profile config, /tmp") + "\n")
	b.WriteString(detail.Render("  Writes blocked:  everything else (home, system, etc.)") + "\n")
	b.WriteString(detail.Render("  Reads blocked:   ~/.ssh, ~/.gnupg, ~/.aws, ~/.docker, ~/.kube") + "\n")
	b.WriteString(detail.Render("  Permission prompts skipped (--dangerously-skip-permissions)") + "\n")
	return b.String()
}

// renderWelcome renders the welcome screen
func (a *App) renderWelcome() string {
	content := a.welcomeContent()

	// Center the content in the pane
	width := a.layout.CenterWidth() - 4 // Account for borders/padding
	height := a.layout.Height() - 2

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func (a *App) welcomeContent() string {
	logo, logoStyle := a.welcomeLogo()
	var b strings.Builder
	b.WriteString(logoStyle.Render(logo))
	b.WriteString("\n\n")

	activeStyle := lipgloss.NewStyle().Foreground(common.ColorForeground).Bold(true)
	inactiveStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)

	addProjectStyle := inactiveStyle
	settingsStyle := inactiveStyle
	if a.centerBtnFocused {
		if a.centerBtnIndex == 0 {
			addProjectStyle = activeStyle
		} else if a.centerBtnIndex == 1 {
			settingsStyle = activeStyle
		}
	}
	addProject := addProjectStyle.Render("[+ Add Workspace]")
	settingsBtn := settingsStyle.Render("[Settings]")
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Left, addProject, "  ", settingsBtn))
	b.WriteString("\n")
	if a.config.UI.ShowKeymapHints {
		b.WriteString(a.styles.Help.Render("Dashboard: j/k to move • Enter to select"))
	}
	return b.String()
}

func (a *App) welcomeLogo() (string, lipgloss.Style) {
	logo := `
                            888
                            888
88888b.d88b.   .d88b.     .d888 888  888 .d8888b   8888b.
888 "888 "88b d8P  Y8b d88" 888 888  888 88K          "88b
888  888  888 88888888 888  888 888  888 "Y8888b. .d888888
888  888  888 Y8b.     Y88b 888 Y88b 888      X88 888  888
888  888  888  "Y8888   "Y88888  "Y88888  88888P' "Y888888`

	logoStyle := lipgloss.NewStyle().
		Foreground(common.ColorPrimary).
		Bold(true)
	return logo, logoStyle
}
