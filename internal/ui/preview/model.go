package preview

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Model renders the preview pane.
type Model struct {
	URL     string
	Running bool
	Started time.Time

	focused bool
	width   int
	height  int

	styles common.Styles

	showKeymapHints bool
	zone            *zone.Manager
}

// New creates a preview model.
func New() *Model {
	return &Model{styles: common.DefaultStyles()}
}

// Init initializes the preview.
func (m *Model) Init() tea.Cmd { return nil }

// SetSize sets dimensions.
func (m *Model) SetSize(width, height int) { m.width, m.height = width, height }

// Focus sets focus.
func (m *Model) Focus() { m.focused = true }

// Blur removes focus.
func (m *Model) Blur() { m.focused = false }

// Focused returns focus state.
func (m *Model) Focused() bool { return m.focused }

// SetShowKeymapHints toggles hints.
func (m *Model) SetShowKeymapHints(show bool) { m.showKeymapHints = show }

// SetZone sets the shared zone manager.
func (m *Model) SetZone(z *zone.Manager) { m.zone = z }

// SetRunning sets whether a dev server is running.
func (m *Model) SetRunning(running bool) {
	if running && !m.Running {
		m.Started = time.Now()
	}
	if !running {
		m.Started = time.Time{}
	}
	m.Running = running
}

// Update handles keys.
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
			return m, func() tea.Msg { return messages.RefreshPreview{} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("c"))):
			return m, func() tea.Msg { return messages.CopyPreviewURL{} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("o"))):
			return m, func() tea.Msg { return messages.OpenDevURL{} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("e"))):
			return m, func() tea.Msg { return messages.EditPreviewURL{} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("l"))):
			return m, func() tea.Msg { return messages.TogglePreviewLogs{} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("s"))):
			if m.Running {
				return m, func() tea.Msg { return messages.StopPreviewServer{} }
			}
			return m, func() tea.Msg { return messages.RunScript{ScriptType: "run"} }
		}
	case tea.MouseClickMsg:
		return m.handleMouse(msg)
	}
	return m, nil
}

func (m *Model) handleMouse(msg tea.MouseClickMsg) (*Model, tea.Cmd) {
	// Zone-based click handling temporarily disabled for bubbletea v2 migration
	// TODO: Implement hit-region based click handling
	return m, nil
}

// View renders preview content.
func (m *Model) View() string {
	contentWidth := m.width - 2
	if contentWidth < 1 {
		contentWidth = 1
	}
	contentHeight := m.height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}

	toolbar := m.toolbarLine(contentWidth)
	if toolbar != "" && contentHeight < 2 {
		toolbar = ""
	}

	helpLines := m.helpLines(contentWidth)
	if !m.showKeymapHints {
		helpLines = nil
	}
	if len(helpLines) > contentHeight-1 {
		helpLines = nil
	}

	var b strings.Builder
	if toolbar != "" {
		b.WriteString(toolbar)
		b.WriteString("\n")
	}

	if m.URL == "" {
		if m.Running {
			if !m.Started.IsZero() && time.Since(m.Started) > 20*time.Second {
				b.WriteString(m.styles.Warning.Render(truncate("Still waiting for server. Check logs or configure run script.", contentWidth)))
			} else {
				b.WriteString(m.styles.Muted.Render("Starting dev server..."))
			}
		} else {
			b.WriteString(m.styles.Muted.Render("No dev server running"))
		}
	} else {
		url := truncate(m.URL, contentWidth)
		if m.zone != nil {
			url = m.zone.Mark(toolbarZoneID("url"), url)
		}
		b.WriteString(m.styles.Title.Render("Preview"))
		b.WriteString("\n")
		b.WriteString(url)
	}

	if len(helpLines) > 0 {
		b.WriteString("\n")
		b.WriteString(strings.Join(helpLines, "\n"))
	}

	style := m.styles.Pane
	if m.focused {
		style = m.styles.FocusedPane
	}
	return style.Width(m.width - 2).Render(b.String())
}

func (m *Model) toolbarLine(contentWidth int) string {
	type toolbarButton struct {
		ID    string
		Label string
	}
	buttons := []toolbarButton{
		{ID: "close", Label: "Close"},
		{ID: "refresh", Label: "Refresh"},
		{ID: "copy", Label: "Copy"},
		{ID: "open", Label: "Open"},
		{ID: "edit", Label: "Edit"},
		{ID: "logs", Label: "Logs"},
		{ID: "config", Label: "Configure"},
	}
	if m.Running {
		buttons = append(buttons, toolbarButton{ID: "stop", Label: "Stop"})
	} else {
		buttons = append(buttons, toolbarButton{ID: "start", Label: "Start"})
	}

	var included []toolbarButton
	lineWidth := 0
	for _, btn := range buttons {
		label := "[" + btn.Label + "]"
		if lineWidth > 0 {
			lineWidth += 1
		}
		lineWidth += lipgloss.Width(label)
		if lineWidth > contentWidth {
			break
		}
		included = append(included, btn)
	}
	if len(included) == 0 {
		return ""
	}

	parts := make([]string, 0, len(included))
	for _, btn := range included {
		text := m.styles.Muted.Render("[" + btn.Label + "]")
		if m.zone != nil {
			text = m.zone.Mark(toolbarZoneID(btn.ID), text)
		}
		parts = append(parts, text)
	}
	line := strings.Join(parts, " ")
	return padRight(line, contentWidth)
}

func (m *Model) helpLines(contentWidth int) []string {
	items := []string{
		common.RenderHelpItem(m.styles, "r", "Refresh"),
		common.RenderHelpItem(m.styles, "c", "Copy URL"),
		common.RenderHelpItem(m.styles, "o", "Open"),
		common.RenderHelpItem(m.styles, "e", "Edit"),
		common.RenderHelpItem(m.styles, "l", "Logs"),
		common.RenderHelpItem(m.styles, "s", "Stop"),
	}
	return common.WrapHelpItems(items, contentWidth)
}

func truncate(text string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

func padRight(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) >= width {
		return text
	}
	return text + strings.Repeat(" ", width-lipgloss.Width(text))
}

func toolbarZoneID(id string) string {
	return "preview-toolbar-" + id
}
