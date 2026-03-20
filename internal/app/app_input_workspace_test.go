package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/sandbox"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

func TestHandleWorkspaceDeletedClearsDirtyWorkspaceMarker(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())

	app := &App{
		dashboard:            dashboard.New(),
		center:               center.New(nil),
		sidebar:              sidebar.NewTabbedSidebar(),
		sidebarTerminal:      sidebar.NewTerminalModel(),
		dirtyWorkspaces:      map[string]bool{wsID: true},
		deletingWorkspaceIDs: map[string]bool{wsID: true},
	}

	app.handleWorkspaceDeleted(messages.WorkspaceDeleted{Workspace: ws})

	if app.isWorkspaceDeleteInFlight(wsID) {
		t.Fatal("expected delete-in-flight marker to be cleared on delete success")
	}
	if app.dirtyWorkspaces[wsID] {
		t.Fatal("expected dirty workspace marker to be cleared on delete success")
	}
}

func TestSyncWorkspaceFromSandboxSkipsDirtyLocalWorkspace(t *testing.T) {
	skipIfNoGit(t)

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("init\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ws := data.NewWorkspace("feature", "feature", "main", repo, repo)
	manager := NewSandboxManager(nil)
	manager.storeSession(&sandboxSession{
		sandbox:       sandbox.NewMockRemoteSandbox("sb-sync"),
		worktreeID:    sandbox.ComputeWorktreeID(ws.Root),
		workspaceRoot: ws.Root,
		workspacePath: "/remote/ws",
		needsSyncDown: true,
	})

	downloadCalls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		downloadCalls++
		return nil
	}

	app := &App{sandboxManager: manager}
	cmd := app.syncWorkspaceFromSandbox(ws, ws)
	if cmd == nil {
		t.Fatal("syncWorkspaceFromSandbox() returned nil command")
	}
	msg := cmd()
	result, ok := msg.(sandboxSyncResultMsg)
	if !ok {
		t.Fatalf("expected sandboxSyncResultMsg, got %T", msg)
	}
	cmds := app.handleSandboxSyncResult(result)
	if len(cmds) != 1 {
		t.Fatalf("handleSandboxSyncResult() cmds = %d, want 1", len(cmds))
	}
	toast, ok := cmds[0]().(messages.Toast)
	if !ok {
		t.Fatalf("expected messages.Toast, got %T", cmds[0]())
	}
	if toast.Level != messages.ToastWarning {
		t.Fatalf("toast.Level = %v, want %v", toast.Level, messages.ToastWarning)
	}
	if toast.Message != "Sandbox sync-down skipped due to local changes" {
		t.Fatalf("toast.Message = %q, want %q", toast.Message, "Sandbox sync-down skipped due to local changes")
	}
	if downloadCalls != 0 {
		t.Fatalf("downloadWorkspace() calls = %d, want 0", downloadCalls)
	}
}

func TestSyncWorkspaceFromSandboxSkipsLiveSandboxSession(t *testing.T) {
	skipIfNoGit(t)

	repo := initRepo(t)
	ws := data.NewWorkspace("feature", "feature", "main", repo, repo)
	manager := NewSandboxManager(nil)
	manager.storeSession(&sandboxSession{
		sandbox:          sandbox.NewMockRemoteSandbox("sb-sync-live"),
		worktreeID:       sandbox.ComputeWorktreeID(ws.Root),
		workspaceRoot:    ws.Root,
		workspacePath:    "/remote/ws",
		tmuxSessionNames: map[string]struct{}{"amux-sandbox-live": {}},
		needsSyncDown:    true,
	})
	manager.sessionStateFor = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}

	downloadCalls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		downloadCalls++
		return nil
	}

	app := &App{sandboxManager: manager}
	cmd := app.syncWorkspaceFromSandbox(ws, ws)
	if cmd == nil {
		t.Fatal("syncWorkspaceFromSandbox() returned nil command")
	}
	msg := cmd()
	result, ok := msg.(sandboxSyncResultMsg)
	if !ok {
		t.Fatalf("expected sandboxSyncResultMsg, got %T", msg)
	}
	if len(app.pendingSandboxSyncs) != 0 {
		t.Fatal("expected async sync command to leave pending-sync bookkeeping untouched before update handling")
	}
	cmds := app.handleSandboxSyncResult(result)
	if len(cmds) != 1 {
		t.Fatalf("handleSandboxSyncResult() cmds = %d, want 1", len(cmds))
	}
	toast, ok := cmds[0]().(messages.Toast)
	if !ok {
		t.Fatalf("expected messages.Toast, got %T", cmds[0]())
	}
	if toast.Level != messages.ToastWarning {
		t.Fatalf("toast.Level = %v, want %v", toast.Level, messages.ToastWarning)
	}
	if toast.Message != "Sandbox sync-down skipped while sandbox session is still running" {
		t.Fatalf("toast.Message = %q, want %q", toast.Message, "Sandbox sync-down skipped while sandbox session is still running")
	}
	if downloadCalls != 0 {
		t.Fatalf("downloadWorkspace() calls = %d, want 0", downloadCalls)
	}
}

func TestSyncWorkspaceFromSandboxRetriesAfterLiveSessionStops(t *testing.T) {
	skipIfNoGit(t)

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
	manager := NewSandboxManager(nil)
	manager.storeSession(&sandboxSession{
		sandbox:          sandbox.NewMockRemoteSandbox("sb-sync-retry"),
		worktreeID:       sandbox.ComputeWorktreeID(source.Root),
		workspaceID:      target.ID(),
		workspaceRoot:    source.Root,
		workspacePath:    "/remote/ws",
		tmuxSessionNames: map[string]struct{}{"amux-sandbox-live": {}},
		needsSyncDown:    true,
	})

	live := true
	manager.sessionStateFor = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: live}, nil
	}

	downloadCalls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		downloadCalls++
		return nil
	}

	app := &App{
		sandboxManager:      manager,
		pendingSandboxSyncs: make(map[string]pendingSandboxSync),
	}
	cmd := app.syncWorkspaceFromSandbox(source, target)
	if cmd == nil {
		t.Fatal("syncWorkspaceFromSandbox() returned nil command")
	}
	msg := cmd()
	result, ok := msg.(sandboxSyncResultMsg)
	if !ok {
		t.Fatalf("expected sandboxSyncResultMsg, got %T", msg)
	}
	if len(app.pendingSandboxSyncs) != 0 {
		t.Fatal("expected async sync command to leave pending-sync bookkeeping untouched before update handling")
	}
	cmds := app.handleSandboxSyncResult(result)
	if len(cmds) != 1 {
		t.Fatalf("handleSandboxSyncResult() cmds = %d, want 1", len(cmds))
	}
	toast, ok := cmds[0]().(messages.Toast)
	if !ok {
		t.Fatalf("expected messages.Toast, got %T", cmds[0]())
	}
	if toast.Message != "Sandbox sync-down skipped while sandbox session is still running" {
		t.Fatalf("toast.Message = %q, want live-session warning", toast.Message)
	}
	if downloadCalls != 0 {
		t.Fatalf("downloadWorkspace() calls = %d, want 0 before retry", downloadCalls)
	}
	if _, ok := app.pendingSandboxSyncs[string(target.ID())]; !ok {
		t.Fatal("expected pending sandbox sync to be tracked after live-session skip")
	}
	if _, ok := app.pendingSandboxSyncs[string(source.ID())]; !ok {
		t.Fatal("expected pending sandbox sync to be addressable by source workspace ID after rebind")
	}

	live = false
	retry := app.retryPendingSandboxSync(string(source.ID()))
	if retry == nil {
		t.Fatal("retryPendingSandboxSync() returned nil command")
	}
	retryMsg := retry()
	retryResult, ok := retryMsg.(sandboxSyncResultMsg)
	if !ok {
		t.Fatalf("expected sandboxSyncResultMsg from retry, got %T", retryMsg)
	}
	cmds = app.handleSandboxSyncResult(retryResult)
	if len(cmds) != 1 {
		t.Fatalf("handleSandboxSyncResult() cmds = %d, want 1 after successful retry", len(cmds))
	}
	if _, ok := cmds[0]().(messages.GitStatusResult); !ok {
		t.Fatalf("expected GitStatusResult after successful retry, got %T", cmds[0]())
	}
	if downloadCalls != 1 {
		t.Fatalf("downloadWorkspace() calls = %d, want 1 after retry", downloadCalls)
	}
	if _, ok := app.pendingSandboxSyncs[string(target.ID())]; ok {
		t.Fatal("expected pending sandbox sync to be cleared after successful retry")
	}
	if _, ok := app.pendingSandboxSyncs[string(source.ID())]; ok {
		t.Fatal("expected pending sandbox sync source alias to be cleared after successful retry")
	}
}

func TestHandleSandboxSyncResultRefreshesGitStatusAfterSuccessfulSync(t *testing.T) {
	source := data.NewWorkspace("feature", "feature", "main", "/repo-old", "/repo-old")
	target := data.NewWorkspace("feature", "feature", "main", "/repo-new", "/repo-new")
	gitStatus := &fileWatcherGitStatusStub{}
	app := &App{
		gitStatus:            gitStatus,
		pendingSandboxSyncs:  make(map[string]pendingSandboxSync),
		deletingWorkspaceIDs: make(map[string]bool),
	}
	app.trackPendingSandboxSync(source, target)

	cmds := app.handleSandboxSyncResult(sandboxSyncResultMsg{
		source: *source,
		target: *target,
	})
	if len(cmds) != 1 {
		t.Fatalf("handleSandboxSyncResult() cmds = %d, want 1 on successful sync", len(cmds))
	}
	msg := cmds[0]()
	result, ok := msg.(messages.GitStatusResult)
	if !ok {
		t.Fatalf("expected GitStatusResult, got %T", msg)
	}
	if result.Root != target.Root {
		t.Fatalf("GitStatusResult.Root = %q, want %q", result.Root, target.Root)
	}
	if len(gitStatus.refreshRoots) != 1 || gitStatus.refreshRoots[0] != target.Root {
		t.Fatalf("Refresh roots = %v, want [%q]", gitStatus.refreshRoots, target.Root)
	}
	if result.Status == nil || !result.Status.HasLineStats {
		t.Fatalf("expected full git status refresh with line stats, got %#v", result.Status)
	}
	if _, ok := app.pendingSandboxSyncs[string(target.ID())]; ok {
		t.Fatal("expected pending sandbox sync to be cleared after successful sync")
	}
	if _, ok := app.pendingSandboxSyncs[string(source.ID())]; ok {
		t.Fatal("expected source alias pending sandbox sync to be cleared after successful sync")
	}
}

func TestRetryPendingSandboxSyncReturnsNilWhileRetryInFlight(t *testing.T) {
	source := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo")
	target := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo")

	app := &App{
		sandboxManager:      NewSandboxManager(nil),
		pendingSandboxSyncs: make(map[string]pendingSandboxSync),
	}
	app.trackPendingSandboxSync(source, target)

	first := app.retryPendingSandboxSync(string(target.ID()))
	if first == nil {
		t.Fatal("expected first retryPendingSandboxSync() call to return a command")
	}
	second := app.retryPendingSandboxSync(string(target.ID()))
	if second != nil {
		t.Fatal("expected second retryPendingSandboxSync() call to be suppressed while retry is in flight")
	}
}

func TestRetargetPendingSandboxSyncsAcrossRepeatedWorkspaceIDRebinds(t *testing.T) {
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

	linkRepo := filepath.Join(base, "repo-link")
	if err := os.Symlink(absRepo, linkRepo); err != nil {
		t.Fatalf("Symlink(%q -> %q) error = %v", linkRepo, absRepo, err)
	}
	linkRoot := filepath.Join(linkRepo, "feature")

	source := data.NewWorkspace("feature", "feature", "main", relRepo, relRoot)
	target1 := data.NewWorkspace("feature", "feature", "main", absRepo, absRoot)
	target2 := data.NewWorkspace("feature", "feature", "main", linkRepo, linkRoot)

	manager := NewSandboxManager(nil)
	manager.storeSession(&sandboxSession{
		sandbox:          sandbox.NewMockRemoteSandbox("sb-sync-rebind"),
		worktreeID:       sandbox.ComputeWorktreeID(source.Root),
		workspaceID:      target1.ID(),
		workspaceRoot:    source.Root,
		workspacePath:    "/remote/ws",
		tmuxSessionNames: map[string]struct{}{"amux-sandbox-live": {}},
		needsSyncDown:    true,
	})

	live := true
	manager.sessionStateFor = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: live}, nil
	}

	var gotCwd string
	downloadCalls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		downloadCalls++
		gotCwd = opts.Cwd
		return nil
	}

	app := &App{
		sandboxManager:      manager,
		pendingSandboxSyncs: make(map[string]pendingSandboxSync),
	}
	cmd := app.syncWorkspaceFromSandbox(source, target1)
	if cmd == nil {
		t.Fatal("syncWorkspaceFromSandbox() returned nil command")
	}
	msg := cmd()
	result, ok := msg.(sandboxSyncResultMsg)
	if !ok {
		t.Fatalf("expected sandboxSyncResultMsg, got %T", msg)
	}
	if len(app.pendingSandboxSyncs) != 0 {
		t.Fatal("expected async sync command to leave pending-sync bookkeeping untouched before update handling")
	}
	cmds := app.handleSandboxSyncResult(result)
	if len(cmds) != 1 {
		t.Fatalf("handleSandboxSyncResult() cmds = %d, want 1", len(cmds))
	}

	app.rememberReboundWorkspaceID(string(target1.ID()), string(target2.ID()))
	app.retargetPendingSandboxSyncs(string(target1.ID()), target2)

	live = false
	retry := app.retryPendingSandboxSync(string(target2.ID()))
	if retry == nil {
		t.Fatal("retryPendingSandboxSync() returned nil command after second rebind")
	}
	retryMsg := retry()
	retryResult, ok := retryMsg.(sandboxSyncResultMsg)
	if !ok {
		t.Fatalf("expected sandboxSyncResultMsg from retry, got %T", retryMsg)
	}
	cmds = app.handleSandboxSyncResult(retryResult)
	if len(cmds) != 1 {
		t.Fatalf("handleSandboxSyncResult() cmds = %d, want 1 after successful retry", len(cmds))
	}
	if _, ok := cmds[0]().(messages.GitStatusResult); !ok {
		t.Fatalf("expected GitStatusResult after successful retry, got %T", cmds[0]())
	}
	if downloadCalls != 1 {
		t.Fatalf("downloadWorkspace() calls = %d, want 1 after retry", downloadCalls)
	}
	if gotCwd != target2.Root {
		t.Fatalf("download Cwd = %q, want %q", gotCwd, target2.Root)
	}
	if _, ok := app.pendingSandboxSyncs[string(target2.ID())]; ok {
		t.Fatal("expected retargeted pending sync to be cleared after retry")
	}
}
