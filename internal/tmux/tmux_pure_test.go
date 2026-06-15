package tmux

import (
	"reflect"
	"testing"
)

// These tests cover the pure / no-subprocess branches of tmux.go without a
// live tmux server. The subprocess-backed happy paths for AllSessionStates,
// SessionStateFor, runTmux, listTmux, hasLivePane, panePIDs, SessionTagValue,
// SetGlobalOptionValue and SetGlobalOptionValues are exercised in the
// *_integration_test.go files behind skipIfNoTmux. Here we assert the
// deterministic, exec-free logic: argument construction and the early-return
// guards that short-circuit before any tmux command runs.

func TestBuildMultiSetOptionArgs(t *testing.T) {
	tests := []struct {
		name      string
		scope     []string
		values    []OptionValue
		wantArgs  []string
		wantAdded int
	}{
		{
			name:      "nil values",
			scope:     []string{"-g"},
			values:    nil,
			wantArgs:  []string{},
			wantAdded: 0,
		},
		{
			name:      "empty values",
			scope:     []string{"-g"},
			values:    []OptionValue{},
			wantArgs:  []string{},
			wantAdded: 0,
		},
		{
			name:      "single global option",
			scope:     []string{"-g"},
			values:    []OptionValue{{Key: "@amux_a", Value: "1"}},
			wantArgs:  []string{"set-option", "-g", "@amux_a", "1"},
			wantAdded: 1,
		},
		{
			name:  "two global options separated by semicolon",
			scope: []string{"-g"},
			values: []OptionValue{
				{Key: "@amux_a", Value: "1"},
				{Key: "@amux_b", Value: "2"},
			},
			wantArgs: []string{
				"set-option", "-g", "@amux_a", "1",
				";",
				"set-option", "-g", "@amux_b", "2",
			},
			wantAdded: 2,
		},
		{
			name:  "session-target scope is threaded into each set-option",
			scope: []string{"-t", "my-session"},
			values: []OptionValue{
				{Key: "@amux_a", Value: "x"},
				{Key: "@amux_b", Value: "y"},
			},
			wantArgs: []string{
				"set-option", "-t", "my-session", "@amux_a", "x",
				";",
				"set-option", "-t", "my-session", "@amux_b", "y",
			},
			wantAdded: 2,
		},
		{
			name:  "blank keys are skipped, no leading separator",
			scope: []string{"-g"},
			values: []OptionValue{
				{Key: "", Value: "skip"},
				{Key: "   ", Value: "also-skip"},
				{Key: "@amux_real", Value: "keep"},
			},
			wantArgs:  []string{"set-option", "-g", "@amux_real", "keep"},
			wantAdded: 1,
		},
		{
			name:  "interior blank key does not emit a stray separator",
			scope: []string{"-g"},
			values: []OptionValue{
				{Key: "@amux_a", Value: "1"},
				{Key: "  ", Value: "ignored"},
				{Key: "@amux_b", Value: "2"},
			},
			wantArgs: []string{
				"set-option", "-g", "@amux_a", "1",
				";",
				"set-option", "-g", "@amux_b", "2",
			},
			wantAdded: 2,
		},
		{
			name:  "key whitespace is trimmed but value is preserved verbatim",
			scope: []string{"-g"},
			values: []OptionValue{
				{Key: "  @amux_padded  ", Value: "  spaced value  "},
			},
			wantArgs:  []string{"set-option", "-g", "@amux_padded", "  spaced value  "},
			wantAdded: 1,
		},
		{
			name:      "empty value is allowed and preserved",
			scope:     []string{"-g"},
			values:    []OptionValue{{Key: "@amux_a", Value: ""}},
			wantArgs:  []string{"set-option", "-g", "@amux_a", ""},
			wantAdded: 1,
		},
		{
			name:  "all keys blank yields empty args and zero added",
			scope: []string{"-g"},
			values: []OptionValue{
				{Key: "", Value: "a"},
				{Key: "\t", Value: "b"},
			},
			wantArgs:  []string{},
			wantAdded: 0,
		},
		{
			name:      "empty scope still emits set-option and key/value",
			scope:     nil,
			values:    []OptionValue{{Key: "@amux_a", Value: "1"}},
			wantArgs:  []string{"set-option", "@amux_a", "1"},
			wantAdded: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotArgs, gotAdded := buildMultiSetOptionArgs(tt.scope, tt.values)
			if gotAdded != tt.wantAdded {
				t.Errorf("added = %d, want %d", gotAdded, tt.wantAdded)
			}
			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Errorf("args = %#v, want %#v", gotArgs, tt.wantArgs)
			}
			// The number of ";" separators must always be added-1 (never leading
			// or trailing), which is the invariant tmux's command chaining relies on.
			seps := 0
			for _, a := range gotArgs {
				if a == ";" {
					seps++
				}
			}
			wantSeps := tt.wantAdded - 1
			if wantSeps < 0 {
				wantSeps = 0
			}
			if seps != wantSeps {
				t.Errorf("semicolon separators = %d, want %d", seps, wantSeps)
			}
		})
	}
}

// TestBuildMultiSetOptionArgs_DoesNotAliasScope guards against the scope slice
// being mutated or shared across the produced argument list (append reuse bugs).
//
// The guard is load-bearing in two complementary ways. First, scope is given
// spare capacity so an in-place / scope[:0] reuse bug would clobber the backing
// array and be caught by the DeepEqual check below. Second — and this is the
// part that actually catches a literal append(scope, ...) reuse — we assert that
// the returned args slice does not share backing storage with scope, since a
// DeepEqual(scope, ...) check alone never detects append(scope, ...) aliasing
// regardless of capacity (append grows into spare cap without mutating len(scope)
// elements).
func TestBuildMultiSetOptionArgs_DoesNotAliasScope(t *testing.T) {
	scope := make([]string, 2, 8)
	scope[0], scope[1] = "-t", "sess"
	values := []OptionValue{
		{Key: "@amux_a", Value: "1"},
		{Key: "@amux_b", Value: "2"},
	}
	args, _ := buildMultiSetOptionArgs(scope, values)
	if !reflect.DeepEqual(scope, []string{"-t", "sess"}) {
		t.Fatalf("scope was mutated: %#v", scope)
	}
	// args must own a fresh backing array: an append(scope, ...) reuse bug would
	// make args[0] point into scope's (spare) backing storage.
	if len(args) > 0 && &args[0] == &scope[:cap(scope)][0] {
		t.Fatalf("args aliases scope backing storage (append reuse): %#v", args)
	}
}

// TestSessionStateFor_EmptyName covers the guard that returns a zero-value
// SessionState before any tmux command (EnsureAvailable) is reached.
func TestSessionStateFor_EmptyName(t *testing.T) {
	st, err := SessionStateFor("", Options{})
	if err != nil {
		t.Fatalf("expected nil error for empty session name, got %v", err)
	}
	if st.Exists || st.HasLivePane {
		t.Fatalf("expected zero-value SessionState for empty name, got %+v", st)
	}
}

// TestSessionTagValue_EmptyArgs covers both blank-session and blank-key guards,
// each of which returns ("", nil) before reaching tmux.
func TestSessionTagValue_EmptyArgs(t *testing.T) {
	tests := []struct {
		name    string
		session string
		key     string
	}{
		{name: "empty session", session: "", key: "@amux"},
		{name: "empty key", session: "sess", key: ""},
		{name: "both empty", session: "", key: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := SessionTagValue(tt.session, tt.key, Options{})
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if val != "" {
				t.Fatalf("expected empty value, got %q", val)
			}
		})
	}
}

// TestSetGlobalOptionValue_BlankKey covers the guard that no-ops on a blank or
// whitespace-only key before EnsureAvailable / any tmux command runs.
func TestSetGlobalOptionValue_BlankKey(t *testing.T) {
	for _, key := range []string{"", "   ", "\t\n"} {
		if err := SetGlobalOptionValue(key, "value", Options{}); err != nil {
			t.Fatalf("expected nil error for blank key %q, got %v", key, err)
		}
	}
}

// TestSetGlobalOptionValues_EmptyInput covers the guard that no-ops on an
// empty/nil slice before EnsureAvailable / any tmux command runs.
func TestSetGlobalOptionValues_EmptyInput(t *testing.T) {
	if err := SetGlobalOptionValues(nil, Options{}); err != nil {
		t.Fatalf("expected nil error for nil values, got %v", err)
	}
	if err := SetGlobalOptionValues([]OptionValue{}, Options{}); err != nil {
		t.Fatalf("expected nil error for empty values, got %v", err)
	}
}
