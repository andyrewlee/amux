package tmux

import (
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// This file pins the subprocess counts behind the tmux fan-out reduction
// (activity scan, discovery, GC, tag writes): each batched read must cost
// exactly one tmux invocation, and SetSessionTagValues must fold its
// has-session guard into the same invocation as the set-option chain rather
// than paying a second subprocess. All calls route through the
// runTmuxCmd/runTmuxCmdCombined var-seams (tmux_runner.go) — no tmux server is
// contacted; skipIfNoTmux only covers functions whose EnsureAvailable gate
// does a PATH lookup.

// recordedTmuxCall is one intercepted tmux invocation.
type recordedTmuxCall struct {
	sub  string
	args []string
}

// fanoutSeam swaps both exec seams for recorders that dispatch on the tmux
// subcommand token and serve canned replies keyed by subcommand. It fails the
// test on a subcommand outside the allowed set, so a regression that re-adds a
// probe (e.g. a separate has-session or display-message) is caught by an
// unexpected-call error even when the recorder count alone would pass.
func fanoutSeam(t *testing.T, allowed map[string]seamResult) *[]recordedTmuxCall {
	t.Helper()
	var calls []recordedTmuxCall
	dispatch := func(cmd *exec.Cmd) ([]byte, error) {
		sub := ""
		for _, arg := range cmd.Args {
			switch arg {
			case "has-session", "list-panes", "list-clients", "list-sessions",
				"capture-pane", "display-message", "set-option":
				if sub == "" {
					sub = arg
				}
			}
		}
		calls = append(calls, recordedTmuxCall{sub: sub, args: cmd.Args})
		res, ok := allowed[sub]
		if !ok {
			return nil, errors.New("unexpected tmux subcommand: " + sub)
		}
		return res.out, res.err
	}
	origOut, origCombined := runTmuxCmd, runTmuxCmdCombined
	runTmuxCmd = dispatch
	runTmuxCmdCombined = dispatch
	t.Cleanup(func() {
		runTmuxCmd = origOut
		runTmuxCmdCombined = origCombined
	})
	return &calls
}

func TestSetSessionTagValues_OneInvocationWithChainedGuard(t *testing.T) {
	skipIfNoTmux(t)
	calls := fanoutSeam(t, map[string]seamResult{
		"has-session": {}, // chained head: serves the whole invocation
	})

	err := SetSessionTagValues("amux-x", []OptionValue{
		{Key: "@amux_k1", Value: "v1"},
		{Key: "@amux_k2", Value: "v2"},
	}, testOpts())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("tag write must cost exactly one tmux invocation, got %d calls: %+v", len(*calls), *calls)
	}
	args := (*calls)[0].args
	// Expected: has-session -t =amux-x ; set-option -t amux-x @amux_k1 v1 ; set-option -t amux-x @amux_k2 v2
	// cmd.Args[0] is the tmux binary; the subcommand sequence follows global flags.
	want := []string{"has-session", "-t", "=amux-x", ";", "set-option", "-t", "amux-x", "@amux_k1", "v1", ";", "set-option", "-t", "amux-x", "@amux_k2", "v2"}
	joined := strings.Join(args, " ")
	wantJoined := strings.Join(want, " ")
	if !strings.Contains(joined, wantJoined) {
		t.Fatalf("chained args mismatch:\n got: %s\nwant substring: %s", joined, wantJoined)
	}
}

func TestAllSessionMeta_SingleListSessionsCall(t *testing.T) {
	skipIfNoTmux(t)
	calls := fanoutSeam(t, map[string]seamResult{
		"list-sessions": {out: []byte("amux-a\t2\t1700000001\namux-b\t0\t1700000002\n")},
	})

	meta, err := AllSessionMeta(testOpts())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("AllSessionMeta must cost one invocation, got %d: %+v", len(*calls), *calls)
	}
	if !argsContain((*calls)[0].args, "#{session_name}\t#{session_attached}\t#{session_created}") {
		t.Fatalf("list-sessions must request name/attached/created, got %v", (*calls)[0].args)
	}
	if meta["amux-a"].Attached != 2 || meta["amux-a"].CreatedAt != 1700000001 {
		t.Fatalf("amux-a meta wrong: %+v", meta["amux-a"])
	}
	if meta["amux-b"].Attached != 0 || meta["amux-b"].CreatedAt != 1700000002 {
		t.Fatalf("amux-b meta wrong: %+v", meta["amux-b"])
	}
}

func TestAllSessionStates_SingleListPanesCall(t *testing.T) {
	skipIfNoTmux(t)
	calls := fanoutSeam(t, map[string]seamResult{
		"list-panes": {out: []byte(
			"amux-a\t1\t0\t0\n" + // dead pane in inactive window
				"amux-a\t0\t1\t1\n" + // live active pane -> ActivePaneLive
				"amux-b\t0\t0\t1\n", // live, but active pane is another row's pane
		)},
	})

	states, err := AllSessionStates(testOpts())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("AllSessionStates must cost one invocation, got %d: %+v", len(*calls), *calls)
	}
	format := "#{session_name}\t#{pane_dead}\t#{pane_active}\t#{window_active}"
	if !argsContain((*calls)[0].args, format) {
		t.Fatalf("list-panes must request the 4-field format, got %v", (*calls)[0].args)
	}
	if !states["amux-a"].ActivePaneLive {
		t.Fatalf("amux-a must report ActivePaneLive, got %+v", states["amux-a"])
	}
	if states["amux-b"].ActivePaneLive || !states["amux-b"].HasLivePane {
		t.Fatalf("amux-b: live pane but no active live pane, got %+v", states["amux-b"])
	}
}

func TestCapturePaneTailChecked_LiveActivePaneCostsOneCall(t *testing.T) {
	calls := fanoutSeam(t, map[string]seamResult{
		"capture-pane": {out: []byte("tail content\n")},
	})

	content, ok := CapturePaneTailChecked("amux-x", 10, true, testOpts())
	if !ok || content != "tail content" {
		t.Fatalf("expected (tail content, true), got (%q, %v)", content, ok)
	}
	if len(*calls) != 1 || (*calls)[0].sub != "capture-pane" {
		t.Fatalf("known-live active pane must capture directly in one call, got %+v", *calls)
	}
}

func TestCapturePaneTailChecked_DeadActivePaneFallsBackWithoutProbe(t *testing.T) {
	calls := fanoutSeam(t, map[string]seamResult{
		"list-panes":   {out: []byte("%1\t1\t1\n%2\t0\t0\n")},
		"capture-pane": {out: []byte("pane tail\n")},
	})

	content, ok := CapturePaneTailChecked("amux-x", 10, false, testOpts())
	if !ok || content != "pane tail" {
		t.Fatalf("expected (pane tail, true), got (%q, %v)", content, ok)
	}
	for _, call := range *calls {
		if call.sub == "display-message" {
			t.Fatalf("checked capture must not re-probe liveness via display-message: %+v", *calls)
		}
	}
}

func TestCapturePaneTail_ProbingVariantStillWorks(t *testing.T) {
	calls := fanoutSeam(t, map[string]seamResult{
		"display-message": {out: []byte("0\n")},
		"capture-pane":    {out: []byte("probe tail\n")},
	})

	content, ok := CapturePaneTail("amux-x", 10, testOpts())
	if !ok || content != "probe tail" {
		t.Fatalf("expected (probe tail, true), got (%q, %v)", content, ok)
	}
	subs := make([]string, 0, len(*calls))
	for _, call := range *calls {
		subs = append(subs, call.sub)
	}
	want := []string{"display-message", "capture-pane"}
	if strings.Join(subs, ",") != strings.Join(want, ",") {
		t.Fatalf("probing capture must probe then capture, got %v", subs)
	}
}

// TestParseSessionStates_ActivePaneLive pins the pane_active × window_active
// aggregation that feeds CapturePaneTailChecked: ActivePaneLive is true only
// when the session's current-window active pane is alive — the same pane
// `display-message -t <session> '#{pane_dead}'` would report on.
func TestParseSessionStates_ActivePaneLive(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  map[string]SessionState
	}{
		{
			name:  "active+window-active live pane sets ActivePaneLive",
			lines: []string{"sess-a\t0\t1\t1"},
			want:  map[string]SessionState{"sess-a": {Exists: true, HasLivePane: true, ActivePaneLive: true}},
		},
		{
			name:  "pane active in an inactive window is not ActivePaneLive",
			lines: []string{"sess-a\t0\t1\t0"},
			want:  map[string]SessionState{"sess-a": {Exists: true, HasLivePane: true}},
		},
		{
			name:  "window-active but pane-inactive is not ActivePaneLive",
			lines: []string{"sess-a\t0\t0\t1"},
			want:  map[string]SessionState{"sess-a": {Exists: true, HasLivePane: true}},
		},
		{
			name: "dead active pane is not ActivePaneLive",
			lines: []string{
				"sess-a\t1\t1\t1", // the active pane is dead
				"sess-a\t0\t0\t0", // a different pane is alive
			},
			want: map[string]SessionState{"sess-a": {Exists: true, HasLivePane: true}},
		},
		{
			name: "active pane on one row marks ActivePaneLive once found",
			lines: []string{
				"sess-a\t1\t0\t0",
				"sess-a\t0\t1\t1",
			},
			want: map[string]SessionState{"sess-a": {Exists: true, HasLivePane: true, ActivePaneLive: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSessionStates(tt.lines)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseSessionStates(%#v) = %#v, want %#v", tt.lines, got, tt.want)
			}
		})
	}
}
