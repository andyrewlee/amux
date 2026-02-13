package center

import (
	"testing"
	"time"
)

func TestEnforceAttachedAgentTabLimit_DetachesLeastRecentlyFocused(t *testing.T) {
	m := newTestModel()
	ws1 := newTestWorkspace("ws1", "/repo/ws1")
	ws2 := newTestWorkspace("ws2", "/repo/ws2")

	now := time.Now()
	ws1ID := string(ws1.ID())
	ws2ID := string(ws2.ID())

	oldest := &Tab{
		ID:            TabID("tab-oldest"),
		Assistant:     "claude",
		Workspace:     ws1,
		Running:       true,
		lastFocusedAt: now.Add(-2 * time.Hour),
		createdAt:     now.Add(-2 * time.Hour).Unix(),
	}
	active := &Tab{
		ID:            TabID("tab-active"),
		Assistant:     "claude",
		Workspace:     ws1,
		Running:       true,
		lastFocusedAt: now.Add(-5 * time.Minute),
		createdAt:     now.Add(-5 * time.Minute).Unix(),
	}
	mid := &Tab{
		ID:            TabID("tab-mid"),
		Assistant:     "claude",
		Workspace:     ws2,
		Running:       true,
		lastFocusedAt: now.Add(-45 * time.Minute),
		createdAt:     now.Add(-45 * time.Minute).Unix(),
	}

	m.tabsByWorkspace[ws1ID] = []*Tab{oldest, active}
	m.tabsByWorkspace[ws2ID] = []*Tab{mid}
	m.activeTabByWorkspace[ws1ID] = 1
	m.workspace = ws1

	detached, _ := m.EnforceAttachedAgentTabLimit(2)
	if len(detached) != 1 {
		t.Fatalf("expected 1 detached tab, got %d", len(detached))
	}
	if detached[0].WorkspaceID != ws1ID || detached[0].TabID != oldest.ID {
		t.Fatalf("expected oldest tab to detach, got workspace=%s tab=%s", detached[0].WorkspaceID, detached[0].TabID)
	}
	if !oldest.Detached || oldest.Running {
		t.Fatalf("expected oldest tab to be detached/stopped, detached=%v running=%v", oldest.Detached, oldest.Running)
	}
	if active.Detached || !active.Running {
		t.Fatalf("expected active tab to remain attached/running")
	}
	if mid.Detached || !mid.Running {
		t.Fatalf("expected non-active recent tab to remain attached/running")
	}
}

func TestEnforceAttachedAgentTabLimit_UsesCreatedAtWhenFocusIsUnknown(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	now := time.Now()

	older := &Tab{
		ID:        TabID("tab-older"),
		Assistant: "claude",
		Workspace: ws,
		Running:   true,
		createdAt: now.Add(-3 * time.Hour).Unix(),
	}
	newer := &Tab{
		ID:        TabID("tab-newer"),
		Assistant: "claude",
		Workspace: ws,
		Running:   true,
		createdAt: now.Add(-1 * time.Hour).Unix(),
	}
	active := &Tab{
		ID:            TabID("tab-active"),
		Assistant:     "claude",
		Workspace:     ws,
		Running:       true,
		lastFocusedAt: now,
		createdAt:     now.Unix(),
	}

	m.tabsByWorkspace[wsID] = []*Tab{older, newer, active}
	m.activeTabByWorkspace[wsID] = 2
	m.workspace = ws

	detached, _ := m.EnforceAttachedAgentTabLimit(2)
	if len(detached) != 1 {
		t.Fatalf("expected 1 detached tab, got %d", len(detached))
	}
	if detached[0].TabID != older.ID {
		t.Fatalf("expected older created tab to detach first, got %s", detached[0].TabID)
	}
	if !older.Detached {
		t.Fatalf("expected older tab to be detached")
	}
	if newer.Detached {
		t.Fatalf("expected newer tab to remain attached")
	}
}

func TestEnforceAttachedAgentTabLimit_ZeroDisablesEnforcement(t *testing.T) {
	m := newTestModel()
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	now := time.Now()

	tabA := &Tab{
		ID:            TabID("tab-a"),
		Assistant:     "claude",
		Workspace:     ws,
		Running:       true,
		lastFocusedAt: now.Add(-time.Hour),
		createdAt:     now.Add(-time.Hour).Unix(),
	}
	tabB := &Tab{
		ID:            TabID("tab-b"),
		Assistant:     "claude",
		Workspace:     ws,
		Running:       true,
		lastFocusedAt: now.Add(-30 * time.Minute),
		createdAt:     now.Add(-30 * time.Minute).Unix(),
	}

	m.tabsByWorkspace[wsID] = []*Tab{tabA, tabB}
	m.activeTabByWorkspace[wsID] = 0
	m.workspace = ws

	detached, cmds := m.EnforceAttachedAgentTabLimit(0)
	if len(detached) != 0 {
		t.Fatalf("expected no detached tabs when disabled, got %d", len(detached))
	}
	if len(cmds) != 0 {
		t.Fatalf("expected no detach commands when disabled, got %d", len(cmds))
	}
	if tabA.Detached || tabB.Detached {
		t.Fatalf("expected tabs to remain attached when disabled")
	}
}

func TestEnforceAttachedAgentTabLimit_DetachesWhenConfigIsNilFallback(t *testing.T) {
	m := New(nil)
	ws := newTestWorkspace("ws", "/repo/ws")
	wsID := string(ws.ID())
	now := time.Now()

	first := &Tab{
		ID:            TabID("tab-first"),
		Assistant:     "claude",
		Workspace:     ws,
		Running:       true,
		lastFocusedAt: now.Add(-2 * time.Hour),
		createdAt:     now.Add(-2 * time.Hour).Unix(),
	}
	second := &Tab{
		ID:            TabID("tab-second"),
		Assistant:     "claude",
		Workspace:     ws,
		Running:       true,
		lastFocusedAt: now.Add(-30 * time.Minute),
		createdAt:     now.Add(-30 * time.Minute).Unix(),
	}

	m.tabsByWorkspace[wsID] = []*Tab{first, second}
	m.activeTabByWorkspace[wsID] = 1
	m.workspace = ws

	detached, _ := m.EnforceAttachedAgentTabLimit(1)
	if len(detached) != 1 {
		t.Fatalf("expected 1 detached tab, got %d", len(detached))
	}
	if detached[0].TabID != first.ID {
		t.Fatalf("expected first tab to detach, got %s", detached[0].TabID)
	}
	if !first.Detached || first.Running {
		t.Fatalf("expected first tab detached/stopped, detached=%v running=%v", first.Detached, first.Running)
	}
}
