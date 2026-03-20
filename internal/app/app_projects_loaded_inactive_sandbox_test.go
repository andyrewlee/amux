package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/sandbox"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

func TestHandleProjectsLoadedRecoversInactiveSandboxSyncAfterWorkspaceRebind(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AMUX_PROVIDER", "fake")

	activeRepo := t.TempDir()
	activeRoot := filepath.Join(activeRepo, "active")
	if err := os.MkdirAll(activeRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(active) error = %v", err)
	}
	inactiveOldRepo := t.TempDir()
	inactiveOldRoot := filepath.Join(inactiveOldRepo, "feature-old")
	if err := os.MkdirAll(inactiveOldRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(inactive old) error = %v", err)
	}
	inactiveNewRepo := t.TempDir()
	inactiveNewRoot := filepath.Join(inactiveNewRepo, "feature-new")
	if err := os.MkdirAll(inactiveNewRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(inactive new) error = %v", err)
	}

	activeWS := data.NewWorkspace("active", "main", "main", activeRepo, activeRoot)
	activeProject := data.NewProject(activeRepo)
	activeProject.AddWorkspace(*activeWS)

	inactiveOldWS := data.NewWorkspace("feature", "feat-branch", "main", inactiveOldRepo, inactiveOldRoot)
	inactiveOldWS.Runtime = data.RuntimeCloudSandbox
	inactiveOldProject := data.NewProject(inactiveOldRepo)
	inactiveOldProject.AddWorkspace(*inactiveOldWS)

	needsSync := true
	if err := sandbox.SaveSandboxMeta(inactiveOldRoot, "fake", sandbox.SandboxMeta{
		SandboxID:     "sb-inactive-rebind",
		Agent:         sandbox.AgentShell,
		Provider:      "fake",
		WorktreeID:    sandbox.ComputeWorktreeID(inactiveOldRoot),
		NeedsSyncDown: &needsSync,
		WorkspaceIDs:  []string{string(inactiveOldWS.ID())},
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	manager := NewSandboxManager(nil)
	session := &sandboxSession{
		sandbox:            sandbox.NewMockRemoteSandbox("sb-inactive-rebind"),
		providerName:       "fake",
		worktreeID:         sandbox.ComputeWorktreeID(inactiveOldRoot),
		workspaceID:        inactiveOldWS.ID(),
		workspaceIDAliases: map[string]struct{}{string(inactiveOldWS.ID()): {}},
		workspaceRoot:      inactiveOldRoot,
		workspaceRepo:      inactiveOldRepo,
		workspacePath:      "/home/daytona/.amux/workspaces/inactive/repo",
		needsSyncDown:      true,
	}
	manager.storeSession(session)

	downloadCalls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		downloadCalls++
		if opts.Cwd != inactiveNewRoot {
			t.Fatalf("download Cwd = %q, want %q", opts.Cwd, inactiveNewRoot)
		}
		return nil
	}

	app := &App{
		dashboard:       dashboard.New(),
		projects:        []data.Project{*activeProject, *inactiveOldProject},
		activeWorkspace: &activeProject.Workspaces[0],
		activeProject:   activeProject,
		showWelcome:     false,
		sandboxManager:  manager,
	}

	inactiveNewWS := data.NewWorkspace("feature", "feat-branch", "main", inactiveNewRepo, inactiveNewRoot)
	inactiveNewWS.Runtime = data.RuntimeLocalWorktree
	inactiveNewProject := data.NewProject(inactiveNewRepo)
	inactiveNewProject.AddWorkspace(*inactiveNewWS)

	cmds := app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*activeProject, *inactiveNewProject}})
	metaNew, err := sandbox.LoadSandboxMeta(inactiveNewRoot, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta(new) error = %v", err)
	}
	if metaNew == nil || metaNew.SandboxID != "sb-inactive-rebind" {
		t.Fatalf("new metadata = %#v, want inactive sandbox metadata moved to new root", metaNew)
	}
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		if msg, ok := cmd().(sandboxSyncResultMsg); ok {
			_ = app.handleSandboxSyncResult(msg)
		}
	}

	if downloadCalls != 1 {
		t.Fatalf("downloadWorkspace() calls = %d, want 1 recovered inactive sync", downloadCalls)
	}
	if session.workspaceRoot != inactiveNewRoot {
		t.Fatalf("session.workspaceRoot = %q, want %q", session.workspaceRoot, inactiveNewRoot)
	}
}

func TestHandleProjectsLoadedRetagsInactiveSandboxTmuxSessionsAfterWorkspaceIDRebind(t *testing.T) {
	activeRepo := t.TempDir()
	activeRoot := filepath.Join(activeRepo, "active")
	if err := os.MkdirAll(activeRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(active) error = %v", err)
	}
	inactiveOldRepo := t.TempDir()
	inactiveOldRoot := filepath.Join(inactiveOldRepo, "feature-old")
	if err := os.MkdirAll(inactiveOldRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(inactive old) error = %v", err)
	}
	inactiveNewRepo := t.TempDir()
	inactiveNewRoot := filepath.Join(inactiveNewRepo, "feature-new")
	if err := os.MkdirAll(inactiveNewRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(inactive new) error = %v", err)
	}

	activeWS := data.NewWorkspace("active", "main", "main", activeRepo, activeRoot)
	activeProject := data.NewProject(activeRepo)
	activeProject.AddWorkspace(*activeWS)

	inactiveOldWS := data.NewWorkspace("feature", "feat-branch", "main", inactiveOldRepo, inactiveOldRoot)
	inactiveOldWS.Runtime = data.RuntimeCloudSandbox
	inactiveOldProject := data.NewProject(inactiveOldRepo)
	inactiveOldProject.AddWorkspace(*inactiveOldWS)

	inactiveNewWS := data.NewWorkspace("feature", "feat-branch", "main", inactiveNewRepo, inactiveNewRoot)
	inactiveNewWS.Runtime = data.RuntimeCloudSandbox
	inactiveNewProject := data.NewProject(inactiveNewRepo)
	inactiveNewProject.AddWorkspace(*inactiveNewWS)

	ops := &rebindCaptureTmuxOps{
		rows: []tmux.SessionTagValues{
			{
				Name: "amux-detached-sandbox",
				Tags: map[string]string{
					"@amux_workspace": string(inactiveOldWS.ID()),
					"@amux_instance":  "instance-old",
				},
			},
		},
	}
	var retagged []struct {
		session string
		key     string
		value   string
	}
	origSetTag := setTmuxSessionTagValue
	setTmuxSessionTagValue = func(sessionName, key, value string, opts tmux.Options) error {
		retagged = append(retagged, struct {
			session string
			key     string
			value   string
		}{session: sessionName, key: key, value: value})
		return nil
	}
	defer func() { setTmuxSessionTagValue = origSetTag }()

	app := &App{
		dashboard:       dashboard.New(),
		projects:        []data.Project{*activeProject, *inactiveOldProject},
		activeWorkspace: &activeProject.Workspaces[0],
		activeProject:   activeProject,
		showWelcome:     false,
		instanceID:      "instance-a",
		tmuxAvailable:   true,
		tmuxService:     newTmuxService(ops),
	}

	cmds := app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*activeProject, *inactiveNewProject}})
	for _, cmd := range cmds {
		if cmd != nil {
			_ = cmd()
		}
	}

	if len(retagged) != 1 {
		t.Fatalf("retagged sessions = %d, want 1", len(retagged))
	}
	if retagged[0].session != "amux-detached-sandbox" {
		t.Fatalf("retagged session = %q, want %q", retagged[0].session, "amux-detached-sandbox")
	}
	if retagged[0].key != "@amux_workspace" {
		t.Fatalf("retag key = %q, want %q", retagged[0].key, "@amux_workspace")
	}
	if retagged[0].value != string(inactiveNewWS.ID()) {
		t.Fatalf("retag value = %q, want %q", retagged[0].value, inactiveNewWS.ID())
	}
}
