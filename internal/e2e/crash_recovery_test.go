package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/tmux"
)

// An unclean exit (SIGKILL, not the quit dialog) is the recovery path that
// clean-quit coverage can't reach: tmux sessions must outlive the app and
// reattach on restart rather than being respawned or orphaned.
func TestUncleanExitReattachesTmuxSessions(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	binDir := writeStubAssistant(t, home, "claude")
	server := fmt.Sprintf("amux-e2e-crash-%d", time.Now().UnixNano())
	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null"}
	defer killTmuxServer(t, server)

	env := sessionEnv(binDir, server)
	first, firstCleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  env,
	})
	if err != nil {
		t.Fatalf("start first session: %v", err)
	}
	defer firstCleanup()

	waitForUIContains(t, first, filepath.Base(repo), persistenceTimeout)
	activatePrimaryWorkspace(t, first)
	waitForUIContains(t, first, "[New agent]", persistenceTimeout)
	createAgentTab(t, first)
	waitForUIContains(t, first, "claude", persistenceTimeout)
	sessionsBefore := waitForAgentSessions(t, opts, persistenceTimeout)
	if len(sessionsBefore) == 0 {
		t.Fatal("no agent sessions before kill — fixture is broken")
	}

	if err := first.Kill(); err != nil {
		t.Fatalf("kill first session: %v", err)
	}
	if err := first.WaitForTermination(persistenceTimeout); err != nil {
		t.Fatalf("waiting for killed process: %v", err)
	}

	// The SIGKILL skipped the quit dialog entirely: every tmux session must
	// still be alive, unchanged.
	sessionsAlive := waitForAgentSessions(t, opts, persistenceTimeout)
	slices.Sort(sessionsBefore)
	slices.Sort(sessionsAlive)
	if !slices.Equal(sessionsBefore, sessionsAlive) {
		t.Fatalf("agent sessions changed across kill: before=%v after=%v", sessionsBefore, sessionsAlive)
	}

	second, secondCleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  env,
	})
	if err != nil {
		t.Fatalf("start second session: %v", err)
	}
	defer secondCleanup()

	waitForUIContains(t, second, filepath.Base(repo), persistenceTimeout)
	activatePrimaryWorkspace(t, second)
	// Reattach shows the live session's terminal, so the stub's tab label
	// returns without a fresh session being minted.
	waitForUIContains(t, second, "claude", persistenceTimeout)
	sessionsAfter := waitForAgentSessions(t, opts, persistenceTimeout)
	slices.Sort(sessionsAfter)
	if !slices.Equal(sessionsBefore, sessionsAfter) {
		t.Fatalf("reattach spawned or lost sessions: before=%v after=%v", sessionsBefore, sessionsAfter)
	}

	quitApp(t, second)
	if err := second.WaitForExit(persistenceTimeout); err != nil {
		t.Fatalf("waiting for clean exit after reattach: %v", err)
	}
}

// A delete interrupted between tombstone-write and metadata removal is the
// startup-recovery contract: the next boot finishes the delete rather than
// surfacing a ghost workspace whose worktree is half-removed. The tombstone is
// seeded directly — racing a Kill into the middle of a real delete is the
// timing flake this test exists to avoid.
func TestInterruptedDeleteTombstoneRecovery(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	writeConfig(t, home, false)
	binDir := writeStubAssistant(t, home, "claude")
	server := fmt.Sprintf("amux-e2e-tombstone-%d", time.Now().UnixNano())
	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null"}
	defer killTmuxServer(t, server)

	env := sessionEnv(binDir, server)
	session, cleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  env,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	waitForUIContains(t, session, filepath.Base(repo), workspaceAgentTimeout)
	createWorkspaceWithAgent(t, session, "doomed", opts, workspaceAgentTimeout)
	quitApp(t, session)
	if err := session.WaitForExit(persistenceTimeout); err != nil {
		t.Fatalf("waiting for exit: %v", err)
	}
	cleanup()

	// Leave the workspace record tombstoned as if the process died mid-delete.
	// The store root holds one dir per workspace plus lockfiles; the created
	// workspace's dir is the one whose record names it.
	metadataDir := filepath.Join(home, ".amux", "workspaces-metadata")
	entries, err := filepath.Glob(filepath.Join(metadataDir, "*"))
	if err != nil {
		t.Fatalf("glob metadata dirs: %v", err)
	}
	wsMetaDir := ""
	wsRoot := ""
	for _, entry := range entries {
		record, readErr := os.ReadFile(filepath.Join(entry, "workspace.json"))
		if readErr != nil {
			continue
		}
		var fields struct {
			Name string `json:"name"`
			Root string `json:"root"`
		}
		if json.Unmarshal(record, &fields) == nil && fields.Name == "doomed" {
			wsMetaDir, wsRoot = entry, fields.Root
			break
		}
	}
	if wsMetaDir == "" || wsRoot == "" {
		t.Fatalf("no metadata dir/root for workspace 'doomed' under %s (entries: %v)", metadataDir, entries)
	}
	// An interrupted delete looks like: tombstone written, worktree removed,
	// metadata removal never reached. A live worktree + tombstone is a
	// different contract entirely (failed-early delete keeps the workspace
	// usable), so the worktree must go — via git, because plain rm leaves a
	// stale .git/worktrees admin entry that makes the recovery branch-delete
	// fail with "checked out".
	runGit(t, repo, "worktree", "remove", "--force", wsRoot)
	if err := os.WriteFile(filepath.Join(wsMetaDir, ".deleting"), []byte("1"), 0o644); err != nil {
		t.Fatalf("seed delete tombstone: %v", err)
	}

	restart, restartCleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  env,
	})
	if err != nil {
		t.Fatalf("restart session: %v", err)
	}
	defer restartCleanup()

	// The dashboard loads — the tombstoned row must never surface, and startup
	// recovery must remove the metadata dir itself.
	waitForUIContains(t, restart, filepath.Base(repo), workspaceAgentTimeout)
	waitForUIConsistentlyAbsent(t, restart, "doomed", 2*time.Second, screenPollInterval)
	testutil.Eventually(t, workspaceAgentTimeout, screenPollInterval, func() bool {
		_, statErr := os.Stat(wsMetaDir)
		return os.IsNotExist(statErr)
	}, "tombstoned metadata dir %s survived startup recovery", wsMetaDir)
}
