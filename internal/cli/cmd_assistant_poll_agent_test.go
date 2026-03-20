package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestAssistantPollAgentLoop_EmitsContentThenIdle(t *testing.T) {
	var buf bytes.Buffer
	capture := fakeCapture("hello", "hello")

	code := runAssistantPollAgentLoop(context.Background(), &buf, assistantPollAgentConfig{
		SessionName: "sess-1",
		Lines:       100,
		Interval:    time.Millisecond,
		Timeout:     time.Nanosecond,
	}, tmux.Options{}, capture)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	output := buf.String()
	if !strings.HasPrefix(output, "hello\n") {
		t.Fatalf("output = %q, want leading captured content", output)
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	var event assistantCompatEvent
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &event); err != nil {
		t.Fatalf("decode idle event: %v\nraw: %s", err, lines[len(lines)-1])
	}
	if event.Type != "idle" {
		t.Fatalf("event.Type = %q, want %q", event.Type, "idle")
	}
	if event.IdleSeconds < 0 {
		t.Fatalf("event.IdleSeconds = %d, want >= 0", event.IdleSeconds)
	}
}

func TestAssistantPollAgentLoop_EmitsExitedWhenSessionDisappears(t *testing.T) {
	origStateFor := tmuxSessionStateFor
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: false}, nil
	}
	defer func() { tmuxSessionStateFor = origStateFor }()

	var buf bytes.Buffer
	capture := fakeCapture("hello")

	code := runAssistantPollAgentLoop(context.Background(), &buf, assistantPollAgentConfig{
		SessionName: "sess-2",
		Lines:       100,
		Interval:    time.Millisecond,
		Timeout:     time.Hour,
	}, tmux.Options{}, capture)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	output := buf.String()
	if !strings.HasPrefix(output, "hello\n") {
		t.Fatalf("output = %q, want captured content prefix", output)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var event assistantCompatEvent
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &event); err != nil {
		t.Fatalf("decode exited event: %v\nraw: %s", err, lines[len(lines)-1])
	}
	if event.Type != "exited" {
		t.Fatalf("event.Type = %q, want %q", event.Type, "exited")
	}
}
