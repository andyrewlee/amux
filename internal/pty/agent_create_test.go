package pty

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

// TestAgentManager_CreateAgentWithTags_RegistersAgent clones the
// TestAgentManager_CreateViewer_RegistersAgent harness (see agent_test.go)
// for the agent path: env assembly, the post-exit terminal-reset escape
// sequence, registration under ws.ID(), and the spawn itself, against a
// real, isolated tmux server. It skips when tmux is not installed so
// tmux-less local runs stay green; CI runs it in the tmux job. It lives in
// its own file to keep agent_test.go under the repo's 500-line file-length
// gate (make check-file-length).
func TestAgentManager_CreateAgentWithTags_RegistersAgent(t *testing.T) {
	if err := tmux.EnsureAvailable(); err != nil {
		t.Skipf("tmux unavailable: %v", err)
	}

	// Isolated tmux server so the test never touches the user's amux server.
	serverName := fmt.Sprintf("amux-ptytest-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", serverName, "kill-server").Run()
	})

	m := NewAgentManager(testConfig())
	m.SetTmuxOptions(tmux.Options{
		ServerName:     serverName,
		ConfigPath:     "/dev/null",
		CommandTimeout: 5 * time.Second,
	})

	ws := &data.Workspace{
		Name: "create-agent-ws",
		Root: t.TempDir(),
		Repo: "/tmp/test-repo",
	}
	sessionName := fmt.Sprintf("amux-test-agent-%d", time.Now().UnixNano())
	agentType := AgentType("claude") // testConfig(): Command "echo claude" — harmless, spawnable.

	agent, err := m.CreateAgentWithTags(ws, agentType, sessionName, 24, 80, tmux.SessionTags{})
	if err != nil {
		t.Fatalf("CreateAgentWithTags failed: %v", err)
	}
	t.Cleanup(func() { _ = m.CloseAgent(agent) })

	if agent == nil {
		t.Fatal("CreateAgentWithTags returned nil agent")
	}
	if agent.Type != agentType {
		t.Errorf("expected type %q, got %q", agentType, agent.Type)
	}
	if agent.Session != sessionName {
		t.Errorf("expected session %q, got %q", sessionName, agent.Session)
	}
	if agent.Workspace != ws {
		t.Error("agent workspace should be the workspace it was created for")
	}
	if agent.Terminal == nil {
		t.Fatal("agent terminal should be non-nil")
	}

	// Env assembly: the workspace vars and COLORTERM must reach the spawned command.
	env := agent.Terminal.cmd.Env
	for _, want := range []string{"WORKSPACE_ROOT=" + ws.Root, "WORKSPACE_NAME=" + ws.Name, "COLORTERM=truecolor"} {
		if !slices.Contains(env, want) {
			t.Errorf("terminal env missing %q", want)
		}
	}

	// Reset sequence: the post-exit escape sequence that exits alt-screen,
	// shows the cursor, resets attrs, and issues RIS (stty sane; printf
	// '\033[?1049l\033[?25h\033[0m\033c') must be embedded in the spawned
	// command. This is the regression guard: if a future change drops the
	// `?1049l` or otherwise mangles this sequence, the pane is left stuck in
	// alt-screen / hidden-cursor after every agent session.
	cmdStr := strings.Join(agent.Terminal.cmd.Args, " ")
	const resetSeq = "\\033[?1049l\\033[?25h\\033[0m\\033c"
	if !strings.Contains(cmdStr, resetSeq) {
		t.Errorf("spawned command missing terminal-reset sequence %q", resetSeq)
	}

	// Registration under ws.ID().
	wsID := ws.ID()
	m.mu.Lock()
	registered := len(m.agents[wsID]) == 1 && m.agents[wsID][0] == agent
	m.mu.Unlock()
	if !registered {
		t.Fatal("agent not registered under ws.ID()")
	}

	// CloseAgent removes it from the registry.
	if err := m.CloseAgent(agent); err != nil {
		t.Fatalf("CloseAgent failed: %v", err)
	}
	m.mu.Lock()
	remaining := len(m.agents[wsID])
	m.mu.Unlock()
	if remaining != 0 {
		t.Errorf("expected 0 agents after CloseAgent, got %d", remaining)
	}
}
