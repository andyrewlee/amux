package sidebar

import (
	"errors"
	"slices"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
)

// TestSidebarTerminalSessionEnv proves the wired session env provider's
// layers reach the spawned tmux client env on both spawn paths (fresh create
// and attach), and that provider errors surface as the path's typed failure
// message instead of silently spawning without the workspace env.
func TestSidebarTerminalSessionEnv(t *testing.T) {
	restoreAttachSeams(t)
	t.Setenv("SHELL", "/bin/sh")

	ensureTmuxAvailableFn = func() error { return nil }
	sessionStateForFn = func(string, tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{}, nil
	}
	var envs [][]string
	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		envs = append(envs, env)
		return &pty.Terminal{}, nil
	}
	verifyTerminalSessionTagsFn = func(string, tmux.SessionTags, tmux.Options) error { return nil }

	m := NewTerminalModel()
	m.SetSessionEnvProvider(func(*data.Workspace) ([]string, error) {
		return []string{"AMUX_PORT=6200", "SESSION_LAYER_VAR=present"}, nil
	})
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")

	if _, ok := m.createTerminalTab(ws)().(SidebarTerminalCreated); !ok {
		t.Fatal("createTerminalTab did not return SidebarTerminalCreated")
	}
	if _, ok := m.attachToSession(ws, TerminalTabID("t"), "session-1", true, "restart", 1)().(SidebarTerminalReattachResult); !ok {
		t.Fatal("attachToSession did not return SidebarTerminalReattachResult")
	}

	if len(envs) != 2 {
		t.Fatalf("tmux client launches = %d, want 2", len(envs))
	}
	for i, env := range envs {
		for _, want := range []string{"AMUX_PORT=6200", "SESSION_LAYER_VAR=present", "COLORTERM=truecolor"} {
			if !slices.Contains(env, want) {
				t.Errorf("launch %d env missing %q", i, want)
			}
		}
	}
}

func TestSidebarTerminalSessionEnvError(t *testing.T) {
	restoreAttachSeams(t)
	t.Setenv("SHELL", "/bin/sh")

	ensureTmuxAvailableFn = func() error { return nil }
	sessionStateForFn = func(string, tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{}, nil
	}
	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		t.Fatal("newPTYWithSizeFn called despite provider error")
		return nil, nil
	}

	sentinel := errors.New("port range exhausted")
	m := NewTerminalModel()
	m.SetSessionEnvProvider(func(*data.Workspace) ([]string, error) {
		return nil, sentinel
	})
	ws := data.NewWorkspace("ws", "main", "main", "/repo/ws", "/repo/ws")

	created, ok := m.createTerminalTab(ws)().(SidebarTerminalCreateFailed)
	if !ok || !errors.Is(created.Err, sentinel) {
		t.Fatalf("createTerminalTab = %v, want SidebarTerminalCreateFailed wrapping %v", created, sentinel)
	}
	reattach, ok := m.attachToSession(ws, TerminalTabID("t"), "session-1", true, "restart", 1)().(SidebarTerminalReattachFailed)
	if !ok || !errors.Is(reattach.Err, sentinel) {
		t.Fatalf("attachToSession = %v, want SidebarTerminalReattachFailed wrapping %v", reattach, sentinel)
	}
}
