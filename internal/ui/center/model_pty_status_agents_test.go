package center

import (
	"sort"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// These tests cover the workspace-level agent-status helpers in
// model_pty_status.go: HasRunningAgents, HasActiveAgents,
// GetActiveWorkspaceRoots, GetRunningWorkspaceRoots.

func TestHasRunningAgents(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name  string
		build func(m *Model)
		want  bool
	}{
		{
			name:  "no workspaces",
			build: func(*Model) {},
			want:  false,
		},
		{
			name: "running chat tab",
			build: func(m *Model) {
				ws := newTestWorkspace("ws", "/repo/ws")
				m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{
					{Assistant: "claude", Workspace: ws, Running: true},
				}
			},
			want: true,
		},
		{
			name: "chat tab not running",
			build: func(m *Model) {
				ws := newTestWorkspace("ws", "/repo/ws")
				m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{
					{Assistant: "claude", Workspace: ws, Running: false},
				}
			},
			want: false,
		},
		{
			name: "running non-chat tab ignored",
			build: func(m *Model) {
				ws := newTestWorkspace("ws", "/repo/ws")
				m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{
					{Assistant: "vim", Workspace: ws, Running: true},
				}
			},
			want: false,
		},
		{
			name: "running but closed tab ignored",
			build: func(m *Model) {
				ws := newTestWorkspace("ws", "/repo/ws")
				closed := &Tab{Assistant: "claude", Workspace: ws, Running: true}
				closed.markClosed()
				m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{closed}
			},
			want: false,
		},
		{
			name: "second tab running across workspaces",
			build: func(m *Model) {
				ws1 := newTestWorkspace("ws1", "/repo/ws1")
				ws2 := newTestWorkspace("ws2", "/repo/ws2")
				m.tabs.ByWorkspace[string(ws1.ID())] = []*Tab{
					{Assistant: "claude", Workspace: ws1, Running: false},
				}
				m.tabs.ByWorkspace[string(ws2.ID())] = []*Tab{
					{Assistant: "vim", Workspace: ws2, Running: true},
					{Assistant: "codex", Workspace: ws2, Running: true},
				}
			},
			want: true,
		},
		{
			name: "active output does not imply running",
			build: func(m *Model) {
				ws := newTestWorkspace("ws", "/repo/ws")
				m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{
					{
						Assistant: "claude",
						Workspace: ws,
						Running:   false,
						tabActivityState: tabActivityState{
							lastVisibleOutput: now.Add(-1 * time.Second),
						},
					},
				}
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			tt.build(m)
			if got := m.HasRunningAgents(); got != tt.want {
				t.Fatalf("HasRunningAgents() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasActiveAgents(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name  string
		build func(m *Model)
		want  bool
	}{
		{
			name:  "no workspaces",
			build: func(*Model) {},
			want:  false,
		},
		{
			name: "chat tab with recent visible output",
			build: func(m *Model) {
				ws := newTestWorkspace("ws", "/repo/ws")
				m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{
					{
						Assistant: "claude",
						Workspace: ws,
						Running:   true,
						tabActivityState: tabActivityState{
							lastVisibleOutput: now.Add(-1 * time.Second),
						},
					},
				}
			},
			want: true,
		},
		{
			name: "running chat tab without recent output is inactive",
			build: func(m *Model) {
				ws := newTestWorkspace("ws", "/repo/ws")
				m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{
					{Assistant: "claude", Workspace: ws, Running: true},
				}
			},
			want: false,
		},
		{
			name: "stale visible output past the activity window is inactive",
			build: func(m *Model) {
				ws := newTestWorkspace("ws", "/repo/ws")
				m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{
					{
						Assistant: "claude",
						Workspace: ws,
						Running:   true,
						tabActivityState: tabActivityState{
							lastVisibleOutput: now.Add(-(tabActiveWindow + time.Second)),
						},
					},
				}
			},
			want: false,
		},
		{
			name: "detached chat tab with recent output is inactive",
			build: func(m *Model) {
				ws := newTestWorkspace("ws", "/repo/ws")
				m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{
					{
						Assistant: "claude",
						Workspace: ws,
						Running:   true,
						Detached:  true,
						tabActivityState: tabActivityState{
							lastVisibleOutput: now.Add(-1 * time.Second),
						},
					},
				}
			},
			want: false,
		},
		{
			name: "non-chat tab with recent output is inactive",
			build: func(m *Model) {
				ws := newTestWorkspace("ws", "/repo/ws")
				m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{
					{
						Assistant: "vim",
						Workspace: ws,
						Running:   true,
						tabActivityState: tabActivityState{
							lastVisibleOutput: now.Add(-1 * time.Second),
						},
					},
				}
			},
			want: false,
		},
		{
			name: "active tab in a second workspace is detected",
			build: func(m *Model) {
				ws1 := newTestWorkspace("ws1", "/repo/ws1")
				ws2 := newTestWorkspace("ws2", "/repo/ws2")
				m.tabs.ByWorkspace[string(ws1.ID())] = []*Tab{
					{Assistant: "claude", Workspace: ws1, Running: true},
				}
				m.tabs.ByWorkspace[string(ws2.ID())] = []*Tab{
					{
						Assistant: "codex",
						Workspace: ws2,
						Running:   true,
						tabActivityState: tabActivityState{
							lastVisibleOutput: now.Add(-1 * time.Second),
						},
					},
				}
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			tt.build(m)
			if got := m.HasActiveAgents(); got != tt.want {
				t.Fatalf("HasActiveAgents() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetActiveWorkspaceRoots(t *testing.T) {
	now := time.Now()

	t.Run("empty model returns no roots", func(t *testing.T) {
		m := newTestModel()
		if roots := m.GetActiveWorkspaceRoots(); len(roots) != 0 {
			t.Fatalf("expected no roots, got %v", roots)
		}
	})

	t.Run("only active workspaces contribute their root", func(t *testing.T) {
		m := newTestModel()
		activeWS := newTestWorkspace("active", "/repo/active")
		idleWS := newTestWorkspace("idle", "/repo/idle")

		m.tabs.ByWorkspace[string(activeWS.ID())] = []*Tab{
			{
				Assistant: "claude",
				Workspace: activeWS,
				Running:   true,
				tabActivityState: tabActivityState{
					lastVisibleOutput: now.Add(-1 * time.Second),
				},
			},
		}
		m.tabs.ByWorkspace[string(idleWS.ID())] = []*Tab{
			{Assistant: "claude", Workspace: idleWS, Running: true},
		}

		roots := m.GetActiveWorkspaceRoots()
		if len(roots) != 1 || roots[0] != "/repo/active" {
			t.Fatalf("expected [/repo/active], got %v", roots)
		}
	})

	t.Run("root taken from first tab carrying a workspace", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{
			{Assistant: "claude", Workspace: nil},
			{
				Assistant: "claude",
				Workspace: ws,
				Running:   true,
				tabActivityState: tabActivityState{
					lastVisibleOutput: now.Add(-1 * time.Second),
				},
			},
		}
		roots := m.GetActiveWorkspaceRoots()
		if len(roots) != 1 || roots[0] != "/repo/ws" {
			t.Fatalf("expected [/repo/ws], got %v", roots)
		}
	})

	t.Run("multiple active workspaces each reported once", func(t *testing.T) {
		m := newTestModel()
		ws1 := newTestWorkspace("ws1", "/repo/ws1")
		ws2 := newTestWorkspace("ws2", "/repo/ws2")
		for _, ws := range []*data.Workspace{ws1, ws2} {
			m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{
				{
					Assistant: "claude",
					Workspace: ws,
					Running:   true,
					tabActivityState: tabActivityState{
						lastVisibleOutput: now.Add(-1 * time.Second),
					},
				},
			}
		}
		roots := m.GetActiveWorkspaceRoots()
		sort.Strings(roots)
		if len(roots) != 2 || roots[0] != "/repo/ws1" || roots[1] != "/repo/ws2" {
			t.Fatalf("expected both roots, got %v", roots)
		}
	})
}

func TestGetRunningWorkspaceRoots(t *testing.T) {
	t.Run("empty model returns no roots", func(t *testing.T) {
		m := newTestModel()
		if roots := m.GetRunningWorkspaceRoots(); len(roots) != 0 {
			t.Fatalf("expected no roots, got %v", roots)
		}
	})

	t.Run("idle but running tab still reported", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{
			// Running with no recent output: active=false but running=true.
			{Assistant: "claude", Workspace: ws, Running: true},
		}
		roots := m.GetRunningWorkspaceRoots()
		if len(roots) != 1 || roots[0] != "/repo/ws" {
			t.Fatalf("expected [/repo/ws], got %v", roots)
		}
	})

	t.Run("non-running tab excluded", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{
			{Assistant: "claude", Workspace: ws, Running: false},
		}
		if roots := m.GetRunningWorkspaceRoots(); len(roots) != 0 {
			t.Fatalf("expected no roots, got %v", roots)
		}
	})

	t.Run("non-chat tab excluded even when running", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{
			{Assistant: "vim", Workspace: ws, Running: true},
		}
		if roots := m.GetRunningWorkspaceRoots(); len(roots) != 0 {
			t.Fatalf("expected no roots, got %v", roots)
		}
	})

	t.Run("running tab without workspace contributes no root", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{
			{Assistant: "claude", Workspace: nil, Running: true},
		}
		if roots := m.GetRunningWorkspaceRoots(); len(roots) != 0 {
			t.Fatalf("expected no roots when running tab lacks a workspace, got %v", roots)
		}
	})

	t.Run("only one root per workspace despite multiple running tabs", func(t *testing.T) {
		m := newTestModel()
		ws := newTestWorkspace("ws", "/repo/ws")
		m.tabs.ByWorkspace[string(ws.ID())] = []*Tab{
			{Assistant: "claude", Workspace: ws, Running: true},
			{Assistant: "codex", Workspace: ws, Running: true},
		}
		roots := m.GetRunningWorkspaceRoots()
		if len(roots) != 1 || roots[0] != "/repo/ws" {
			t.Fatalf("expected exactly one root for the workspace, got %v", roots)
		}
	})

	t.Run("multiple workspaces each contribute a root", func(t *testing.T) {
		m := newTestModel()
		ws1 := newTestWorkspace("ws1", "/repo/ws1")
		ws2 := newTestWorkspace("ws2", "/repo/ws2")
		m.tabs.ByWorkspace[string(ws1.ID())] = []*Tab{
			{Assistant: "claude", Workspace: ws1, Running: true},
		}
		m.tabs.ByWorkspace[string(ws2.ID())] = []*Tab{
			{Assistant: "codex", Workspace: ws2, Running: true},
		}
		roots := m.GetRunningWorkspaceRoots()
		sort.Strings(roots)
		if len(roots) != 2 || roots[0] != "/repo/ws1" || roots[1] != "/repo/ws2" {
			t.Fatalf("expected both running roots, got %v", roots)
		}
	})
}
