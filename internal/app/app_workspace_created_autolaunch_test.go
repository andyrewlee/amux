package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/layout"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// A workspace created from the dialog already carries the assistant the user
// picked, so creation must activate that workspace and arm its assistant rather
// than dropping the user back on the dashboard to pick the same agent again.
func TestHandleWorkspaceCreatedActivatesAndArmsAssistant(t *testing.T) {
	project := data.NewProject("/repo")
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	ws.Assistant = "claude"

	app := &App{
		lifecycle: newWorkspaceLifecycleState(),
		dashboard: dashboard.New(),
	}
	app.projects = []data.Project{*project}

	cmds := app.handleWorkspaceCreated(messages.WorkspaceCreated{Workspace: ws})

	var activated *messages.WorkspaceActivated
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		if msg, ok := cmd().(messages.WorkspaceActivated); ok {
			activated = &msg
		}
	}
	if activated == nil {
		t.Fatal("expected the created workspace to be activated")
	}
	if activated.Workspace != ws {
		t.Fatalf("expected activation of the created workspace, got %v", activated.Workspace)
	}
	if activated.Project == nil || activated.Project.Path != "/repo" {
		t.Fatalf("expected activation to carry the owning project, got %v", activated.Project)
	}
	if got := app.pendingLaunchAssistants[string(ws.ID())]; got != "claude" {
		t.Fatalf("expected claude armed for %s, got %q", ws.ID(), got)
	}
}

// activationApp builds an App wired enough to run handleWorkspaceActivated,
// with the center pane visible.
func activationApp(t *testing.T) *App {
	t.Helper()
	layoutManager := layout.NewManager()
	layoutManager.Resize(140, 40)
	return &App{
		layout:          layoutManager,
		dashboard:       dashboard.New(),
		center:          center.New(nil),
		sidebar:         sidebar.NewTabbedSidebar(),
		sidebarTerminal: sidebar.NewTerminalModel(),
	}
}

// Activating the workspace the assistant was armed for consumes the arming and
// contributes a launch command. The command count is compared against an
// identical unarmed activation because the launch is a queued tea.Cmd with no
// synchronous side effect to observe — dropping it (rather than appending it to
// cmds) would leave the arming cleared but start no agent, which a
// fields-only assertion cannot catch.
func TestHandleWorkspaceActivatedConsumesArmedAssistant(t *testing.T) {
	project := data.NewProject("/repo")
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	project.Workspaces = append(project.Workspaces, *ws)
	activation := messages.WorkspaceActivated{Project: project, Workspace: ws}

	unarmed := activationApp(t)
	baseline := len(unarmed.handleWorkspaceActivated(activation))

	armed := activationApp(t)
	armed.pendingLaunchAssistants = map[string]string{string(ws.ID()): "claude"}
	got := len(armed.handleWorkspaceActivated(activation))

	if len(armed.pendingLaunchAssistants) != 0 {
		t.Fatalf("expected armed assistant to be consumed, got %v", armed.pendingLaunchAssistants)
	}
	if got != baseline+1 {
		t.Fatalf("expected the armed activation to queue one extra (launch) command: got %d, unarmed %d", got, baseline)
	}
}

// The armed activation must skip the tmux agent discovery it would otherwise
// queue alongside the launch, so a scan cannot adopt the session being created
// into a duplicate tab. Removing that guard is invisible to a test that only
// exercises hasPendingAgentLaunch directly, so this asserts the command count
// actually drops.
func TestHandleWorkspaceActivatedSkipsDiscoveryWhileArmed(t *testing.T) {
	project := data.NewProject("/repo")
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	ws.OpenTabs = []data.TabInfo{{Assistant: "claude", Name: "claude", SessionName: "amux-existing", Status: "running"}}
	project.Workspaces = append(project.Workspaces, *ws)
	activation := messages.WorkspaceActivated{Project: project, Workspace: ws}

	// tmuxService must be present for discoverWorkspaceTabsFromTmux to produce a
	// command at all; tmuxAvailable gates it.
	unarmed := activationApp(t)
	unarmed.tmuxAvailable = true
	unarmed.tmuxService = stubTmuxOps{}
	withDiscovery := len(unarmed.handleWorkspaceActivated(activation))

	armed := activationApp(t)
	armed.tmuxAvailable = true
	armed.tmuxService = stubTmuxOps{}
	armed.pendingLaunchAssistants = map[string]string{string(ws.ID()): "claude"}
	withLaunch := len(armed.handleWorkspaceActivated(activation))

	// Armed: one discovery command dropped, one launch command added.
	if withLaunch != withDiscovery {
		t.Fatalf("expected the armed activation to trade discovery for the launch: got %d, unarmed %d", withLaunch, withDiscovery)
	}
	// Guard the guard: prove discovery really was queued in the unarmed case, so
	// the equality above cannot be satisfied by discovery never running at all.
	noTmux := activationApp(t)
	if base := len(noTmux.handleWorkspaceActivated(activation)); base >= withDiscovery {
		t.Fatalf("expected tmux-enabled activation to queue a discovery command: %d vs %d without tmux", withDiscovery, base)
	}
}

// Activating some other workspace while a creation is in flight must leave the
// arming alone: the agent belongs to the workspace that was created.
func TestLaunchPendingAgentIgnoresOtherWorkspaces(t *testing.T) {
	created := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	other := data.NewWorkspace("other", "other", "main", "/repo", "/repo/other")

	app := &App{
		pendingLaunchAssistants: map[string]string{string(created.ID()): "claude"},
	}

	if cmd := app.launchPendingAgent(other); cmd != nil {
		t.Fatal("expected no launch for an unrelated workspace")
	}
	if app.pendingLaunchAssistants[string(created.ID())] != "claude" {
		t.Fatalf("expected arming to survive, got %v", app.pendingLaunchAssistants)
	}
}

// hasPendingAgentLaunch gates the activation-time tmux agent discovery, which
// would otherwise race the launch and adopt its session into a second tab. It
// must be true only for the workspace whose launch is still armed.
func TestHasPendingAgentLaunchMatchesOnlyTheArmedWorkspace(t *testing.T) {
	created := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	other := data.NewWorkspace("other", "other", "main", "/repo", "/repo/other")

	app := &App{
		pendingLaunchAssistants: map[string]string{string(created.ID()): "claude"},
	}

	if !app.hasPendingAgentLaunch(created) {
		t.Fatal("expected the armed workspace to skip agent discovery")
	}
	if app.hasPendingAgentLaunch(other) {
		t.Fatal("expected an unrelated workspace to still discover agent tabs")
	}
	if app.hasPendingAgentLaunch(nil) {
		t.Fatal("expected no match for a nil workspace")
	}

	delete(app.pendingLaunchAssistants, string(created.ID()))
	if app.hasPendingAgentLaunch(created) {
		t.Fatal("expected discovery to resume once the launch is consumed")
	}
}

// An unknown assistant (a stale value on the workspace record) must not arm a
// launch; creation still activates the workspace.
func TestActivateCreatedWorkspaceSkipsUnknownAssistant(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	ws.Assistant = "not-an-agent"

	app := &App{
		dashboard: dashboard.New(),
		config: &config.Config{
			Assistants: map[string]config.AssistantConfig{"claude": {Command: "claude"}},
		},
	}
	app.projects = []data.Project{*data.NewProject("/repo")}

	if cmd := app.activateCreatedWorkspace(ws); cmd == nil {
		t.Fatal("expected the workspace to be activated anyway")
	}
	if len(app.pendingLaunchAssistants) != 0 {
		t.Fatalf("expected no arming for an unknown assistant, got %v", app.pendingLaunchAssistants)
	}
}

// Agent tabs need tmux. Without it the explicit new-agent paths report an
// install hint, so an auto-launch must not fire and fail on its own.
func TestActivateCreatedWorkspaceSkipsLaunchWithoutTmux(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	ws.Assistant = "claude"

	app := &App{dashboard: dashboard.New(), tmuxCheckDone: true, tmuxAvailable: false}
	app.projects = []data.Project{*data.NewProject("/repo")}

	if cmd := app.activateCreatedWorkspace(ws); cmd == nil {
		t.Fatal("expected the workspace to be activated even without tmux")
	}
	if len(app.pendingLaunchAssistants) != 0 {
		t.Fatalf("expected no arming without tmux, got %v", app.pendingLaunchAssistants)
	}
}

// A workspace whose project is not loaded must not be activated: that would
// leave activeWorkspace set with a nil activeProject, disabling the
// workspace-scoped commands.
func TestActivateCreatedWorkspaceSkipsUnknownProject(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/gone", "/gone/feature")
	ws.Assistant = "claude"

	app := &App{dashboard: dashboard.New()}

	if cmd := app.activateCreatedWorkspace(ws); cmd != nil {
		t.Fatal("expected no activation for a workspace with no loaded project")
	}
	if len(app.pendingLaunchAssistants) != 0 {
		t.Fatalf("expected no arming without an activation, got %v", app.pendingLaunchAssistants)
	}
}

// A layout too narrow to render the center pane must not take keyboard focus
// when a tab is created: the auto-launch now creates a tab on every workspace
// creation, and focusing a hidden pane would send keystrokes to an agent the
// user cannot see.
func TestHandleTabCreatedKeepsFocusWhenCenterHidden(t *testing.T) {
	narrow := layout.NewManager()
	narrow.Resize(60, 30)
	if narrow.ShowCenter() {
		t.Fatal("expected a 60-column layout to hide the center pane")
	}

	app := &App{layout: narrow, center: center.New(nil), dashboard: dashboard.New()}
	app.setFocusedPane(messages.PaneDashboard)

	app.handleTabCreated(messages.TabCreated{Name: "claude"})

	if app.focusedPane != messages.PaneDashboard {
		t.Fatalf("expected focus to stay on the dashboard, got %v", app.focusedPane)
	}
}

// The same tab creation in a layout that does render the center pane still
// focuses it, so the user lands in the agent they just started.
func TestHandleTabCreatedFocusesVisibleCenter(t *testing.T) {
	wide := layout.NewManager()
	wide.Resize(140, 40)
	if !wide.ShowCenter() {
		t.Fatal("expected a 140-column layout to show the center pane")
	}

	app := &App{layout: wide, center: center.New(nil), dashboard: dashboard.New()}
	app.setFocusedPane(messages.PaneDashboard)

	app.handleTabCreated(messages.TabCreated{Name: "claude"})

	if app.focusedPane != messages.PaneCenter {
		t.Fatalf("expected focus to move to the center pane, got %v", app.focusedPane)
	}
}

// Two creations can be in flight at once — the create dialog closes immediately
// and the dashboard stays usable — and their completion order is not
// guaranteed. Each workspace must keep its own armed assistant: a single shared
// slot let the second creation overwrite the first, so the first workspace was
// activated with nothing armed and silently got no agent.
func TestConcurrentCreationsEachKeepTheirArmedAssistant(t *testing.T) {
	project := data.NewProject("/repo")
	first := data.NewWorkspace("first", "first", "main", "/repo", "/repo/first")
	first.Assistant = "claude"
	second := data.NewWorkspace("second", "second", "main", "/repo", "/repo/second")
	second.Assistant = "codex"

	app := &App{
		lifecycle: newWorkspaceLifecycleState(),
		dashboard: dashboard.New(),
		center:    center.New(nil),
	}
	app.projects = []data.Project{*project}

	// Both creations complete before either activation is processed.
	app.handleWorkspaceCreated(messages.WorkspaceCreated{Workspace: first})
	app.handleWorkspaceCreated(messages.WorkspaceCreated{Workspace: second})

	if got := app.pendingLaunchAssistants[string(first.ID())]; got != "claude" {
		t.Fatalf("expected the first workspace to keep claude armed, got %q", got)
	}
	if got := app.pendingLaunchAssistants[string(second.ID())]; got != "codex" {
		t.Fatalf("expected the second workspace to keep codex armed, got %q", got)
	}

	// Each activation consumes only its own arming.
	if cmd := app.launchPendingAgent(first); cmd == nil {
		t.Fatal("expected the first workspace to still launch its agent")
	}
	if app.hasPendingAgentLaunch(first) {
		t.Fatal("expected the first arming to be consumed")
	}
	if got := app.pendingLaunchAssistants[string(second.ID())]; got != "codex" {
		t.Fatalf("expected the second arming to survive, got %q", got)
	}
}

// Deleting a workspace disarms its pending launch. Workspace IDs are a hash of
// project+name, so a delete-then-recreate at the same name reuses the ID and a
// stale arming would fire against the new workspace.
func TestDeleteWorkspaceDisarmsPendingLaunch(t *testing.T) {
	doomed := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	other := data.NewWorkspace("other", "other", "main", "/repo", "/repo/other")
	project := data.NewProject("/repo")

	app := &App{
		lifecycle: newWorkspaceLifecycleState(),
		dashboard: dashboard.New(),
		pendingLaunchAssistants: map[string]string{
			string(doomed.ID()): "claude",
			string(other.ID()):  "codex",
		},
	}

	app.handleDeleteWorkspace(messages.DeleteWorkspace{Project: project, Workspace: doomed})

	if app.hasPendingAgentLaunch(doomed) {
		t.Fatal("expected the deleted workspace's launch to be disarmed")
	}
	if got := app.pendingLaunchAssistants[string(other.ID())]; got != "codex" {
		t.Fatalf("expected an unrelated arming to survive the delete, got %q", got)
	}
}

// The tmux check resolves during startup, long before a workspace can be
// created. Treating "not yet checked" as unavailable would skip the launch on a
// perfectly good host, so the unresolved case must arm.
func TestCanAutoLaunchAssistantArmsBeforeTmuxCheckResolves(t *testing.T) {
	unresolved := &App{tmuxCheckDone: false, tmuxAvailable: false}
	if !unresolved.canAutoLaunchAssistant("claude") {
		t.Fatal("expected an unresolved tmux check to arm the launch")
	}
	resolvedMissing := &App{tmuxCheckDone: true, tmuxAvailable: false}
	if resolvedMissing.canAutoLaunchAssistant("claude") {
		t.Fatal("expected a resolved-missing tmux check to skip the launch")
	}
	if (&App{tmuxAvailable: true}).canAutoLaunchAssistant("") {
		t.Fatal("expected an empty assistant to never arm")
	}
}
