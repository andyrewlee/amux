package app

import (
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/sandbox"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestTmuxSessionNameUsesSandboxNamespace(t *testing.T) {
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")

	gotDefault := tmuxSessionName(ws, pty.AgentCodex, "")
	if !strings.HasPrefix(gotDefault, "amux-sandbox-") {
		t.Fatalf("tmuxSessionName() = %q, want sandbox namespace prefix", gotDefault)
	}

	gotLegacy := tmuxSessionName(ws, pty.AgentCodex, "amux-legacy-session")
	if gotLegacy != "amux-legacy-session" {
		t.Fatalf("tmuxSessionName() with legacy session = %q, want %q", gotLegacy, "amux-legacy-session")
	}

	gotExplicit := tmuxSessionName(ws, pty.AgentCodex, "reattach")
	if gotExplicit != "reattach" {
		t.Fatalf("tmuxSessionName() with explicit session = %q, want %q", gotExplicit, "reattach")
	}
}

func TestNewTmuxSessionTerminalCleanupDoesNotRunTwiceOnClose(t *testing.T) {
	manager := NewSandboxManager(nil)
	manager.launchPollInterval = 10 * time.Millisecond
	var live atomic.Bool
	live.Store(true)
	cleanupDone := make(chan struct{}, 1)
	manager.sessionStateFor = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		connected := live.Load()
		return tmux.SessionState{Exists: connected, HasLivePane: connected}, nil
	}
	cleanupCalls := 0

	term, err := manager.newTmuxSessionTerminal("sleep 30", t.TempDir(), nil, "amux-sandbox-test-close", 0, 0, func() {
		cleanupCalls++
		select {
		case cleanupDone <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("newTmuxSessionTerminal() error = %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := term.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if cleanupCalls != 0 {
		t.Fatalf("cleanup calls = %d, want 0 while tmux session is still live", cleanupCalls)
	}

	live.Store(false)
	select {
	case <-cleanupDone:
	case <-time.After(2 * time.Second):
		t.Fatal("expected cleanup once tmux session exits")
	}
	if cleanupCalls != 1 {
		t.Fatalf("cleanup calls = %d, want 1 after tmux session exit", cleanupCalls)
	}
}

func TestNewTmuxSessionTerminalRunsCleanupOnLaunchFailure(t *testing.T) {
	manager := NewSandboxManager(nil)
	cleanupCalls := 0

	_, err := manager.newTmuxSessionTerminal("echo hi", filepath.Join(t.TempDir(), "missing"), nil, "", 0, 0, func() {
		cleanupCalls++
	})
	if err == nil {
		t.Fatal("newTmuxSessionTerminal() error = nil, want launch failure")
	}
	if cleanupCalls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", cleanupCalls)
	}
}

func TestNewTmuxSessionTerminalRunsCleanupAfterSuccessfulLaunch(t *testing.T) {
	manager := NewSandboxManager(nil)
	manager.launchPollInterval = 10 * time.Millisecond
	var live atomic.Bool
	live.Store(true)
	manager.sessionStateFor = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		connected := live.Load()
		return tmux.SessionState{Exists: connected, HasLivePane: connected}, nil
	}

	cleanupDone := make(chan struct{}, 1)
	term, err := manager.newTmuxSessionTerminal("sleep 30", t.TempDir(), nil, "amux-sandbox-test", 0, 0, func() {
		select {
		case cleanupDone <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("newTmuxSessionTerminal() error = %v", err)
	}
	defer func() { _ = term.Close() }()

	time.Sleep(50 * time.Millisecond)
	select {
	case <-cleanupDone:
		t.Fatal("cleanup should not run while tmux session is still live")
	default:
	}

	live.Store(false)
	select {
	case <-cleanupDone:
	case <-time.After(2 * time.Second):
		t.Fatal("expected cleanup to run after tmux session exit")
	}
}

func TestNewTmuxSessionTerminalDoesNotRunCleanupOnStartupTimeout(t *testing.T) {
	manager := NewSandboxManager(nil)
	manager.launchPollInterval = 10 * time.Millisecond
	manager.launchWatchTimeout = 30 * time.Millisecond
	manager.sessionStateFor = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{}, nil
	}

	cleanupCalls := 0
	term, err := manager.newTmuxSessionTerminal("sleep 1", t.TempDir(), nil, "amux-sandbox-slow-start", 0, 0, func() {
		cleanupCalls++
	})
	if err != nil {
		t.Fatalf("newTmuxSessionTerminal() error = %v", err)
	}
	defer func() { _ = term.Close() }()

	time.Sleep(150 * time.Millisecond)
	if cleanupCalls != 0 {
		t.Fatalf("cleanup calls = %d, want 0 after startup timeout without a live tmux pane", cleanupCalls)
	}
}

func TestNewTmuxSessionTerminalKeepsCleanupArmedAfterSlowStartup(t *testing.T) {
	manager := NewSandboxManager(nil)
	manager.launchPollInterval = 10 * time.Millisecond
	manager.launchWatchTimeout = 30 * time.Millisecond

	started := time.Now()
	var live atomic.Bool
	manager.sessionStateFor = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		if time.Since(started) < 80*time.Millisecond {
			return tmux.SessionState{}, nil
		}
		connected := live.Load()
		return tmux.SessionState{Exists: connected, HasLivePane: connected}, nil
	}

	cleanupDone := make(chan struct{}, 1)
	term, err := manager.newTmuxSessionTerminal("sleep 30", t.TempDir(), nil, "amux-sandbox-slow-start-eventual-connect", 0, 0, func() {
		select {
		case cleanupDone <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatalf("newTmuxSessionTerminal() error = %v", err)
	}
	defer func() { _ = term.Close() }()

	time.Sleep(120 * time.Millisecond)
	select {
	case <-cleanupDone:
		t.Fatal("cleanup should stay armed while waiting for a slow tmux startup")
	default:
	}

	live.Store(true)
	time.Sleep(150 * time.Millisecond)
	select {
	case <-cleanupDone:
		t.Fatal("cleanup should not run while the tmux session is live")
	default:
	}

	live.Store(false)
	select {
	case <-cleanupDone:
	case <-time.After(2 * time.Second):
		t.Fatal("expected cleanup after slow tmux startup eventually exits")
	}
}

func TestCleanupTmuxLaunchTokenBacksOffAfterConnection(t *testing.T) {
	manager := NewSandboxManager(nil)
	manager.launchPollInterval = 10 * time.Millisecond
	manager.launchWatchTimeout = 30 * time.Millisecond

	started := time.Now()
	var calls atomic.Int32
	manager.sessionStateFor = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		calls.Add(1)
		if time.Since(started) < 250*time.Millisecond {
			return tmux.SessionState{Exists: true, HasLivePane: true}, nil
		}
		return tmux.SessionState{}, nil
	}

	cleanupDone := make(chan struct{})
	go manager.cleanupTmuxLaunchToken("amux-sandbox-backoff", func() {
		close(cleanupDone)
	}, make(chan struct{}))

	select {
	case <-cleanupDone:
	case <-time.After(2 * time.Second):
		t.Fatal("expected cleanup when the tmux session exits")
	}

	if got := calls.Load(); got > 8 {
		t.Fatalf("sessionStateFor() calls = %d, want <= 8 after steady-state backoff", got)
	}
}

func TestNewExistingTmuxAttachTerminalKillsDeadSession(t *testing.T) {
	manager := NewSandboxManager(nil)
	manager.sessionStateFor = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: false}, nil
	}
	killed := ""
	manager.killTmuxSession = func(sessionName string, opts tmux.Options) error {
		killed = sessionName
		return nil
	}

	term, ok, err := manager.newExistingTmuxAttachTerminal("amux-sandbox-dead", 0, 0, tmux.SessionTags{})
	if err != nil {
		t.Fatalf("newExistingTmuxAttachTerminal() error = %v", err)
	}
	if ok {
		t.Fatal("expected dead tmux session to be recreated, not attached")
	}
	if term != nil {
		t.Fatalf("expected nil terminal for dead tmux session, got %#v", term)
	}
	if killed != "amux-sandbox-dead" {
		t.Fatalf("killTmuxSession() session = %q, want %q", killed, "amux-sandbox-dead")
	}
}

func TestNewExistingTmuxAttachTerminalRefreshesTagsOnReattach(t *testing.T) {
	manager := NewSandboxManager(nil)
	manager.sessionStateFor = func(sessionName string, opts tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	var gotSession string
	var gotTags []tmux.OptionValue
	manager.setSessionTagValues = func(sessionName string, tags []tmux.OptionValue, opts tmux.Options) error {
		gotSession = sessionName
		gotTags = append([]tmux.OptionValue(nil), tags...)
		return nil
	}
	manager.attachTmuxTerminal = func(sessionName string, rows, cols uint16, opts tmux.Options) (*pty.Terminal, error) {
		return nil, nil
	}

	tags := tmux.SessionTags{
		WorkspaceID:  "ws-1",
		TabID:        "tab-1",
		Type:         "agent",
		Runtime:      string(data.RuntimeCloudSandbox),
		Assistant:    "codex",
		InstanceID:   "instance-a",
		SessionOwner: "instance-a",
		LeaseAtMS:    1234,
	}
	term, ok, err := manager.newExistingTmuxAttachTerminal("amux-sandbox-live", 0, 0, tags)
	if err != nil {
		t.Fatalf("newExistingTmuxAttachTerminal() error = %v", err)
	}
	if !ok {
		t.Fatal("expected existing live tmux session to attach")
	}
	if term != nil {
		t.Fatalf("expected nil terminal from stub attach, got %#v", term)
	}
	if gotSession != "amux-sandbox-live" {
		t.Fatalf("setSessionTagValues() session = %q, want %q", gotSession, "amux-sandbox-live")
	}
	if len(gotTags) == 0 {
		t.Fatal("expected reattach to refresh tmux tags")
	}
}

func TestCreateShellStartupFailureReleasesShellTracking(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	manager := NewSandboxManager(nil)
	root := t.TempDir()
	ws := data.NewWorkspace("ws", "main", "main", root, root)
	if err := sandbox.SaveSandboxMeta(ws.Root, "fake", sandbox.SandboxMeta{
		SandboxID:  "sb-shell-start",
		Agent:      sandbox.AgentShell,
		Provider:   "fake",
		WorktreeID: sandbox.ComputeWorktreeID(ws.Root),
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}
	session := &sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-shell-start"},
		providerName:  "fake",
		worktreeID:    sandbox.ComputeWorktreeID(ws.Root),
		workspaceID:   ws.ID(),
		workspaceRepo: ws.Repo,
		workspaceRoot: ws.Root,
		workspacePath: "/remote/ws",
	}
	manager.storeSession(session)
	manager.buildSSHCommand = func(sb sandbox.RemoteSandbox, remoteCommand string) (*exec.Cmd, func(), error) {
		return exec.Command("command-that-does-not-exist-for-amux-test"), nil, nil
	}

	term, err := manager.CreateShell(ws)
	if err == nil {
		if term != nil {
			_ = term.Close()
		}
		t.Fatal("CreateShell() error = nil, want startup failure")
	}
	if session.activeShells != 0 {
		t.Fatalf("activeShells = %d, want 0 after shell startup failure", session.activeShells)
	}
	meta, err := sandbox.LoadSandboxMeta(ws.Root, "fake")
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if meta == nil {
		t.Fatal("expected sandbox metadata to remain present")
	}
	if meta.NeedsSyncDown != nil && *meta.NeedsSyncDown {
		t.Fatal("expected failed shell startup to leave NeedsSyncDown false")
	}
}

func TestCreateShellNaturalExitReleasesShellTracking(t *testing.T) {
	manager := NewSandboxManager(nil)
	ws := data.NewWorkspace("ws", "main", "main", t.TempDir(), t.TempDir())
	session := &sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-shell-exit"},
		worktreeID:    sandbox.ComputeWorktreeID(ws.Root),
		workspaceID:   ws.ID(),
		workspaceRoot: ws.Root,
		workspacePath: "/remote/ws",
	}
	manager.storeSession(session)
	manager.buildSSHCommand = func(sb sandbox.RemoteSandbox, remoteCommand string) (*exec.Cmd, func(), error) {
		return exec.Command("sh", "-c", "exit 0"), nil, nil
	}

	term, err := manager.CreateShell(ws)
	if err != nil {
		t.Fatalf("CreateShell() error = %v", err)
	}
	defer func() { _ = term.Close() }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if session.activeShells == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("activeShells = %d, want 0 after natural shell exit", session.activeShells)
}

func TestCreateShellNaturalExitNotifiesAfterDetachCleanup(t *testing.T) {
	manager := NewSandboxManager(nil)
	ws := data.NewWorkspace("ws", "main", "main", t.TempDir(), t.TempDir())
	session := &sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-shell-exit-notify"},
		worktreeID:    sandbox.ComputeWorktreeID(ws.Root),
		workspaceID:   ws.ID(),
		workspaceRoot: ws.Root,
		workspacePath: "/remote/ws",
	}
	manager.storeSession(session)
	detached := make(chan string, 1)
	manager.SetShellDetachedCallback(func(workspaceID string) {
		if session.activeShells != 0 {
			t.Fatalf("activeShells = %d, want 0 before shell-detached callback", session.activeShells)
		}
		detached <- workspaceID
	})
	manager.buildSSHCommand = func(sb sandbox.RemoteSandbox, remoteCommand string) (*exec.Cmd, func(), error) {
		return exec.Command("sh", "-c", "exit 0"), nil, nil
	}

	term, err := manager.CreateShell(ws)
	if err != nil {
		t.Fatalf("CreateShell() error = %v", err)
	}
	defer func() { _ = term.Close() }()

	select {
	case got := <-detached:
		if got != string(ws.ID()) {
			t.Fatalf("shell detached workspace ID = %q, want %q", got, ws.ID())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected shell-detached callback after natural exit")
	}
}
