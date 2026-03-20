package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/sandbox"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestSandboxManagerSyncAllToLocalDownloadsTrackedSessions(t *testing.T) {
	skipIfNoGit(t)

	manager := NewSandboxManager(nil)
	sb := &rollbackSandbox{id: "sb-sync"}
	workspaceRoot := initRepo(t)
	session := &sandboxSession{
		sandbox:       sb,
		worktreeID:    "wt-sync",
		workspaceRoot: workspaceRoot,
		workspacePath: "/home/daytona/.amux/workspaces/wt-sync/repo",
		needsSyncDown: true,
	}
	manager.storeSession(session)

	var gotCwds []string
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		gotCwds = append(gotCwds, opts.Cwd)
		if opts.WorktreeID != session.worktreeID {
			t.Fatalf("WorktreeID = %q, want %q", opts.WorktreeID, session.worktreeID)
		}
		return nil
	}

	if err := manager.SyncAllToLocal(); err != nil {
		t.Fatalf("SyncAllToLocal() error = %v", err)
	}
	if len(gotCwds) != 1 || gotCwds[0] != workspaceRoot {
		t.Fatalf("download Cwd values = %v, want [%q]", gotCwds, workspaceRoot)
	}
}

func TestSandboxManagerSyncAllToLocalAggregatesErrors(t *testing.T) {
	skipIfNoGit(t)

	manager := NewSandboxManager(nil)
	repoA := initRepo(t)
	repoB := initRepo(t)
	manager.storeSession(&sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-a"},
		worktreeID:    "wt-a",
		workspaceRoot: repoA,
		workspacePath: "/remote/a",
		needsSyncDown: true,
	})
	manager.storeSession(&sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-b"},
		worktreeID:    "wt-b",
		workspaceRoot: repoB,
		workspacePath: "/remote/b",
		needsSyncDown: true,
	})

	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		return errors.New("download failed")
	}

	err := manager.SyncAllToLocal()
	if err == nil {
		t.Fatal("SyncAllToLocal() error = nil, want aggregated error")
	}
	if !strings.Contains(err.Error(), repoA+": download failed") && !strings.Contains(err.Error(), repoB+": download failed") {
		t.Fatalf("SyncAllToLocal() error = %v, want workspace roots in message", err)
	}
}

func TestSandboxManagerSyncAllToLocalSkipsCleanSessions(t *testing.T) {
	manager := NewSandboxManager(nil)
	manager.storeSession(&sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-clean"},
		worktreeID:    "wt-clean",
		workspaceRoot: "/repo/clean",
		workspacePath: "/remote/clean",
		needsSyncDown: false,
	})

	calls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		calls++
		return nil
	}

	if err := manager.SyncAllToLocal(); err != nil {
		t.Fatalf("SyncAllToLocal() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("downloadWorkspace() calls = %d, want 0", calls)
	}
}

func TestSandboxManagerSyncToLocalClearsDirtyWithoutLiveTmux(t *testing.T) {
	skipIfNoGit(t)

	manager := NewSandboxManager(nil)
	repo := initRepo(t)
	ws := data.NewWorkspace("ws", "main", "main", repo, repo)
	session := &sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-dirty"},
		worktreeID:    sandbox.ComputeWorktreeID(ws.Root),
		workspaceRoot: ws.Root,
		workspacePath: "/remote/ws",
		needsSyncDown: true,
	}
	manager.storeSession(session)

	calls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		calls++
		if opts.Cwd != ws.Root {
			t.Fatalf("download Cwd = %q, want %q", opts.Cwd, ws.Root)
		}
		return nil
	}

	if err := manager.SyncToLocal(ws); err != nil {
		t.Fatalf("SyncToLocal() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("downloadWorkspace() calls = %d, want 1", calls)
	}
	if session.needsSyncDown {
		t.Fatal("expected session to be clean after SyncToLocal() with no live tmux sessions")
	}
}

func TestSandboxManagerSyncToLocalSkipsLiveTmuxSession(t *testing.T) {
	skipIfNoGit(t)

	manager := NewSandboxManager(nil)
	repo := initRepo(t)
	ws := data.NewWorkspace("ws", "main", "main", repo, repo)
	session := &sandboxSession{
		sandbox:          &rollbackSandbox{id: "sb-live"},
		worktreeID:       sandbox.ComputeWorktreeID(ws.Root),
		workspaceRoot:    ws.Root,
		workspacePath:    "/remote/ws",
		tmuxSessionNames: map[string]struct{}{"amux-sandbox-live": {}},
		needsSyncDown:    true,
	}
	manager.storeSession(session)
	manager.sessionStateFor = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}

	calls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		calls++
		return nil
	}

	err := manager.SyncToLocal(ws)
	if !errors.Is(err, errSandboxSyncLive) {
		t.Fatalf("SyncToLocal() error = %v, want errSandboxSyncLive", err)
	}
	if calls != 0 {
		t.Fatalf("downloadWorkspace() calls = %d, want 0", calls)
	}
	if !session.needsSyncDown {
		t.Fatal("expected session to remain dirty after skipped SyncToLocal() with live tmux session")
	}
}

func TestSandboxManagerSyncToLocalDiscoversPersistedSandboxTmuxSessions(t *testing.T) {
	skipIfNoGit(t)

	manager := NewSandboxManager(nil)
	repo := initRepo(t)
	ws := data.NewWorkspace("ws", "main", "main", repo, repo)
	sessionName := tmux.SessionName(sandboxTmuxNamespace, string(ws.ID()), "tab-1")
	session := &sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-persisted-live"},
		worktreeID:    sandbox.ComputeWorktreeID(ws.Root),
		workspaceID:   ws.ID(),
		workspaceRoot: ws.Root,
		workspacePath: "/remote/ws",
		needsSyncDown: true,
	}
	manager.storeSession(session)
	manager.sessionsWithTags = func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
		if got := match["@amux"]; got != "1" {
			t.Fatalf("@amux match = %q, want %q", got, "1")
		}
		if got := match["@amux_workspace"]; got != "" {
			t.Fatalf("workspace match = %q, want empty broad match", got)
		}
		if len(keys) != 3 || keys[0] != "@amux_workspace" || keys[1] != "@amux_runtime" || keys[2] != "@amux_type" {
			t.Fatalf("keys = %v, want [@amux_workspace @amux_runtime @amux_type]", keys)
		}
		return []tmux.SessionTagValues{{Name: sessionName, Tags: map[string]string{"@amux_workspace": string(ws.ID()), "@amux_runtime": string(data.RuntimeCloudSandbox), "@amux_type": "agent"}}}, nil
	}
	manager.sessionStateFor = func(got string, opts tmux.Options) (tmux.SessionState, error) {
		if got != sessionName {
			t.Fatalf("sessionStateFor() session = %q, want %q", got, sessionName)
		}
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}

	calls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		calls++
		return nil
	}

	err := manager.SyncToLocal(ws)
	if !errors.Is(err, errSandboxSyncLive) {
		t.Fatalf("SyncToLocal() error = %v, want errSandboxSyncLive", err)
	}
	if calls != 0 {
		t.Fatalf("downloadWorkspace() calls = %d, want 0", calls)
	}
	if _, ok := session.tmuxSessionNames[sessionName]; !ok {
		t.Fatalf("expected discovered tmux session %q to be tracked", sessionName)
	}
}

func TestSandboxManagerSyncToLocalSkipsDirtyLocalWorkspace(t *testing.T) {
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

	manager := NewSandboxManager(nil)
	ws := data.NewWorkspace("ws", "main", "main", repo, repo)
	session := &sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-dirty-local"},
		worktreeID:    sandbox.ComputeWorktreeID(ws.Root),
		workspaceRoot: ws.Root,
		workspacePath: "/remote/ws",
		needsSyncDown: true,
	}
	manager.storeSession(session)

	calls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		calls++
		return nil
	}

	err := manager.SyncToLocal(ws)
	if !errors.Is(err, errSandboxSyncConflict) {
		t.Fatalf("SyncToLocal() error = %v, want errSandboxSyncConflict", err)
	}
	if calls != 0 {
		t.Fatalf("downloadWorkspace() calls = %d, want 0", calls)
	}
	if !session.needsSyncDown {
		t.Fatal("expected session to remain dirty after skipped SyncToLocal()")
	}
}

func TestSandboxManagerSyncAllToLocalSkipsLiveTmuxSessions(t *testing.T) {
	skipIfNoGit(t)

	manager := NewSandboxManager(nil)
	repo := initRepo(t)
	manager.storeSession(&sandboxSession{
		sandbox:          &rollbackSandbox{id: "sb-live"},
		worktreeID:       "wt-live",
		workspaceRoot:    repo,
		workspacePath:    "/remote/live",
		tmuxSessionNames: map[string]struct{}{"amux-sandbox-live": {}},
		needsSyncDown:    true,
	})
	manager.sessionStateFor = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}

	calls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		calls++
		return nil
	}

	if err := manager.SyncAllToLocal(); err != nil {
		t.Fatalf("SyncAllToLocal() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("downloadWorkspace() calls = %d, want 0", calls)
	}
}

func TestSandboxManagerSyncToLocalSkipsActiveShellSession(t *testing.T) {
	skipIfNoGit(t)

	manager := NewSandboxManager(nil)
	repo := initRepo(t)
	ws := data.NewWorkspace("ws", "main", "main", repo, repo)
	session := &sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-shell-live"},
		worktreeID:    sandbox.ComputeWorktreeID(ws.Root),
		workspaceRoot: ws.Root,
		workspacePath: "/remote/ws",
		activeShells:  1,
		needsSyncDown: true,
	}
	manager.storeSession(session)

	calls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		calls++
		return nil
	}

	err := manager.SyncToLocal(ws)
	if !errors.Is(err, errSandboxSyncLive) {
		t.Fatalf("SyncToLocal() error = %v, want errSandboxSyncLive", err)
	}
	if calls != 0 {
		t.Fatalf("downloadWorkspace() calls = %d, want 0", calls)
	}
}

func TestSandboxManagerSyncToLocalAllowsEmptyNonGitTarget(t *testing.T) {
	manager := NewSandboxManager(nil)
	root := t.TempDir()
	ws := data.NewWorkspace("ws", "main", "main", root, root)
	session := &sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-empty"},
		worktreeID:    sandbox.ComputeWorktreeID(ws.Root),
		workspaceRoot: ws.Root,
		workspacePath: "/remote/ws",
		needsSyncDown: true,
	}
	manager.storeSession(session)

	calls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		calls++
		return nil
	}

	if err := manager.SyncToLocal(ws); err != nil {
		t.Fatalf("SyncToLocal() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("downloadWorkspace() calls = %d, want 1", calls)
	}
	if session.needsSyncDown {
		t.Fatal("expected session to be clean after SyncToLocal() into empty non-git target")
	}
}

func TestAttachSessionWithoutMetadataDoesNotRequireAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspaceRoot := filepath.Join(home, "repo")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	manager := NewSandboxManager(nil)
	ws := &data.Workspace{Root: workspaceRoot}

	session, err := manager.attachSession(ws)
	if err != nil {
		t.Fatalf("attachSession() error = %v, want nil without metadata", err)
	}
	if session != nil {
		t.Fatalf("attachSession() = %#v, want nil without metadata", session)
	}
}

func TestAttachSessionRecordsNewWorkspaceIDForExistingAttachedSandbox(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	repo := t.TempDir()
	relRepo, err := filepath.Rel(wd, repo)
	if err != nil {
		t.Fatalf("Rel() error = %v", err)
	}

	oldWS := data.NewWorkspace("ws", "main", "main", relRepo, repo)
	newWS := data.NewWorkspace("ws", "main", "main", repo, repo)
	if oldWS.ID() == newWS.ID() {
		t.Fatalf("expected distinct workspace IDs, both were %q", oldWS.ID())
	}

	manager := NewSandboxManager(nil)
	session := &sandboxSession{
		sandbox:            &rollbackSandbox{id: "sb-attached"},
		worktreeID:         sandbox.ComputeWorktreeID(repo),
		workspaceID:        oldWS.ID(),
		workspaceIDAliases: map[string]struct{}{string(oldWS.ID()): {}},
		workspaceRepo:      oldWS.Repo,
		workspaceRoot:      oldWS.Root,
		workspacePath:      "/remote/ws",
	}
	manager.storeSession(session)

	got, err := manager.attachSession(newWS)
	if err != nil {
		t.Fatalf("attachSession() error = %v", err)
	}
	if got != session {
		t.Fatal("expected existing attached sandbox session to be reused")
	}
	if got.workspaceRepo != newWS.Repo {
		t.Fatalf("workspaceRepo = %q, want %q", got.workspaceRepo, newWS.Repo)
	}
	if got.workspaceRoot != newWS.Root {
		t.Fatalf("workspaceRoot = %q, want %q", got.workspaceRoot, newWS.Root)
	}
	ids := manager.sessionWorkspaceIDs(got)
	if !slices.Contains(ids, string(oldWS.ID())) || !slices.Contains(ids, string(newWS.ID())) {
		t.Fatalf("session workspace IDs = %v, want both %q and %q", ids, oldWS.ID(), newWS.ID())
	}
}

func TestEnsureSessionRefreshesWorkspaceForExistingAttachedSandbox(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	repo := t.TempDir()
	relRepo, err := filepath.Rel(wd, repo)
	if err != nil {
		t.Fatalf("Rel() error = %v", err)
	}

	oldWS := data.NewWorkspace("ws", "main", "main", relRepo, repo)
	newWS := data.NewWorkspace("ws", "main", "main", repo, repo)
	if oldWS.ID() == newWS.ID() {
		t.Fatalf("expected distinct workspace IDs, both were %q", oldWS.ID())
	}

	manager := NewSandboxManager(nil)
	session := &sandboxSession{
		sandbox:            &rollbackSandbox{id: "sb-ensure"},
		worktreeID:         sandbox.ComputeWorktreeID(repo),
		workspaceID:        oldWS.ID(),
		workspaceIDAliases: map[string]struct{}{string(oldWS.ID()): {}},
		workspaceRepo:      oldWS.Repo,
		workspaceRoot:      oldWS.Root,
		workspacePath:      "/remote/ws",
	}
	manager.storeSession(session)

	got, err := manager.ensureSession(newWS, sandbox.AgentShell)
	if err != nil {
		t.Fatalf("ensureSession() error = %v", err)
	}
	if got != session {
		t.Fatal("expected existing attached sandbox session to be reused")
	}
	if got.workspaceRepo != newWS.Repo {
		t.Fatalf("workspaceRepo = %q, want %q", got.workspaceRepo, newWS.Repo)
	}
	if got.workspaceRoot != newWS.Root {
		t.Fatalf("workspaceRoot = %q, want %q", got.workspaceRoot, newWS.Root)
	}
	ids := manager.sessionWorkspaceIDs(got)
	if !slices.Contains(ids, string(oldWS.ID())) || !slices.Contains(ids, string(newWS.ID())) {
		t.Fatalf("session workspace IDs = %v, want both %q and %q", ids, oldWS.ID(), newWS.ID())
	}
}
