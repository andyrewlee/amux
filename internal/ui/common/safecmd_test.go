package common

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

// stringMsg is a trivial message type used to assert that a command's return
// value is propagated unchanged through the Safe* wrappers.
type stringMsg string

// asError extracts a messages.Error from an arbitrary message, reporting
// whether the message was in fact an error. It centralizes the type assertion
// the panic-path tests share.
func asError(t *testing.T, msg tea.Msg) (messages.Error, bool) {
	t.Helper()
	e, ok := msg.(messages.Error)
	return e, ok
}

func TestSafeCmd_NilReturnsNil(t *testing.T) {
	if got := SafeCmd(nil); got != nil {
		t.Fatalf("SafeCmd(nil) = %v, want nil", got)
	}
}

func TestSafeCmd_PassesThroughMessage(t *testing.T) {
	want := stringMsg("hello")
	wrapped := SafeCmd(func() tea.Msg { return want })
	if wrapped == nil {
		t.Fatal("SafeCmd returned nil for a non-nil command")
	}

	got, ok := wrapped().(stringMsg)
	if !ok {
		t.Fatalf("wrapped command returned %T, want stringMsg", wrapped())
	}
	if got != want {
		t.Errorf("wrapped command returned %q, want %q", got, want)
	}
}

func TestSafeCmd_PropagatesNilMessage(t *testing.T) {
	// A command that legitimately returns nil must not be turned into an error.
	wrapped := SafeCmd(func() tea.Msg { return nil })
	if wrapped == nil {
		t.Fatal("SafeCmd returned nil for a non-nil command")
	}
	if msg := wrapped(); msg != nil {
		t.Fatalf("wrapped command returned %v, want nil", msg)
	}
}

func TestSafeCmd_RecoversPanicAsError(t *testing.T) {
	wrapped := SafeCmd(func() tea.Msg { panic("boom") })
	if wrapped == nil {
		t.Fatal("SafeCmd returned nil for a non-nil command")
	}

	// Invoking the wrapped command must not propagate the panic.
	msg := wrapped()
	errMsg, ok := asError(t, msg)
	if !ok {
		t.Fatalf("panicking command returned %T, want messages.Error", msg)
	}
	if errMsg.Err == nil {
		t.Fatal("recovered error message has a nil Err")
	}
	if !errMsg.Logged {
		t.Error("recovered command error should be marked Logged")
	}
	if errMsg.Context != "command" {
		t.Errorf("Context = %q, want %q", errMsg.Context, "command")
	}
	// The original panic value should be embedded in the error text.
	if got := errMsg.Err.Error(); got != "command panic: boom" {
		t.Errorf("error text = %q, want %q", got, "command panic: boom")
	}
}

func TestSafeCmd_RecoversRuntimePanic(t *testing.T) {
	// A runtime panic (index out of range) must be recovered just like an
	// explicit panic. We use a slice access the static analyzer cannot fold to
	// a constant, so this exercises the genuine runtime-panic path.
	wrapped := SafeCmd(func() tea.Msg {
		empty := make([]int, 0)
		_ = empty[indexOutOfRange()] // index out of range -> runtime panic
		return nil
	})

	msg := wrapped()
	if _, ok := asError(t, msg); !ok {
		t.Fatalf("out-of-range command returned %T, want messages.Error", msg)
	}
}

// indexOutOfRange returns an index that is always out of bounds for an empty
// slice but is opaque to static analysis, forcing the panic to occur at
// runtime rather than being flagged by govet's nilness/index checks.
func indexOutOfRange() int { return 5 }

func TestSafeBatch_EmptyAndNilInputs(t *testing.T) {
	tests := []struct {
		name string
		cmds []tea.Cmd
	}{
		{name: "no args", cmds: nil},
		{name: "explicit empty slice", cmds: []tea.Cmd{}},
		{name: "single nil cmd", cmds: []tea.Cmd{nil}},
		{name: "all nil cmds", cmds: []tea.Cmd{nil, nil, nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SafeBatch(tt.cmds...); got != nil {
				t.Errorf("SafeBatch(%v) = %v, want nil", tt.name, got)
			}
		})
	}
}

func TestSafeBatch_SingleCommandRunsAndPasses(t *testing.T) {
	want := stringMsg("only")
	batch := SafeBatch(func() tea.Msg { return want })
	if batch == nil {
		t.Fatal("SafeBatch returned nil for one valid command")
	}

	// tea.Batch collapses a single command to the command itself, so invoking
	// the batch yields the wrapped command's message directly (not a BatchMsg).
	got, ok := batch().(stringMsg)
	if !ok {
		t.Fatalf("single-command batch returned %T, want stringMsg", batch())
	}
	if got != want {
		t.Errorf("single-command batch returned %q, want %q", got, want)
	}
}

func TestSafeBatch_SkipsNilCommands(t *testing.T) {
	// Mixing nils with one real command should behave like the single-command
	// case: nils are filtered, leaving exactly one command.
	want := stringMsg("kept")
	batch := SafeBatch(nil, func() tea.Msg { return want }, nil)
	if batch == nil {
		t.Fatal("SafeBatch returned nil despite a valid command among nils")
	}
	got, ok := batch().(stringMsg)
	if !ok {
		t.Fatalf("filtered batch returned %T, want stringMsg", batch())
	}
	if got != want {
		t.Errorf("filtered batch returned %q, want %q", got, want)
	}
}

func TestSafeBatch_MultipleCommandsProduceBatchMsg(t *testing.T) {
	a := func() tea.Msg { return stringMsg("a") }
	b := func() tea.Msg { return stringMsg("b") }
	batch := SafeBatch(a, b)
	if batch == nil {
		t.Fatal("SafeBatch returned nil for two valid commands")
	}

	msg := batch()
	bm, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("multi-command batch returned %T, want tea.BatchMsg", msg)
	}
	if len(bm) != 2 {
		t.Fatalf("BatchMsg has %d commands, want 2", len(bm))
	}

	// Each inner command should still produce its original message, and remain
	// individually wrapped so a panic in one does not abort the others.
	results := make(map[stringMsg]bool)
	for _, c := range bm {
		got, ok := c().(stringMsg)
		if !ok {
			t.Fatalf("inner command returned %T, want stringMsg", c())
		}
		results[got] = true
	}
	if !results["a"] || !results["b"] {
		t.Errorf("inner command results = %v, want both a and b", results)
	}
}

func TestSafeBatch_WrapsEachCommandInRecovery(t *testing.T) {
	// One panicking command and one healthy command. The panic must be
	// converted to an error message rather than crashing, and the healthy
	// command must still deliver its message.
	panicCmd := func() tea.Msg { panic("inner boom") }
	okCmd := func() tea.Msg { return stringMsg("survivor") }

	batch := SafeBatch(panicCmd, okCmd)
	bm, ok := batch().(tea.BatchMsg)
	if !ok {
		t.Fatalf("batch returned %T, want tea.BatchMsg", batch())
	}

	var sawError, sawSurvivor bool
	for _, c := range bm {
		switch m := c().(type) {
		case messages.Error:
			sawError = true
			if m.Context != "command" {
				t.Errorf("recovered error Context = %q, want %q", m.Context, "command")
			}
		case stringMsg:
			if m == "survivor" {
				sawSurvivor = true
			}
		default:
			t.Errorf("unexpected inner message type %T", m)
		}
	}
	if !sawError {
		t.Error("panicking command in batch was not converted to an error message")
	}
	if !sawSurvivor {
		t.Error("healthy command in batch did not deliver its message")
	}
}

func TestSafeTick_NilFnReturnsNil(t *testing.T) {
	if got := SafeTick(time.Millisecond, nil); got != nil {
		t.Fatalf("SafeTick(_, nil) = %v, want nil", got)
	}
}

func TestSafeTick_InvokesCallbackWithTime(t *testing.T) {
	want := stringMsg("ticked")
	var gotTime time.Time
	cmd := SafeTick(time.Millisecond, func(ts time.Time) tea.Msg {
		gotTime = ts
		return want
	})
	if cmd == nil {
		t.Fatal("SafeTick returned nil for a non-nil callback")
	}

	got, ok := cmd().(stringMsg)
	if !ok {
		t.Fatalf("tick command returned %T, want stringMsg", cmd())
	}
	if got != want {
		t.Errorf("tick command returned %q, want %q", got, want)
	}
	if gotTime.IsZero() {
		t.Error("tick callback was passed a zero time")
	}
}

func TestSafeTick_RecoversPanicAsError(t *testing.T) {
	cmd := SafeTick(time.Millisecond, func(time.Time) tea.Msg {
		panic("tick boom")
	})
	if cmd == nil {
		t.Fatal("SafeTick returned nil for a non-nil callback")
	}

	msg := cmd()
	errMsg, ok := asError(t, msg)
	if !ok {
		t.Fatalf("panicking tick returned %T, want messages.Error", msg)
	}
	if errMsg.Context != "tick" {
		t.Errorf("Context = %q, want %q", errMsg.Context, "tick")
	}
	if !errMsg.Logged {
		t.Error("recovered tick error should be marked Logged")
	}
	if got := errMsg.Err.Error(); got != "tick panic: tick boom" {
		t.Errorf("error text = %q, want %q", got, "tick panic: tick boom")
	}
}

func TestSafeTick_ZeroDurationStillFires(t *testing.T) {
	// A zero duration is a valid boundary: the timer fires immediately and the
	// callback must still run exactly once with a real timestamp.
	want := stringMsg("zero")
	cmd := SafeTick(0, func(time.Time) tea.Msg { return want })
	if cmd == nil {
		t.Fatal("SafeTick(0, fn) returned nil")
	}
	got, ok := cmd().(stringMsg)
	if !ok {
		t.Fatalf("zero-duration tick returned %T, want stringMsg", cmd())
	}
	if got != want {
		t.Errorf("zero-duration tick returned %q, want %q", got, want)
	}
}
