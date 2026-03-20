package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/sandbox"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

func TestHandleSandboxSyncResultPersistsDeferredSyncTargetForRestartRecovery(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	base := t.TempDir()
	absRepo := filepath.Join(base, "repo")
	absRoot := filepath.Join(base, "workspaces", "repo", "feature")
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", absRoot, err)
	}
	relRepo, err := filepath.Rel(wd, absRepo)
	if err != nil {
		t.Fatalf("Rel(repo) error = %v", err)
	}
	relRoot, err := filepath.Rel(wd, absRoot)
	if err != nil {
		t.Fatalf("Rel(root) error = %v", err)
	}

	source := data.NewWorkspace("feature", "feature", "main", relRepo, relRoot)
	target := data.NewWorkspace("feature", "feature", "main", absRepo, absRoot)

	needsSync := true
	if err := sandbox.SaveSandboxMeta(source.Root, "fake", sandbox.SandboxMeta{
		SandboxID:     "sb-persist-live-skip",
		Agent:         sandbox.AgentShell,
		Provider:      "fake",
		WorktreeID:    sandbox.ComputeWorktreeID(source.Root),
		NeedsSyncDown: &needsSync,
		WorkspaceIDs:  []string{string(source.ID())},
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	manager := NewSandboxManager(nil)
	manager.storeSession(&sandboxSession{
		sandbox:            sandbox.NewMockRemoteSandbox("sb-persist-live-skip"),
		providerName:       "fake",
		worktreeID:         sandbox.ComputeWorktreeID(source.Root),
		workspaceID:        source.ID(),
		workspaceIDAliases: map[string]struct{}{string(source.ID()): {}},
		workspaceRoot:      source.Root,
		workspacePath:      "/remote/ws",
		needsSyncDown:      true,
	})

	app := &App{
		sandboxManager:      manager,
		pendingSandboxSyncs: make(map[string]pendingSandboxSync),
	}

	cmds := app.handleSandboxSyncResult(sandboxSyncResultMsg{
		source:       *source,
		target:       *target,
		notifyOnLive: true,
		err:          errSandboxSyncLive,
	})
	if len(cmds) != 1 {
		t.Fatalf("handleSandboxSyncResult() cmds = %d, want 1", len(cmds))
	}

	metaNew, err := sandbox.LoadSandboxMeta(target.Root, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta(target) error = %v", err)
	}
	if metaNew == nil || metaNew.SandboxID != "sb-persist-live-skip" {
		t.Fatalf("target metadata = %#v, want sandbox metadata moved to retry target", metaNew)
	}
	if metaOld, err := sandbox.LoadSandboxMeta(source.Root, "fake"); err != nil {
		t.Fatalf("LoadSandboxMeta(source) error = %v", err)
	} else if sandbox.ComputeWorktreeID(source.Root) != sandbox.ComputeWorktreeID(target.Root) && metaOld != nil {
		t.Fatalf("expected source metadata moved after deferred sync persistence, got %#v", metaOld)
	}
	foundTargetID := false
	for _, id := range metaNew.WorkspaceIDs {
		if id == string(target.ID()) {
			foundTargetID = true
			break
		}
	}
	if !foundTargetID {
		t.Fatalf("expected moved sandbox metadata to retain target workspace ID %q, got %v", target.ID(), metaNew.WorkspaceIDs)
	}
	if _, ok := app.pendingSandboxSyncs[string(source.ID())]; !ok {
		t.Fatal("expected deferred sync to remain addressable by original workspace ID")
	}
}

func TestHandleProjectsLoadedRecoversPersistedPendingSandboxSyncs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AMUX_PROVIDER", "fake")

	repo := t.TempDir()
	ws := data.NewWorkspace("feature", "feature", "main", repo, repo)
	ws.Runtime = data.RuntimeLocalWorktree
	project := data.NewProject(repo)
	project.AddWorkspace(*ws)

	needsSync := true
	if err := sandbox.SaveSandboxMeta(ws.Root, "fake", sandbox.SandboxMeta{
		SandboxID:     "sb-recover-pending",
		Agent:         sandbox.AgentShell,
		Provider:      "fake",
		WorktreeID:    sandbox.ComputeWorktreeID(ws.Root),
		NeedsSyncDown: &needsSync,
		WorkspaceIDs:  []string{string(ws.ID())},
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	manager := NewSandboxManager(nil)
	manager.attachSessionFn = func(wt *data.Workspace) (*sandboxSession, error) {
		return &sandboxSession{
			sandbox:       sandbox.NewMockRemoteSandbox("sb-recover-pending"),
			worktreeID:    sandbox.ComputeWorktreeID(wt.Root),
			workspaceID:   wt.ID(),
			workspaceRoot: wt.Root,
			workspacePath: "/remote/ws",
			needsSyncDown: true,
		}, nil
	}
	downloadCalls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		downloadCalls++
		if opts.Cwd != ws.Root {
			t.Fatalf("download Cwd = %q, want %q", opts.Cwd, ws.Root)
		}
		return nil
	}

	app := &App{
		dashboard:            dashboard.New(),
		sandboxManager:       manager,
		pendingSandboxSyncs:  make(map[string]pendingSandboxSync),
		reboundWorkspaceIDs:  make(map[string]string),
		deletingWorkspaceIDs: make(map[string]bool),
	}

	cmds := app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*project}})
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		if msg, ok := cmd().(sandboxSyncResultMsg); ok {
			_ = app.handleSandboxSyncResult(msg)
		}
	}

	if downloadCalls != 1 {
		t.Fatalf("downloadWorkspace() calls = %d, want 1 after recovering persisted pending sync", downloadCalls)
	}
	if _, ok := app.pendingSandboxSyncs[string(ws.ID())]; ok {
		t.Fatal("expected recovered pending sync to clear after successful retry")
	}
}

func TestSandboxShellDetachedRetriesPendingSyncAfterCleanup(t *testing.T) {
	repo := t.TempDir()
	ws := data.NewWorkspace("feature", "feature", "main", repo, repo)

	manager := NewSandboxManager(nil)
	manager.attachSessionFn = func(wt *data.Workspace) (*sandboxSession, error) {
		return &sandboxSession{
			sandbox:       sandbox.NewMockRemoteSandbox("sb-shell-detached"),
			worktreeID:    sandbox.ComputeWorktreeID(wt.Root),
			workspaceID:   wt.ID(),
			workspaceRoot: wt.Root,
			workspacePath: "/remote/ws",
			needsSyncDown: true,
		}, nil
	}
	downloadCalls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		downloadCalls++
		return nil
	}

	app := &App{
		sandboxManager:      manager,
		sidebarTerminal:     sidebar.NewTerminalModel(),
		pendingSandboxSyncs: make(map[string]pendingSandboxSync),
	}
	app.trackPendingSandboxSync(ws, ws)

	_, stoppedCmd := app.update(messages.SidebarPTYStopped{WorkspaceID: string(ws.ID())})
	if stoppedCmd != nil {
		if _, ok := stoppedCmd().(sandboxSyncResultMsg); ok {
			t.Fatal("expected SidebarPTYStopped not to trigger a sandbox sync retry")
		}
	}
	if downloadCalls != 0 {
		t.Fatalf("downloadWorkspace() calls = %d, want 0 before shell cleanup completes", downloadCalls)
	}

	_, detachedCmd := app.update(messages.SandboxShellDetached{WorkspaceID: string(ws.ID())})
	if detachedCmd == nil {
		t.Fatal("expected SandboxShellDetached to trigger a retry command")
	}
	msg := detachedCmd()
	result, ok := msg.(sandboxSyncResultMsg)
	if !ok {
		t.Fatalf("expected sandboxSyncResultMsg, got %T", msg)
	}
	cmds := app.handleSandboxSyncResult(result)
	if len(cmds) != 1 {
		t.Fatalf("handleSandboxSyncResult() cmds = %d, want 1 after detached retry succeeds", len(cmds))
	}
	if _, ok := cmds[0]().(messages.GitStatusResult); !ok {
		t.Fatalf("expected GitStatusResult after detached retry succeeds, got %T", cmds[0]())
	}
	if downloadCalls != 1 {
		t.Fatalf("downloadWorkspace() calls = %d, want 1 after shell cleanup completion", downloadCalls)
	}
}
