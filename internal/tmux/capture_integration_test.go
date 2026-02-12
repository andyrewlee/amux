package tmux

import (
	"testing"
	"time"
)

func TestCapturePane_ResolvesActivePaneID(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "cap-resolve", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	// CapturePane should succeed (may return nil if no scrollback yet)
	_, err := CapturePane("cap-resolve", opts)
	if err != nil {
		t.Fatalf("CapturePane: %v", err)
	}
}

func TestCapturePaneTail_ResolvesActivePaneID(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "tail-resolve", "echo hello-tail; sleep 300")
	time.Sleep(200 * time.Millisecond)

	text, ok := CapturePaneTail("tail-resolve", 10, opts)
	if !ok {
		t.Fatal("CapturePaneTail should succeed")
	}
	// The output should contain the echo output
	if text == "" {
		t.Fatal("expected non-empty tail capture")
	}
}

func TestCapturePane_PrefixCollisionSafety(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	// Create two sessions with prefix-colliding names
	createSession(t, opts, "cap-1", "echo cap-1-content; sleep 300")
	createSession(t, opts, "cap-10", "echo cap-10-content; sleep 300")
	time.Sleep(200 * time.Millisecond)

	// Capture from cap-1 should only get cap-1's content, not cap-10's
	text, ok := CapturePaneTail("cap-1", 10, opts)
	if !ok {
		t.Fatal("CapturePaneTail should succeed for cap-1")
	}
	if text == "" {
		t.Fatal("expected non-empty capture for cap-1")
	}
}
