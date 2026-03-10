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
		{name: "shift tab", data: "\x1b[Z", want: true},
		{name: "ctrl a", data: "\x01", want: true},
		{name: "ctrl c", data: "\x03", want: false},
		{name: "ctrl d", data: "\x04", want: false},
		{name: "ctrl e", data: "\x05", want: true},
		{name: "ctrl k", data: "\x0b", want: true},
		{name: "ctrl u", data: "\x15", want: true},
		{name: "enter", data: "\r", want: false},
		{name: "arrow up csi", data: "\x1b[A", want: true},
		{name: "arrow with modifier csi", data: "\x1b[1;5C", want: true},
		{name: "home ss3", data: "\x1bOH", want: true},
		{name: "raw escape", data: "\x1b", want: false},
		{name: "alt b", data: "\x1bb", want: false},
		{name: "alt f", data: "\x1bf", want: false},
		{name: "non-editing escape seq", data: "\x1b]0;title\x07", want: false},
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

func TestRecordLocalInputEchoWindow_TypingSetsButEnterKeepsPromptWindowOnly(t *testing.T) {
	tab := &Tab{}
	now := time.Now()

	recordLocalInputEchoWindow(tab, "x", now)
	tab.mu.Lock()
	firstEcho := tab.lastUserInputAt
	firstPrompt := tab.lastPromptInputAt
	tab.mu.Unlock()
	if firstEcho.IsZero() {
		t.Fatal("expected typing input to set lastUserInputAt")
	}
	if firstPrompt.IsZero() {
		t.Fatal("expected typing input to set lastPromptInputAt")
	}

	recordLocalInputEchoWindow(tab, "\r", now.Add(100*time.Millisecond))
	tab.mu.Lock()
	secondEcho := tab.lastUserInputAt
	secondPrompt := tab.lastPromptInputAt
	secondSubmit := tab.lastPromptSubmitAt
	tab.mu.Unlock()
	if !secondEcho.IsZero() {
		t.Fatal("expected enter input to clear lastUserInputAt")
	}
	if secondPrompt.IsZero() {
		t.Fatal("expected enter input to set lastPromptInputAt")
	}
	if secondSubmit.IsZero() {
		t.Fatal("expected enter input to set lastPromptSubmitAt")
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

func TestRecordLocalInputEchoWindow_ArrowKeySetsWindow(t *testing.T) {
	tab := &Tab{}
	now := time.Now()

	recordLocalInputEchoWindow(tab, "\x1b[D", now)
	tab.mu.Lock()
	echoStamp := tab.lastUserInputAt
	promptStamp := tab.lastPromptInputAt
	tab.mu.Unlock()
	if echoStamp.IsZero() {
		t.Fatal("expected arrow-key input to set lastUserInputAt")
	}
	if promptStamp.IsZero() {
		t.Fatal("expected arrow-key input to set lastPromptInputAt")
	}
}

func TestRecordLocalInputEchoWindow_ShiftTabSetsWindow(t *testing.T) {
	tab := &Tab{}
	now := time.Now()

	recordLocalInputEchoWindow(tab, "\x1b[Z", now)
	tab.mu.Lock()
	echoStamp := tab.lastUserInputAt
	promptStamp := tab.lastPromptInputAt
	tab.mu.Unlock()
	if echoStamp.IsZero() {
		t.Fatal("expected Shift-Tab input to set lastUserInputAt")
	}
	if promptStamp.IsZero() {
		t.Fatal("expected Shift-Tab input to set lastPromptInputAt")
	}
}

func TestRecordLocalInputEchoWindow_ReadlineControlSetsBothWindows(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "ctrl a", data: "\x01"},
		{name: "ctrl e", data: "\x05"},
		{name: "ctrl k", data: "\x0b"},
		{name: "ctrl u", data: "\x15"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tab := &Tab{}
			now := time.Now()

			recordLocalInputEchoWindow(tab, tt.data, now)
			tab.mu.Lock()
			echoStamp := tab.lastUserInputAt
			promptStamp := tab.lastPromptInputAt
			tab.mu.Unlock()
			if echoStamp.IsZero() {
				t.Fatalf("expected %s input to set lastUserInputAt", tt.name)
			}
			if promptStamp.IsZero() {
				t.Fatalf("expected %s input to set lastPromptInputAt", tt.name)
			}
		})
	}
}

func TestRecordLocalInputEchoWindow_CtrlDDoesNotSetWindows(t *testing.T) {
	tab := &Tab{}
	now := time.Now()

	recordLocalInputEchoWindow(tab, "\x04", now)
	tab.mu.Lock()
	echoStamp := tab.lastUserInputAt
	promptStamp := tab.lastPromptInputAt
	tab.mu.Unlock()
	if !echoStamp.IsZero() {
		t.Fatal("expected Ctrl-D not to set lastUserInputAt")
	}
	if !promptStamp.IsZero() {
		t.Fatal("expected Ctrl-D not to set lastPromptInputAt")
	}
}

func TestRecordLocalInputEchoWindow_CtrlCSetsPromptWindowOnly(t *testing.T) {
	tab := &Tab{}
	now := time.Now()

	recordLocalInputEchoWindow(tab, "\x03", now)
	tab.mu.Lock()
	echoStamp := tab.lastUserInputAt
	promptStamp := tab.lastPromptInputAt
	submitStamp := tab.lastPromptSubmitAt
	tab.mu.Unlock()
	if !echoStamp.IsZero() {
		t.Fatal("expected Ctrl-C not to set lastUserInputAt")
	}
	if promptStamp.IsZero() {
		t.Fatal("expected Ctrl-C to set lastPromptInputAt")
	}
	if submitStamp.IsZero() {
		t.Fatal("expected Ctrl-C to set lastPromptSubmitAt")
	}
}

func TestRecordLocalInputEchoWindow_ReadlineEscapeSetsBothWindows(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "escape", data: "\x1b"},
		{name: "alt b", data: "\x1bb"},
		{name: "alt f", data: "\x1bf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tab := &Tab{}
			now := time.Now()

			recordLocalInputEchoWindow(tab, tt.data, now)
			tab.mu.Lock()
			echoStamp := tab.lastUserInputAt
			promptStamp := tab.lastPromptInputAt
			tab.mu.Unlock()
			if echoStamp.IsZero() {
				t.Fatalf("expected %s input to set lastUserInputAt", tt.name)
			}
			if promptStamp.IsZero() {
				t.Fatalf("expected %s input to set lastPromptInputAt", tt.name)
			}
		})
	}
}

func TestRecordLocalInputEchoWindow_BracketedPasteWithoutSubmitSetsBothWindows(t *testing.T) {
	tab := &Tab{}
	now := time.Now()

	recordLocalInputEchoWindow(tab, "\x1b[200~hello\x1b[201~", now)
	tab.mu.Lock()
	echoStamp := tab.lastUserInputAt
	promptStamp := tab.lastPromptInputAt
	tab.mu.Unlock()
	if echoStamp.IsZero() {
		t.Fatal("expected prompt-only bracketed paste to set lastUserInputAt")
	}
	if promptStamp.IsZero() {
		t.Fatal("expected bracketed paste to set lastPromptInputAt")
	}
}

func TestRecordLocalInputEchoWindow_BracketedPasteWithSubmitSetsPromptWindowOnly(t *testing.T) {
	tab := &Tab{}
	now := time.Now()

	recordLocalInputEchoWindow(tab, "\x1b[200~hello\r\x1b[201~", now)
	tab.mu.Lock()
	echoStamp := tab.lastUserInputAt
	promptStamp := tab.lastPromptInputAt
	submitStamp := tab.lastPromptSubmitAt
	tab.mu.Unlock()
	if !echoStamp.IsZero() {
		t.Fatal("expected submit bracketed paste not to set lastUserInputAt")
	}
	if promptStamp.IsZero() {
		t.Fatal("expected submit bracketed paste to set lastPromptInputAt")
	}
	if submitStamp.IsZero() {
		t.Fatal("expected submit bracketed paste to set lastPromptSubmitAt")
	}
}

func TestRecordLocalInputEchoWindow_SubmittedPasteTracksAndClearsPendingEcho(t *testing.T) {
	tab := &Tab{}
	now := time.Now()

	recordLocalInputEchoWindow(tab, "\x1b[200~hello\r\nworld\r\x1b[201~", now)
	tab.mu.Lock()
	pending := tab.pendingSubmitPasteEcho
	tab.mu.Unlock()
	if pending != "hello\nworld" {
		t.Fatalf("expected submitted paste echo to be normalized and tracked, got %q", pending)
	}

	recordLocalInputEchoWindow(tab, "x", now.Add(time.Millisecond))
	tab.mu.Lock()
	pending = tab.pendingSubmitPasteEcho
	echoStamp := tab.lastUserInputAt
	submitStamp := tab.lastPromptSubmitAt
	tab.mu.Unlock()
	if pending != "" {
		t.Fatalf("expected non-submit input to clear stale pending paste echo, got %q", pending)
	}
	if echoStamp.IsZero() {
		t.Fatal("expected typing input to set lastUserInputAt after clearing stale paste state")
	}
	if !submitStamp.IsZero() {
		t.Fatal("expected non-submit input to clear lastPromptSubmitAt")
	}
}
