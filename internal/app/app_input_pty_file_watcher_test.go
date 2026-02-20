package app

import (
	"context"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

type fileWatcherGitStatusStub struct {
	invalidateRoots  []string
	refreshRoots     []string
	refreshFastRoots []string
	cacheByRoot      map[string]*git.StatusResult
}

func (s *fileWatcherGitStatusStub) Run(context.Context) error { return nil }

func (s *fileWatcherGitStatusStub) GetCached(root string) *git.StatusResult {
	if s.cacheByRoot == nil {
		return nil
	}
	return s.cacheByRoot[root]
}

func (s *fileWatcherGitStatusStub) UpdateCache(root string, status *git.StatusResult) {
	if s.cacheByRoot == nil {
		s.cacheByRoot = make(map[string]*git.StatusResult)
	}
	s.cacheByRoot[root] = status
}

func (s *fileWatcherGitStatusStub) Invalidate(root string) {
	s.invalidateRoots = append(s.invalidateRoots, root)
}

func (s *fileWatcherGitStatusStub) Refresh(root string) (*git.StatusResult, error) {
	s.refreshRoots = append(s.refreshRoots, root)
	return &git.StatusResult{HasLineStats: true}, nil
}

func (s *fileWatcherGitStatusStub) RefreshFast(root string) (*git.StatusResult, error) {
	s.refreshFastRoots = append(s.refreshFastRoots, root)
	return &git.StatusResult{HasLineStats: false}, nil
}

func TestHandleFileWatcherEvent_ActiveWorkspaceRequestsFullStatus(t *testing.T) {
	active := &data.Workspace{
		Repo: "/tmp/repo",
		Root: "/tmp/repo/ws-active",
	}
	stub := &fileWatcherGitStatusStub{}
	app := &App{
		gitStatus:            stub,
		dashboard:            dashboard.New(),
		activeWorkspace:      active,
		dirtyWorkspaces:      make(map[string]bool),
		creatingWorkspaceIDs: make(map[string]bool),
	}

	cmds := app.handleFileWatcherEvent(messages.FileWatcherEvent{Root: active.Root})
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}
	if cmds[0] == nil {
		t.Fatal("expected status command")
	}
	msg := cmds[0]()
	result, ok := msg.(messages.GitStatusResult)
	if !ok {
		t.Fatalf("expected GitStatusResult, got %T", msg)
	}
	if result.Root != active.Root {
		t.Fatalf("expected root %q, got %q", active.Root, result.Root)
	}
	if result.Status == nil || !result.Status.HasLineStats {
		t.Fatalf("expected full status with line stats")
	}
	if len(stub.refreshRoots) != 1 {
		t.Fatalf("expected full refresh call, got %d", len(stub.refreshRoots))
	}
	if len(stub.refreshFastRoots) != 0 {
		t.Fatalf("expected no fast refresh call, got %d", len(stub.refreshFastRoots))
	}
}

func TestHandleFileWatcherEvent_InactiveWorkspaceRequestsFastStatus(t *testing.T) {
	active := &data.Workspace{
		Repo: "/tmp/repo",
		Root: "/tmp/repo/ws-active",
	}
	otherRoot := "/tmp/repo/ws-other"
	stub := &fileWatcherGitStatusStub{}
	app := &App{
		gitStatus:            stub,
		dashboard:            dashboard.New(),
		activeWorkspace:      active,
		dirtyWorkspaces:      make(map[string]bool),
		creatingWorkspaceIDs: make(map[string]bool),
	}

	cmds := app.handleFileWatcherEvent(messages.FileWatcherEvent{Root: otherRoot})
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}
	if cmds[0] == nil {
		t.Fatal("expected status command")
	}
	msg := cmds[0]()
	result, ok := msg.(messages.GitStatusResult)
	if !ok {
		t.Fatalf("expected GitStatusResult, got %T", msg)
	}
	if result.Root != otherRoot {
		t.Fatalf("expected root %q, got %q", otherRoot, result.Root)
	}
	if result.Status == nil || result.Status.HasLineStats {
		t.Fatalf("expected fast status without line stats")
	}
	if len(stub.refreshRoots) != 0 {
		t.Fatalf("expected no full refresh call, got %d", len(stub.refreshRoots))
	}
	if len(stub.refreshFastRoots) != 1 {
		t.Fatalf("expected one fast refresh call, got %d", len(stub.refreshFastRoots))
	}
}

func TestHandleGitStatusTick_ActiveWorkspaceCacheMissRequestsFullStatus(t *testing.T) {
	active := &data.Workspace{
		Repo: "/tmp/repo",
		Root: "/tmp/repo/ws-active",
	}
	stub := &fileWatcherGitStatusStub{}
	app := &App{
		gitStatus:            stub,
		dashboard:            dashboard.New(),
		activeWorkspace:      active,
		dirtyWorkspaces:      make(map[string]bool),
		creatingWorkspaceIDs: make(map[string]bool),
	}

	cmds := app.handleGitStatusTick()
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}
	if cmds[0] == nil {
		t.Fatal("expected status command")
	}
	msg := cmds[0]()
	result, ok := msg.(messages.GitStatusResult)
	if !ok {
		t.Fatalf("expected GitStatusResult, got %T", msg)
	}
	if result.Root != active.Root {
		t.Fatalf("expected root %q, got %q", active.Root, result.Root)
	}
	if result.Status == nil || !result.Status.HasLineStats {
		t.Fatalf("expected full status with line stats on cache miss")
	}
	if len(stub.refreshRoots) != 1 {
		t.Fatalf("expected full refresh call, got %d", len(stub.refreshRoots))
	}
	if len(stub.refreshFastRoots) != 0 {
		t.Fatalf("expected no fast refresh call, got %d", len(stub.refreshFastRoots))
	}
}

func TestHandleGitStatusTick_ActiveWorkspaceCachedStatusSkipsRefresh(t *testing.T) {
	active := &data.Workspace{
		Repo: "/tmp/repo",
		Root: "/tmp/repo/ws-active",
	}
	stub := &fileWatcherGitStatusStub{
		cacheByRoot: map[string]*git.StatusResult{
			active.Root: {HasLineStats: true},
		},
	}
	app := &App{
		gitStatus:            stub,
		dashboard:            dashboard.New(),
		activeWorkspace:      active,
		dirtyWorkspaces:      make(map[string]bool),
		creatingWorkspaceIDs: make(map[string]bool),
	}

	cmds := app.handleGitStatusTick()
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}
	if cmds[0] == nil {
		t.Fatal("expected status command")
	}
	msg := cmds[0]()
	result, ok := msg.(messages.GitStatusResult)
	if !ok {
		t.Fatalf("expected GitStatusResult, got %T", msg)
	}
	if result.Status == nil || !result.Status.HasLineStats {
		t.Fatalf("expected cached status with line stats")
	}
	if len(stub.refreshRoots) != 0 {
		t.Fatalf("expected no full refresh call when cached, got %d", len(stub.refreshRoots))
	}
	if len(stub.refreshFastRoots) != 0 {
		t.Fatalf("expected no fast refresh call when cached, got %d", len(stub.refreshFastRoots))
	}
}
