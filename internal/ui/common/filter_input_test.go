package common

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func keyRune(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func TestFilterInputStartActivatesAndFocuses(t *testing.T) {
	f := NewFilterInput()
	if f.Active() {
		t.Fatal("filter should start inactive")
	}
	if cmd := f.Start(); cmd == nil {
		t.Fatal("Start should return the input focus (blink) command")
	}
	if !f.Active() {
		t.Fatal("Start should enter input mode")
	}
	if !f.Focused() {
		t.Fatal("Start should focus the input")
	}
}

func TestFilterInputTypedKeysUpdateQuery(t *testing.T) {
	f := NewFilterInput()
	f.Start()
	for _, r := range "api" {
		_, handled, changed := f.Update(keyRune(r))
		if !handled {
			t.Fatalf("key %q should be handled while filtering", r)
		}
		if !changed {
			t.Fatalf("key %q should report a query change", r)
		}
	}
	if got := f.Query(); got != "api" {
		t.Fatalf("Query() = %q, want %q", got, "api")
	}
	if !f.Active() {
		t.Fatal("typing should keep input mode")
	}
}

func TestFilterInputDigitKeysAreCaptured(t *testing.T) {
	f := NewFilterInput()
	f.Start()
	// The whole point of input mode: digit keys type into the filter rather
	// than acting as jump/tab bindings on the owning surface.
	if _, handled, _ := f.Update(keyRune('2')); !handled {
		t.Fatal("digit key should be captured by the filter")
	}
	if got := f.Query(); got != "2" {
		t.Fatalf("Query() = %q, want %q", got, "2")
	}
}

func TestFilterInputEnterExitsKeepingQuery(t *testing.T) {
	f := NewFilterInput()
	f.Start()
	f.Update(keyRune('x'))
	_, handled, changed := f.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("enter should be handled")
	}
	if changed {
		t.Fatal("enter should not report a query change — it keeps the query")
	}
	if f.Active() {
		t.Fatal("enter should exit input mode")
	}
	if got := f.Query(); got != "x" {
		t.Fatalf("Query() = %q after enter, want %q", got, "x")
	}
	if f.Focused() {
		t.Fatal("enter should blur the input")
	}
}

func TestFilterInputEscClearsQueryAndExits(t *testing.T) {
	f := NewFilterInput()
	f.Start()
	f.Update(keyRune('x'))
	_, handled, changed := f.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !handled {
		t.Fatal("esc should be handled")
	}
	if !changed {
		t.Fatal("esc should report a change — the list must un-filter")
	}
	if f.Active() {
		t.Fatal("esc should exit input mode")
	}
	if got := f.Query(); got != "" {
		t.Fatalf("Query() = %q after esc, want empty", got)
	}
	if got := f.Value(); got != "" {
		t.Fatalf("input value = %q after esc, want empty", got)
	}
}

func TestFilterInputExitKeepPreservesQueryAndStaleValue(t *testing.T) {
	f := NewFilterInput()
	f.Start()
	f.Update(keyRune('q'))
	f.ExitKeep()
	if f.Active() {
		t.Fatal("ExitKeep should leave input mode")
	}
	if got := f.Query(); got != "q" {
		t.Fatalf("Query() = %q after ExitKeep, want %q", got, "q")
	}
	// The input buffer is untouched — matches the pre-widget blur contract
	// where a stale value could linger.
	if got := f.Value(); got != "q" {
		t.Fatalf("Value() = %q after ExitKeep, want %q", got, "q")
	}
}

func TestFilterInputClearDropsEverything(t *testing.T) {
	f := NewFilterInput()
	f.Start()
	f.Update(keyRune('q'))
	f.Clear()
	if f.Active() {
		t.Fatal("Clear should exit input mode")
	}
	if f.Query() != "" || f.Value() != "" {
		t.Fatalf("Clear should drop query and value, got %q/%q", f.Query(), f.Value())
	}
	if f.Focused() {
		t.Fatal("Clear should blur the input")
	}
}

func TestFilterInputIgnoresNonKeyMessages(t *testing.T) {
	f := NewFilterInput()
	f.Start()
	if _, handled, _ := f.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown}); handled {
		t.Fatal("non-key messages must pass through — mouse scroll still works while filtering")
	}
}

func TestFilterInputInactiveUpdateDoesNothing(t *testing.T) {
	f := NewFilterInput()
	if _, handled, _ := f.Update(keyRune('x')); handled {
		t.Fatal("inactive filter must not consume keys")
	}
}
