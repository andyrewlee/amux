package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/sandbox"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestSandboxManagerSyncToLocalDiscoversLegacySandboxSessionNames(t *testing.T) {
	skipIfNoGit(t)

	manager := NewSandboxManager(nil)
	repo := initRepo(t)
	ws := data.NewWorkspace("ws", "main", "main", repo, repo)
	sessionName := "amux-legacy-session"
	session := &sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-legacy-live"},
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
		return []tmux.SessionTagValues{{Name: sessionName, Tags: map[string]string{"@amux_workspace": string(ws.ID()), "@amux_runtime": string(data.RuntimeCloudSandbox)}}}, nil
	}
	manager.sessionStateFor = func(got string, opts tmux.Options) (tmux.SessionState, error) {
		if got != sessionName {
			t.Fatalf("sessionStateFor() session = %q, want %q", got, sessionName)
		}
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}

	err := manager.SyncToLocal(ws)
	if !errors.Is(err, errSandboxSyncLive) {
		t.Fatalf("SyncToLocal() error = %v, want errSandboxSyncLive", err)
	}
	if _, ok := session.tmuxSessionNames[sessionName]; !ok {
		t.Fatalf("expected legacy tmux session %q to be tracked", sessionName)
	}
}

func TestSandboxManagerSyncToLocalIgnoresLocalTmuxSessions(t *testing.T) {
	skipIfNoGit(t)

	manager := NewSandboxManager(nil)
	repo := initRepo(t)
	ws := data.NewWorkspace("ws", "main", "main", repo, repo)
	session := &sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-local-ignored"},
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
		return []tmux.SessionTagValues{{Name: "amux-local-main-codex", Tags: map[string]string{"@amux_runtime": ""}}}, nil
	}

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
}

func TestSandboxManagerSyncToLocalDiscoversLegacySandboxPrefixWithoutRuntimeTag(t *testing.T) {
	skipIfNoGit(t)

	manager := NewSandboxManager(nil)
	repo := initRepo(t)
	ws := data.NewWorkspace("ws", "main", "main", repo, repo)
	sessionName := tmux.SessionName(sandboxTmuxNamespace, string(ws.ID()), "tab-2")
	session := &sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-prefix-live"},
		worktreeID:    sandbox.ComputeWorktreeID(ws.Root),
		workspaceID:   ws.ID(),
		workspaceRoot: ws.Root,
		workspacePath: "/remote/ws",
		needsSyncDown: true,
	}
	manager.storeSession(session)
	manager.sessionsWithTags = func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
		return []tmux.SessionTagValues{{Name: sessionName, Tags: map[string]string{"@amux_runtime": ""}}}, nil
	}
	manager.sessionStateFor = func(got string, opts tmux.Options) (tmux.SessionState, error) {
		if got != sessionName {
			t.Fatalf("sessionStateFor() session = %q, want %q", got, sessionName)
		}
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}

	err := manager.SyncToLocal(ws)
	if !errors.Is(err, errSandboxSyncLive) {
		t.Fatalf("SyncToLocal() error = %v, want errSandboxSyncLive", err)
	}
	if _, ok := session.tmuxSessionNames[sessionName]; !ok {
		t.Fatalf("expected sandbox-prefix tmux session %q to be tracked", sessionName)
	}
}

func TestSandboxManagerSyncToLocalIgnoresWorkspaceScopedAgentWithoutSandboxPrefixWhenRuntimeMissing(t *testing.T) {
	skipIfNoGit(t)

	manager := NewSandboxManager(nil)
	repo := initRepo(t)
	ws := data.NewWorkspace("ws", "main", "main", repo, repo)
	sessionName := "amux-" + string(ws.ID()) + "-tab-1"
	session := &sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-local-agent-live"},
		worktreeID:    sandbox.ComputeWorktreeID(ws.Root),
		workspaceID:   ws.ID(),
		workspaceRoot: ws.Root,
		workspacePath: "/remote/ws",
		needsSyncDown: true,
	}
	manager.storeSession(session)
	manager.sessionsWithTags = func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
		return []tmux.SessionTagValues{{
			Name: sessionName,
			Tags: map[string]string{
				"@amux_workspace": string(ws.ID()),
				"@amux_runtime":   "",
				"@amux_type":      "agent",
			},
		}}, nil
	}
	calls := 0
	manager.downloadWorkspace = func(computer sandbox.RemoteSandbox, opts sandbox.SyncOptions, verbose bool) error {
		calls++
		return nil
	}

	if err := manager.SyncToLocal(ws); err != nil {
		t.Fatalf("SyncToLocal() error = %v", err)
	}
	if _, ok := session.tmuxSessionNames[sessionName]; ok {
		t.Fatalf("did not expect local workspace-scoped tmux session %q to be tracked", sessionName)
	}
	if calls != 1 {
		t.Fatalf("downloadWorkspace() calls = %d, want 1", calls)
	}
}

func TestSandboxManagerSyncToLocalIgnoresLocalTmuxSessionsMatchingWorkspaceID(t *testing.T) {
	skipIfNoGit(t)

	manager := NewSandboxManager(nil)
	repo := initRepo(t)
	ws := data.NewWorkspace("ws", "main", "main", repo, repo)
	session := &sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-local-workspace-name"},
		worktreeID:    sandbox.ComputeWorktreeID(ws.Root),
		workspaceID:   ws.ID(),
		workspaceRoot: ws.Root,
		workspacePath: "/remote/ws",
		needsSyncDown: true,
	}
	manager.storeSession(session)
	manager.sessionsWithTags = func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
		return []tmux.SessionTagValues{{
			Name: "amux-" + string(ws.ID()) + "-tab-1",
			Tags: map[string]string{"@amux_runtime": ""},
		}}, nil
	}

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
}

func TestLoadPersistedSessionDiscoversSandboxTmuxWithWorkspaceIDAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AMUX_PROVIDER", "fake")

	repo := initRepo(t)
	ws := data.NewWorkspace("ws", "main", "main", repo, repo)
	const oldWorkspaceID = "ws-old-canonical"
	sessionName := "amux-sandbox-old-session"
	needsSync := true
	if err := sandbox.SaveSandboxMeta(ws.Root, "fake", sandbox.SandboxMeta{
		SandboxID:     "sb-alias",
		Agent:         sandbox.AgentShell,
		Provider:      "fake",
		WorktreeID:    sandbox.ComputeWorktreeID(ws.Root),
		NeedsSyncDown: &needsSync,
		WorkspaceIDs:  []string{oldWorkspaceID},
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	manager := NewSandboxManager(nil)
	manager.sessionsWithTags = func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
		if got := match["@amux"]; got != "1" {
			t.Fatalf("@amux match = %q, want %q", got, "1")
		}
		return []tmux.SessionTagValues{{Name: sessionName, Tags: map[string]string{"@amux_workspace": oldWorkspaceID, "@amux_runtime": string(data.RuntimeCloudSandbox)}}}, nil
	}

	session, err := manager.loadPersistedSession(ws)
	if err != nil {
		t.Fatalf("loadPersistedSession() error = %v", err)
	}
	if session == nil {
		t.Fatal("expected persisted session")
	}
	if _, ok := session.tmuxSessionNames[sessionName]; !ok {
		t.Fatalf("expected aliased tmux session %q to be rediscovered", sessionName)
	}
}

func TestLoadPersistedSessionTreatsLegacyMetadataAsNeedingSync(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AMUX_PROVIDER", "fake")

	repo := initRepo(t)
	ws := data.NewWorkspace("ws", "main", "main", repo, repo)
	if err := sandbox.SaveSandboxMeta(ws.Root, "fake", sandbox.SandboxMeta{
		SandboxID:  "sb-legacy",
		Agent:      sandbox.AgentShell,
		Provider:   "fake",
		WorktreeID: sandbox.ComputeWorktreeID(ws.Root),
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}

	manager := NewSandboxManager(nil)
	session, err := manager.loadPersistedSession(ws)
	if err != nil {
		t.Fatalf("loadPersistedSession() error = %v", err)
	}
	if session == nil {
		t.Fatal("expected persisted session")
	}
	if !session.needsSyncDown {
		t.Fatal("expected legacy persisted metadata without needsSyncDown to default to synced=true")
	}
}

func TestHydrateSessionFromMetaPreservesWorkspaceAliasesForRediscovery(t *testing.T) {
	skipIfNoGit(t)

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
	sessionName := "amux-sandbox-old-alias-session"
	needsSync := true
	session := manager.hydrateSessionFromMeta(nil, newWS, &sandbox.SandboxMeta{
		Provider:      "fake",
		NeedsSyncDown: &needsSync,
		WorkspaceIDs:  []string{string(oldWS.ID())},
	})
	manager.sessionsWithTags = func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
		if got := match["@amux"]; got != "1" {
			t.Fatalf("@amux match = %q, want %q", got, "1")
		}
		return []tmux.SessionTagValues{{
			Name: sessionName,
			Tags: map[string]string{"@amux_workspace": string(oldWS.ID()), "@amux_runtime": string(data.RuntimeCloudSandbox)},
		}}, nil
	}
	manager.sessionStateFor = func(got string, opts tmux.Options) (tmux.SessionState, error) {
		if got != sessionName {
			t.Fatalf("sessionStateFor() session = %q, want %q", got, sessionName)
		}
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}

	if err := manager.discoverTrackedTmuxSessions(session); err != nil {
		t.Fatalf("discoverTrackedTmuxSessions() error = %v", err)
	}

	ids := manager.sessionWorkspaceIDs(session)
	if !slices.Contains(ids, string(oldWS.ID())) || !slices.Contains(ids, string(newWS.ID())) {
		t.Fatalf("session workspace IDs = %v, want both %q and %q", ids, oldWS.ID(), newWS.ID())
	}
	if _, ok := session.tmuxSessionNames[sessionName]; !ok {
		t.Fatalf("expected aliased tmux session %q to be rediscovered", sessionName)
	}
}

func TestSandboxManagerStopTrackedTmuxSessionsOnlyKillsOwnedSessions(t *testing.T) {
	manager := NewSandboxManager(nil)
	manager.SetInstanceID("instance-a")
	session := &sandboxSession{
		sandbox:            &rollbackSandbox{id: "sb-owned"},
		worktreeID:         "wt-owned",
		workspaceID:        "ws-owned",
		workspaceIDAliases: map[string]struct{}{"ws-owned-old": {}},
		workspaceRoot:      "/repo/ws-owned",
		workspacePath:      "/remote/ws-owned",
	}
	manager.storeSession(session)

	manager.sessionsWithTags = func(match map[string]string, keys []string, opts tmux.Options) ([]tmux.SessionTagValues, error) {
		if got := match[tmux.TagSessionOwner]; got != "instance-a" {
			t.Fatalf("owner match = %q, want %q", got, "instance-a")
		}
		if got := match["@amux"]; got != "1" {
			t.Fatalf("@amux match = %q, want %q", got, "1")
		}
		return []tmux.SessionTagValues{{Name: "amux-sandbox-owned", Tags: map[string]string{"@amux_workspace": "ws-owned-old"}}}, nil
	}
	manager.sessionStateFor = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}

	killed := ""
	manager.killTmuxSession = func(sessionName string, opts tmux.Options) error {
		killed = sessionName
		return nil
	}

	if err := manager.StopTrackedTmuxSessions(); err != nil {
		t.Fatalf("StopTrackedTmuxSessions() error = %v", err)
	}
	if killed != "amux-sandbox-owned" {
		t.Fatalf("killTmuxSession() session = %q, want %q", killed, "amux-sandbox-owned")
	}
}
