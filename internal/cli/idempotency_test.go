package cli

import (
	"bytes"
	"testing"
	"time"
)

func TestIdempotencyReplaySuccessEnvelope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setResponseContext("req-1", "agent run")
	defer clearResponseContext()

	var first bytes.Buffer
	var firstErr bytes.Buffer
	code := returnJSONSuccessWithIdempotency(
		&first,
		&firstErr,
		GlobalFlags{JSON: true},
		"test-v1",
		"agent.run",
		"idem-1",
		map[string]any{"session_name": "amux-ws-tab"},
	)
	if code != ExitOK {
		t.Fatalf("first write code = %d, want %d", code, ExitOK)
	}
	if firstErr.Len() != 0 {
		t.Fatalf("expected no stderr warnings, got %q", firstErr.String())
	}

	var replay bytes.Buffer
	var replayErr bytes.Buffer
	handled, replayCode := maybeReplayIdempotentResponse(
		&replay,
		&replayErr,
		GlobalFlags{JSON: true},
		"test-v1",
		"agent.run",
		"idem-1",
	)
	if !handled {
		t.Fatalf("expected replay hit")
	}
	if replayCode != ExitOK {
		t.Fatalf("replay code = %d, want %d", replayCode, ExitOK)
	}
	if got, want := replay.String(), first.String(); got != want {
		t.Fatalf("replayed envelope mismatch:\n got: %s\nwant: %s", got, want)
	}
	if replayErr.Len() != 0 {
		t.Fatalf("expected no stderr replay warnings, got %q", replayErr.String())
	}
}

func TestIdempotencyReplayMissByCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	setResponseContext("req-1", "agent run")
	defer clearResponseContext()

	var out bytes.Buffer
	var errOut bytes.Buffer
	_ = returnJSONSuccessWithIdempotency(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		"test-v1",
		"agent.run",
		"idem-2",
		map[string]any{"session_name": "amux-ws-tab"},
	)

	var replay bytes.Buffer
	handled, _ := maybeReplayIdempotentResponse(
		&replay,
		&errOut,
		GlobalFlags{JSON: true},
		"test-v1",
		"workspace.create",
		"idem-2",
	)
	if handled {
		t.Fatalf("expected replay miss for different command")
	}
	if replay.Len() != 0 {
		t.Fatalf("expected no replay output, got %q", replay.String())
	}
}

func TestIdempotencyRequiresJSON(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	handled, code := maybeReplayIdempotentResponse(
		&out,
		&errOut,
		GlobalFlags{JSON: false},
		"test-v1",
		"agent.run",
		"idem-3",
	)
	if !handled {
		t.Fatalf("expected non-json guard to handle request")
	}
	if code != ExitUsage {
		t.Fatalf("code = %d, want %d", code, ExitUsage)
	}
	if errOut.Len() == 0 {
		t.Fatalf("expected human-readable guard message")
	}
}

func TestIdempotencyReplaySkipsExpiredEntries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := newIdempotencyStore()
	if err != nil {
		t.Fatalf("newIdempotencyStore() error = %v", err)
	}
	if err := store.store("agent.run", "expired-key", ExitOK, []byte("{\"ok\":true}\n")); err != nil {
		t.Fatalf("store.store() error = %v", err)
	}

	lockFile, err := lockIdempotencyFile(store.lockPath(), false)
	if err != nil {
		t.Fatalf("lockIdempotencyFile() error = %v", err)
	}
	state, err := store.loadState()
	if err != nil {
		unlockIdempotencyFile(lockFile)
		t.Fatalf("store.loadState() error = %v", err)
	}
	entryKey := store.entryKey("agent.run", "expired-key")
	entry := state.Entries[entryKey]
	entry.CreatedAt = time.Now().Add(-idempotencyRetention - time.Hour).Unix()
	state.Entries[entryKey] = entry
	if err := store.saveState(state); err != nil {
		unlockIdempotencyFile(lockFile)
		t.Fatalf("store.saveState() error = %v", err)
	}
	unlockIdempotencyFile(lockFile)

	var out bytes.Buffer
	var errOut bytes.Buffer
	handled, code := maybeReplayIdempotentResponse(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		"test-v1",
		"agent.run",
		"expired-key",
	)
	if handled {
		t.Fatalf("expected expired idempotency entry to miss replay")
	}
	if code != 0 {
		t.Fatalf("code = %d, want %d", code, 0)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no replay output, got %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", errOut.String())
	}
}

func TestWriteJSONEnvelopeWithIdempotencyNoKeyPreservesExitCode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := writeJSONEnvelopeWithIdempotency(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		"test-v1",
		"agent.send",
		"",
		ExitInternalError,
		errorEnvelope("send_failed", "send failed", nil, "test-v1"),
	)
	if code != ExitInternalError {
		t.Fatalf("code = %d, want %d", code, ExitInternalError)
	}
	if out.Len() == 0 {
		t.Fatalf("expected envelope output")
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", errOut.String())
	}
}
