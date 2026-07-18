package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupStaleBuiltAmuxBinariesProtectsLiveAndFreshBuilds(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	live := makeE2EBuildDir(t, root, "live", "101", now.Add(-25*time.Hour))
	dead := makeE2EBuildDir(t, root, "dead", "202", now)
	legacyOld := makeE2EBuildDir(t, root, "legacy-old", "", now.Add(-25*time.Hour))
	legacyFresh := makeE2EBuildDir(t, root, "legacy-fresh", "", now)
	invalidFresh := makeE2EBuildDir(t, root, "invalid-fresh", "invalid", now)
	unrelated := filepath.Join(root, "unrelated")
	if err := os.Mkdir(unrelated, 0o700); err != nil {
		t.Fatal(err)
	}

	err := cleanupStaleBuiltAmuxBinariesWith(root, now, func(pid int) bool {
		return pid == 101
	})
	if err != nil {
		t.Fatalf("cleanupStaleBuiltAmuxBinariesWith: %v", err)
	}

	assertPathExists(t, live)
	assertPathMissing(t, dead)
	assertPathMissing(t, legacyOld)
	assertPathExists(t, legacyFresh)
	assertPathExists(t, invalidFresh)
	assertPathExists(t, unrelated)
}

func makeE2EBuildDir(t *testing.T, root, suffix, owner string, modTime time.Time) string {
	t.Helper()
	dir := filepath.Join(root, e2eBuildDirPrefix+suffix)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if owner != "" {
		if err := os.WriteFile(filepath.Join(dir, e2eBuildOwnerFilename), []byte(owner), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(dir, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	return dir
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be removed, stat error: %v", path, err)
	}
}
