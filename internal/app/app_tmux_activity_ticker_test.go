package app

import (
	"testing"
)

// These tests cover the two ticker/trigger entry points in app_tmux_activity.go:
//   - startTmuxActivityTicker: bumps the activity-scan generation token and
//     returns a (non-nil) scheduling command.
//   - triggerTmuxActivityScan: returns a command that, when run, yields a
//     tmuxActivityTick stamped with the token captured at trigger time.
//
// Both functions only manipulate in-memory bookkeeping and build tea.Cmds; they
// do not exec tmux/git or require a live Bubble Tea program, so they are fully
// unit-testable here. The command produced by startTmuxActivityTicker wraps a
// 5s tea.Tick, so we assert its synchronous side effect (the token bump) and
// that a command is returned, rather than running the tick.

func TestStartTmuxActivityTicker_BumpsTokenAndSchedules(t *testing.T) {
	tests := []struct {
		name       string
		startToken activityScanToken
		wantToken  activityScanToken
	}{
		{name: "from zero", startToken: 0, wantToken: 1},
		{name: "from positive", startToken: 41, wantToken: 42},
		{name: "from negative", startToken: -3, wantToken: -2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &App{tmuxActivity: tmuxActivityState{token: tt.startToken}}

			cmd := app.startTmuxActivityTicker()

			if cmd == nil {
				t.Fatal("expected a non-nil scheduling command")
			}
			if app.tmuxActivity.token != tt.wantToken {
				t.Fatalf("token = %d, want %d (must bump by exactly one)", app.tmuxActivity.token, tt.wantToken)
			}
		})
	}
}

func TestStartTmuxActivityTicker_MonotonicAcrossCalls(t *testing.T) {
	app := &App{tmuxActivity: tmuxActivityState{token: 0}}

	for i := 1; i <= 4; i++ {
		cmd := app.startTmuxActivityTicker()
		if cmd == nil {
			t.Fatalf("call %d: expected a non-nil scheduling command", i)
		}
		if got := app.tmuxActivity.token; got != activityScanToken(i) {
			t.Fatalf("call %d: token = %d, want %d", i, got, i)
		}
	}
}

func TestTriggerTmuxActivityScan_EmitsTickWithCurrentToken(t *testing.T) {
	tests := []struct {
		name  string
		token activityScanToken
	}{
		{name: "zero token", token: 0},
		{name: "positive token", token: 9},
		{name: "negative token", token: -7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &App{tmuxActivity: tmuxActivityState{token: tt.token}}

			cmd := app.triggerTmuxActivityScan()
			if cmd == nil {
				t.Fatal("expected a non-nil trigger command")
			}

			// triggerTmuxActivityScan must NOT advance the generation token; it
			// re-emits a tick for the existing scan generation.
			if app.tmuxActivity.token != tt.token {
				t.Fatalf("token mutated to %d, want unchanged %d", app.tmuxActivity.token, tt.token)
			}

			msg := cmd()
			tick, ok := msg.(tmuxActivityTick)
			if !ok {
				t.Fatalf("expected tmuxActivityTick, got %T", msg)
			}
			if tick.Token != tt.token {
				t.Fatalf("tick token = %d, want %d", tick.Token, tt.token)
			}
		})
	}
}

func TestTriggerTmuxActivityScan_CapturesTokenAtTriggerTime(t *testing.T) {
	// The closure must snapshot the token when triggerTmuxActivityScan is called,
	// not when the returned command runs. Mutating the token afterwards must not
	// change the tick that was already scheduled.
	app := &App{tmuxActivity: tmuxActivityState{token: 5}}

	cmd := app.triggerTmuxActivityScan()
	if cmd == nil {
		t.Fatal("expected a non-nil trigger command")
	}

	// A later scan generation begins before the queued command is delivered.
	app.tmuxActivity.token = 99

	msg := cmd()
	tick, ok := msg.(tmuxActivityTick)
	if !ok {
		t.Fatalf("expected tmuxActivityTick, got %T", msg)
	}
	if tick.Token != 5 {
		t.Fatalf("tick token = %d, want 5 (token captured at trigger time)", tick.Token)
	}
}

func TestTriggerTmuxActivityScan_StaleTickIgnoredByHandler(t *testing.T) {
	// End-to-end: a trigger captured under an old generation is dropped by
	// handleTmuxActivityTick once the token has advanced, which the handler
	// signals by only rescheduling the ticker (one cmd) without starting a scan.
	app := &App{
		tmuxActivity:  tmuxActivityState{token: 3},
		tmuxAvailable: true,
	}

	cmd := app.triggerTmuxActivityScan()
	if cmd == nil {
		t.Fatal("expected a non-nil trigger command")
	}

	// Generation advances (e.g. a fresh ticker started) before the stale tick lands.
	app.startTmuxActivityTicker()
	if app.tmuxActivity.token != 4 {
		t.Fatalf("token = %d, want 4 after ticker restart", app.tmuxActivity.token)
	}

	tick, ok := cmd().(tmuxActivityTick)
	if !ok {
		t.Fatal("expected tmuxActivityTick from trigger command")
	}
	if tick.Token != 3 {
		t.Fatalf("captured tick token = %d, want stale value 3", tick.Token)
	}

	cmds := app.handleTmuxActivityTick(tick)
	if len(cmds) != 1 {
		t.Fatalf("expected only ticker reschedule for stale tick, got %d cmds", len(cmds))
	}
	if app.tmuxActivity.scanInFlight {
		t.Fatal("expected no scan to start for a stale tick")
	}
}

func TestTriggerTmuxActivityScan_FreshTickStartsScan(t *testing.T) {
	// Counterpart to the stale case: a tick whose token matches the current
	// generation (and tmux is available, no scan in flight) starts a scan, so the
	// handler returns both the reschedule and the scan command.
	app := &App{
		tmuxActivity:  tmuxActivityState{token: 8},
		tmuxAvailable: true,
	}

	cmd := app.triggerTmuxActivityScan()
	tick, ok := cmd().(tmuxActivityTick)
	if !ok {
		t.Fatal("expected tmuxActivityTick from trigger command")
	}

	cmds := app.handleTmuxActivityTick(tick)
	if len(cmds) != 2 {
		t.Fatalf("expected reschedule + scan for fresh tick, got %d cmds", len(cmds))
	}
	if !app.tmuxActivity.scanInFlight {
		t.Fatal("expected scan to be marked in flight for a fresh tick")
	}
	if app.tmuxActivity.token != 9 {
		t.Fatalf("expected scan generation token to advance to 9, got %d", app.tmuxActivity.token)
	}
}
