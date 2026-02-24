package center

import (
	"testing"
	"time"
)

func TestShouldSuppressLocalEchoInput(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{name: "typing char", data: "a", want: true},
		{name: "typing utf8 latin", data: "é", want: true},
		{name: "typing utf8 cjk", data: "你", want: true},
		{name: "space", data: " ", want: true},
		{name: "backspace", data: "\x7f", want: true},
		{name: "tab", data: "\t", want: true},
		{name: "enter", data: "\r", want: false},
		{name: "escape seq", data: "\x1b[A", want: false},
		{name: "bracketed paste", data: "\x1b[200~hello\x1b[201~", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSuppressLocalEchoInput(tt.data); got != tt.want {
				t.Fatalf("shouldSuppressLocalEchoInput(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

func TestRecordLocalInputEchoWindow_TypingSetsButEnterClears(t *testing.T) {
	tab := &Tab{}
	now := time.Now()

	recordLocalInputEchoWindow(tab, "x", now)
	tab.mu.Lock()
	first := tab.lastUserInputAt
	tab.mu.Unlock()
	if first.IsZero() {
		t.Fatal("expected typing input to set lastUserInputAt")
	}

	recordLocalInputEchoWindow(tab, "\r", now.Add(100*time.Millisecond))
	tab.mu.Lock()
	second := tab.lastUserInputAt
	tab.mu.Unlock()
	if !second.IsZero() {
		t.Fatal("expected enter input to clear lastUserInputAt")
	}
}

func TestRecordLocalInputEchoWindow_ClearsBootstrapPhaseOnUserInput(t *testing.T) {
	tab := &Tab{
		bootstrapActivity:     true,
		bootstrapLastOutputAt: time.Now(),
	}

	recordLocalInputEchoWindow(tab, "x", time.Now())
	tab.mu.Lock()
	bootstrap := tab.bootstrapActivity
	last := tab.bootstrapLastOutputAt
	tab.mu.Unlock()
	if bootstrap {
		t.Fatal("expected user input to exit bootstrap phase")
	}
	if !last.IsZero() {
		t.Fatal("expected bootstrapLastOutputAt cleared when exiting bootstrap phase")
	}
}
