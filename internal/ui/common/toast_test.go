package common

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestNewToastModel(t *testing.T) {
	m := NewToastModel()
	if m == nil {
		t.Fatal("NewToastModel returned nil")
	}
	if m.current != nil {
		t.Errorf("expected no current toast on a fresh model, got %+v", m.current)
	}
	if m.Visible() {
		t.Error("a fresh toast model should not be visible")
	}
	if got := m.View(); got != "" {
		t.Errorf("fresh model View() = %q, want empty", got)
	}
	// The model should be initialized with the default styles so View() can
	// render without a nil-style panic.
	defaultToast := DefaultStyles().ToastInfo
	if m.styles.ToastInfo.String() != defaultToast.String() {
		t.Error("NewToastModel did not initialize styles with DefaultStyles")
	}
}

func TestSetStyles(t *testing.T) {
	m := NewToastModel()
	custom := DefaultStyles()
	custom.ToastInfo = custom.ToastInfo.Bold(true)

	m.SetStyles(custom)
	if m.styles.ToastInfo.String() != custom.ToastInfo.String() {
		t.Error("SetStyles did not update the model styles")
	}
}

func TestShowSetsStateAndReturnsCmd(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		toastType ToastType
		duration  time.Duration
	}{
		{"info", "hello", ToastInfo, time.Second},
		{"success", "done", ToastSuccess, 2 * time.Second},
		{"error", "boom", ToastError, 3 * time.Second},
		{"warning", "careful", ToastWarning, 4 * time.Second},
		{"empty message", "", ToastInfo, time.Second},
		{"zero duration", "instant", ToastInfo, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewToastModel()
			before := time.Now()
			cmd := m.Show(tt.message, tt.toastType, tt.duration)

			if m.current == nil {
				t.Fatal("Show did not set the current toast")
			}
			if m.current.Message != tt.message {
				t.Errorf("Message = %q, want %q", m.current.Message, tt.message)
			}
			if m.current.Type != tt.toastType {
				t.Errorf("Type = %v, want %v", m.current.Type, tt.toastType)
			}
			if m.current.Duration != tt.duration {
				t.Errorf("Duration = %v, want %v", m.current.Duration, tt.duration)
			}

			// showUntil should be set to roughly now+duration.
			wantMin := before.Add(tt.duration)
			if m.showUntil.Before(wantMin) {
				t.Errorf("showUntil = %v, want >= %v", m.showUntil, wantMin)
			}

			if cmd == nil {
				t.Fatal("Show returned a nil command")
			}
		})
	}
}

// TestShowCommandYieldsDismissed drives the command returned by Show to make
// sure it eventually resolves to a ToastDismissed message. A tiny duration is
// used so the underlying tea.Tick fires almost immediately.
func TestShowCommandYieldsDismissed(t *testing.T) {
	m := NewToastModel()
	cmd := m.Show("blip", ToastInfo, time.Millisecond)
	if cmd == nil {
		t.Fatal("Show returned a nil command")
	}
	if _, ok := runCmdForDismissed(t, cmd); !ok {
		t.Error("Show command did not produce a ToastDismissed message")
	}
}

func TestShowHelpers(t *testing.T) {
	tests := []struct {
		name         string
		call         func(*ToastModel, string) tea.Cmd
		wantType     ToastType
		wantDuration time.Duration
	}{
		{"ShowSuccess", (*ToastModel).ShowSuccess, ToastSuccess, 3 * time.Second},
		{"ShowError", (*ToastModel).ShowError, ToastError, 5 * time.Second},
		{"ShowInfo", (*ToastModel).ShowInfo, ToastInfo, 3 * time.Second},
		{"ShowWarning", (*ToastModel).ShowWarning, ToastWarning, 4 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewToastModel()
			cmd := tt.call(m, "msg-"+tt.name)

			if cmd == nil {
				t.Fatal("helper returned a nil command")
			}
			if m.current == nil {
				t.Fatal("helper did not set the current toast")
			}
			if m.current.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", m.current.Type, tt.wantType)
			}
			if m.current.Message != "msg-"+tt.name {
				t.Errorf("Message = %q, want %q", m.current.Message, "msg-"+tt.name)
			}
			if m.current.Duration != tt.wantDuration {
				t.Errorf("Duration = %v, want %v", m.current.Duration, tt.wantDuration)
			}
			if !m.Visible() {
				t.Error("toast should be visible immediately after a Show helper")
			}
		})
	}
}

func TestUpdateDismissAfterExpiry(t *testing.T) {
	m := NewToastModel()
	// A toast whose window has already closed.
	m.current = &Toast{Message: "old", Type: ToastInfo, Duration: time.Millisecond}
	m.showUntil = time.Now().Add(-time.Second)

	got, cmd := m.Update(ToastDismissed{})
	if got != m {
		t.Error("Update should return the same model pointer")
	}
	if cmd != nil {
		t.Errorf("Update returned a non-nil command: %v", cmd)
	}
	if m.current != nil {
		t.Error("expired toast should be cleared on ToastDismissed")
	}
}

func TestUpdateKeepsUnexpiredToast(t *testing.T) {
	m := NewToastModel()
	m.current = &Toast{Message: "fresh", Type: ToastInfo, Duration: time.Hour}
	m.showUntil = time.Now().Add(time.Hour)

	m.Update(ToastDismissed{})
	if m.current == nil {
		t.Error("an unexpired toast must not be cleared by a stale ToastDismissed")
	}
}

func TestUpdateIgnoresUnknownMessage(t *testing.T) {
	m := NewToastModel()
	m.current = &Toast{Message: "stay", Type: ToastInfo, Duration: time.Hour}
	m.showUntil = time.Now().Add(time.Hour)

	got, cmd := m.Update(struct{}{})
	if got != m {
		t.Error("Update should return the same model pointer")
	}
	if cmd != nil {
		t.Error("Update should not emit a command for unknown messages")
	}
	if m.current == nil {
		t.Error("unrelated messages must not clear the current toast")
	}
}

func TestViewEmptyWhenNoToast(t *testing.T) {
	m := NewToastModel()
	if got := m.View(); got != "" {
		t.Errorf("View() = %q, want empty string when no toast", got)
	}
}

func TestViewRendersByType(t *testing.T) {
	tests := []struct {
		name      string
		toastType ToastType
		message   string
		wantIcon  string
	}{
		{"success", ToastSuccess, "saved", Icons.Clean},
		{"error", ToastError, "failed", Icons.Dirty},
		{"warning", ToastWarning, "watch out", "!"},
		{"info", ToastInfo, "fyi", "i "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewToastModel()
			m.Show(tt.message, tt.toastType, time.Hour)

			out := m.View()
			if out == "" {
				t.Fatal("View() returned empty for a visible toast")
			}
			if !strings.Contains(out, tt.message) {
				t.Errorf("View() = %q, want it to contain message %q", out, tt.message)
			}
			if !strings.Contains(out, tt.wantIcon) {
				t.Errorf("View() = %q, want it to contain icon %q", out, tt.wantIcon)
			}
		})
	}
}

func TestViewClearsExpiredToast(t *testing.T) {
	m := NewToastModel()
	m.current = &Toast{Message: "gone", Type: ToastInfo, Duration: time.Millisecond}
	m.showUntil = time.Now().Add(-time.Second)

	if got := m.View(); got != "" {
		t.Errorf("View() = %q, want empty for an expired toast", got)
	}
	if m.current != nil {
		t.Error("View() should clear an expired toast as a side effect")
	}
}

func TestVisible(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*ToastModel)
		wantVisib bool
	}{
		{
			name:      "no toast",
			setup:     func(*ToastModel) {},
			wantVisib: false,
		},
		{
			name: "active toast",
			setup: func(m *ToastModel) {
				m.current = &Toast{Message: "x", Type: ToastInfo}
				m.showUntil = time.Now().Add(time.Hour)
			},
			wantVisib: true,
		},
		{
			name: "expired toast",
			setup: func(m *ToastModel) {
				m.current = &Toast{Message: "x", Type: ToastInfo}
				m.showUntil = time.Now().Add(-time.Hour)
			},
			wantVisib: false,
		},
		{
			name: "current set but window already closed at boundary",
			setup: func(m *ToastModel) {
				m.current = &Toast{Message: "x", Type: ToastInfo}
				m.showUntil = time.Now()
			},
			wantVisib: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewToastModel()
			tt.setup(m)
			if got := m.Visible(); got != tt.wantVisib {
				t.Errorf("Visible() = %v, want %v", got, tt.wantVisib)
			}
		})
	}
}

func TestDismiss(t *testing.T) {
	m := NewToastModel()
	m.Show("bye", ToastInfo, time.Hour)
	if !m.Visible() {
		t.Fatal("toast should be visible before Dismiss")
	}

	m.Dismiss()
	if m.current != nil {
		t.Error("Dismiss should clear the current toast")
	}
	if m.Visible() {
		t.Error("toast should not be visible after Dismiss")
	}
	if got := m.View(); got != "" {
		t.Errorf("View() = %q, want empty after Dismiss", got)
	}
}

func TestDismissIsIdempotent(t *testing.T) {
	m := NewToastModel()
	m.Dismiss()
	m.Dismiss()
	if m.current != nil {
		t.Error("Dismiss on an empty model should remain a no-op")
	}
}

// runCmdForDismissed invokes a tea.Cmd (and any nested batch) and reports
// whether a ToastDismissed message was produced. It bounds execution time so a
// misbehaving command cannot hang the test suite.
func runCmdForDismissed(t *testing.T, cmd tea.Cmd) (tea.Msg, bool) {
	t.Helper()
	if cmd == nil {
		return nil, false
	}

	done := make(chan tea.Msg, 1)
	go func() {
		done <- cmd()
	}()

	select {
	case msg := <-done:
		if _, ok := msg.(ToastDismissed); ok {
			return msg, true
		}
		return msg, false
	case <-time.After(2 * time.Second):
		t.Fatal("toast command did not return within the timeout")
		return nil, false
	}
}
