package tmux

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// This file covers the pure parse/filter/aggregate helpers extracted from the
// fused read wrappers in internal/tmux:
//
//   - parseActiveAgentSessions (activity.go) — the amux- prefix special case,
//     type!=agent filtering, malformed/zero activity skip, window filtering and
//     keep-most-recent dedup behind ActiveAgentSessionsByActivity.
//   - latestActivitySeconds (activity.go) — max-over-windows behind
//     sessionLatestActivitySeconds.
//   - parseSessionStates (tmux.go) — the pane_dead==0 → HasLivePane aggregation
//     across multiple panes per session behind AllSessionStates.
//   - parseSessionTagRows (tags.go) — tag field split and the i+1>=len(parts)
//     empty-tag off-by-one behind listSessionsWithTags.
//
// Each was previously reachable only through a real tmux server, so this is the
// first non-integration coverage of the genuinely bug-prone parse loops.

// ---------------------------------------------------------------------------
// parseActiveAgentSessions
// ---------------------------------------------------------------------------

// fields builds one tab-separated list-windows line in the exact column order
// parseActiveAgentSessions expects:
// session_name, window_activity, @amux, @amux_workspace, @amux_tab, @amux_type.
func activityLine(name, activity, amux, ws, tab, typ string) string {
	return name + "\t" + activity + "\t" + amux + "\t" + ws + "\t" + tab + "\t" + typ
}

func sessionsByName(sessions []SessionActivity) map[string]SessionActivity {
	out := make(map[string]SessionActivity, len(sessions))
	for _, s := range sessions {
		out[s.Name] = s
	}
	return out
}

func TestParseActiveAgentSessions(t *testing.T) {
	// A fixed activity stamp well in the past; window filtering is exercised
	// separately so these cases use applyWindow=false.
	const act = "1000"

	tests := []struct {
		name  string
		lines []string
		want  map[string]SessionActivity
	}{
		{
			name:  "tagged session discovered via @amux regardless of name",
			lines: []string{activityLine("plain-name", act, "1", "ws-1", "tab-1", "agent")},
			want: map[string]SessionActivity{
				"plain-name": {Name: "plain-name", WorkspaceID: "ws-1", TabID: "tab-1", Type: "agent", Tagged: true},
			},
		},
		{
			name:  "untagged amux- prefixed session discovered via name fallback",
			lines: []string{activityLine("amux-ws-tab", act, "", "", "", "")},
			want: map[string]SessionActivity{
				"amux-ws-tab": {Name: "amux-ws-tab", Tagged: false},
			},
		},
		{
			name:  "@amux set to 0 is treated as untagged, dropped without amux- prefix",
			lines: []string{activityLine("plain-name", act, "0", "ws", "tab", "agent")},
			want:  map[string]SessionActivity{},
		},
		{
			name:  "@amux=0 but amux- prefix still kept via fallback as untagged",
			lines: []string{activityLine("amux-zero", act, "0", "ws", "tab", "agent")},
			want: map[string]SessionActivity{
				"amux-zero": {Name: "amux-zero", WorkspaceID: "ws", TabID: "tab", Type: "agent", Tagged: false},
			},
		},
		{
			name:  "untagged non-amux session excluded",
			lines: []string{activityLine("irrelevant", act, "", "", "", "")},
			want:  map[string]SessionActivity{},
		},
		{
			name:  "type set to non-agent is filtered out even when tagged",
			lines: []string{activityLine("amux-shell", act, "1", "ws", "tab", "shell")},
			want:  map[string]SessionActivity{},
		},
		{
			name:  "empty type is allowed (treated as agent)",
			lines: []string{activityLine("amux-blanktype", act, "1", "ws", "tab", "")},
			want: map[string]SessionActivity{
				"amux-blanktype": {Name: "amux-blanktype", WorkspaceID: "ws", TabID: "tab", Type: "", Tagged: true},
			},
		},
		{
			name:  "trimmed blank type field is allowed",
			lines: []string{"amux-trimmed\t" + act + "\t1\tws\tab"},
			want: map[string]SessionActivity{
				"amux-trimmed": {Name: "amux-trimmed", WorkspaceID: "ws", TabID: "ab", Type: "", Tagged: true},
			},
		},
		{
			name:  "trimmed untagged prefix row is allowed",
			lines: []string{"amux-trimmed-prefix\t" + act},
			want: map[string]SessionActivity{
				"amux-trimmed-prefix": {Name: "amux-trimmed-prefix", Tagged: false},
			},
		},
		{
			name:  "too few fields skipped",
			lines: []string{"only\ttwo\tfields"},
			want:  map[string]SessionActivity{},
		},
		{
			name:  "blank activity skipped",
			lines: []string{activityLine("amux-a", "", "1", "ws", "tab", "agent")},
			want:  map[string]SessionActivity{},
		},
		{
			name:  "non-numeric activity skipped",
			lines: []string{activityLine("amux-a", "not-a-number", "1", "ws", "tab", "agent")},
			want:  map[string]SessionActivity{},
		},
		{
			name:  "zero activity skipped",
			lines: []string{activityLine("amux-a", "0", "1", "ws", "tab", "agent")},
			want:  map[string]SessionActivity{},
		},
		{
			name:  "negative activity skipped",
			lines: []string{activityLine("amux-a", "-5", "1", "ws", "tab", "agent")},
			want:  map[string]SessionActivity{},
		},
		{
			name: "keep-most-recent dedup fills missing metadata across windows",
			lines: []string{
				// First window: tagged but missing workspace/tab/type metadata.
				activityLine("amux-multi", act, "1", "", "", ""),
				// Later window for the same session carries the metadata.
				activityLine("amux-multi", act, "", "ws-9", "tab-9", "agent"),
			},
			want: map[string]SessionActivity{
				"amux-multi": {Name: "amux-multi", WorkspaceID: "ws-9", TabID: "tab-9", Type: "agent", Tagged: true},
			},
		},
		{
			name: "dedup promotes untagged-first to tagged when a later window is tagged",
			lines: []string{
				// First window seen via amux- name fallback, untagged.
				activityLine("amux-promote", act, "", "", "", ""),
				// Later window is explicitly tagged.
				activityLine("amux-promote", act, "1", "ws", "tab", "agent"),
			},
			want: map[string]SessionActivity{
				"amux-promote": {Name: "amux-promote", WorkspaceID: "ws", TabID: "tab", Type: "agent", Tagged: true},
			},
		},
		{
			name: "dedup keeps the first non-empty metadata, ignores later blanks",
			lines: []string{
				activityLine("amux-first", act, "1", "ws-first", "tab-first", "agent"),
				activityLine("amux-first", act, "1", "", "", ""),
			},
			want: map[string]SessionActivity{
				"amux-first": {Name: "amux-first", WorkspaceID: "ws-first", TabID: "tab-first", Type: "agent", Tagged: true},
			},
		},
		{
			name:  "empty input yields nil result",
			lines: nil,
			want:  map[string]SessionActivity{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseActiveAgentSessions(tt.lines, 0, time.Unix(2000, 0), false)
			gotByName := sessionsByName(got)
			if !reflect.DeepEqual(gotByName, tt.want) {
				t.Fatalf("parseActiveAgentSessions = %#v, want %#v", gotByName, tt.want)
			}
		})
	}
}

func TestParseActiveAgentSessions_WindowFiltering(t *testing.T) {
	now := time.Unix(10_000, 0)
	// recent is 30s old, stale is 600s old.
	lines := []string{
		activityLine("amux-recent", "9970", "1", "ws", "tab", "agent"),
		activityLine("amux-stale", "9400", "1", "ws", "tab", "agent"),
	}

	// applyWindow=false: window is ignored, both kept.
	all := parseActiveAgentSessions(lines, time.Minute, now, false)
	if len(all) != 2 {
		t.Fatalf("expected both sessions when applyWindow=false, got %#v", all)
	}

	// applyWindow=true with a 1m window: only the 30s-old session survives.
	within := parseActiveAgentSessions(lines, time.Minute, now, true)
	byName := sessionsByName(within)
	if _, ok := byName["amux-recent"]; !ok {
		t.Fatalf("expected amux-recent within a 1m window, got %#v", within)
	}
	if _, ok := byName["amux-stale"]; ok {
		t.Fatalf("expected amux-stale (10m old) filtered out by a 1m window, got %#v", within)
	}
}

// ---------------------------------------------------------------------------
// latestActivitySeconds
// ---------------------------------------------------------------------------

func TestLatestActivitySeconds(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  int64
	}{
		{name: "nil", lines: nil, want: 0},
		{name: "single value", lines: []string{"1234"}, want: 1234},
		{
			name:  "max across windows",
			lines: []string{"100", "9999", "5000"},
			want:  9999,
		},
		{
			name:  "whitespace trimmed",
			lines: []string{"  4242  "},
			want:  4242,
		},
		{
			name:  "blank, non-numeric, zero and negative skipped",
			lines: []string{"", "  ", "abc", "0", "-7", "42"},
			want:  42,
		},
		{
			name:  "all invalid yields zero",
			lines: []string{"", "x", "0", "-1"},
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := latestActivitySeconds(tt.lines); got != tt.want {
				t.Fatalf("latestActivitySeconds(%#v) = %d, want %d", tt.lines, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseSessionStates
// ---------------------------------------------------------------------------

func TestParseSessionStates(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  map[string]SessionState
	}{
		{
			name:  "nil yields empty (non-nil) map",
			lines: nil,
			want:  map[string]SessionState{},
		},
		{
			name:  "single live pane",
			lines: []string{"sess-a\t0"},
			want:  map[string]SessionState{"sess-a": {Exists: true, HasLivePane: true}},
		},
		{
			name:  "single dead pane: exists but no live pane",
			lines: []string{"sess-a\t1"},
			want:  map[string]SessionState{"sess-a": {Exists: true, HasLivePane: false}},
		},
		{
			name: "multiple panes per session: any live pane sets HasLivePane",
			lines: []string{
				"sess-a\t1",
				"sess-a\t1",
				"sess-a\t0", // one live pane is enough
			},
			want: map[string]SessionState{"sess-a": {Exists: true, HasLivePane: true}},
		},
		{
			name: "all panes dead leaves HasLivePane false",
			lines: []string{
				"sess-dead\t1",
				"sess-dead\t1",
			},
			want: map[string]SessionState{"sess-dead": {Exists: true, HasLivePane: false}},
		},
		{
			name: "multiple sessions aggregated independently",
			lines: []string{
				"sess-live\t0",
				"sess-dead\t1",
				"sess-mixed\t1",
				"sess-mixed\t0",
			},
			want: map[string]SessionState{
				"sess-live":  {Exists: true, HasLivePane: true},
				"sess-dead":  {Exists: true, HasLivePane: false},
				"sess-mixed": {Exists: true, HasLivePane: true},
			},
		},
		{
			name: "live pane before dead pane stays live (no clobber)",
			lines: []string{
				"sess-a\t0",
				"sess-a\t1",
			},
			want: map[string]SessionState{"sess-a": {Exists: true, HasLivePane: true}},
		},
		{
			name: "lines without a tab separator are skipped",
			lines: []string{
				"no-tab-here",
				"sess-a\t0",
			},
			want: map[string]SessionState{"sess-a": {Exists: true, HasLivePane: true}},
		},
		{
			name:  "non-numeric dead field is treated as not-live but session exists",
			lines: []string{"sess-a\tweird"},
			want:  map[string]SessionState{"sess-a": {Exists: true, HasLivePane: false}},
		},
		{
			name:  "session name containing a tab keeps only the first segment as name",
			lines: []string{"sess-a\t0\textra"},
			// SplitN(_, 2) keeps "0\textra" as the dead field, which is != "0".
			want: map[string]SessionState{"sess-a": {Exists: true, HasLivePane: false}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSessionStates(tt.lines)
			if got == nil {
				t.Fatal("parseSessionStates returned a nil map, want non-nil")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseSessionStates(%#v) = %#v, want %#v", tt.lines, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseSessionTagRows
// ---------------------------------------------------------------------------

func TestParseSessionTagRows(t *testing.T) {
	// sep mirrors tagFieldSeparator so the test stays correct if the constant
	// changes.
	sep := tagFieldSeparator

	join := func(parts ...string) string {
		return strings.Join(parts, sep)
	}

	tests := []struct {
		name  string
		lines []string
		keys  []string
		want  []sessionTagRow
	}{
		{
			name:  "nil lines yields nil rows",
			lines: nil,
			keys:  []string{"@a"},
			want:  nil,
		},
		{
			name:  "name only, no keys requested",
			lines: []string{"sess-a"},
			keys:  nil,
			want:  []sessionTagRow{{Name: "sess-a", Tags: map[string]string{}}},
		},
		{
			name:  "all tags present and split on the separator",
			lines: []string{join("sess-a", "v1", "v2")},
			keys:  []string{"@a", "@b"},
			want: []sessionTagRow{
				{Name: "sess-a", Tags: map[string]string{"@a": "v1", "@b": "v2"}},
			},
		},
		{
			name:  "tag values are whitespace-trimmed",
			lines: []string{join("sess-a", "  v1  ", " v2 ")},
			keys:  []string{"@a", "@b"},
			want: []sessionTagRow{
				{Name: "sess-a", Tags: map[string]string{"@a": "v1", "@b": "v2"}},
			},
		},
		{
			name: "session-name is trimmed",
			// parseOutputLines normally trims, but the helper trims defensively too.
			lines: []string{join("  sess-a  ", "v1")},
			keys:  []string{"@a"},
			want: []sessionTagRow{
				{Name: "sess-a", Tags: map[string]string{"@a": "v1"}},
			},
		},
		{
			name: "missing trailing tag uses the i+1>=len(parts) empty branch",
			// Only the name and the first tag are present; @b has no field.
			lines: []string{join("sess-a", "v1")},
			keys:  []string{"@a", "@b"},
			want: []sessionTagRow{
				{Name: "sess-a", Tags: map[string]string{"@a": "v1", "@b": ""}},
			},
		},
		{
			name:  "name only with keys requested: every key reads empty",
			lines: []string{"sess-a"},
			keys:  []string{"@a", "@b"},
			want: []sessionTagRow{
				{Name: "sess-a", Tags: map[string]string{"@a": "", "@b": ""}},
			},
		},
		{
			name:  "empty-string field is kept as empty (not skipped)",
			lines: []string{join("sess-a", "", "v2")},
			keys:  []string{"@a", "@b"},
			want: []sessionTagRow{
				{Name: "sess-a", Tags: map[string]string{"@a": "", "@b": "v2"}},
			},
		},
		{
			name: "multiple rows",
			lines: []string{
				join("sess-a", "1"),
				join("sess-b", "2"),
			},
			keys: []string{"@a"},
			want: []sessionTagRow{
				{Name: "sess-a", Tags: map[string]string{"@a": "1"}},
				{Name: "sess-b", Tags: map[string]string{"@a": "2"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSessionTagRows(tt.lines, tt.keys)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseSessionTagRows(%#v, %v) = %#v, want %#v", tt.lines, tt.keys, got, tt.want)
			}
		})
	}
}

// TestParseSessionTagRows_KeyOrderIsPositional confirms keys are mapped to
// fields by their slice position, not sorted or looked up by name. listSessions
// builds the format string in the same key order, so a positional mismatch here
// would silently misattribute tag values.
func TestParseSessionTagRows_KeyOrderIsPositional(t *testing.T) {
	keys := []string{"@z", "@a"} // intentionally not sorted
	line := "sess" + tagFieldSeparator + "zval" + tagFieldSeparator + "aval"
	got := parseSessionTagRows([]string{line}, keys)
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if got[0].Tags["@z"] != "zval" || got[0].Tags["@a"] != "aval" {
		t.Fatalf("expected positional mapping @z=zval @a=aval, got %#v", got[0].Tags)
	}
	// Guard the key set is exactly the requested keys.
	gotKeys := make([]string, 0, len(got[0].Tags))
	for k := range got[0].Tags {
		gotKeys = append(gotKeys, k)
	}
	sort.Strings(gotKeys)
	if !reflect.DeepEqual(gotKeys, []string{"@a", "@z"}) {
		t.Fatalf("expected keys {@a,@z}, got %v", gotKeys)
	}
}
