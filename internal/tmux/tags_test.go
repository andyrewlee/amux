package tmux

import (
	"sort"
	"testing"
	"time"
)

// These tests cover tags.go. The pure, exec-free logic — matchesTags and the
// early-return guards of SessionsWithTags / SetSessionTagValue /
// SetSessionTagValues that short-circuit before any tmux command — is asserted
// directly without a live tmux server. The subprocess-backed happy paths
// (listSessionsWithTags filtering, the actual set-option writes) are exercised
// behind skipIfNoTmux against an isolated test server, matching the convention
// in tmux_pure_test.go and the *_integration_test.go siblings.

// ---------------------------------------------------------------------------
// matchesTags — pure predicate, fully unit-testable
// ---------------------------------------------------------------------------

func TestMatchesTags(t *testing.T) {
	tests := []struct {
		name string
		row  sessionTagRow
		tags map[string]string
		keys []string
		want bool
	}{
		{
			name: "no keys matches everything",
			row:  sessionTagRow{Name: "s", Tags: map[string]string{"@a": "x"}},
			tags: map[string]string{},
			keys: nil,
			want: true,
		},
		{
			name: "single exact match",
			row:  sessionTagRow{Tags: map[string]string{"@a": "1"}},
			tags: map[string]string{"@a": "1"},
			keys: []string{"@a"},
			want: true,
		},
		{
			name: "single value mismatch",
			row:  sessionTagRow{Tags: map[string]string{"@a": "2"}},
			tags: map[string]string{"@a": "1"},
			keys: []string{"@a"},
			want: false,
		},
		{
			name: "empty want requires presence, present passes",
			row:  sessionTagRow{Tags: map[string]string{"@a": "anything"}},
			tags: map[string]string{"@a": ""},
			keys: []string{"@a"},
			want: true,
		},
		{
			name: "empty want requires presence, absent fails",
			row:  sessionTagRow{Tags: map[string]string{"@a": ""}},
			tags: map[string]string{"@a": ""},
			keys: []string{"@a"},
			want: false,
		},
		{
			name: "empty want, key missing from row entirely fails",
			row:  sessionTagRow{Tags: map[string]string{}},
			tags: map[string]string{"@a": ""},
			keys: []string{"@a"},
			want: false,
		},
		{
			name: "row value is whitespace-trimmed before exact compare",
			row:  sessionTagRow{Tags: map[string]string{"@a": "  1  "}},
			tags: map[string]string{"@a": "1"},
			keys: []string{"@a"},
			want: true,
		},
		{
			name: "row value of only whitespace counts as absent for presence check",
			row:  sessionTagRow{Tags: map[string]string{"@a": "   "}},
			tags: map[string]string{"@a": ""},
			keys: []string{"@a"},
			want: false,
		},
		{
			name: "all of several keys must match",
			row: sessionTagRow{Tags: map[string]string{
				"@a": "1", "@b": "2", "@c": "3",
			}},
			tags: map[string]string{"@a": "1", "@b": "2", "@c": "3"},
			keys: []string{"@a", "@b", "@c"},
			want: true,
		},
		{
			name: "one mismatch among several fails the whole match",
			row: sessionTagRow{Tags: map[string]string{
				"@a": "1", "@b": "WRONG", "@c": "3",
			}},
			tags: map[string]string{"@a": "1", "@b": "2", "@c": "3"},
			keys: []string{"@a", "@b", "@c"},
			want: false,
		},
		{
			name: "mix of presence and exact requirements all satisfied",
			row: sessionTagRow{Tags: map[string]string{
				"@present": "set", "@exact": "v",
			}},
			tags: map[string]string{"@present": "", "@exact": "v"},
			keys: []string{"@exact", "@present"},
			want: true,
		},
		{
			name: "mix where presence requirement is unmet",
			row: sessionTagRow{Tags: map[string]string{
				"@present": "", "@exact": "v",
			}},
			tags: map[string]string{"@present": "", "@exact": "v"},
			keys: []string{"@exact", "@present"},
			want: false,
		},
		{
			name: "key listed in orderedKeys but absent from tags map treats want as empty (presence)",
			row:  sessionTagRow{Tags: map[string]string{"@a": "x"}},
			tags: map[string]string{}, // want lookup yields ""
			keys: []string{"@a"},
			want: true,
		},
		{
			name: "nil row tags with exact want fails",
			row:  sessionTagRow{Tags: nil},
			tags: map[string]string{"@a": "1"},
			keys: []string{"@a"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesTags(tt.row, tt.tags, tt.keys)
			if got != tt.want {
				t.Errorf("matchesTags(%+v, %v, %v) = %v, want %v",
					tt.row, tt.tags, tt.keys, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Early-return guards that never reach tmux (exec-free)
// ---------------------------------------------------------------------------

// TestSessionsWithTags_NoMatchNoKeys covers the guard that returns (nil, nil)
// before EnsureAvailable / any tmux command when both match and keys are empty.
func TestSessionsWithTags_NoMatchNoKeys(t *testing.T) {
	cases := []struct {
		name  string
		match map[string]string
		keys  []string
	}{
		{name: "both nil", match: nil, keys: nil},
		{name: "empty map nil keys", match: map[string]string{}, keys: nil},
		{name: "nil map empty keys", match: nil, keys: []string{}},
		{name: "empty map empty keys", match: map[string]string{}, keys: []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := SessionsWithTags(tc.match, tc.keys, Options{})
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if rows != nil {
				t.Fatalf("expected nil rows, got %#v", rows)
			}
		})
	}
}

// TestSetSessionTagValue_BlankKey covers the guard that no-ops (returns nil)
// on an empty key before delegating to SetSessionTagValues / any tmux command.
func TestSetSessionTagValue_BlankKey(t *testing.T) {
	if err := SetSessionTagValue("some-session", "", "value", Options{}); err != nil {
		t.Fatalf("expected nil error for blank key, got %v", err)
	}
}

// TestSetSessionTagValues_EmptyInputGuards covers the guards that no-op
// (return nil) before EnsureAvailable / any tmux command when the session name
// is blank or the tag slice is empty.
func TestSetSessionTagValues_EmptyInputGuards(t *testing.T) {
	cases := []struct {
		name    string
		session string
		tags    []OptionValue
	}{
		{name: "empty session", session: "", tags: []OptionValue{{Key: "@a", Value: "1"}}},
		{name: "nil tags", session: "sess", tags: nil},
		{name: "empty tags", session: "sess", tags: []OptionValue{}},
		{name: "both empty", session: "", tags: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := SetSessionTagValues(tc.session, tc.tags, Options{}); err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Subprocess-backed behavior, gated on a real tmux server
// ---------------------------------------------------------------------------

// TestSessionsWithTags_FiltersAndReportsKeys verifies the full read path:
// listSessionsWithTags reads the matrix from tmux, SessionsWithTags filters by
// the match map and surfaces the requested extra key values.
func TestSessionsWithTags_FiltersAndReportsKeys(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "swt-match", "sleep 300")
	createSession(t, opts, "swt-other", "sleep 300")
	createSession(t, opts, "swt-nomatch", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	setTag(t, opts, "swt-match", "@amux", "1")
	setTag(t, opts, "swt-match", "@amux_workspace", "ws-1")
	setTag(t, opts, "swt-match", "@amux_owner", "alice")

	setTag(t, opts, "swt-other", "@amux", "1")
	setTag(t, opts, "swt-other", "@amux_workspace", "ws-2")

	// swt-nomatch intentionally has no @amux tag.

	rows, err := SessionsWithTags(
		map[string]string{"@amux": "1", "@amux_workspace": "ws-1"},
		[]string{"@amux_owner"},
		opts,
	)
	if err != nil {
		t.Fatalf("SessionsWithTags: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 matching session, got %d: %#v", len(rows), rows)
	}
	if rows[0].Name != "swt-match" {
		t.Fatalf("expected swt-match, got %q", rows[0].Name)
	}
	if got := rows[0].Tags["@amux_owner"]; got != "alice" {
		t.Fatalf("expected requested key @amux_owner=alice, got %q", got)
	}
}

// TestSessionsWithTags_KeysOnlyReturnsAllWithValues covers the keys-only path
// (no match filter): every session is returned with the requested key values.
func TestSessionsWithTags_KeysOnlyReturnsAllWithValues(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "ko-a", "sleep 300")
	createSession(t, opts, "ko-b", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	setTag(t, opts, "ko-a", "@amux_owner", "owner-a")
	// ko-b deliberately left without the tag → must read back as empty string.

	rows, err := SessionsWithTags(nil, []string{"@amux_owner"}, opts)
	if err != nil {
		t.Fatalf("SessionsWithTags: %v", err)
	}

	got := make(map[string]string, len(rows))
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		got[r.Name] = r.Tags["@amux_owner"]
		names = append(names, r.Name)
	}
	sort.Strings(names)

	if got["ko-a"] != "owner-a" {
		t.Fatalf("expected ko-a owner=owner-a, got %q", got["ko-a"])
	}
	if v, ok := got["ko-b"]; !ok {
		t.Fatalf("expected ko-b in results, names=%v", names)
	} else if v != "" {
		t.Fatalf("expected ko-b owner to read as empty, got %q", v)
	}
}

// TestSessionsWithTags_NoMatchingSessions confirms an empty (nil) result when
// no live session satisfies the match filter.
func TestSessionsWithTags_NoMatchingSessions(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "nm-a", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	rows, err := SessionsWithTags(
		map[string]string{"@amux": "definitely-not-set"},
		nil,
		opts,
	)
	if err != nil {
		t.Fatalf("SessionsWithTags: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no matches, got %d: %#v", len(rows), rows)
	}
}

// TestListSessionsWithTags_ReturnsSortedKeys asserts that listSessionsWithTags
// reports the requested tag keys sorted, and reads each session's value matrix.
func TestListSessionsWithTags_ReturnsSortedKeys(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "lst-a", "sleep 300")
	time.Sleep(50 * time.Millisecond)
	setTag(t, opts, "lst-a", "@amux_b", "bb")
	setTag(t, opts, "lst-a", "@amux_a", "aa")

	rows, keys, err := listSessionsWithTags(
		map[string]string{"@amux_b": "", "@amux_a": ""},
		opts,
	)
	if err != nil {
		t.Fatalf("listSessionsWithTags: %v", err)
	}
	if want := []string{"@amux_a", "@amux_b"}; !equalStrings(keys, want) {
		t.Fatalf("expected sorted keys %v, got %v", want, keys)
	}

	var found *sessionTagRow
	for i := range rows {
		if rows[i].Name == "lst-a" {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected lst-a in rows, got %#v", rows)
	}
	if found.Tags["@amux_a"] != "aa" || found.Tags["@amux_b"] != "bb" {
		t.Fatalf("unexpected tag values: %#v", found.Tags)
	}
}

// TestSetSessionTagValue_Roundtrip writes a single tag through the public API
// and reads it back, exercising the hasSession pre-check + set-option path.
func TestSetSessionTagValue_Roundtrip(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "sstv-a", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	if err := SetSessionTagValue("sstv-a", TagSessionOwner, "owner-x", opts); err != nil {
		t.Fatalf("SetSessionTagValue: %v", err)
	}
	got, err := SessionTagValue("sstv-a", TagSessionOwner, opts)
	if err != nil {
		t.Fatalf("SessionTagValue: %v", err)
	}
	if got != "owner-x" {
		t.Fatalf("expected owner-x, got %q", got)
	}
}

// TestTagAgentState_KeyPinned pins the @amux_agent_state tag key string. This
// is part of the external orchestration contract (docs/ORCHESTRATION.md) — a
// rename here must be a deliberate, documented breaking change, not an
// incidental refactor.
func TestTagAgentState_KeyPinned(t *testing.T) {
	if TagAgentState != "@amux_agent_state" {
		t.Fatalf("TagAgentState = %q, want %q", TagAgentState, "@amux_agent_state")
	}
}

// TestSetSessionTagValue_AgentStateRoundtrip writes each documented
// @amux_agent_state value (idle/working/done) through the same public
// SetSessionTagValue path the activity scan's best-effort tag write uses, and
// reads each back — proving the tag key/value pair actually round-trips
// through tmux, not just that the Go constant string is correct.
func TestSetSessionTagValue_AgentStateRoundtrip(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "sstv-agentstate", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	for _, want := range []string{"idle", "working", "done"} {
		if err := SetSessionTagValue("sstv-agentstate", TagAgentState, want, opts); err != nil {
			t.Fatalf("SetSessionTagValue(%q): %v", want, err)
		}
		got, err := SessionTagValue("sstv-agentstate", TagAgentState, opts)
		if err != nil {
			t.Fatalf("SessionTagValue: %v", err)
		}
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	}
}

// TestSetSessionTagValues_MissingSessionIsNoError verifies the hasSession
// pre-check short-circuits to nil (no spurious error) for a session that does
// not exist, the "killed between create and tag" case.
func TestSetSessionTagValues_MissingSessionIsNoError(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	err := SetSessionTagValues("no-such-session-xyz", []OptionValue{
		{Key: TagLastOutputAt, Value: "1"},
	}, opts)
	if err != nil {
		t.Fatalf("expected nil error for missing session, got %v", err)
	}
}

// TestSetSessionTagValues_AllBlankKeysNoOps verifies that when every supplied
// key is blank, buildMultiSetOptionArgs adds nothing and the call no-ops
// against a live session without error.
func TestSetSessionTagValues_AllBlankKeysNoOps(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "blank-keys", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	err := SetSessionTagValues("blank-keys", []OptionValue{
		{Key: "", Value: "a"},
		{Key: "   ", Value: "b"},
	}, opts)
	if err != nil {
		t.Fatalf("expected nil error when all keys blank, got %v", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
