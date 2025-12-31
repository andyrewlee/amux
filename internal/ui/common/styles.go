package common

import "github.com/charmbracelet/lipgloss"

// Styles contains all the application styles
type Styles struct {
	// Layout - Pane borders and structure
	Pane        lipgloss.Style
	FocusedPane lipgloss.Style

	// Text hierarchy
	Title    lipgloss.Style // App name, section headers
	Subtitle lipgloss.Style // Secondary headers
	Body     lipgloss.Style // Normal text
	Muted    lipgloss.Style // De-emphasized text
	Bold     lipgloss.Style // Emphasized text

	// Dashboard - Project tree
	ProjectHeader  lipgloss.Style
	WorktreeRow    lipgloss.Style
	ActiveWorktree lipgloss.Style
	SelectedRow    lipgloss.Style
	CreateButton   lipgloss.Style
	HomeRow        lipgloss.Style
	AddProjectRow  lipgloss.Style

	// Status badges
	StatusClean   lipgloss.Style
	StatusDirty   lipgloss.Style
	StatusPending lipgloss.Style
	StatusRunning lipgloss.Style

	// Git status file indicators
	StatusModified  lipgloss.Style
	StatusAdded     lipgloss.Style
	StatusDeleted   lipgloss.Style
	StatusRenamed   lipgloss.Style
	StatusUntracked lipgloss.Style

	// Center pane - Tabs
	Tab       lipgloss.Style
	ActiveTab lipgloss.Style
	TabBar    lipgloss.Style
	TabPlus   lipgloss.Style

	// Center pane - Agent indicators
	AgentClaude   lipgloss.Style
	AgentCodex    lipgloss.Style
	AgentGemini   lipgloss.Style
	AgentAmp      lipgloss.Style
	AgentOpencode lipgloss.Style
	AgentTerm     lipgloss.Style

	// Sidebar
	SidebarHeader lipgloss.Style
	SidebarRow    lipgloss.Style
	BranchName    lipgloss.Style
	FilePath      lipgloss.Style

	// Help bar
	Help          lipgloss.Style
	HelpKey       lipgloss.Style
	HelpDesc      lipgloss.Style
	HelpSeparator lipgloss.Style

	// Dialogs
	DialogBox     lipgloss.Style
	DialogTitle   lipgloss.Style
	DialogMessage lipgloss.Style
	DialogOption  lipgloss.Style
	DialogActive  lipgloss.Style

	// Feedback
	Error   lipgloss.Style
	Success lipgloss.Style
	Warning lipgloss.Style
	Info    lipgloss.Style

	// Toast notifications
	ToastSuccess lipgloss.Style
	ToastError   lipgloss.Style
	ToastInfo    lipgloss.Style
}

// activeTabBorder returns a border for active tabs (open bottom to connect to content)
func activeTabBorder() lipgloss.Border {
	b := lipgloss.RoundedBorder()
	b.BottomLeft = "│"
	b.Bottom = " "
	b.BottomRight = "│"
	return b
}

// inactiveTabBorder returns a border for inactive tabs (closed bottom)
func inactiveTabBorder() lipgloss.Border {
	b := lipgloss.RoundedBorder()
	b.BottomLeft = "┴"
	b.Bottom = "─"
	b.BottomRight = "┴"
	return b
}

// TabGap returns a string to fill the gap between tabs in the tab bar
func TabGap() string {
	return lipgloss.NewStyle().
		Border(lipgloss.Border{Bottom: "─"}, false, false, true, false).
		BorderForeground(ColorBorder).
		Render(" ")
}

// DefaultStyles returns the default application styles using Tokyo Night palette
func DefaultStyles() Styles {
	return Styles{
		// Layout - Pane borders
		Pane: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(0, 1),

		FocusedPane: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorderFocused).
			Padding(0, 1),

		// Text hierarchy
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary),

		Subtitle: lipgloss.NewStyle().
			Foreground(ColorForeground),

		Body: lipgloss.NewStyle().
			Foreground(ColorForeground),

		Muted: lipgloss.NewStyle().
			Foreground(ColorMuted),

		Bold: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorForeground),

		// Dashboard - Project tree
		ProjectHeader: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorMuted).
			MarginTop(1),

		WorktreeRow: lipgloss.NewStyle().
			PaddingLeft(2).
			Foreground(ColorForeground),

		ActiveWorktree: lipgloss.NewStyle().
			PaddingLeft(2).
			Bold(true).
			Foreground(ColorPrimary),

		SelectedRow: lipgloss.NewStyle().
			PaddingLeft(2).
			Bold(true).
			Foreground(ColorForeground).
			Background(ColorSelection),

		CreateButton: lipgloss.NewStyle().
			PaddingLeft(2).
			Foreground(ColorMuted),

		HomeRow: lipgloss.NewStyle().
			Foreground(ColorSecondary),

		AddProjectRow: lipgloss.NewStyle().
			Foreground(ColorMuted),

		// Status badges
		StatusClean: lipgloss.NewStyle().
			Foreground(ColorSuccess),

		StatusDirty: lipgloss.NewStyle().
			Foreground(ColorError),

		StatusPending: lipgloss.NewStyle().
			Foreground(ColorWarning),

		StatusRunning: lipgloss.NewStyle().
			Foreground(ColorSecondary),

		// Git status file indicators
		StatusModified: lipgloss.NewStyle().
			Foreground(ColorWarning),

		StatusAdded: lipgloss.NewStyle().
			Foreground(ColorSuccess),

		StatusDeleted: lipgloss.NewStyle().
			Foreground(ColorError),

		StatusRenamed: lipgloss.NewStyle().
			Foreground(ColorInfo),

		StatusUntracked: lipgloss.NewStyle().
			Foreground(ColorMuted),

		// Center pane - Tabs (browser-style with rounded corners)
		Tab: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(ColorMuted).
			Border(inactiveTabBorder(), true).
			BorderForeground(ColorBorder),

		ActiveTab: lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true).
			Foreground(ColorForeground).
			Border(activeTabBorder(), true).
			BorderForeground(ColorPrimary),

		TabBar: lipgloss.NewStyle(),

		TabPlus: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(ColorMuted),

		// Center pane - Agent indicators
		AgentClaude: lipgloss.NewStyle().
			Foreground(ColorClaude),

		AgentCodex: lipgloss.NewStyle().
			Foreground(ColorCodex),

		AgentGemini: lipgloss.NewStyle().
			Foreground(ColorGemini),

		AgentAmp: lipgloss.NewStyle().
			Foreground(ColorAmp),

		AgentOpencode: lipgloss.NewStyle().
			Foreground(ColorOpencode),

		AgentTerm: lipgloss.NewStyle().
			Foreground(ColorForeground),

		// Sidebar
		SidebarHeader: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorMuted),

		SidebarRow: lipgloss.NewStyle().
			Foreground(ColorForeground),

		BranchName: lipgloss.NewStyle().
			Foreground(ColorSecondary),

		FilePath: lipgloss.NewStyle().
			Foreground(ColorForeground),

		// Help bar
		Help: lipgloss.NewStyle().
			Foreground(ColorMuted),

		HelpKey: lipgloss.NewStyle().
			Foreground(ColorPrimary),

		HelpDesc: lipgloss.NewStyle().
			Foreground(ColorMuted),

		HelpSeparator: lipgloss.NewStyle().
			Foreground(ColorBorder),

		// Dialogs
		DialogBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary).
			Padding(1, 2).
			Width(50),

		DialogTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			MarginBottom(1),

		DialogMessage: lipgloss.NewStyle().
			Foreground(ColorForeground),

		DialogOption: lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(ColorMuted),

		DialogActive: lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true).
			Foreground(ColorForeground).
			Background(ColorPrimary),

		// Feedback
		Error: lipgloss.NewStyle().
			Foreground(ColorError),

		Success: lipgloss.NewStyle().
			Foreground(ColorSuccess),

		Warning: lipgloss.NewStyle().
			Foreground(ColorWarning),

		Info: lipgloss.NewStyle().
			Foreground(ColorInfo),

		// Toast notifications
		ToastSuccess: lipgloss.NewStyle().
			Padding(0, 1).
			Background(ColorSuccess).
			Foreground(ColorBackground),

		ToastError: lipgloss.NewStyle().
			Padding(0, 1).
			Background(ColorError).
			Foreground(ColorBackground),

		ToastInfo: lipgloss.NewStyle().
			Padding(0, 1).
			Background(ColorInfo).
			Foreground(ColorBackground),
	}
}

// RenderHelpBar renders a help bar with the given key-description pairs
func RenderHelpBar(s Styles, items []struct{ Key, Desc string }, width int) string {
	var parts []string
	for _, item := range items {
		key := s.HelpKey.Render(item.Key)
		desc := s.HelpDesc.Render(item.Desc)
		parts = append(parts, key+":"+desc)
	}

	joined := lipgloss.JoinHorizontal(lipgloss.Center, parts...)
	return s.Help.Width(width).Render(joined)
}
