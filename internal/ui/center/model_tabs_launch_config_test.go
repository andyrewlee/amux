package center

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

// launchConfigCapture records the AssistantConfig each dispatched launch
// command delivers to the spawn seam. Under the snapshot contract the seam
// argument is the ONLY config the async command may use.
type launchConfigCapture struct {
	mu   sync.Mutex
	cfgs []config.AssistantConfig
}

func (c *launchConfigCapture) install() {
	createAgentWithConfigFn = func(
		manager *appPty.AgentManager,
		ws *data.Workspace,
		agentType appPty.AgentType,
		sessionName string,
		rows, cols uint16,
		tags tmux.SessionTags,
		cfg config.AssistantConfig,
	) (*appPty.Agent, error) {
		c.mu.Lock()
		c.cfgs = append(c.cfgs, cfg)
		c.mu.Unlock()
		return &appPty.Agent{Session: sessionName, Config: cfg}, nil
	}
}

func (c *launchConfigCapture) at(i int) config.AssistantConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfgs[i]
}

func (c *launchConfigCapture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.cfgs)
}

// installLaunchSeams installs the full reattach/bootstrap seam suite plus the
// config-capturing create seam. Fresh-create closures also hit real tmux via
// HistoryCaptureSize/CapturePane — those calls fail softly (errors discarded)
// when the session doesn't exist, so no stub is needed for them.
func installLaunchSeams(t *testing.T, capture *launchConfigCapture) {
	t.Helper()
	var calls []string
	installHistoryFallbackSeams(t, &calls, eligibleReattachProbe())
	capture.install()
}

var (
	launchCfgV1 = config.AssistantConfig{Command: "claude --v1", InterruptCount: 1, InterruptDelayMs: 11}
	launchCfgV2 = config.AssistantConfig{Command: "claude --v2", InterruptCount: 3, InterruptDelayMs: 33}
)

func detachedLaunchTab(ws *data.Workspace, id string) *Tab {
	return &Tab{
		ID:          TabID(id),
		Assistant:   "claude",
		Workspace:   ws,
		SessionName: "session-" + id,
		Detached:    true,
	}
}

func isErrorMsg(msg tea.Msg) bool {
	_, ok := msg.(messages.Error)
	return ok
}

// TestLaunchConfigSnapshot_DispatchedCommandSeesCapturedValues pins the core
// contract for every async spawn path: the config value captured at dispatch
// time is the one the command uses, and a later dispatch sees later settings.
func TestLaunchConfigSnapshot_DispatchedCommandSeesCapturedValues(t *testing.T) {
	newModel := func() (*Model, *data.Workspace) {
		m := newTestModel()
		setKnownViewport(m)
		m.config.Assistants["claude"] = launchCfgV1
		ws := newTestWorkspace("ws", t.TempDir())
		m.workspace = ws
		return m, ws
	}

	cases := []struct {
		name string
		// setupTab installs the tab fixture the tab-scoped paths need.
		setupTab func(m *Model, ws *data.Workspace)
		// dispatch runs on the "UI goroutine" — it must capture the config.
		dispatch func(m *Model, ws *data.Workspace) tea.Cmd
	}{
		{
			name: "fresh create",
			dispatch: func(m *Model, ws *data.Workspace) tea.Cmd {
				return m.createAgentTab("claude", ws)
			},
		},
		{
			name: "placeholder restore",
			dispatch: func(m *Model, ws *data.Workspace) tea.Cmd {
				return m.reattachToSession(ws, TabID("tab-restore"), "claude", "session-restore", 1)
			},
		},
		{
			name: "manual reattach",
			setupTab: func(m *Model, ws *data.Workspace) {
				tab := detachedLaunchTab(ws, "tab-reattach")
				wsID := string(ws.ID())
				m.tabs.ByWorkspace[wsID] = []*Tab{tab}
				m.tabs.ActiveByWorkspace[wsID] = 0
			},
			dispatch: func(m *Model, ws *data.Workspace) tea.Cmd {
				return m.ReattachActiveTab()
			},
		},
		{
			name: "restart",
			setupTab: func(m *Model, ws *data.Workspace) {
				tab := detachedLaunchTab(ws, "tab-restart")
				tab.Detached = false
				wsID := string(ws.ID())
				m.tabs.ByWorkspace[wsID] = []*Tab{tab}
				m.tabs.ActiveByWorkspace[wsID] = 0
			},
			dispatch: func(m *Model, ws *data.Workspace) tea.Cmd {
				return m.RestartActiveTab()
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := &launchConfigCapture{}
			installLaunchSeams(t, capture)
			m, ws := newModel()
			if tc.setupTab != nil {
				tc.setupTab(m, ws)
			}

			cmd := tc.dispatch(m, ws)
			if cmd == nil {
				t.Fatal("dispatch returned nil command")
			}

			// Settings edits the shared map AFTER dispatch but BEFORE the
			// command runs — the retained command must still see v1.
			m.config.Assistants["claude"] = launchCfgV2
			msg := cmd()
			if isErrorMsg(msg) {
				t.Fatalf("first command failed: %+v", msg)
			}
			if capture.count() != 1 {
				t.Fatalf("expected 1 seam call, got %d", capture.count())
			}
			if got := capture.at(0); got != launchCfgV1 {
				t.Fatalf("dispatched command used cfg %+v, want dispatch-time snapshot %+v", got, launchCfgV1)
			}

			// The result handler would release the reattach guard when the
			// command's message lands; do the same so the second dispatch is
			// not rejected as already in flight.
			for _, tb := range m.tabs.ByWorkspace[string(ws.ID())] {
				tb.mu.Lock()
				tb.endReattachLocked()
				tb.mu.Unlock()
			}

			// A second dispatch after the edit sees the current settings.
			cmd2 := tc.dispatch(m, ws)
			if cmd2 == nil {
				t.Fatal("second dispatch returned nil command")
			}
			if msg2 := cmd2(); isErrorMsg(msg2) {
				t.Fatalf("second command failed: %+v", msg2)
			}
			if capture.count() != 2 {
				t.Fatalf("expected 2 seam calls, got %d", capture.count())
			}
			if got := capture.at(1); got != launchCfgV2 {
				t.Fatalf("second command used cfg %+v, want current settings %+v", got, launchCfgV2)
			}
		})
	}
}

// TestLaunchConfigSnapshot_UnknownAndLateAdded covers the two map-shape edges:
// an assistant absent at dispatch produces each path's existing failure mode,
// and an assistant added between dispatches is visible to the later command.
func TestLaunchConfigSnapshot_UnknownAndLateAdded(t *testing.T) {
	capture := &launchConfigCapture{}
	installLaunchSeams(t, capture)
	m := newTestModel()
	setKnownViewport(m)
	m.config.Assistants["claude"] = launchCfgV1
	ws := newTestWorkspace("ws", t.TempDir())
	m.workspace = ws

	// Unknown assistant — fresh create reports the error as a message.
	if msg := m.createAgentTab("ghost", ws)(); !isErrorMsg(msg) {
		t.Fatalf("expected error message for unknown assistant, got %T", msg)
	}

	// Unknown assistant — restore reports a reattach failure (and releases the
	// reattach guard through the normal failed-result handler).
	failed, ok := m.reattachToSession(ws, TabID("tab-ghost"), "ghost", "session-ghost", 1)().(ptyTabReattachFailed)
	if !ok {
		t.Fatal("expected ptyTabReattachFailed for unknown assistant")
	}
	if failed.Err == nil || !strings.Contains(failed.Err.Error(), "unknown agent type") {
		t.Fatalf("expected unknown-agent-type error, got %v", failed.Err)
	}

	// Custom assistant added between operations: a later dispatch sees it.
	custom := config.AssistantConfig{Command: "custom-cli --flag", InterruptCount: 5}
	m.config.Assistants["custom"] = custom
	if msg := m.createAgentTab("custom", ws)(); isErrorMsg(msg) {
		t.Fatalf("dispatch for late-added assistant failed: %+v", msg)
	}
	if capture.count() != 1 || capture.at(0) != custom {
		t.Fatalf("late-added assistant: n=%d, want cfg %+v", capture.count(), custom)
	}
}

// TestLaunchConfigSnapshot_NoSharedMapAccessUnderMutation is the race variant:
// a goroutine continuously mutates the UI-owned Assistants map while the
// retained command runs. Under -race any map read inside the command flags;
// deterministically, the captured value must remain the dispatch-time one.
func TestLaunchConfigSnapshot_NoSharedMapAccessUnderMutation(t *testing.T) {
	capture := &launchConfigCapture{}
	installLaunchSeams(t, capture)
	m := newTestModel()
	setKnownViewport(m)
	m.config.Assistants["claude"] = launchCfgV1
	ws := newTestWorkspace("ws", t.TempDir())
	m.workspace = ws

	cmd := m.createAgentTab("claude", ws)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				m.config.Assistants["claude"] = config.AssistantConfig{
					Command:          fmt.Sprintf("claude --mut-%d", i),
					InterruptCount:   i,
					InterruptDelayMs: i,
				}
			}
		}
	}()

	msg := cmd()
	close(stop)
	wg.Wait()

	if isErrorMsg(msg) {
		t.Fatalf("command failed: %+v", msg)
	}
	if capture.count() != 1 {
		t.Fatalf("expected 1 seam call, got %d", capture.count())
	}
	if got := capture.at(0); got != launchCfgV1 {
		t.Fatalf("command observed a mutated config %+v; dispatch-time snapshot was %+v", got, launchCfgV1)
	}
}
