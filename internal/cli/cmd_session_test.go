package cli

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// --- classifyForPrune tests ---

func TestClassifyForPruneAttachedNeverPruned(t *testing.T) {
	valid := map[string]bool{"ws-a": true}
	reason := classifyForPrune("ws-a", "term-tab", true, valid)
	if reason != "" {
		t.Fatalf("expected no prune for attached session, got %q", reason)
	}
}

func TestClassifyForPruneOrphanedWorkspace(t *testing.T) {
	valid := map[string]bool{"ws-a": true}
	reason := classifyForPrune("ws-gone", "agent", false, valid)
	if reason != "orphaned_workspace" {
		t.Fatalf("expected orphaned_workspace, got %q", reason)
	}
}

func TestClassifyForPruneDetachedTermTab(t *testing.T) {
	valid := map[string]bool{"ws-a": true}
	reason := classifyForPrune("ws-a", "term-tab", false, valid)
	if reason != "detached_terminal" {
		t.Fatalf("expected detached_terminal, got %q", reason)
	}
}

func TestClassifyForPruneDetachedTerminal(t *testing.T) {
	valid := map[string]bool{"ws-a": true}
	reason := classifyForPrune("ws-a", "terminal", false, valid)
	if reason != "detached_terminal" {
		t.Fatalf("expected detached_terminal, got %q", reason)
	}
}

func TestClassifyForPruneDetachedAgentNotPruned(t *testing.T) {
	valid := map[string]bool{"ws-a": true}
	reason := classifyForPrune("ws-a", "agent", false, valid)
	if reason != "" {
		t.Fatalf("expected no prune for detached agent in valid workspace, got %q", reason)
	}
}

func TestClassifyForPruneEmptyWorkspaceNotPruned(t *testing.T) {
	valid := map[string]bool{"ws-a": true}
	reason := classifyForPrune("", "agent", false, valid)
	if reason != "" {
		t.Fatalf("expected no prune for empty workspace ID, got %q", reason)
	}
}

// --- inferWorkspaceID tests ---

func TestInferWorkspaceIDTermTab(t *testing.T) {
	got := inferWorkspaceID("amux-abc123-term-tab-3")
	if got != "abc123" {
		t.Fatalf("inferWorkspaceID() = %q, want %q", got, "abc123")
	}
}

func TestInferWorkspaceIDTab(t *testing.T) {
	got := inferWorkspaceID("amux-abc123-tab-1")
	if got != "abc123" {
		t.Fatalf("inferWorkspaceID() = %q, want %q", got, "abc123")
	}
}

func TestInferWorkspaceIDNoPrefix(t *testing.T) {
	got := inferWorkspaceID("other-session")
	if got != "" {
		t.Fatalf("inferWorkspaceID() = %q, want empty", got)
	}
}

func TestInferWorkspaceIDNoSuffix(t *testing.T) {
	got := inferWorkspaceID("amux-abc123")
	if got != "abc123" {
		t.Fatalf("inferWorkspaceID() = %q, want %q", got, "abc123")
	}
}

// --- inferSessionType tests ---

func TestInferSessionTypeTermTab(t *testing.T) {
	got := inferSessionType("amux-abc123-term-tab-3")
	if got != "term-tab" {
		t.Fatalf("inferSessionType() = %q, want %q", got, "term-tab")
	}
}

func TestInferSessionTypeAgent(t *testing.T) {
	got := inferSessionType("amux-abc123-tab-1")
	if got != "agent" {
		t.Fatalf("inferSessionType() = %q, want %q", got, "agent")
	}
}

func TestInferSessionTypeUnknown(t *testing.T) {
	got := inferSessionType("amux-abc123")
	if got != "unknown" {
		t.Fatalf("inferSessionType() = %q, want %q", got, "unknown")
	}
}

// --- formatAge tests ---

func TestFormatAgeSeconds(t *testing.T) {
	if got := formatAge(30); got != "30s" {
		t.Fatalf("formatAge(30) = %q, want %q", got, "30s")
	}
}

func TestFormatAgeMinutes(t *testing.T) {
	if got := formatAge(300); got != "5m" {
		t.Fatalf("formatAge(300) = %q, want %q", got, "5m")
	}
}

func TestFormatAgeHours(t *testing.T) {
	if got := formatAge(7200); got != "2h" {
		t.Fatalf("formatAge(7200) = %q, want %q", got, "2h")
	}
}

func TestFormatAgeDays(t *testing.T) {
	if got := formatAge(172800); got != "2d" {
		t.Fatalf("formatAge(172800) = %q, want %q", got, "2d")
	}
}

// --- buildSessionList tests ---

func TestBuildSessionListUsesTagsOverInference(t *testing.T) {
	now := time.Unix(1000, 0)
	rows := []sessionRow{
		{
			name: "amux-ws1-tab-1",
			tags: map[string]string{
				"@amux_workspace": "ws-tagged",
				"@amux_type":      "agent",
			},
			createdAt: 900,
		},
	}
	entries := buildSessionList(rows, now)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].WorkspaceID != "ws-tagged" {
		t.Errorf("WorkspaceID = %q, want %q", entries[0].WorkspaceID, "ws-tagged")
	}
	if entries[0].Type != "agent" {
		t.Errorf("Type = %q, want %q", entries[0].Type, "agent")
	}
	if entries[0].AgeSeconds != 100 {
		t.Errorf("AgeSeconds = %d, want 100", entries[0].AgeSeconds)
	}
}

func TestBuildSessionListFallsBackToInference(t *testing.T) {
	now := time.Unix(1000, 0)
	rows := []sessionRow{
		{
			name:      "amux-abc123-term-tab-3",
			tags:      map[string]string{},
			createdAt: 500,
		},
	}
	entries := buildSessionList(rows, now)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].WorkspaceID != "abc123" {
		t.Errorf("WorkspaceID = %q, want %q", entries[0].WorkspaceID, "abc123")
	}
	if entries[0].Type != "term-tab" {
		t.Errorf("Type = %q, want %q", entries[0].Type, "term-tab")
	}
}

// --- findPruneCandidates tests ---

func TestFindPruneCandidatesOrphanedSession(t *testing.T) {
	now := time.Unix(1000, 0)
	rows := []sessionRow{
		{name: "amux-gone-tab-1", tags: map[string]string{"@amux_workspace": "gone"}, createdAt: 500},
	}
	candidates := findPruneCandidates(rows, []data.WorkspaceID{"ws-a"}, 0, now)
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}
	if candidates[0].Reason != "orphaned_workspace" {
		t.Errorf("reason = %q, want %q", candidates[0].Reason, "orphaned_workspace")
	}
}

func TestFindPruneCandidatesDetachedTermTab(t *testing.T) {
	now := time.Unix(1000, 0)
	rows := []sessionRow{
		{name: "amux-ws-a-term-tab-1", tags: map[string]string{"@amux_workspace": "ws-a", "@amux_type": "term-tab"}, createdAt: 500},
	}
	candidates := findPruneCandidates(rows, []data.WorkspaceID{"ws-a"}, 0, now)
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}
	if candidates[0].Reason != "detached_terminal" {
		t.Errorf("reason = %q, want %q", candidates[0].Reason, "detached_terminal")
	}
}

func TestFindPruneCandidatesAttachedNotPruned(t *testing.T) {
	now := time.Unix(1000, 0)
	rows := []sessionRow{
		{name: "amux-ws-a-term-tab-1", tags: map[string]string{"@amux_workspace": "ws-a", "@amux_type": "term-tab"}, attached: true, createdAt: 500},
	}
	candidates := findPruneCandidates(rows, []data.WorkspaceID{"ws-a"}, 0, now)
	if len(candidates) != 0 {
		t.Fatalf("got %d candidates, want 0 (attached sessions should not be pruned)", len(candidates))
	}
}

func TestFindPruneCandidatesAgentNotPruned(t *testing.T) {
	now := time.Unix(1000, 0)
	rows := []sessionRow{
		{name: "amux-ws-a-tab-1", tags: map[string]string{"@amux_workspace": "ws-a", "@amux_type": "agent"}, createdAt: 500},
	}
	candidates := findPruneCandidates(rows, []data.WorkspaceID{"ws-a"}, 0, now)
	if len(candidates) != 0 {
		t.Fatalf("got %d candidates, want 0 (detached agents in valid workspace should not be pruned)", len(candidates))
	}
}

func TestFindPruneCandidatesOlderThanSkipsUnknownAge(t *testing.T) {
	now := time.Unix(1000, 0)
	rows := []sessionRow{
		{name: "amux-ws-a-term-tab-1", tags: map[string]string{"@amux_workspace": "ws-a", "@amux_type": "term-tab"}, createdAt: 0},
	}
	candidates := findPruneCandidates(rows, []data.WorkspaceID{"ws-a"}, 10*time.Minute, now)
	if len(candidates) != 0 {
		t.Fatalf("got %d candidates, want 0 (unknown age should be skipped when --older-than is set)", len(candidates))
	}
}

func TestFindPruneCandidatesOlderThanFilter(t *testing.T) {
	now := time.Unix(1000, 0)
	rows := []sessionRow{
		{name: "amux-ws-a-term-tab-1", tags: map[string]string{"@amux_workspace": "ws-a", "@amux_type": "term-tab"}, createdAt: 999},
		{name: "amux-ws-a-term-tab-2", tags: map[string]string{"@amux_workspace": "ws-a", "@amux_type": "term-tab"}, createdAt: 100},
	}
	candidates := findPruneCandidates(rows, []data.WorkspaceID{"ws-a"}, 10*time.Minute, now)
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}
	if candidates[0].Session != "amux-ws-a-term-tab-2" {
		t.Errorf("session = %q, want %q", candidates[0].Session, "amux-ws-a-term-tab-2")
	}
}

func TestFindPruneCandidatesSkipsNonAmuxSessions(t *testing.T) {
	now := time.Unix(1000, 0)
	rows := []sessionRow{
		{name: "my-term-tab-1", tags: map[string]string{}, createdAt: 100},
	}
	candidates := findPruneCandidates(rows, []data.WorkspaceID{"ws-a"}, 0, now)
	if len(candidates) != 0 {
		t.Fatalf("got %d candidates, want 0 (non-amux sessions should not be pruned)", len(candidates))
	}
}

// --- isAmuxSession tests ---

func TestIsAmuxSessionTagged(t *testing.T) {
	row := sessionRow{name: "whatever", tags: map[string]string{"@amux_workspace": "ws-a"}}
	if !isAmuxSession(row) {
		t.Fatal("expected true for tagged session")
	}
}

func TestIsAmuxSessionPrefixed(t *testing.T) {
	row := sessionRow{name: "amux-ws-a-tab-1", tags: map[string]string{}}
	if !isAmuxSession(row) {
		t.Fatal("expected true for amux-prefixed session")
	}
}

func TestIsAmuxSessionForeignNotMatched(t *testing.T) {
	row := sessionRow{name: "my-term-tab-1", tags: map[string]string{}}
	if isAmuxSession(row) {
		t.Fatal("expected false for non-amux session")
	}
}

// --- humanReason tests ---

func TestHumanReasonOrphaned(t *testing.T) {
	if got := humanReason("orphaned_workspace"); got != "orphaned workspace" {
		t.Fatalf("humanReason() = %q, want %q", got, "orphaned workspace")
	}
}

func TestHumanReasonDetached(t *testing.T) {
	if got := humanReason("detached_terminal"); got != "detached terminal" {
		t.Fatalf("humanReason() = %q, want %q", got, "detached terminal")
	}
}

func TestHumanReasonUnknown(t *testing.T) {
	if got := humanReason("custom"); got != "custom" {
		t.Fatalf("humanReason() = %q, want %q", got, "custom")
	}
}
