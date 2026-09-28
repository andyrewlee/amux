package pty

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

// TestAgentManager_CreateAgentWithConfig_UsesSuppliedSnapshot verifies the
// explicit-config spawn path uses the passed AssistantConfig value — never
// m.config.Assistants — so a concurrent settings save can neither race nor
// retarget an in-flight launch. Owns its own isolated tmux server like
// agent_create_test.go.
func TestAgentManager_CreateAgentWithConfig_UsesSuppliedSnapshot(t *testing.T) {
	if err := tmux.EnsureAvailable(); err != nil {
		t.Skipf("tmux unavailable: %v", err)
	}

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
		Name: "create-config-ws",
		Root: t.TempDir(),
		Repo: "/tmp/test-repo",
	}
	// The snapshot deliberately differs from testConfig()'s "echo claude"
	// entry on every field — any observed map value means the spawn re-read
	// the shared map.
	snapshot := config.AssistantConfig{
		Command:          "echo snapshot-cfg",
		InterruptCount:   7,
		InterruptDelayMs: 900,
	}
	agent, err := m.CreateAgentWithConfig(ws, "claude", fmt.Sprintf("amux-test-cfg-%d", time.Now().UnixNano()), 24, 80, tmux.SessionTags{}, snapshot)
	if err != nil {
		t.Fatalf("CreateAgentWithConfig failed: %v", err)
	}
	t.Cleanup(func() { _ = m.CloseAgent(agent) })

	if agent.Config != snapshot {
		t.Errorf("agent.Config = %+v, want snapshot %+v", agent.Config, snapshot)
	}
	cmdStr := strings.Join(agent.Terminal.cmd.Args, " ")
	if !strings.Contains(cmdStr, snapshot.Command) {
		t.Errorf("spawned command missing snapshot command %q", snapshot.Command)
	}
	if strings.Contains(cmdStr, "echo claude") {
		t.Error("spawned command used the manager's map entry, not the snapshot")
	}
}

// TestAgentManager_CreateAgentWithConfig_UnknownNameStillSpawns proves the
// explicit-config path performs no map lookup at all: an assistant name that
// is absent from m.config.Assistants spawns fine when the caller supplies the
// config. Membership validation is the caller's job at snapshot time.
func TestAgentManager_CreateAgentWithConfig_UnknownNameStillSpawns(t *testing.T) {
	if err := tmux.EnsureAvailable(); err != nil {
		t.Skipf("tmux unavailable: %v", err)
	}

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
		Name: "create-config-unknown-ws",
		Root: t.TempDir(),
		Repo: "/tmp/test-repo",
	}
	snapshot := config.AssistantConfig{Command: "echo ghost", InterruptCount: 1}
	agent, err := m.CreateAgentWithConfig(ws, "ghost-not-in-map", fmt.Sprintf("amux-test-ghost-%d", time.Now().UnixNano()), 24, 80, tmux.SessionTags{}, snapshot)
	if err != nil {
		t.Fatalf("CreateAgentWithConfig for unmapped name failed: %v", err)
	}
	t.Cleanup(func() { _ = m.CloseAgent(agent) })
	if agent.Config != snapshot {
		t.Errorf("agent.Config = %+v, want %+v", agent.Config, snapshot)
	}
}

func TestAgentManager_CreateAgentWithConfig_NilWorkspace(t *testing.T) {
	m := NewAgentManager(testConfig())
	if _, err := m.CreateAgentWithConfig(nil, "claude", "s", 24, 80, tmux.SessionTags{}, config.AssistantConfig{Command: "true"}); err == nil {
		t.Fatal("expected error for nil workspace")
	}
}

// TestAgentManager_CreateAgentWithTags_StillResolvesMap keeps the synchronous
// convenience path pinned: it resolves m.config.Assistants and delegates with
// the looked-up value, so the unknown-type error contract is unchanged for
// existing synchronous callers.
func TestAgentManager_CreateAgentWithTags_StillResolvesMap(t *testing.T) {
	m := NewAgentManager(testConfig())
	ws := &data.Workspace{Name: "w", Root: t.TempDir(), Repo: "/r"}
	if _, err := m.CreateAgentWithTags(ws, "no-such-assistant", "s", 24, 80, tmux.SessionTags{}); err == nil || !strings.Contains(err.Error(), "unknown agent type") {
		t.Fatalf("expected unknown-agent-type error, got %v", err)
	}
}
