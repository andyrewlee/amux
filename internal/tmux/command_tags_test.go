package tmux

import (
	"strings"
	"testing"
)

// The display tags (@amux_workspace_name/@amux_project) are additive,
// write-only labels for external orchestrators — they must emit when
// populated, never disturb the identity tags, and never turn a display-only
// tag set into a bare `@amux 1`.
func TestSessionTagArgs_DisplayTagsEmitWhenPopulated(t *testing.T) {
	tags := SessionTags{
		WorkspaceID:   "ws-123",
		WorkspaceName: "my-ws",
		ProjectName:   "amux",
	}
	args := sessionTagArgs("s1", tags)
	joined := flattenArgs(args)
	if !strings.Contains(joined, "@amux_workspace_name 'my-ws'") {
		t.Fatalf("missing @amux_workspace_name in %v", args)
	}
	if !strings.Contains(joined, "@amux_project 'amux'") {
		t.Fatalf("missing @amux_project in %v", args)
	}
	if !strings.Contains(joined, "@amux_workspace 'ws-123'") {
		t.Fatalf("identity tag lost: %v", args)
	}
}

func TestSessionTagArgs_DisplayTagsOmittedWhenEmpty(t *testing.T) {
	args := sessionTagArgs("s1", SessionTags{WorkspaceID: "ws-123"})
	joined := flattenArgs(args)
	if strings.Contains(joined, "@amux_workspace_name") || strings.Contains(joined, "@amux_project ") {
		t.Fatalf("empty display tags must not emit options: %v", args)
	}
}

func TestSessionTagArgs_DisplayOnlyFieldsStayUntagged(t *testing.T) {
	// A tag set carrying only display fields must not emit `@amux 1` — the
	// all-empty guard covers them so the marker stays honest.
	if args := sessionTagArgs("s1", SessionTags{WorkspaceName: "x", ProjectName: "y"}); args != nil {
		t.Fatalf("display-only tags emitted %v, want nil", args)
	}
}

func TestSessionTagArgs_DisplayValuesSanitized(t *testing.T) {
	args := sessionTagArgs("s1", SessionTags{
		WorkspaceID:   "ws-1",
		WorkspaceName: "ok",
		ProjectName:   "bad\x1bname\nwith\x00ctl",
	})
	joined := flattenArgs(args)
	if strings.Contains(joined, "\x1b") || strings.Contains(joined, "\x00") || strings.Contains(joined, "bad\x1bname") {
		t.Fatalf("control bytes survived into tag args: %q", joined)
	}
	if !strings.Contains(joined, "badnamewithctl") {
		t.Fatalf("sanitized value missing from %q", joined)
	}
}

func flattenArgs(args [][]string) string {
	var b strings.Builder
	for _, a := range args {
		b.WriteString(strings.Join(a, " "))
		b.WriteString("\n")
	}
	return b.String()
}

// TestSessionTagPairs pins the single-source mapping: every
// consumer — sessionTagArgs emission and the sidebar's verify/retag checks —
// resolves the identical ordered pairs, so a new SessionTags field cannot
// drift between writers. Asserts order, normalization, and the guard.
func TestSessionTagPairs(t *testing.T) {
	full := SessionTags{
		WorkspaceID:   "ws-1",
		TabID:         "tab-2",
		Type:          "agent",
		Assistant:     "claude",
		CreatedAt:     1700000000,
		InstanceID:    "inst-9",
		SessionOwner:  "owner-x",
		LeaseAtMS:     1700000000123,
		WorkspaceName: "feature-x",
		ProjectName:   "proj",
	}
	pairs := SessionTagPairs(full)
	wantKeys := []string{
		"@amux", "@amux_workspace", "@amux_tab", "@amux_type",
		"@amux_assistant", "@amux_created_at", "@amux_instance",
		TagSessionOwner, TagSessionLeaseAt, TagSessionOwnerHeartbeatAt,
		"@amux_workspace_name", "@amux_project",
	}
	if len(pairs) != len(wantKeys) {
		t.Fatalf("expected %d pairs, got %d: %+v", len(wantKeys), len(pairs), pairs)
	}
	for i, key := range wantKeys {
		if pairs[i].Key != key {
			t.Fatalf("pair %d: expected key %q, got %q", i, key, pairs[i].Key)
		}
	}
	if pairs[0].Value != "1" {
		t.Fatalf("marker value = %q, want 1", pairs[0].Value)
	}
	if pairs[10].Value != "feature-x" || pairs[11].Value != "proj" {
		t.Fatalf("display values = %q/%q", pairs[10].Value, pairs[11].Value)
	}

	// Identity strings are trimmed before the guard and the emit.
	pairs = SessionTagPairs(SessionTags{WorkspaceID: "  ws-1  "})
	if len(pairs) != 2 || pairs[1].Value != "ws-1" {
		t.Fatalf("whitespace identity not normalized: %+v", pairs)
	}
	if SessionTagPairs(SessionTags{WorkspaceID: "   "}) != nil {
		t.Fatal("whitespace-only identity must refuse the marker")
	}
	// Negative/zero timestamps are unset for every consumer.
	pairs = SessionTagPairs(SessionTags{WorkspaceID: "ws", CreatedAt: -1, LeaseAtMS: -5})
	for _, p := range pairs {
		if p.Key == "@amux_created_at" || p.Key == TagSessionLeaseAt || p.Key == TagSessionOwnerHeartbeatAt {
			t.Fatalf("non-positive timestamp emitted: %+v", p)
		}
	}
	// Display-only sets and empty sets emit nothing.
	if SessionTagPairs(SessionTags{WorkspaceName: "x", ProjectName: "y"}) != nil {
		t.Fatal("display-only tag set must refuse the marker")
	}
	if SessionTagPairs(SessionTags{}) != nil {
		t.Fatal("empty tag set must emit nothing")
	}
}
