package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func newPrefixTestApp(t *testing.T) (*App, *data.Workspace, *center.Model) {
	t.Helper()

	cfg := &config.Config{
		Assistants: map[string]config.AssistantConfig{
			"claude": {},
		},
	}
	ws := &data.Workspace{
		Name: "ws",
		Repo: "/repo/ws",
		Root: "/repo/ws",
	}
	centerModel := center.New(cfg)
	centerModel.SetWorkspace(ws)

	app := &App{
		center:      centerModel,
		keymap:      DefaultKeyMap(),
		focusedPane: messages.PaneCenter,
	}
	return app, ws, centerModel
}

func TestHandlePrefixNumericTabSelection_InvalidIndexNoOp(t *testing.T) {
	app, ws, centerModel := newPrefixTestApp(t)
	centerModel.AddTab(&center.Tab{
		ID:          center.TabID("tab-1"),
		Name:        "Claude",
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "sess-1",
		Detached:    true,
	})

	status, cmd := app.handlePrefixCommand(tea.KeyPressMsg{Code: '9', Text: "9"})
	if status != prefixMatchComplete {
		t.Fatalf("expected numeric shortcut to complete, got %v", status)
	}
	if cmd != nil {
		t.Fatalf("expected out-of-range numeric selection to return nil command")
	}
}

func TestHandlePrefixNumericTabSelection_ValidIndexTriggersReattach(t *testing.T) {
	app, ws, centerModel := newPrefixTestApp(t)
	centerModel.AddTab(&center.Tab{
		ID:          center.TabID("tab-1"),
		Name:        "Claude 1",
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "sess-1",
		Detached:    false,
		Running:     true,
	})
	centerModel.AddTab(&center.Tab{
		ID:          center.TabID("tab-2"),
		Name:        "Claude 2",
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "sess-2",
		Detached:    true,
	})

	status, cmd := app.handlePrefixCommand(tea.KeyPressMsg{Code: '2', Text: "2"})
	if status != prefixMatchComplete {
		t.Fatalf("expected numeric shortcut to complete, got %v", status)
	}
	if cmd == nil {
		t.Fatalf("expected valid numeric selection to trigger follow-up command")
	}
}

func TestHandlePrefixNextTab_SingleTabNoOp(t *testing.T) {
	app, ws, centerModel := newPrefixTestApp(t)
	centerModel.AddTab(&center.Tab{
		ID:          center.TabID("tab-1"),
		Name:        "Claude",
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "sess-1",
		Detached:    true,
	})

	status, cmd := app.handlePrefixCommand(tea.KeyPressMsg{Code: 't', Text: "t"})
	if status != prefixMatchPartial {
		t.Fatalf("expected first key to narrow prefix sequence, got %v", status)
	}
	if cmd != nil {
		t.Fatalf("expected partial sequence to return nil command")
	}

	status, cmd = app.handlePrefixCommand(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if status != prefixMatchComplete {
		t.Fatalf("expected next-tab sequence to complete, got %v", status)
	}
	if cmd != nil {
		t.Fatalf("expected single-tab next to be a no-op without reattach command")
	}
}

func TestHandlePrefixPrevTab_SingleTabNoOp(t *testing.T) {
	app, ws, centerModel := newPrefixTestApp(t)
	centerModel.AddTab(&center.Tab{
		ID:          center.TabID("tab-1"),
		Name:        "Claude",
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "sess-1",
		Detached:    true,
	})

	status, cmd := app.handlePrefixCommand(tea.KeyPressMsg{Code: 't', Text: "t"})
	if status != prefixMatchPartial {
		t.Fatalf("expected first key to narrow prefix sequence, got %v", status)
	}
	if cmd != nil {
		t.Fatalf("expected partial sequence to return nil command")
	}

	status, cmd = app.handlePrefixCommand(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if status != prefixMatchComplete {
		t.Fatalf("expected prev-tab sequence to complete, got %v", status)
	}
	if cmd != nil {
		t.Fatalf("expected single-tab prev to be a no-op without reattach command")
	}
}

func TestHandlePrefixCommand_BackspaceAtRootNoop(t *testing.T) {
	app, _, _ := newPrefixTestApp(t)

	status, cmd := app.handlePrefixCommand(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if status != prefixMatchPartial {
		t.Fatalf("expected backspace at root to keep prefix active, got %v", status)
	}
	if cmd != nil {
		t.Fatalf("expected backspace at root to return nil command")
	}
	if len(app.prefixSequence) != 0 {
		t.Fatalf("expected empty prefix sequence after root backspace, got %v", app.prefixSequence)
	}
}

func TestHandlePrefixCommand_BackspaceUndoesLastToken(t *testing.T) {
	app, _, _ := newPrefixTestApp(t)
	app.prefixSequence = []string{"t", "n"}

	status, cmd := app.handlePrefixCommand(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if status != prefixMatchPartial {
		t.Fatalf("expected backspace undo to keep prefix active, got %v", status)
	}
	if cmd != nil {
		t.Fatalf("expected backspace undo to return nil command")
	}
	if len(app.prefixSequence) != 1 || app.prefixSequence[0] != "t" {
		t.Fatalf("expected sequence to be reduced to [t], got %v", app.prefixSequence)
	}
}

func TestHandleKeyPress_BackspaceAtRootRefreshesPrefixTimeout(t *testing.T) {
	app, _, _ := newPrefixTestApp(t)
	app.prefixActive = true
	beforeToken := app.prefixToken

	cmd := app.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if cmd == nil {
		t.Fatalf("expected timeout refresh command")
	}
	if !app.prefixActive {
		t.Fatalf("expected prefix mode to remain active")
	}
	if len(app.prefixSequence) != 0 {
		t.Fatalf("expected prefix sequence to remain empty, got %v", app.prefixSequence)
	}
	if app.prefixToken != beforeToken+1 {
		t.Fatalf("expected prefix token increment, got %d want %d", app.prefixToken, beforeToken+1)
	}
}

func TestIsPrefixKey_DoesNotAcceptPrintableAliases(t *testing.T) {
	app, _, _ := newPrefixTestApp(t)

	if app.isPrefixKey(tea.KeyPressMsg{Code: '?', Text: "?"}) {
		t.Fatal("expected '?' not to be treated as global prefix key")
	}
	if app.isPrefixKey(tea.KeyPressMsg{Code: 'H', Text: "H"}) {
		t.Fatal("expected 'H' not to be treated as global prefix key")
	}
}

func TestHandleKeyPress_PrefixAliasOpensPaletteWhenSafe(t *testing.T) {
	app, _, _ := newPrefixTestApp(t)
	app.focusedPane = messages.PaneDashboard

	cmd := app.handleKeyPress(tea.KeyPressMsg{Code: '?', Text: "?"})
	if cmd == nil {
		t.Fatal("expected alias leader to enter prefix mode")
	}
	if !app.prefixActive {
		t.Fatal("expected prefix mode to become active")
	}
}

func TestHandleKeyPress_PrefixAliasIgnoredInActiveCenterTerminal(t *testing.T) {
	app, ws, centerModel := newPrefixTestApp(t)
	centerModel.AddTab(&center.Tab{
		ID:          center.TabID("tab-1"),
		Name:        "Claude",
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "sess-1",
		Detached:    true,
	})
	app.focusedPane = messages.PaneCenter

	_ = app.handleKeyPress(tea.KeyPressMsg{Code: '?', Text: "?"})
	if app.prefixActive {
		t.Fatal("expected alias leader not to open palette while center terminal is active")
	}
}

func TestHandleKeyPress_PrefixQuestionStillTriggersHelp(t *testing.T) {
	app, _, _ := newPrefixTestApp(t)
	app.prefixActive = true
	app.helpOverlay = common.NewHelpOverlay()
	app.width = 80
	app.height = 24

	_ = app.handleKeyPress(tea.KeyPressMsg{Code: '?', Text: "?"})
	if app.prefixActive {
		t.Fatal("expected prefix mode to complete after '?' command")
	}
	if !app.helpOverlay.Visible() {
		t.Fatal("expected '?' prefix command to toggle help overlay")
	}
}

func TestHandleKeyPress_PrefixHResetsAndDoesNotPassThrough(t *testing.T) {
	app, ws, centerModel := newPrefixTestApp(t)
	centerModel.AddTab(&center.Tab{
		ID:          center.TabID("tab-1"),
		Name:        "Claude",
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "sess-1",
		Detached:    true,
	})
	app.focusedPane = messages.PaneCenter
	app.prefixActive = true
	app.prefixSequence = []string{"t"}
	beforeToken := app.prefixToken

	cmd := app.handleKeyPress(tea.KeyPressMsg{Code: 'H', Text: "H"})
	if cmd == nil {
		t.Fatal("expected prefix reset command for H while palette is open")
	}
	if !app.prefixActive {
		t.Fatal("expected prefix mode to remain active")
	}
	if len(app.prefixSequence) != 0 {
		t.Fatalf("expected prefix sequence reset, got %v", app.prefixSequence)
	}
	if app.prefixToken != beforeToken+1 {
		t.Fatalf("expected prefix token increment, got %d want %d", app.prefixToken, beforeToken+1)
	}
}

func TestRunPrefixAction_AddProject(t *testing.T) {
	app, _, _ := newPrefixTestApp(t)

	cmd := app.runPrefixAction("add_project")
	if cmd == nil {
		t.Fatal("expected add_project to return command")
	}
	msg := cmd()
	if _, ok := msg.(messages.ShowAddProjectDialog); !ok {
		t.Fatalf("expected ShowAddProjectDialog message, got %T", msg)
	}
}

func TestRunPrefixAction_OpenSettings(t *testing.T) {
	app, _, _ := newPrefixTestApp(t)

	cmd := app.runPrefixAction("open_settings")
	if cmd == nil {
		t.Fatal("expected open_settings to return command")
	}
	msg := cmd()
	if _, ok := msg.(messages.ShowSettingsDialog); !ok {
		t.Fatalf("expected ShowSettingsDialog message, got %T", msg)
	}
}

func TestRunPrefixAction_DeleteWorkspaceRequiresSelection(t *testing.T) {
	app, ws, _ := newPrefixTestApp(t)

	if cmd := app.runPrefixAction("delete_workspace"); cmd != nil {
		t.Fatal("expected nil command when no workspace/project selection exists")
	}

	project := &data.Project{Name: "p", Path: "/repo/ws"}
	app.activeProject = project
	app.activeWorkspace = ws
	cmd := app.runPrefixAction("delete_workspace")
	if cmd == nil {
		t.Fatal("expected delete_workspace command when selection exists")
	}
	result := cmd()
	msg, ok := result.(messages.ShowDeleteWorkspaceDialog)
	if !ok {
		t.Fatalf("expected ShowDeleteWorkspaceDialog message, got %T", result)
	}
	if msg.Project != project || msg.Workspace != ws {
		t.Fatalf("unexpected delete payload: %+v", msg)
	}
}
