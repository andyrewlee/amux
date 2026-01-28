package board

import (
	"strconv"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// BoardSelection tracks selected column/row.
type BoardSelection struct {
	Column int
	Row    int
}

// BoardFilters describes board filters.
type BoardFilters struct {
	Search            string
	ActiveOnly        bool
	ShowCanceled      bool
	Account           string
	Project           string
	Label             string
	Assignee          string
	UpdatedWithinDays int
}

// BoardColumn represents a Kanban column.
type BoardColumn struct {
	Name  string
	Cards []IssueCard
}

// IssueCard represents a card on the board.
type IssueCard struct {
	IssueID    string
	Identifier string
	Title      string
	Labels     []string
	Assignee   string
	Badges     []string
	UpdatedAt  time.Time
	Account    string
	StateName  string
	PRURL      string
}

type toolbarAction struct {
	ID                string
	Label             string
	RequiresSelection bool
}

// Model is the Bubbletea model for the board pane.
type Model struct {
	Columns   []BoardColumn
	Selection BoardSelection
	Filters   BoardFilters

	focused bool
	width   int
	height  int

	scrollX       int
	scrollOffsets []int

	styles common.Styles
	zone   *zone.Manager

	showKeymapHints bool

	backoffUntil time.Time
	authMissing  []string
	wipLimits    map[string]int
}

// New creates a new board model.
func New() *Model {
	return &Model{
		Columns:   []BoardColumn{},
		Selection: BoardSelection{Column: 0, Row: 0},
		styles:    common.DefaultStyles(),
	}
}

// SetZone sets the shared zone manager for click targets.
func (m *Model) SetZone(z *zone.Manager) { m.zone = z }

// SetShowKeymapHints controls whether helper text is rendered.
func (m *Model) SetShowKeymapHints(show bool) { m.showKeymapHints = show }

// Init initializes the board.
func (m *Model) Init() tea.Cmd { return nil }

// Focus sets focus.
func (m *Model) Focus() { m.focused = true }

// Blur removes focus.
func (m *Model) Blur() { m.focused = false }

// Focused returns focus state.
func (m *Model) Focused() bool { return m.focused }

// SetSize sets the board size.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetColumns replaces board columns.
func (m *Model) SetColumns(cols []BoardColumn) {
	m.Columns = cols
	m.ensureScrollOffsets()
	m.clampSelection()
}

// SetBackoffUntil updates the rate limit backoff timestamp.
func (m *Model) SetBackoffUntil(until time.Time) { m.backoffUntil = until }

// SetAuthMissing sets accounts missing auth.
func (m *Model) SetAuthMissing(accounts []string) { m.authMissing = accounts }

// SetWIPLimits sets WIP limits per column.
func (m *Model) SetWIPLimits(limits map[string]int) { m.wipLimits = limits }

// SetStyles sets the styles for the board.
func (m *Model) SetStyles(styles common.Styles) { m.styles = styles }

// SelectedCard returns the selected card.
func (m *Model) SelectedCard() *IssueCard {
	if m.Selection.Column < 0 || m.Selection.Column >= len(m.Columns) {
		return nil
	}
	col := m.Columns[m.Selection.Column]
	if m.Selection.Row < 0 || m.Selection.Row >= len(col.Cards) {
		return nil
	}
	return &col.Cards[m.Selection.Row]
}

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (*Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
		m.moveRow(1)
	case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
		m.moveRow(-1)
	case key.Matches(msg, key.NewBinding(key.WithKeys("h", "left"))):
		m.moveColumn(-1)
	case key.Matches(msg, key.NewBinding(key.WithKeys("l", "right"))):
		m.moveColumn(1)
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		if card := m.SelectedCard(); card != nil {
			return m, func() tea.Msg { return messages.IssueSelected{IssueID: card.IssueID} }
		}
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+enter", "meta+enter"))):
		return m, func() tea.Msg { return messages.CycleAuxView{Direction: 1} }
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+shift+enter", "meta+shift+enter"))):
		return m, func() tea.Msg { return messages.CycleAuxView{Direction: -1} }
	case key.Matches(msg, key.NewBinding(key.WithKeys("s"))):
		if card := m.SelectedCard(); card != nil {
			return m, func() tea.Msg { return messages.StartIssueWork{IssueID: card.IssueID} }
		}
	case key.Matches(msg, key.NewBinding(key.WithKeys("m"))):
		if card := m.SelectedCard(); card != nil {
			return m, func() tea.Msg { return messages.MoveIssueState{IssueID: card.IssueID} }
		}
	case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
		if m.backoffActive() {
			return m, nil
		}
		return m, func() tea.Msg { return messages.RefreshBoard{} }
	case key.Matches(msg, key.NewBinding(key.WithKeys("/"))):
		return m, func() tea.Msg { return messages.ShowBoardSearchDialog{} }
	case key.Matches(msg, key.NewBinding(key.WithKeys("f"))):
		m.Filters.ActiveOnly = !m.Filters.ActiveOnly
		return m, func() tea.Msg { return messages.BoardFilterChanged{} }
	case key.Matches(msg, key.NewBinding(key.WithKeys("L"))):
		return m, func() tea.Msg { return messages.ShowLabelFilterDialog{} }
	case key.Matches(msg, key.NewBinding(key.WithKeys("R"))):
		return m, func() tea.Msg { return messages.ShowRecentFilterDialog{} }
	case key.Matches(msg, key.NewBinding(key.WithKeys("c"))):
		m.Filters.ShowCanceled = !m.Filters.ShowCanceled
		return m, func() tea.Msg { return messages.BoardFilterChanged{} }
	case key.Matches(msg, key.NewBinding(key.WithKeys("a"))):
		if card := m.SelectedCard(); card != nil {
			return m, func() tea.Msg { return messages.RunAgentForIssue{IssueID: card.IssueID} }
		}
	case key.Matches(msg, key.NewBinding(key.WithKeys("A"))):
		return m, func() tea.Msg { return messages.ShowAccountFilterDialog{} }
	case key.Matches(msg, key.NewBinding(key.WithKeys("o"))):
		if len(m.authMissing) > 0 {
			return m, func() tea.Msg { return messages.ShowOAuthDialog{} }
		}
	case key.Matches(msg, key.NewBinding(key.WithKeys("p"))):
		if card := m.SelectedCard(); card != nil {
			return m, func() tea.Msg { return messages.CreatePRForIssue{IssueID: card.IssueID} }
		}
	case key.Matches(msg, key.NewBinding(key.WithKeys("P"))):
		return m, func() tea.Msg { return messages.ShowProjectFilterDialog{} }
	case key.Matches(msg, key.NewBinding(key.WithKeys("d"))):
		if card := m.SelectedCard(); card != nil {
			return m, func() tea.Msg { return messages.OpenIssueDiff{IssueID: card.IssueID} }
		}
	case key.Matches(msg, key.NewBinding(key.WithKeys("."))):
		if card := m.SelectedCard(); card != nil {
			return m, func() tea.Msg { return messages.ShowIssueMenu{IssueID: card.IssueID} }
		}
	}

	return m, nil
}

func (m *Model) handleMouseWheel(msg tea.MouseWheelMsg) (*Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseWheelUp:
		m.moveRow(-1)
	case tea.MouseWheelDown:
		m.moveRow(1)
	case tea.MouseWheelLeft:
		m.moveColumn(-1)
	case tea.MouseWheelRight:
		m.moveColumn(1)
	}
	return m, nil
}

func (m *Model) handleMouseClick(msg tea.MouseClickMsg) (*Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	// Zone-based click handling temporarily disabled for bubbletea v2 migration
	// TODO: Implement hit-region based click handling
	return m, nil
}

func (m *Model) badgeAction(card IssueCard, badgeIdx int) tea.Cmd {
	if badgeIdx < 0 || badgeIdx >= len(card.Badges) {
		return nil
	}
	switch card.Badges[badgeIdx] {
	case "CHANGES":
		return func() tea.Msg { return messages.OpenIssueDiff{IssueID: card.IssueID} }
	case "PR":
		if card.PRURL != "" {
			return func() tea.Msg { return messages.OpenURL{URL: card.PRURL} }
		}
		return func() tea.Msg { return messages.IssueSelected{IssueID: card.IssueID} }
	case "RUNNING":
		return func() tea.Msg { return messages.IssueSelected{IssueID: card.IssueID} }
	default:
		return func() tea.Msg { return messages.IssueSelected{IssueID: card.IssueID} }
	}
}

func (m *Model) moveRow(delta int) {
	if len(m.Columns) == 0 {
		return
	}
	col := m.Selection.Column
	if col < 0 || col >= len(m.Columns) {
		return
	}
	rows := len(m.Columns[col].Cards)
	if rows == 0 {
		m.Selection.Row = 0
		return
	}
	m.Selection.Row += delta
	if m.Selection.Row < 0 {
		m.Selection.Row = 0
	}
	if m.Selection.Row >= rows {
		m.Selection.Row = rows - 1
	}
}

func (m *Model) moveColumn(delta int) {
	if len(m.Columns) == 0 {
		return
	}
	m.Selection.Column += delta
	if m.Selection.Column < 0 {
		m.Selection.Column = 0
	}
	if m.Selection.Column >= len(m.Columns) {
		m.Selection.Column = len(m.Columns) - 1
	}
	col := m.Columns[m.Selection.Column]
	if m.Selection.Row >= len(col.Cards) {
		m.Selection.Row = max(0, len(col.Cards)-1)
	}
}

func (m *Model) backoffActive() bool {
	return !m.backoffUntil.IsZero() && time.Now().Before(m.backoffUntil)
}

func (m *Model) ensureScrollOffsets() {
	if len(m.scrollOffsets) == len(m.Columns) {
		return
	}
	m.scrollOffsets = make([]int, len(m.Columns))
}

func (m *Model) clampSelection() {
	if len(m.Columns) == 0 {
		m.Selection = BoardSelection{}
		return
	}
	if m.Selection.Column < 0 {
		m.Selection.Column = 0
	}
	if m.Selection.Column >= len(m.Columns) {
		m.Selection.Column = len(m.Columns) - 1
	}
	rows := len(m.Columns[m.Selection.Column].Cards)
	if rows == 0 {
		m.Selection.Row = 0
		return
	}
	if m.Selection.Row < 0 {
		m.Selection.Row = 0
	}
	if m.Selection.Row >= rows {
		m.Selection.Row = rows - 1
	}
}

func toolbarActions() []toolbarAction {
	return []toolbarAction{
		{ID: "new", Label: "New", RequiresSelection: false},
		{ID: "open", Label: "Open", RequiresSelection: true},
		{ID: "start", Label: "Start", RequiresSelection: true},
		{ID: "move", Label: "Move", RequiresSelection: true},
		{ID: "agent", Label: "Agent", RequiresSelection: true},
		{ID: "diff", Label: "Diff", RequiresSelection: true},
		{ID: "pr", Label: "PR", RequiresSelection: true},
		{ID: "menu", Label: "Menu", RequiresSelection: true},
		{ID: "search", Label: "Search", RequiresSelection: false},
		{ID: "account", Label: "Account", RequiresSelection: false},
		{ID: "project", Label: "Project", RequiresSelection: false},
		{ID: "label", Label: "Label", RequiresSelection: false},
		{ID: "recent", Label: "Recent", RequiresSelection: false},
		{ID: "auth", Label: "Auth", RequiresSelection: false},
		{ID: "filter", Label: "Filter", RequiresSelection: false},
		{ID: "canceled", Label: "Canceled", RequiresSelection: false},
		{ID: "refresh", Label: "Refresh", RequiresSelection: false},
	}
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
