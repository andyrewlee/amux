package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeGuardWorkspaceFile writes a workspace.json-shaped file the
// fingerprint helper can read and returns its cleaned path.
func writeGuardWorkspaceFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wsid", "workspace.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"name":"ws"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return filepath.Clean(path)
}

// seedGuardMarker installs one marker at the given timestamp under the
// lock — the shape markLocalWorkspaceSavePath leaves behind.
func seedGuardMarker(t *testing.T, a *App, path string, at time.Time) {
	t.Helper()
	fingerprint, ok := workspaceMetadataFingerprint(path)
	if !ok {
		t.Fatalf("fingerprint of seeded file failed")
	}
	a.lifecycle.localSaveMu.Lock()
	if a.lifecycle.localSavesAt == nil {
		a.lifecycle.localSavesAt = make(map[string]localWorkspaceSaveMarker)
	}
	a.lifecycle.localSavesAt[path] = localWorkspaceSaveMarker{at: at, fingerprint: fingerprint}
	a.lifecycle.localSaveMu.Unlock()
}

// TestShouldSuppressWorkspaceReload_ExpiredMarkerMustNotSuppress is the
// expiry-branch regression the audit flagged: a marker older than the
// suppression window must be treated as absent — an inverted or missing
// comparison would suppress every external workspace.json change forever.
func TestShouldSuppressWorkspaceReload_ExpiredMarkerMustNotSuppress(t *testing.T) {
	app := &App{}
	path := writeGuardWorkspaceFile(t)
	now := time.Now()
	seedGuardMarker(t, app, path, now.Add(-2*localWorkspaceReloadSuppressWindow))

	if app.shouldSuppressWorkspaceReload([]string{path}, now) {
		t.Fatal("expired marker suppressed a reload — the window comparison is wrong")
	}
	// The expired marker must also be gone from the map — prune ran.
	app.lifecycle.localSaveMu.Lock()
	_, still := app.lifecycle.localSavesAt[path]
	app.lifecycle.localSaveMu.Unlock()
	if still {
		t.Fatal("expired marker survived prune inside shouldSuppressWorkspaceReload")
	}
}

// TestShouldSuppressWorkspaceReload_FreshMarkerSuppresses guards the other
// direction: an in-window marker with a matching fingerprint still
// suppresses — the expiry fix must not break the suppression itself.
func TestShouldSuppressWorkspaceReload_FreshMarkerSuppresses(t *testing.T) {
	app := &App{}
	path := writeGuardWorkspaceFile(t)
	now := time.Now()
	seedGuardMarker(t, app, path, now.Add(-time.Millisecond))

	if !app.shouldSuppressWorkspaceReload([]string{path}, now) {
		t.Fatal("fresh marker with matching fingerprint failed to suppress")
	}
}

// TestPruneOldLocalWorkspaceSavesLocked covers the map sweep directly:
// expired and future-dated markers are dropped, a fresh marker is kept.
func TestPruneOldLocalWorkspaceSavesLocked(t *testing.T) {
	now := time.Now()
	var fp workspaceFileFingerprint
	saves := map[string]localWorkspaceSaveMarker{
		"fresh":    {at: now.Add(-time.Millisecond), fingerprint: fp},
		"expired":  {at: now.Add(-2 * localWorkspaceReloadSuppressWindow), fingerprint: fp},
		"future":   {at: now.Add(time.Hour), fingerprint: fp},
		"zeroTime": {fingerprint: fp},
	}
	pruneOldLocalWorkspaceSavesLocked(saves, now)

	if _, ok := saves["fresh"]; !ok {
		t.Fatal("fresh marker was pruned")
	}
	for _, k := range []string{"expired", "future", "zeroTime"} {
		if _, ok := saves[k]; ok {
			t.Fatalf("marker %q survived prune", k)
		}
	}
}
