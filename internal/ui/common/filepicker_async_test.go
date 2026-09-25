package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestFilePickerAsyncLoadRoundTrip verifies the split: Show marks the load
// pending (no fs work happens synchronously), a directoryLoadedMsg populates
// the listing, and the loading state clears.
func TestFilePickerAsyncLoadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmp, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fp := NewFilePicker("id", tmp, true)
	fp.Show()

	// No synchronous read happened: the listing is empty and a load is due.
	if !fp.needsLoad {
		t.Fatal("expected needsLoad after Show")
	}
	if len(fp.entries) != 0 {
		t.Fatalf("expected no entries before the async load, got %d", len(fp.entries))
	}
	if !strings.Contains(strings.Join(fp.renderLines(), "\n"), "Loading…") {
		t.Fatal("expected a Loading… row while the load is pending")
	}

	// The picker issues the pending read through Update (the fallback used by
	// opens whose caller cannot emit a cmd).
	_, cmd := fp.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if cmd == nil {
		t.Fatal("expected Update to issue the due directory load")
	}
	msgs := pumpMsgs(cmd)
	if len(msgs) != 1 {
		t.Fatalf("expected one directoryLoadedMsg, got %d", len(msgs))
	}
	if _, ok := msgs[0].(directoryLoadedMsg); !ok {
		t.Fatalf("expected directoryLoadedMsg, got %T", msgs[0])
	}
	fp.Update(msgs[0])

	if fp.needsLoad || fp.loadInFlight {
		t.Fatal("expected load state cleared after delivery")
	}
	if len(fp.entries) != 1 || fp.entries[0].Name() != "alpha" {
		t.Fatalf("expected alpha entry after async load, got %v", fp.entries)
	}
}

// TestFilePickerStaleLoadResultDiscarded verifies a result issued for a
// superseded path is dropped rather than populating the listing.
func TestFilePickerStaleLoadResultDiscarded(t *testing.T) {
	tmp := t.TempDir()
	fp := NewFilePicker("id", tmp, true)
	fp.Show()
	pumpPicker(fp)

	// Simulate a result issued for a different directory arriving late.
	fp.needsLoad = true
	_, cmd := fp.Update(directoryLoadedMsg{path: filepath.Join(tmp, "elsewhere"), entries: nil})

	if !fp.needsLoad {
		t.Fatal("stale result must not clear needsLoad — a fresh read is still due")
	}
	if len(fp.entries) != 0 {
		t.Fatalf("stale result must not populate entries, got %v", fp.entries)
	}
	// The wrapper re-issued a load for the current path on that same Update.
	pumpPicker(fp, cmd)
	if fp.needsLoad || fp.loadInFlight {
		t.Fatal("expected reload to complete after stale drop")
	}
}

// TestFilePickerLoadErrorLeavesEmpty verifies a failed ReadDir leaves the
// listing empty with loading cleared (same contract as the old sync path).
func TestFilePickerLoadErrorLeavesEmpty(t *testing.T) {
	fp := NewFilePicker("id", filepath.Join(t.TempDir(), "does-not-exist"), true)
	fp.Show()
	pumpPicker(fp)

	if fp.needsLoad || fp.loadInFlight {
		t.Fatal("expected load state cleared after error result")
	}
	if len(fp.entries) != 0 || len(fp.filteredIdx) != 0 {
		t.Fatalf("expected empty listing on error, got %d entries", len(fp.entries))
	}
}

// TestFilePickerStaleResolveDiscarded verifies a pathResolvedMsg whose input
// has moved on is dropped without navigating.
func TestFilePickerStaleResolveDiscarded(t *testing.T) {
	tmp := t.TempDir()
	child := filepath.Join(tmp, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fp := NewFilePicker("id", tmp, true)
	fp.Show()
	pumpPicker(fp)
	fp.input.SetValue(child)

	cmd := fp.handleOpenFromInput()
	if cmd == nil {
		t.Fatal("expected a resolve cmd for a non-empty input")
	}
	// The user edits the input before the stat lands — the result is stale.
	fp.input.SetValue(child + "-edited")
	pumpPicker(fp, cmd)

	if fp.currentPath != tmp {
		t.Fatalf("stale resolve navigated to %q, want %q", fp.currentPath, tmp)
	}
}
