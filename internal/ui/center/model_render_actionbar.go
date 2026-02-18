package center

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/ui/common"
)

type actionBarItem struct {
	kind    actionBarButtonKind
	label   string
	enabled bool
}

// actionBarItems returns the visible action bar items based on workspace state.
// Note: Copy button is handled separately (next to path), so not included here.
func (m *Model) actionBarItems() []actionBarItem {
	if m.workspace == nil {
		return nil
	}

	// Check if we're on main branch
	onMainBranch := m.workspace.IsMainBranch()
	defaultBranch := m.getDefaultBranch()

	return []actionBarItem{
		{kind: actionBarCommit, label: "Commit", enabled: true},
		{kind: actionBarMergeToMain, label: "Merge to " + defaultBranch + " (local)", enabled: !onMainBranch},
	}
}

// getDefaultBranch returns the default branch name (main or master) for the repo.
// This uses cached value to avoid running git commands on every render.
func (m *Model) getDefaultBranch() string {
	if m.workspace == nil {
		return "main"
	}

	// If Base is set and not "HEAD", use it (cleaned up)
	if m.workspace.Base != "" && m.workspace.Base != "HEAD" {
		base := m.workspace.Base
		base = strings.TrimPrefix(base, "origin/")
		return base
	}

	// Default fallback - actual detection is done async when workspace is set
	return "main"
}

// getBaseBranchDisplay returns the base branch string for info bar display.
// Shows origin/main for remote refs, or "branch (local)" for local branches.
func (m *Model) getBaseBranchDisplay() string {
	if m.workspace == nil {
		return "main"
	}
	if m.workspace.Base != "" && m.workspace.Base != "HEAD" {
		base := m.workspace.Base
		if strings.HasPrefix(base, "origin/") {
			return base
		}
		return base + " (local)"
	}
	return "main"
}

// renderInfoBar renders the info bar with workspace details and action buttons.
// Layout: [branch info] │ [path] [Copy] ... [action buttons]
// Also renders a subtle separator line below.
func (m *Model) renderInfoBar(width int) string {
	m.actionBarHits = m.actionBarHits[:0]

	if m.workspace == nil || width < 20 {
		return ""
	}

	ws := m.workspace

	// Styles
	mutedStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	branchStyle := lipgloss.NewStyle().Foreground(common.ColorInfo)
	pathStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	separatorStyle := lipgloss.NewStyle().Foreground(common.ColorSurface2)
	buttonStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	disabledButtonStyle := lipgloss.NewStyle().Foreground(common.ColorSurface2)

	// Build branch info: "origin/main ← feature-branch" or just "main" if on main
	baseBranchDisplay := m.getBaseBranchDisplay()
	var branchInfo string
	if ws.IsMainBranch() {
		branchInfo = branchStyle.Render(ws.Branch)
	} else {
		branchInfo = mutedStyle.Render(baseBranchDisplay) + mutedStyle.Render(" ← ") + branchStyle.Render(ws.Branch)
	}

	// Build action buttons (right side)
	items := m.actionBarItems()
	var buttonParts []string

	for _, item := range items {
		label := "[" + item.label + "]"
		var style lipgloss.Style
		if !item.enabled {
			style = disabledButtonStyle
		} else {
			style = buttonStyle
		}
		rendered := style.Render(label)
		buttonParts = append(buttonParts, rendered)
	}

	buttonsStr := strings.Join(buttonParts, " ")
	buttonsWidth := lipgloss.Width(buttonsStr)

	// Calculate left side content
	separator := separatorStyle.Render(" │ ")
	separatorWidth := lipgloss.Width(separator)

	// Copy and IDE buttons (placed next to path)
	copyBtn := buttonStyle.Render("[Copy]")
	copyBtnWidth := lipgloss.Width(copyBtn)
	ideBtn := buttonStyle.Render("[IDE]")
	ideBtnWidth := lipgloss.Width(ideBtn)

	// Build path info (shortened)
	// Reserve space for: branchInfo + separator + path + space + copyBtn + space + ideBtn + gap + buttons
	minGap := 2
	reservedForRight := buttonsWidth + minGap
	reservedForLeft := lipgloss.Width(branchInfo) + separatorWidth + 1 + copyBtnWidth + 1 + ideBtnWidth // +1 for spaces
	availableForPath := width - reservedForLeft - reservedForRight
	if availableForPath < 10 {
		availableForPath = 10
	}

	pathInfo := shortenPath(ws.Root, availableForPath)
	pathRendered := pathStyle.Render(pathInfo)

	// Left content: branch │ path [Copy] [IDE]
	leftContent := branchInfo + separator + pathRendered + " " + copyBtn + " " + ideBtn
	leftWidth := lipgloss.Width(leftContent)

	// Calculate padding to right-align buttons
	padding := width - leftWidth - buttonsWidth
	if padding < minGap {
		padding = minGap
	}

	// Track Copy button hit region (it's part of left content)
	copyBtnX := lipgloss.Width(branchInfo + separator + pathRendered + " ")
	m.actionBarHits = append(m.actionBarHits, actionBarButton{
		kind:  actionBarCopyDir,
		label: "Copy",
		region: common.HitRegion{
			X:      copyBtnX,
			Y:      0,
			Width:  copyBtnWidth,
			Height: 1,
		},
	})

	// Track IDE button hit region (after Copy button)
	ideBtnX := copyBtnX + copyBtnWidth + 1 // +1 for space
	m.actionBarHits = append(m.actionBarHits, actionBarButton{
		kind:  actionBarOpenIDE,
		label: "IDE",
		region: common.HitRegion{
			X:      ideBtnX,
			Y:      0,
			Width:  ideBtnWidth,
			Height: 1,
		},
	})

	// Now calculate the actual X positions for action button hit regions
	buttonStartX := leftWidth + padding
	x := buttonStartX
	for i, item := range items {
		label := "[" + item.label + "]"
		labelWidth := lipgloss.Width(label)

		if item.enabled && labelWidth > 0 {
			m.actionBarHits = append(m.actionBarHits, actionBarButton{
				kind:  item.kind,
				label: item.label,
				region: common.HitRegion{
					X:      x,
					Y:      0,
					Width:  labelWidth,
					Height: 1,
				},
			})
		}
		x += labelWidth
		if i < len(items)-1 {
			x++ // space between buttons
		}
	}

	// Build the main line
	mainLine := leftContent + strings.Repeat(" ", padding) + buttonsStr

	// Add a subtle separator line below (using dim box-drawing character)
	separatorLine := separatorStyle.Render(strings.Repeat("─", width))

	return mainLine + "\n" + separatorLine
}

// shortenPath shortens a path to fit within maxLen characters.
// It replaces the home directory with ~ for more readable paths.
func shortenPath(path string, maxLen int) string {
	// First, try to replace home directory with ~
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if strings.HasPrefix(path, home) {
			path = "~" + strings.TrimPrefix(path, home)
		}
	}

	if len(path) <= maxLen {
		return path
	}

	// Take last parts of the path to fit within maxLen
	parts := strings.Split(path, string(filepath.Separator))
	result := ""
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]
		if part == "" {
			continue
		}
		if result == "" {
			result = part
		} else {
			candidate := part + string(filepath.Separator) + result
			if len(candidate)+4 > maxLen { // +4 for ".../"
				break
			}
			result = candidate
		}
	}

	// Add ellipsis prefix if we truncated
	if !strings.HasPrefix(path, result) && !strings.HasPrefix(result, "~") {
		result = "..." + string(filepath.Separator) + result
	}

	return result
}

// actionBarCommand returns the command for the given button kind.
func (m *Model) actionBarCommand(kind actionBarButtonKind) tea.Cmd {
	if m.workspace == nil {
		return nil
	}

	ws := m.workspace
	switch kind {
	case actionBarCopyDir:
		return func() tea.Msg {
			return messages.ActionBarCopyDir{WorkspaceRoot: ws.Root}
		}
	case actionBarOpenIDE:
		return func() tea.Msg {
			return messages.ActionBarOpenIDE{WorkspaceRoot: ws.Root}
		}
	case actionBarCommit:
		// Send commit instruction to the agent (use \r for Enter key)
		m.sendInputToActiveTab("commit changes related to what we're talking about\r")
		return nil
	case actionBarMergeToMain:
		if ws.IsMainBranch() {
			return nil
		}
		// Send merge instruction to the agent (use \r for Enter key)
		// Format: git -C {repo path} merge {branch name}
		instruction := "Merge this branch to the main branch by running git -C " + ws.Repo + " merge " + ws.Branch + "\r"
		m.sendInputToActiveTab(instruction)
		return nil
	}
	return nil
}

// sendInputToActiveTab sends text input to the active tab's terminal using tmux send-keys.
// This ensures Enter is properly sent as a keypress rather than a literal character.
func (m *Model) sendInputToActiveTab(text string) {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if activeIdx >= len(tabs) {
		return
	}
	tab := tabs[activeIdx]
	if tab == nil || tab.isClosed() {
		return
	}

	// Get session name for tmux send-keys
	sessionName := tab.SessionName
	if sessionName == "" && tab.Agent != nil {
		sessionName = tab.Agent.Session
	}
	if sessionName == "" {
		return
	}

	// Use tmux send-keys which properly handles Enter as a key press
	// Strip trailing \r since we'll send Enter separately via tmux
	text = strings.TrimSuffix(text, "\r")
	text = strings.TrimSuffix(text, "\n")

	// Get tmux options for server name
	opts := m.getTmuxOptions()
	baseArgs := []string{}
	if opts.ServerName != "" {
		baseArgs = append(baseArgs, "-L", opts.ServerName)
	}

	// Execute tmux send-keys in background
	// Send text first, then Enter separately with a small delay
	// This prevents Claude from treating it as a single paste event
	go func() {
		// Send text with -l (literal) flag
		textArgs := append(baseArgs, "send-keys", "-t", sessionName, "-l", text)
		cmd := exec.Command("tmux", textArgs...)
		_ = cmd.Run()

		// Small delay to separate text from Enter
		time.Sleep(50 * time.Millisecond)

		// Send Enter key separately
		enterArgs := append(baseArgs, "send-keys", "-t", sessionName, "Enter")
		cmd = exec.Command("tmux", enterArgs...)
		_ = cmd.Run()
	}()
}

// infoBarHeight returns the height of the info bar (2 if visible: content + separator, 0 otherwise).
func (m *Model) infoBarHeight() int {
	if m.workspace == nil {
		return 0
	}
	tabs := m.getTabs()
	if len(tabs) == 0 {
		return 0
	}
	return 2 // Main line + separator line
}

// InfoBarView returns the rendered info bar string for layer-based rendering.
func (m *Model) InfoBarView(width int) string {
	if m.infoBarHeight() == 0 {
		return ""
	}
	return m.renderInfoBar(width)
}

// InfoBarHeight returns the info bar height (exported for app rendering).
func (m *Model) InfoBarHeight() int {
	return m.infoBarHeight()
}

// SetInfoBarY sets the Y position of the info bar for mouse hit testing.
func (m *Model) SetInfoBarY(y int) {
	m.actionBarY = y
}

// handleInfoBarClick checks if a click is on an info bar button and returns the appropriate command.
func (m *Model) handleInfoBarClick(contentX, contentY int) tea.Cmd {
	if m.infoBarHeight() == 0 {
		return nil
	}

	// Check if click is within the info bar area (first line only, not separator)
	if contentY != m.actionBarY {
		return nil
	}

	// Check button hits
	for _, hit := range m.actionBarHits {
		if hit.region.Contains(contentX, 0) {
			return m.actionBarCommand(hit.kind)
		}
	}
	return nil
}

// Legacy compatibility - these now delegate to info bar
func (m *Model) actionBarHeight() int {
	return m.infoBarHeight()
}

func (m *Model) ActionBarHeight() int {
	return m.InfoBarHeight()
}

func (m *Model) ActionBarView(width int) string {
	return m.InfoBarView(width)
}

func (m *Model) SetActionBarY(y int) {
	m.SetInfoBarY(y)
}

func (m *Model) handleActionBarClick(contentX, contentY int) tea.Cmd {
	return m.handleInfoBarClick(contentX, contentY)
}
