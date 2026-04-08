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

func TestSessionPaneID_ResolvesForDetachedSession(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "pane-id-detached", "sleep 300")
	time.Sleep(100 * time.Millisecond)

	paneID, err := sessionPaneID("pane-id-detached", opts)
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

func TestPaneCursorPosition_TargetsExactSplitPane(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "pane-cursor-split", "printf 1111; sleep 300")

	args := tmuxArgs(opts, "split-window", "-d", "-t", "pane-cursor-split", "printf 22222222; sleep 300")
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("split-window: %v\n%s", err, out)
	}
	time.Sleep(200 * time.Millisecond)

	args = tmuxArgs(opts, "list-panes", "-t", "pane-cursor-split", "-F", "#{pane_id}\t#{cursor_x}\t#{cursor_y}")
	cmd = exec.Command("tmux", args...)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list-panes: %v\n%s", err, out)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected split layout with 2 panes, got %q", out)
	}
	first := strings.Split(lines[0], "\t")
	second := strings.Split(lines[1], "\t")
	if len(first) < 3 || len(second) < 3 {
		t.Fatalf("unexpected pane listing: %q", out)
	}
	if first[1] == second[1] && first[2] == second[2] {
		t.Fatalf("expected distinct cursor positions across panes, got %q", out)
	}

	wantX, err := strconv.Atoi(strings.TrimSpace(second[1]))
	if err != nil {
		t.Fatalf("parse expected cursor_x: %v", err)
	}
	wantY, err := strconv.Atoi(strings.TrimSpace(second[2]))
	if err != nil {
		t.Fatalf("parse expected cursor_y: %v", err)
	}

	gotX, gotY, ok, err := paneCursorPosition(strings.TrimSpace(second[0]), opts)
	if err != nil {
		t.Fatalf("paneCursorPosition: %v", err)
	}
	if !ok {
		t.Fatal("expected pane cursor position to resolve")
	}
	if gotX != wantX || gotY != wantY {
		t.Fatalf("expected cursor (%d,%d) for pane %s, got (%d,%d)", wantX, wantY, strings.TrimSpace(second[0]), gotX, gotY)
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
