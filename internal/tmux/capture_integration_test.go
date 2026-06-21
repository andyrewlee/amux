package tmux

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCapturePane_ResolvesActivePaneID(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "cap-resolve", "sleep 300")

	// CapturePane should succeed (may return nil if no scrollback yet). Poll
	// until it resolves instead of guessing a fixed settle time.
	var err error
	if !eventually(5*time.Second, func() bool {
		_, err = CapturePane("cap-resolve", opts)
		return err == nil
	}) {
		t.Fatalf("CapturePane: %v", err)
	}
}

func TestCapturePaneTail_ResolvesActivePaneID(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "tail-resolve", "echo hello-tail; sleep 300")

	// Poll for the echo output rather than sleeping a fixed window.
	var text string
	if !eventually(5*time.Second, func() bool {
		out, ok := CapturePaneTail("tail-resolve", 10, opts)
		if ok {
			text = out
		}
		return ok && text != ""
	}) {
		t.Fatalf("expected a non-empty tail capture, got %q", text)
	}
}

func TestCapturePaneTail_CapturesDetachedSession(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	// createSession makes a detached session (new-session -d, no attached
	// client). The session-target fast path must capture its pane content; if
	// it can't, the pane-ID fallback must. Either way the known content returns.
	createSession(t, opts, "tail-detached", "echo detached-marker; sleep 300")

	var text string
	if !eventually(5*time.Second, func() bool {
		out, ok := CapturePaneTail("tail-detached", 10, opts)
		if ok {
			text = out
		}
		return ok && strings.Contains(text, "detached-marker")
	}) {
		t.Fatalf("expected detached-session tail to contain %q, got %q", "detached-marker", text)
	}
}

func TestCapturePaneTail_FallsBackFromDeadActivePane(t *testing.T) {
	opts := realTmuxServerWithKeepalive(t)

	createSession(t, opts, "tail-dead-active", "exec sh")
	args := tmuxArgs(opts, "set-option", "-t", "tail-dead-active", "remain-on-exit", "on")
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("set remain-on-exit: %v\n%s", err, out)
	}

	args = tmuxArgs(opts, "split-window", "-d", "-t", "tail-dead-active", "printf live-marker; sleep 300")
	cmd = exec.Command("tmux", args...)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("split-window: %v\n%s", err, out)
	}

	args = tmuxArgs(opts, "send-keys", "-t", "tail-dead-active:.0", "printf dead-marker; exit", "C-m")
	cmd = exec.Command("tmux", args...)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("send-keys: %v\n%s", err, out)
	}

	if !eventually(5*time.Second, func() bool {
		args := tmuxArgs(opts, "display-message", "-p", "-t", "tail-dead-active:.0", "#{pane_dead}")
		cmd := exec.Command("tmux", args...)
		out, err := cmd.Output()
		return err == nil && strings.TrimSpace(string(out)) == "1"
	}) {
		t.Fatal("timed out waiting for active pane to become dead")
	}

	var text string
	if !eventually(5*time.Second, func() bool {
		out, ok := CapturePaneTail("tail-dead-active", 10, opts)
		if ok {
			text = out
		}
		return ok && strings.Contains(text, "live-marker")
	}) {
		t.Fatalf("expected live fallback pane capture to contain %q, got %q", "live-marker", text)
	}
	if strings.Contains(text, "dead-marker") {
		t.Fatalf("expected dead active pane to be ignored, got %q", text)
	}
}

func TestSessionPaneID_ResolvesForDetachedSession(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "pane-id-detached", "sleep 300")

	var (
		paneID string
		err    error
	)
	eventually(5*time.Second, func() bool {
		paneID, err = sessionPaneID("pane-id-detached", opts)
		return err == nil && paneID != ""
	})
	if err != nil {
		t.Fatalf("sessionPaneID: %v", err)
	}
	if paneID == "" || paneID[0] != '%' {
		t.Fatalf("expected pane ID with %% prefix, got %q", paneID)
	}
}

func TestCapturePane_PrefixCollisionSafety(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	// Create two sessions with prefix-colliding names
	createSession(t, opts, "cap-1", "echo cap-1-content; sleep 300")
	createSession(t, opts, "cap-10", "echo cap-10-content; sleep 300")

	// Capture from cap-1 should only get cap-1's content, not cap-10's. Poll for
	// the capture instead of a fixed settle wait.
	var text string
	if !eventually(5*time.Second, func() bool {
		out, ok := CapturePaneTail("cap-1", 10, opts)
		if ok {
			text = out
		}
		return ok && text != ""
	}) {
		t.Fatalf("expected non-empty capture for cap-1, got %q", text)
	}
}

func TestCapturePaneSnapshot_PreservesTrailingSpaces(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "snapshot-trailing-spaces", "printf 'pad   '; sleep 300")
	time.Sleep(200 * time.Millisecond)

	snapshot, err := CapturePaneSnapshot("snapshot-trailing-spaces", opts)
	if err != nil {
		t.Fatalf("CapturePaneSnapshot: %v", err)
	}
	if !strings.Contains(string(snapshot.Data), "pad   ") {
		t.Fatalf("expected trailing spaces to be preserved in snapshot, got %q", snapshot.Data)
	}
}

func TestCapturePane_PreservesTrailingSpaces(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(
		t,
		opts,
		"capture-trailing-spaces",
		`i=1; printf 'pad   \n'; while [ "$i" -le 80 ]; do printf 'line%02d\n' "$i"; i=$((i+1)); done; sleep 300`,
	)
	time.Sleep(200 * time.Millisecond)

	scrollback, err := CapturePane("capture-trailing-spaces", opts)
	if err != nil {
		t.Fatalf("CapturePane: %v", err)
	}
	if !strings.Contains(string(scrollback), "pad   ") {
		t.Fatalf("expected trailing spaces to be preserved in scrollback capture, got %q", scrollback)
	}
}

func TestCapturePaneSnapshot_RejectsMultiPaneWindow(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "snapshot-multi-pane", "printf left; sleep 300")

	args := tmuxArgs(opts, "split-window", "-d", "-t", "snapshot-multi-pane", "printf right; sleep 300")
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("split-window: %v\n%s", err, out)
	}
	time.Sleep(200 * time.Millisecond)

	_, err = CapturePaneSnapshot("snapshot-multi-pane", opts)
	if !errors.Is(err, errPaneSnapshotNotWholeWindow) {
		t.Fatalf("expected multi-pane snapshot rejection, got %v", err)
	}
}

func TestCapturePaneSnapshot_RejectsZoomedSplitWindow(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "snapshot-zoomed-pane", "printf left; sleep 300")

	args := tmuxArgs(opts, "split-window", "-d", "-t", "snapshot-zoomed-pane", "printf right; sleep 300")
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("split-window: %v\n%s", err, out)
	}

	args = tmuxArgs(opts, "resize-pane", "-Z", "-t", "snapshot-zoomed-pane")
	cmd = exec.Command("tmux", args...)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("resize-pane -Z: %v\n%s", err, out)
	}
	time.Sleep(200 * time.Millisecond)

	_, err = CapturePaneSnapshot("snapshot-zoomed-pane", opts)
	if !errors.Is(err, errPaneSnapshotNotWholeWindow) {
		t.Fatalf("expected zoomed split pane snapshot rejection, got %v", err)
	}
}

func TestCapturePaneSnapshot_MissingSessionReturnsUnavailableError(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	_, err := CapturePaneSnapshot("snapshot-missing-session", opts)
	if !errors.Is(err, errPaneSnapshotUnavailable) {
		t.Fatalf("expected missing session snapshot to fail with unavailable error, got %v", err)
	}
}

func TestResizePaneToSize_ResizesDetachedWindow(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "resize-detached-window", "sleep 300")
	time.Sleep(100 * time.Millisecond)

	if err := ResizePaneToSize("resize-detached-window", 91, 27, opts); err != nil {
		t.Fatalf("ResizePaneToSize: %v", err)
	}

	args := tmuxArgs(opts, "display-message", "-p", "-t", "resize-detached-window", "#{window_width}\t#{window_height}")
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("display-message: %v\n%s", err, out)
	}

	parts := strings.Split(strings.TrimSpace(string(out)), "\t")
	if len(parts) != 2 {
		t.Fatalf("unexpected window size output %q", out)
	}
	gotWidth, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		t.Fatalf("parse window width: %v", err)
	}
	gotHeight, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		t.Fatalf("parse window height: %v", err)
	}
	if gotWidth != 91 || gotHeight != 27 {
		t.Fatalf("expected detached window size 91x27 after resize, got %dx%d", gotWidth, gotHeight)
	}
}
