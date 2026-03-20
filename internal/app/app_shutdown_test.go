package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/sandbox"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/center"
)

func TestAppShutdownSyncsSandboxSessions(t *testing.T) {
	skipIfNoGit(t)

	repo := initRepo(t)
	manager := NewSandboxManager(nil)
	manager.storeSession(&sandboxSession{
		sandbox:       sandbox.NewMockRemoteSandbox("sb-shutdown"),
		worktreeID:    "wt-shutdown",
		workspaceRoot: repo,
		workspacePath: "/home/daytona/.amux/workspaces/wt-shutdown/repo",
		needsSyncDown: true,
	})

	var synced bool
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		synced = true
		if opts.Cwd != repo {
			t.Fatalf("download Cwd = %q, want %q", opts.Cwd, repo)
		}
		return nil
	}

	app := &App{sandboxManager: manager}
	app.Shutdown()

	if !synced {
		t.Fatal("expected Shutdown() to sync sandbox sessions to local")
	}
}

type shutdownAgentProviderStub struct {
	closed bool
}

func (s *shutdownAgentProviderStub) CreateAgent(*data.Workspace, pty.AgentType, uint16, uint16) (*pty.Agent, error) {
	return nil, nil
}

func (s *shutdownAgentProviderStub) CreateAgentWithTags(*data.Workspace, pty.AgentType, string, uint16, uint16, tmux.SessionTags) (*pty.Agent, error) {
	return nil, nil
}

func (s *shutdownAgentProviderStub) CreateViewer(*data.Workspace, string, uint16, uint16) (*pty.Agent, error) {
	return nil, nil
}

func (s *shutdownAgentProviderStub) CreateViewerWithTags(*data.Workspace, string, string, uint16, uint16, tmux.SessionTags) (*pty.Agent, error) {
	return nil, nil
}

func (s *shutdownAgentProviderStub) CloseAgent(*pty.Agent) error { return nil }

func (s *shutdownAgentProviderStub) CloseAll() {
	s.closed = true
}

func TestAppShutdownClosesCenterBeforeSync(t *testing.T) {
	skipIfNoGit(t)

	repo := initRepo(t)
	manager := NewSandboxManager(nil)
	manager.storeSession(&sandboxSession{
		sandbox:       sandbox.NewMockRemoteSandbox("sb-shutdown-order"),
		worktreeID:    "wt-shutdown-order",
		workspaceRoot: repo,
		workspacePath: "/home/daytona/.amux/workspaces/wt-shutdown-order/repo",
		needsSyncDown: true,
	})

	provider := &shutdownAgentProviderStub{}
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		if !provider.closed {
			t.Fatal("expected center.Close() to run before shutdown sync")
		}
		return nil
	}

	app := &App{
		center:         center.New(nil, provider),
		sandboxManager: manager,
	}
	app.Shutdown()
}

func TestAppShutdownLeavesLiveSandboxTmuxSessionsRunning(t *testing.T) {
	skipIfNoGit(t)

	repo := initRepo(t)
	manager := NewSandboxManager(nil)
	manager.SetInstanceID("instance-a")
	session := &sandboxSession{
		sandbox:          sandbox.NewMockRemoteSandbox("sb-shutdown-live"),
		worktreeID:       "wt-shutdown-live",
		workspaceID:      "main",
		workspaceRoot:    repo,
		workspacePath:    "/home/daytona/.amux/workspaces/wt-shutdown-live/repo",
		tmuxSessionNames: map[string]struct{}{"amux-sandbox-main-tab-1": {}},
		needsSyncDown:    true,
	}
	manager.storeSession(session)

	manager.sessionStateFor = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		if sessionName != "amux-sandbox-main-tab-1" {
			t.Fatalf("sessionStateFor() session = %q, want %q", sessionName, "amux-sandbox-main-tab-1")
		}
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	killed := false
	manager.killTmuxSession = func(sessionName string, opts tmux.Options) error {
		killed = true
		return nil
	}
	synced := false
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		synced = true
		return nil
	}

	app := &App{sandboxManager: manager}
	app.Shutdown()

	if killed {
		t.Fatal("expected Shutdown() to leave tracked sandbox tmux sessions running for reattach")
	}
	if synced {
		t.Fatal("expected Shutdown() to skip syncing live sandbox tmux sessions")
	}
}

func TestAppShutdownDoesNotDiscoverPersistedSandboxSessions(t *testing.T) {
	skipIfNoGit(t)

	repo := initRepo(t)
	ws := data.NewWorkspace("ws", "main", "main", repo, repo)
	manager := NewSandboxManager(nil)
	discovered := false
	attached := false
	manager.attachSessionFn = func(wt *data.Workspace) (*sandboxSession, error) {
		discovered = true
		attached = true
		return nil, nil
	}

	var synced bool
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		synced = true
		return nil
	}

	app := &App{
		sandboxManager: manager,
		projects: []data.Project{{
			Name:       "repo",
			Path:       repo,
			Workspaces: []data.Workspace{*ws},
		}},
	}
	app.Shutdown()

	if discovered {
		t.Fatal("expected Shutdown() to ignore persisted sandbox metadata it never attached to")
	}
	if attached {
		t.Fatal("expected Shutdown() to avoid attaching persisted sandbox sessions it never owned")
	}
	if synced {
		t.Fatal("expected Shutdown() to skip syncing persisted sandbox sessions it never attached to")
	}
}
