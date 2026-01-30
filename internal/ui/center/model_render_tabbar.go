package center

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// renderTabBar renders the tab bar with activity indicators
func (m *Model) renderTabBar() string {
	m.tabHits = m.tabHits[:0]
	currentTabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()

	if len(currentTabs) == 0 {
		empty := m.styles.TabPlus.Render("New agent")
		emptyWidth := lipgloss.Width(empty)
		if emptyWidth > 0 {
			m.tabHits = append(m.tabHits, tabHit{
				kind:  tabHitPlus,
				index: -1,
				region: common.HitRegion{
					X:      0,
					Y:      0,
					Width:  emptyWidth,
					Height: 1,
				},
			})
		}
		return empty
	}

	var renderedTabs []string
	x := 0

	for i, tab := range currentTabs {
		name := tab.Name
		if name == "" {
			name = tab.Assistant
		}

		// Check if tab is disconnected (detached or stopped)
		tab.mu.Lock()
		tabDisconnected := tab.Detached || !tab.Running
		tab.mu.Unlock()

		// Add brand color indicator for agent tabs (not file viewers)
		var indicator string
		var tabActive bool
		isAgent := tab.Assistant == "claude" || tab.Assistant == "codex" ||
			tab.Assistant == "gemini" || tab.Assistant == "amp" ||
			tab.Assistant == "opencode" || tab.Assistant == "droid" ||
			tab.Assistant == "cursor"
		if isAgent {
			if tabDisconnected {
				indicator = common.Icons.Idle + " " // Disconnected indicator
			} else {
				indicator = common.Icons.Running + " " // Brand color dot
			}
			tabActive = m.IsTabActive(tab)
		}

		// Get agent-specific color
		var agentStyle lipgloss.Style
		switch tab.Assistant {
		case "claude":
			agentStyle = m.styles.AgentClaude
		case "codex":
			agentStyle = m.styles.AgentCodex
		case "gemini":
			agentStyle = m.styles.AgentGemini
		case "amp":
			agentStyle = m.styles.AgentAmp
		case "opencode":
			agentStyle = m.styles.AgentOpencode
		case "droid":
			agentStyle = m.styles.AgentDroid
		case "cursor":
			agentStyle = m.styles.AgentCursor
		default:
			agentStyle = m.styles.AgentTerm
		}

		// Build tab content with close affordance
		closeLabel := m.styles.Muted.Render("×")
		var rendered string
		var style lipgloss.Style
		if i == activeIdx {
			// Active tab - each part styled with same background
			bg := common.ColorSurface2
			pad := lipgloss.NewStyle().Background(bg).Render(" ")
			// Use muted color for disconnected tabs
			indicatorFg := agentStyle.GetForeground()
			if tabDisconnected {
				indicatorFg = common.ColorMuted
			}
			indicatorPart := lipgloss.NewStyle().Foreground(indicatorFg).Background(bg).Render(indicator)
			// Use primary color and bold when actively working, muted when disconnected
			nameStyle := lipgloss.NewStyle().Foreground(common.ColorForeground).Background(bg)
			if tabDisconnected {
				nameStyle = nameStyle.Foreground(common.ColorMuted)
			} else if tabActive {
				nameStyle = nameStyle.Foreground(common.ColorPrimary).Bold(true)
			}
			namePart := nameStyle.Render(name)
			space := lipgloss.NewStyle().Background(bg).Render(" ")
			closePart := lipgloss.NewStyle().Foreground(common.ColorMuted).Background(bg).Render("×")
			rendered = pad + indicatorPart + namePart + space + closePart + pad
			style = m.styles.ActiveTab
		} else {
			// Inactive tab - muted with colored indicator, or primary color + bold when active
			var nameStyled string
			if tabDisconnected {
				nameStyled = m.styles.Muted.Render(name)
			} else if tabActive {
				nameStyled = lipgloss.NewStyle().Foreground(common.ColorPrimary).Bold(true).Render(name)
			} else {
				nameStyled = m.styles.Muted.Render(name)
			}
			// Use muted indicator color for disconnected tabs
			var indicatorStyled string
			if tabDisconnected {
				indicatorStyled = m.styles.Muted.Render(indicator)
			} else {
				indicatorStyled = agentStyle.Render(indicator)
			}
			content := indicatorStyled + nameStyled + " " + closeLabel
			rendered = m.styles.Tab.Render(content)
			style = m.styles.Tab
		}
		renderedWidth := lipgloss.Width(rendered)
		if renderedWidth > 0 {
			m.tabHits = append(m.tabHits, tabHit{
				kind:  tabHitTab,
				index: i,
				region: common.HitRegion{
					X:      x,
					Y:      0,
					Width:  renderedWidth,
					Height: 1,
				},
			})

			frameX, _ := style.GetFrameSize()
			leftFrame := frameX / 2
			prefixWidth := lipgloss.Width(agentStyle.Render(indicator) + name + " ")
			closeWidth := lipgloss.Width(closeLabel)
			closeX := x + leftFrame + prefixWidth
			if closeWidth > 0 {
				// Expand close button hit region for easier clicking
				expandedCloseX := closeX - 1
				expandedCloseWidth := renderedWidth - leftFrame - prefixWidth + 1
				m.tabHits = append(m.tabHits, tabHit{
					kind:  tabHitClose,
					index: i,
					region: common.HitRegion{
						X:      expandedCloseX,
						Y:      0,
						Width:  expandedCloseWidth,
						Height: 1,
					},
				})
			}
		}
		x += renderedWidth
		renderedTabs = append(renderedTabs, rendered)
	}

	// Add control buttons with matching border style
	btn := m.styles.TabPlus.Render("+ New")
	btnWidth := lipgloss.Width(btn)
	if btnWidth > 0 {
		m.tabHits = append(m.tabHits, tabHit{
			kind:  tabHitPlus,
			index: -1,
			region: common.HitRegion{
				X:      x,
				Y:      0,
				Width:  btnWidth,
				Height: 1,
			},
		})
	}
	renderedTabs = append(renderedTabs, btn)

	// Join tabs horizontally at the bottom so borders align
	return lipgloss.JoinHorizontal(lipgloss.Bottom, renderedTabs...)
}

func (m *Model) handleTabBarClick(msg tea.MouseClickMsg) tea.Cmd {
	// Tab bar is at screen Y=1: Y=0 is pane border, Y=1 is tab content (compact, no tab border)
	// Account for border (1) and padding (1) on the left side when converting X coordinates
	const (
		borderTop   = 1
		borderLeft  = 1
		paddingLeft = 1
	)
	if msg.Y != borderTop {
		return nil
	}
	// Convert screen X to content X (subtract pane offset, border, and padding)
	localX := msg.X - m.offsetX - borderLeft - paddingLeft
	if localX < 0 {
		return nil
	}
	// Convert screen Y to local Y within tab bar content (all tab hits are at Y=0)
	localY := msg.Y - borderTop
	// Check close buttons first (they overlap with tab regions)
	for _, hit := range m.tabHits {
		if hit.kind == tabHitClose && hit.region.Contains(localX, localY) {
			return m.closeTabAt(hit.index)
		}
	}
	// Then check tabs and other buttons
	for _, hit := range m.tabHits {
		if hit.region.Contains(localX, localY) {
			switch hit.kind {
			case tabHitPlus:
				return func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
			case tabHitTab:
				m.setActiveTabIdx(hit.index)
				return m.tabSelectionChangedCmd()
			}
		}
	}
	return nil
}
