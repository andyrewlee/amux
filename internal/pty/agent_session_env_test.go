package pty

import (
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

// TestAgentManager_SessionEnvLayers covers the provider seam itself: no
// provider yields no layers (the standalone/test manager path), provider
// layers pass through, and provider errors propagate to the caller instead
// of silently spawning a session without its reservation.
func TestAgentManager_SessionEnvLayers(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Root: t.TempDir()}

	m := NewAgentManager(testConfig())
	env, err := m.sessionEnvLayers(ws)
	if err != nil || env != nil {
		t.Fatalf("nil provider: env=%v err=%v, want nil/nil", env, err)
	}

	want := []string{"AMUX_PORT=6200", "MY_VAR=1"}
	m.SetSessionEnvProvider(func(got *data.Workspace) ([]string, error) {
		if got != ws {
			t.Errorf("provider got ws %p, want %p", got, ws)
		}
		return want, nil
	})
	env, err = m.sessionEnvLayers(ws)
	if err != nil {
		t.Fatalf("sessionEnvLayers() error = %v", err)
	}
	if !slices.Equal(env, want) {
		t.Fatalf("sessionEnvLayers() = %v, want %v", env, want)
	}

	sentinel := errors.New("port range exhausted")
	m.SetSessionEnvProvider(func(*data.Workspace) ([]string, error) {
		return nil, sentinel
	})
	if _, err := m.sessionEnvLayers(ws); !errors.Is(err, sentinel) {
		t.Fatalf("provider error = %v, want propagated %v", err, sentinel)
	}
}

// TestAgentManager_CreateAgentWithTags_SessionEnv proves the provider's
// layered env reaches the spawned tmux command env (AMUX_PORT + user vars),
// and that spawn-specific vars appended after the layers still win — a
// user-provided LINES must not break PTY sizing.
func TestAgentManager_CreateAgentWithTags_SessionEnv(t *testing.T) {
	if err := tmux.EnsureAvailable(); err != nil {
		t.Skipf("tmux unavailable: %v", err)
	}
	serverName := fmt.Sprintf("amux-ptytest-env-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", serverName, "kill-server").Run()
	})

	m := NewAgentManager(testConfig())
	m.SetTmuxOptions(tmux.Options{
		ServerName:     serverName,
		ConfigPath:     "/dev/null",
		CommandTimeout: 5 * time.Second,
	})
	m.SetSessionEnvProvider(func(*data.Workspace) ([]string, error) {
		return []string{
			"AMUX_PORT=6200",
			"AMUX_PORT_RANGE=6209",
			"SESSION_LAYER_VAR=present",
			"LINES=999", // user-layer collision: spawn-specific must win
		}, nil
	})

	ws := &data.Workspace{Name: "env-agent-ws", Root: t.TempDir(), Repo: "/tmp/test-repo"}
	sessionName := fmt.Sprintf("amux-test-env-%d", time.Now().UnixNano())
	agent, err := m.CreateAgentWithTags(ws, AgentType("claude"), sessionName, 24, 80, tmux.SessionTags{})
	if err != nil {
		t.Fatalf("CreateAgentWithTags failed: %v", err)
	}
	t.Cleanup(func() { _ = m.CloseAgent(agent) })

	env := agent.Terminal.cmd.Env
	for _, want := range []string{"AMUX_PORT=6200", "AMUX_PORT_RANGE=6209", "SESSION_LAYER_VAR=present"} {
		if !slices.Contains(env, want) {
			t.Errorf("terminal env missing provider layer %q", want)
		}
	}
	// Spawn specifics are appended last: the empty LINES (ioctl sizing) must
	// be the final LINES in the list, not the provider's 999.
	lastLines := ""
	for _, kv := range env {
		if len(kv) >= 6 && kv[:6] == "LINES=" {
			lastLines = kv
		}
	}
	if lastLines != "LINES=" {
		t.Errorf("final LINES = %q, want %q (spawn specifics win over layers)", lastLines, "LINES=")
	}
}
