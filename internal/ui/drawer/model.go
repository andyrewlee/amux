package drawer

import (
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Pane identifies drawer subpanes.
type Pane int

const (
	PaneLogs Pane = iota
	PaneApprovals
	PaneProcesses
)

// ProcessInfo represents a running process.
type ProcessInfo struct {
	ID           string
	Name         string
	Status       string
	Kind         string
	WorktreeRoot string
	WorktreeID   string
	ScriptType   string
	StartedAt    time.Time
	CompletedAt  time.Time
	ExitCode     *int
	PID          int
}

type logViewItem struct {
	Index int
	Entry common.ActivityEntry
}

// ApprovalItem represents a pending approval in the drawer.
type ApprovalItem struct {
	ID        string
	Summary   string
	Details   []string
	Requested time.Time
	ExpiresAt time.Time
}

// Model is the drawer UI model.
type Model struct {
	focused bool
	width   int
	height  int

	pane Pane

	logs      []common.ActivityEntry
	approvals []ApprovalItem
	processes []ProcessInfo

	devURL string

	styles common.Styles
	zone   *zone.Manager

	showKeymapHints bool

	selectedProc     int
	selectedLog      int
	selectedApproval int
	expandedLogs     map[string]bool
}

// New creates a drawer model.
func New() *Model {
	return &Model{
		styles:           common.DefaultStyles(),
		selectedProc:     -1,
		selectedLog:      -1,
		selectedApproval: -1,
		expandedLogs:     make(map[string]bool),
	}
}

// Init initializes the drawer.
func (m *Model) Init() tea.Cmd { return nil }

// SetZone sets zone manager.
func (m *Model) SetZone(z *zone.Manager) { m.zone = z }

// SetShowKeymapHints toggles key hints.
func (m *Model) SetShowKeymapHints(show bool) { m.showKeymapHints = show }

// SetSize sets dimensions.
func (m *Model) SetSize(width, height int) { m.width, m.height = width, height }

// Focus sets focus.
func (m *Model) Focus() { m.focused = true }

// Blur removes focus.
func (m *Model) Blur() { m.focused = false }

// Focused returns focus state.
func (m *Model) Focused() bool { return m.focused }

// SetLogs sets log lines.
func (m *Model) SetLogs(lines []common.ActivityEntry) {
	m.logs = lines
	if len(m.logs) == 0 {
		m.selectedLog = -1
	} else if m.selectedLog < 0 {
		m.selectedLog = len(m.logs) - 1
	} else if m.selectedLog >= len(m.logs) {
		m.selectedLog = len(m.logs) - 1
	}
}

// SetProcesses sets process list.
func (m *Model) SetProcesses(list []ProcessInfo) {
	m.processes = list
	if len(m.processes) == 0 {
		m.selectedProc = -1
	} else if m.selectedProc < 0 {
		m.selectedProc = 0
	} else if m.selectedProc >= len(m.processes) {
		m.selectedProc = len(m.processes) - 1
	}
}

// SetApprovals sets approvals list.
func (m *Model) SetApprovals(list []ApprovalItem) {
	m.approvals = list
	if len(m.approvals) == 0 {
		m.selectedApproval = -1
	} else if m.selectedApproval < 0 {
		m.selectedApproval = 0
	} else if m.selectedApproval >= len(m.approvals) {
		m.selectedApproval = len(m.approvals) - 1
	}
}

// SetDevURL sets dev server URL.
func (m *Model) SetDevURL(url string) { m.devURL = url }

// DevURL returns the current dev server URL.
func (m *Model) DevURL() string { return m.devURL }

// SetPane sets the active drawer pane.
func (m *Model) SetPane(p Pane) { m.pane = p }

// SetStyles sets the styles for the drawer.
func (m *Model) SetStyles(styles common.Styles) { m.styles = styles }

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("["))):
			m.prevPane()
		case key.Matches(msg, key.NewBinding(key.WithKeys("]"))):
			m.nextPane()
		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			switch m.pane {
			case PaneProcesses:
				if len(m.processes) > 0 {
					m.selectedProc = min(m.selectedProc+1, len(m.processes)-1)
				}
			case PaneLogs:
				logs := m.visibleLogs()
				if len(logs) > 0 {
					m.selectedLog = min(m.selectedLog+1, len(logs)-1)
				}
			case PaneApprovals:
				if len(m.approvals) > 0 {
					m.selectedApproval = min(m.selectedApproval+1, len(m.approvals)-1)
				}
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			switch m.pane {
			case PaneProcesses:
				if len(m.processes) > 0 {
					m.selectedProc = max(0, m.selectedProc-1)
				}
			case PaneLogs:
				logs := m.visibleLogs()
				if len(logs) > 0 {
					m.selectedLog = max(0, m.selectedLog-1)
				}
			case PaneApprovals:
				if len(m.approvals) > 0 {
					m.selectedApproval = max(0, m.selectedApproval-1)
				}
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			if m.pane == PaneLogs {
				if entry := m.selectedLogEntry(); entry != nil && entry.ID != "" && len(entry.Details) > 0 {
					m.expandedLogs[entry.ID] = !m.expandedLogs[entry.ID]
				}
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("x"))):
			return m, func() tea.Msg { return messages.StopProcess{} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("o"))):
			return m, func() tea.Msg { return messages.OpenDevURL{} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("c"))):
			if m.pane == PaneLogs {
				return m, func() tea.Msg { return messages.CopyProcessLogs{} }
			}
		}
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	}
	return m, nil
}

func (m *Model) prevPane() { m.pane = Pane((int(m.pane) + 2) % 3) }
func (m *Model) nextPane() { m.pane = Pane((int(m.pane) + 1) % 3) }

// SelectedProcess returns the currently selected process info.
func (m *Model) SelectedProcess() *ProcessInfo {
	if m.selectedProc < 0 || m.selectedProc >= len(m.processes) {
		return nil
	}
	return &m.processes[m.selectedProc]
}

func (m *Model) visibleLogs() []logViewItem {
	if len(m.logs) == 0 {
		return nil
	}
	if proc := m.SelectedProcess(); proc != nil {
		filtered := []logViewItem{}
		for i, entry := range m.logs {
			if entry.ProcessID == proc.ID || (proc.Kind == "agent" && proc.WorktreeID != "" && entry.ProcessID == proc.WorktreeID) {
				filtered = append(filtered, logViewItem{Index: i, Entry: entry})
			}
		}
		return filtered
	}
	out := make([]logViewItem, 0, len(m.logs))
	for i, entry := range m.logs {
		out = append(out, logViewItem{Index: i, Entry: entry})
	}
	return out
}

func (m *Model) selectedLogEntry() *common.ActivityEntry {
	logs := m.visibleLogs()
	if len(logs) == 0 || m.selectedLog < 0 || m.selectedLog >= len(logs) {
		return nil
	}
	entry := logs[m.selectedLog].Entry
	return &entry
}

func (m *Model) handleMouseClick(msg tea.MouseClickMsg) (*Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	// Zone-based click handling temporarily disabled for bubbletea v2 migration
	// TODO: Implement hit-region based click handling
	return m, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
