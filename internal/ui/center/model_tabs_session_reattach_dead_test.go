package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// TestReattachActiveTab_DeadSessionRefused pins the dead-session policy that
// center and sidebar now share (ptyio.SessionAttachable): a missing or dead
// session yields a Stopped failure so the user chooses restart explicitly —
// reattach must NOT kill the dead session and silently recreate (the pre-054
// drift), and must not spawn an agent.
func TestReattachActiveTab_DeadSessionRefused(t *testing.T) {
	restoreReattachSeams(t)
	killCalls := 0
	ptyio.KillSessionFn = func(string, tmux.Options) error {
		killCalls++
		return nil
	}
	createCalls := 0
	createAgentWithConfigFn = func(
		manager *appPty.AgentManager,
		ws *data.Workspace,
		agentType appPty.AgentType,
		sessionName string,
		rows, cols uint16,
		tags tmux.SessionTags,
		cfg config.AssistantConfig,
	) (*appPty.Agent, error) {
		createCalls++
		return &appPty.Agent{Session: sessionName}, nil
	}

	for _, state := range []tmux.SessionState{
		{Exists: false, HasLivePane: false}, // session gone
		{Exists: true, HasLivePane: false},  // dead pane
	} {
		sessionStateForFn = func(string, tmux.Options) (tmux.SessionState, error) {
			return state, nil
		}
		m := newTestModel()
		setKnownViewport(m)
		ws := newTestWorkspace("ws", "/repo/ws")
		wsID := string(ws.ID())
		tab := &Tab{
			ID:          TabID("tab-dead"),
			Assistant:   "claude",
			Workspace:   ws,
			SessionName: "amux-ws-dead",
			Detached:    true,
		}
		m.workspace = ws
		m.tabs.ByWorkspace[wsID] = []*Tab{tab}
		m.tabs.ActiveByWorkspace[wsID] = 0

		msg := m.ReattachActiveTab()()
		failed, ok := msg.(ptyTabReattachFailed)
		if !ok {
			t.Fatalf("state %+v: expected ptyTabReattachFailed, got %T", state, msg)
		}
		if !failed.Stopped {
			t.Fatalf("state %+v: expected Stopped failure, got %+v", state, failed)
		}
	}
	if killCalls != 0 {
		t.Fatalf("reattach must not kill the dead session (restart owns that), got %d kills", killCalls)
	}
	if createCalls != 0 {
		t.Fatalf("reattach must not recreate a session, got %d agent creates", createCalls)
	}
}

// TestReattachActiveTab_ForeignSessionRefused pins the shared-server
// squatting guard (ptyio.SessionOwned): a live session under our name that
// fails the ownership tag check yields a Stopped failure — attaching would
// hand a client to a foreign session and then launder it by re-tagging it as
// ours. The squatter dies on the user's explicit restart, which kills by
// name before recreating.
func TestReattachActiveTab_ForeignSessionRefused(t *testing.T) {
	restoreReattachSeams(t)
	createCalls := 0
	createAgentWithConfigFn = func(
		manager *appPty.AgentManager,
		ws *data.Workspace,
		agentType appPty.AgentType,
		sessionName string,
		rows, cols uint16,
		tags tmux.SessionTags,
		cfg config.AssistantConfig,
	) (*appPty.Agent, error) {
		createCalls++
		return &appPty.Agent{Session: sessionName}, nil
	}
	// Live session under the name, but the ownership check fails — the shape
	// of a same-server squatter (and of a plain name collision).
	sessionStateForFn = func(string, tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	sessionOwnedFn = func(string, []string, tmux.Options) (bool, error) {
		return false, nil
	}

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	tab := &Tab{
		ID:          TabID("tab-squat"),
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "amux-ws-squat",
		Detached:    true,
	}
	m.workspace = ws
	m.tabs.ByWorkspace[wsID] = []*Tab{tab}
	m.tabs.ActiveByWorkspace[wsID] = 0

	msg := m.ReattachActiveTab()()
	failed, ok := msg.(ptyTabReattachFailed)
	if !ok {
		t.Fatalf("expected ptyTabReattachFailed, got %T", msg)
	}
	if !failed.Stopped {
		t.Fatalf("foreign session must report Stopped so restart is the explicit remedy, got %+v", failed)
	}
	if createCalls != 0 {
		t.Fatalf("reattach must not attach/recreate on a foreign session, got %d agent creates", createCalls)
	}
}

// TestReattachToSession_DeadSessionRefused is the restore-path twin of the
// reattach refusal test — the pipeline merge (plan 108) exists because this
// path had already drifted to an inlined !Exists || !HasLivePane check. Both
// entry points must yield the same Stopped refusal.
func TestReattachToSession_DeadSessionRefused(t *testing.T) {
	restoreReattachSeams(t)
	createCalls := 0
	createAgentWithConfigFn = func(
		manager *appPty.AgentManager,
		ws *data.Workspace,
		agentType appPty.AgentType,
		sessionName string,
		rows, cols uint16,
		tags tmux.SessionTags,
		cfg config.AssistantConfig,
	) (*appPty.Agent, error) {
		createCalls++
		return &appPty.Agent{Session: sessionName}, nil
	}
	sessionStateForFn = func(string, tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: false}, nil
	}

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")

	msg := m.reattachToSession(ws, TabID("tab-dead"), "claude", "amux-ws-dead", 1)()
	failed, ok := msg.(ptyTabReattachFailed)
	if !ok {
		t.Fatalf("expected ptyTabReattachFailed, got %T", msg)
	}
	if !failed.Stopped {
		t.Fatalf("restore-path attach to a dead pane must report Stopped, got %+v", failed)
	}
	if createCalls != 0 {
		t.Fatalf("restore-path attach must not spawn an agent on a dead session, got %d", createCalls)
	}
}

// TestReattachToSession_ForeignSessionRefused is the restore-path twin of the
// squatting guard — stored metadata can name a session this workspace never
// created, and ownership must still be verified before attaching.
func TestReattachToSession_ForeignSessionRefused(t *testing.T) {
	restoreReattachSeams(t)
	createCalls := 0
	createAgentWithConfigFn = func(
		manager *appPty.AgentManager,
		ws *data.Workspace,
		agentType appPty.AgentType,
		sessionName string,
		rows, cols uint16,
		tags tmux.SessionTags,
		cfg config.AssistantConfig,
	) (*appPty.Agent, error) {
		createCalls++
		return &appPty.Agent{Session: sessionName}, nil
	}
	sessionStateForFn = func(string, tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	sessionOwnedFn = func(string, []string, tmux.Options) (bool, error) {
		return false, nil
	}

	m := newTestModel()
	setKnownViewport(m)
	ws := newTestWorkspace("ws", "/repo/ws")

	msg := m.reattachToSession(ws, TabID("tab-squat"), "claude", "amux-ws-squat", 1)()
	failed, ok := msg.(ptyTabReattachFailed)
	if !ok {
		t.Fatalf("expected ptyTabReattachFailed, got %T", msg)
	}
	if !failed.Stopped {
		t.Fatalf("foreign session must report Stopped on the restore path too, got %+v", failed)
	}
	if createCalls != 0 {
		t.Fatalf("restore-path attach must not attach to a foreign session, got %d agent creates", createCalls)
	}
}
