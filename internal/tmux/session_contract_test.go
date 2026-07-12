// This test pins the external orchestration contract documented in
// docs/ORCHESTRATION.md — a failure here is a breaking change for external
// orchestrators; update the doc and release notes, not just this test.
//
// It is deliberately pure string logic (SessionName + WorkspaceIDFromSessionName)
// and must NOT require a running tmux server. It lives in the external test
// package tmux_test so it can import internal/app/activity for the round-trip
// half without creating an import cycle (activity imports tmux).
package tmux_test

import (
	"testing"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/tmux"
)

// TestSessionNameContract pins the documented session-name grammar and the
// sanitize rules that produce it. The construction (parts, order, sanitize,
// join) is the seam external orchestrators depend on.
func TestSessionNameContract(t *testing.T) {
	cases := []struct {
		name  string
		parts []string
		want  string
	}{
		{
			name:  "interactive agent tab, real ID shapes",
			parts: []string{"amux", "9f8e7d6c5b4a3210", "tab-1a2b3c4d-5"},
			want:  "amux-9f8e7d6c5b4a3210-tab-1a2b3c4d-5",
		},
		{
			name:  "viewer session",
			parts: []string{"amux", "0011aabb2233ccdd", "viewer"},
			want:  "amux-0011aabb2233ccdd-viewer",
		},
		{
			name:  "uppercase parts are lowercased",
			parts: []string{"amux", "WS", "TAB"},
			want:  "amux-ws-tab",
		},
		{
			name:  "underscore is preserved, other punctuation becomes a dash",
			parts: []string{"amux", "ws_1", "feature/foo"},
			want:  "amux-ws_1-feature-foo",
		},
		{
			name:  "leading/trailing dashes are trimmed per part",
			parts: []string{"amux", "-ws-", "tab"},
			want:  "amux-ws-tab",
		},
		{
			name:  "a part that sanitizes to empty is dropped",
			parts: []string{"amux", "///", "tab"},
			want:  "amux-tab",
		},
		{
			name:  "empty and whitespace parts are dropped",
			parts: []string{"amux", "", "  ", "ws1", "tab-1"},
			want:  "amux-ws1-tab-1",
		},
		{
			name:  "no surviving parts yields the bare marker",
			parts: []string{"", "  ", "///"},
			want:  "amux",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tmux.SessionName(tc.parts...)
			if got != tc.want {
				t.Fatalf("SessionName(%q) = %q, want %q", tc.parts, got, tc.want)
			}
		})
	}
}

// TestWorkspaceIDRoundTripContract pins that a workspace ID survives the trip
// from SessionName construction back through WorkspaceIDFromSessionName. This is
// the guarantee external tools rely on to map a discovered session to a
// workspace. It holds because real workspace IDs are dashless hex; the final
// two cases pin the documented caveats (a dash in the ID position truncates, and
// the bare "amux" marker has no workspace).
func TestWorkspaceIDRoundTripContract(t *testing.T) {
	cases := []struct {
		name    string
		wsID    string
		tabPart string
		want    string
	}{
		{
			name:    "agent tab round-trips the workspace ID",
			wsID:    "9f8e7d6c5b4a3210",
			tabPart: "tab-1a2b3c4d-5",
			want:    "9f8e7d6c5b4a3210",
		},
		{
			name:    "viewer session round-trips the workspace ID",
			wsID:    "0011aabb2233ccdd",
			tabPart: "viewer",
			want:    "0011aabb2233ccdd",
		},
		{
			// Documented caveat: WorkspaceIDFromSessionName returns only the
			// first '-'-delimited segment, so a dash in the ID position
			// truncates. Real workspace IDs are dashless hex, so this never
			// occurs in practice — pinned as known behavior, not a parser bug.
			name:    "dash in the ID position truncates (documented caveat)",
			wsID:    "feature-foo",
			tabPart: "tab-x-1",
			want:    "feature",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := tmux.SessionName("amux", tc.wsID, tc.tabPart)
			got := activity.WorkspaceIDFromSessionName(session)
			if got != tc.want {
				t.Fatalf("WorkspaceIDFromSessionName(%q) = %q, want %q", session, got, tc.want)
			}
		})
	}

	// A name that is not an amux session (or is the bare marker with no
	// workspace segment) yields no workspace ID.
	for _, name := range []string{"amux", "some-other-session", ""} {
		if got := activity.WorkspaceIDFromSessionName(name); got != "" {
			t.Fatalf("WorkspaceIDFromSessionName(%q) = %q, want empty", name, got)
		}
	}
}
