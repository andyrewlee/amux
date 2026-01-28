package inspector

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone"

	"github.com/andyrewlee/amux/internal/linear"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Mode describes inspector view mode.
type Mode int

const (
	ModeTask Mode = iota
	ModeAttempt
)

// AttemptInfo represents an attempt entry for display.
type AttemptInfo struct {
	Branch   string
	Executor string
	Updated  string
	Status   string
}

// Action represents an inspector action.
type Action struct {
	ID      string
	Label   string
	Enabled bool
}

// GitInfo represents git status for the current worktree.
type GitInfo struct {
	Branch           string
	Base             string
	Summary          string
	Ahead            int
	Behind           int
	RebaseInProgress bool
}

// Model is the inspector pane model.
type Model struct {
	Issue         *linear.Issue
	Attempts      []AttemptInfo
	Actions       []Action
	Comments      []linear.Comment
	ParentAttempt string
	AttemptBranch string
	RepoName      string

	PRURL    string
	PRState  string
	PRNumber int

	AgentProfile string

	GitLine      string
	GitInfo      GitInfo
	AgentRunning bool

	Logs              []common.ActivityEntry
	QueuedMessage     string
	ReviewPreview     string
	NextActionSummary string
	NextActionStatus  string
	AuthRequired      bool
	HasWorktree       bool
	ScriptRunning     bool

	Mode     Mode
	Cursor   int
	auxMode  int
	Conflict bool

	focused bool
	width   int
	height  int

	styles common.Styles
	zone   *zone.Manager

	composer textinput.Model

	showKeymapHints bool
}

// New creates an inspector model.
func New() *Model {
	input := textinput.New()
	input.Placeholder = "Follow-up..."
	input.CharLimit = 500
	input.SetWidth(40)
	return &Model{
		Mode:     ModeTask,
		Actions:  defaultActions(),
		styles:   common.DefaultStyles(),
		composer: input,
	}
}

// Init initializes the inspector.
func (m *Model) Init() tea.Cmd { return nil }

func defaultActions() []Action {
	return []Action{
		{ID: "start", Label: "Start", Enabled: true},
		{ID: "resume", Label: "Resume", Enabled: true},
		{ID: "new_attempt", Label: "New Attempt", Enabled: true},
		{ID: "run_agent", Label: "Run Agent", Enabled: true},
		{ID: "start_server", Label: "Start Server", Enabled: true},
		{ID: "stop_server", Label: "Stop Server", Enabled: true},
		{ID: "diff", Label: "Diff", Enabled: true},
		{ID: "pr", Label: "Create PR", Enabled: true},
		{ID: "comment", Label: "Add Comment", Enabled: true},
		{ID: "attempts", Label: "Attempts", Enabled: true},
		{ID: "move", Label: "Move State", Enabled: true},
		{ID: "mark_done", Label: "Mark Done", Enabled: true},
		{ID: "rebase", Label: "Rebase", Enabled: true},
		{ID: "resolve", Label: "Resolve Conflicts", Enabled: true},
		{ID: "send_feedback", Label: "Send Feedback", Enabled: true},
		{ID: "send_message", Label: "Send Message", Enabled: true},
		{ID: "insert_pr_comments", Label: "Insert PR Comments", Enabled: true},
		{ID: "cancel_queue", Label: "Cancel Queue", Enabled: true},
		{ID: "rehydrate", Label: "Rehydrate", Enabled: true},
		{ID: "open_editor", Label: "Open in Editor", Enabled: true},
		{ID: "view_processes", Label: "View Processes", Enabled: true},
		{ID: "create_subtask", Label: "Create Subtask", Enabled: true},
	}
}

// SetZone sets the zone manager.
func (m *Model) SetZone(z *zone.Manager) { m.zone = z }

// SetShowKeymapHints toggles key hints.
func (m *Model) SetShowKeymapHints(show bool) { m.showKeymapHints = show }

// SetSize sets the inspector size.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.composer.SetWidth(max(10, width-6))
}

// Focus sets focus.
func (m *Model) Focus() { m.focused = true }

// Blur removes focus.
func (m *Model) Blur() { m.focused = false }

// Focused returns focus state.
func (m *Model) Focused() bool { return m.focused }

// SetIssue sets the selected issue.
func (m *Model) SetIssue(issue *linear.Issue) { m.Issue = issue }

// SetAttempts sets attempt list.
func (m *Model) SetAttempts(attempts []AttemptInfo) { m.Attempts = attempts }

// SetComments sets comments list.
func (m *Model) SetComments(comments []linear.Comment) { m.Comments = comments }

// SetParentAttempt sets parent attempt branch info.
func (m *Model) SetParentAttempt(branch string) { m.ParentAttempt = branch }

// SetAttemptBranch sets current attempt branch label.
func (m *Model) SetAttemptBranch(branch string) { m.AttemptBranch = branch }

// SetRepoName sets repo display name.
func (m *Model) SetRepoName(name string) { m.RepoName = name }

// SetPR sets the PR info for display.
func (m *Model) SetPR(url, state string, number int) {
	m.PRURL = url
	m.PRState = state
	m.PRNumber = number
}

// SetAgentProfile sets the active agent profile name.
func (m *Model) SetAgentProfile(profile string) { m.AgentProfile = profile }

// SetGitLine sets git status line.
func (m *Model) SetGitLine(line string) { m.GitLine = line }

// SetGitInfo sets detailed git info.
func (m *Model) SetGitInfo(info GitInfo) { m.GitInfo = info }

// SetAgentRunning sets agent running status.
func (m *Model) SetAgentRunning(running bool) { m.AgentRunning = running }

// SetLogs sets log lines for attempt view.
func (m *Model) SetLogs(lines []common.ActivityEntry) { m.Logs = lines }

// SetQueuedMessage sets queued follow-up message.
func (m *Model) SetQueuedMessage(msg string) { m.QueuedMessage = msg }

// SetReviewPreview sets the review preview content.
func (m *Model) SetReviewPreview(preview string) { m.ReviewPreview = preview }

// SetNextActionSummary sets next action summary content.
func (m *Model) SetNextActionSummary(summary, status string) {
	m.NextActionSummary = summary
	m.NextActionStatus = status
}

// SetAuthRequired sets auth-required state.
func (m *Model) SetAuthRequired(required bool) { m.AuthRequired = required }

// SetHasWorktree sets whether a worktree exists for the issue.
func (m *Model) SetHasWorktree(has bool) { m.HasWorktree = has }

// SetScriptRunning sets dev server running status.
func (m *Model) SetScriptRunning(running bool) { m.ScriptRunning = running }

// AppendToComposer appends text to the follow-up composer.
func (m *Model) AppendToComposer(text string) {
	if text == "" {
		return
	}
	current := m.composer.Value()
	if current != "" && !strings.HasSuffix(current, "\n") {
		current += "\n"
	}
	m.composer.SetValue(current + text)
}

// SetMode sets the view mode.
func (m *Model) SetMode(mode Mode) { m.Mode = mode }

// SetAuxMode sets aux mode for header toggles (0 none, 1 preview, 2 diff).
func (m *Model) SetAuxMode(mode int) { m.auxMode = mode }

// SetConflict sets conflict banner state.
func (m *Model) SetConflict(conflict bool) { m.Conflict = conflict }

// SetStyles sets the styles for the inspector.
func (m *Model) SetStyles(styles common.Styles) { m.styles = styles }

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if !m.focused {
			return m, nil
		}
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			return m, m.runSelectedAction()
		case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+enter", "meta+enter"))):
			if m.Mode == ModeAttempt {
				body := strings.TrimSpace(m.composer.Value())
				if body == "" {
					return m, nil
				}
				m.composer.SetValue("")
				return m, func() tea.Msg { return messages.SendFollowUp{IssueID: m.issueID(), Body: body} }
			}
			return m, func() tea.Msg { return messages.CycleAuxView{Direction: 1} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("c"))):
			return m, func() tea.Msg { return messages.AddIssueComment{IssueID: m.issueID(), Body: ""} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("t"))):
			return m, func() tea.Msg { return messages.ShowAttemptsDialog{IssueID: m.issueID()} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("g"))):
			return m, func() tea.Msg { return messages.OpenIssueDiff{IssueID: m.issueID()} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("m"))):
			return m, func() tea.Msg { return messages.MoveIssueState{IssueID: m.issueID()} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("p"))):
			return m, func() tea.Msg { return messages.CreatePRForIssue{IssueID: m.issueID()} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("b"))):
			return m, func() tea.Msg { return messages.RebaseWorkspace{IssueID: m.issueID()} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("x"))):
			return m, func() tea.Msg { return messages.ResolveConflicts{IssueID: m.issueID()} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("v"))):
			if m.Mode == ModeAttempt {
				return m, func() tea.Msg { return messages.ShowAgentProfileDialog{IssueID: m.issueID()} }
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("a"))):
			if m.Mode == ModeAttempt {
				return m, func() tea.Msg { return messages.ShowAttachDialog{IssueID: m.issueID()} }
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("i"))):
			if m.Mode == ModeAttempt {
				return m, func() tea.Msg { return messages.ShowPRCommentsDialog{IssueID: m.issueID()} }
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			m.Cursor = min(m.Cursor+1, len(m.Actions)-1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			m.Cursor = max(0, m.Cursor-1)
		default:
			if m.Mode == ModeAttempt {
				m.composer, cmd = m.composer.Update(msg)
			}
		}
	case tea.MouseClickMsg:
		if true && msg.Button == tea.MouseLeft {
			if cmd := m.handleMouse(msg); cmd != nil {
				return m, cmd
			}
		}
	}
	return m, cmd
}

func (m *Model) issueID() string {
	if m.Issue == nil {
		return ""
	}
	return m.Issue.ID
}

func (m *Model) runSelectedAction() tea.Cmd {
	issueID := m.issueID()
	if issueID == "" {
		return nil
	}
	if m.Cursor < 0 || m.Cursor >= len(m.Actions) {
		return nil
	}
	action := m.Actions[m.Cursor]
	if !action.Enabled {
		return nil
	}
	switch action.ID {
	case "start":
		return func() tea.Msg { return messages.StartIssueWork{IssueID: issueID} }
	case "resume":
		return func() tea.Msg { return messages.ResumeIssueWork{IssueID: issueID} }
	case "new_attempt":
		return func() tea.Msg { return messages.NewAttempt{IssueID: issueID} }
	case "run_agent":
		return func() tea.Msg { return messages.RunAgentForIssue{IssueID: issueID} }
	case "start_server":
		return func() tea.Msg { return messages.RunScript{ScriptType: "run", IssueID: issueID} }
	case "stop_server":
		return func() tea.Msg { return messages.StopPreviewServer{} }
	case "diff":
		return func() tea.Msg { return messages.OpenIssueDiff{IssueID: issueID} }
	case "comment":
		return func() tea.Msg { return messages.AddIssueComment{IssueID: issueID, Body: ""} }
	case "attempts":
		return func() tea.Msg { return messages.ShowAttemptsDialog{IssueID: issueID} }
	case "send_feedback":
		return func() tea.Msg { return messages.SendReviewFeedback{IssueID: issueID} }
	case "pr":
		return func() tea.Msg { return messages.CreatePRForIssue{IssueID: issueID} }
	case "move":
		return func() tea.Msg { return messages.MoveIssueState{IssueID: issueID} }
	case "mark_done":
		return func() tea.Msg { return messages.SetIssueStateType{IssueID: issueID, StateType: "completed"} }
	case "rebase":
		return func() tea.Msg { return messages.RebaseWorkspace{IssueID: issueID} }
	case "resolve":
		return func() tea.Msg { return messages.ResolveConflicts{IssueID: issueID} }
	case "send_message":
		body := strings.TrimSpace(m.composer.Value())
		if body == "" {
			return nil
		}
		m.composer.SetValue("")
		return func() tea.Msg { return messages.SendFollowUp{IssueID: issueID, Body: body} }
	case "insert_pr_comments":
		return func() tea.Msg { return messages.ShowPRCommentsDialog{IssueID: issueID} }
	case "cancel_queue":
		return func() tea.Msg { return messages.CancelQueuedMessage{IssueID: issueID} }
	case "rehydrate":
		return func() tea.Msg { return messages.RehydrateIssueWorktree{IssueID: issueID} }
	case "open_editor":
		return func() tea.Msg { return messages.OpenWorkspaceInEditor{IssueID: issueID} }
	case "view_processes":
		return func() tea.Msg { return messages.ShowDrawerPane{Pane: "processes"} }
	case "create_subtask":
		return func() tea.Msg { return messages.CreateSubtask{IssueID: issueID} }
	}
	return nil
}

func (m *Model) handleMouse(msg tea.MouseClickMsg) tea.Cmd {
	// Zone-based click handling temporarily disabled for bubbletea v2 migration
	// TODO: Implement hit-region based click handling
	return nil
}
