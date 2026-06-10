package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMarker(t *testing.T, workspacePath, content string) {
	t.Helper()
	if err := os.WriteFile(prunedWorkspaceRetryMarkerPath(workspacePath), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceCleanupMarkerWritesVersionedJSON(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	cleanup := filepath.Join(dir, "ws.amux-cleanup")

	if err := writeWorkspaceCleanupState(ws, workspaceCleanupState{
		RepoPath:        filepath.Join(dir, "repo"),
		CleanupPath:     cleanup,
		NeedsUnregister: true,
	}); err != nil {
		t.Fatalf("writeWorkspaceCleanupState error = %v", err)
	}

	raw, err := os.ReadFile(prunedWorkspaceRetryMarkerPath(ws))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
		t.Fatalf("expected JSON marker, got %q", raw)
	}
	if !strings.Contains(string(raw), `"version":1`) {
		t.Fatalf("expected version field, got %q", raw)
	}

	state, marked, err := readWorkspaceCleanupState(ws)
	if err != nil || !marked {
		t.Fatalf("readWorkspaceCleanupState = (%+v, %v, %v)", state, marked, err)
	}
	if state.CleanupPath != filepath.Clean(cleanup) || !state.NeedsUnregister {
		t.Fatalf("round-trip mismatch: %+v", state)
	}
}

func TestWorkspaceCleanupMarkerReadsLegacyFormats(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	cleanup := filepath.Join(dir, "stage")

	// Legacy ambiguous prose.
	writeMarker(t, ws, "pruned workspace cleanup pending")
	state, marked, err := readWorkspaceCleanupState(ws)
	if err != nil || !marked || !state.LegacyAmbiguous {
		t.Fatalf("legacy prose: (%+v, %v, %v)", state, marked, err)
	}

	// Legacy prefixed single line.
	writeMarker(t, ws, "su:"+cleanup)
	state, marked, err = readWorkspaceCleanupState(ws)
	if err != nil || !marked || !state.NeedsUnregister || state.CleanupPath != filepath.Clean(cleanup) {
		t.Fatalf("legacy prefixed: (%+v, %v, %v)", state, marked, err)
	}

	// Legacy key=value.
	writeMarker(t, ws, "repo_path=\ncleanup_path="+cleanup+"\nneeds_unregister=true\nworkspace_git_ref=\nworkspace_git_ref_mtime_unix_nano=0\n")
	state, marked, err = readWorkspaceCleanupState(ws)
	if err != nil || !marked || !state.NeedsUnregister || state.CleanupPath != filepath.Clean(cleanup) {
		t.Fatalf("legacy key=value: (%+v, %v, %v)", state, marked, err)
	}
}

func TestWorkspaceCleanupMarkerRejectsUnknownVersion(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	writeMarker(t, ws, `{"version":99,"cleanup_path":"/x","needs_unregister":true}`)
	if _, _, err := readWorkspaceCleanupState(ws); err == nil {
		t.Fatal("expected unknown marker version to be rejected")
	}
}
