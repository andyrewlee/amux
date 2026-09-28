package update

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallBinary(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source binary
	srcPath := filepath.Join(tmpDir, "new-amux")
	if err := os.WriteFile(srcPath, []byte("new binary"), 0o755); err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	// Create destination binary
	destPath := filepath.Join(tmpDir, "am ux'bin")
	if err := os.WriteFile(destPath, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("Failed to create dest: %v", err)
	}

	// Install
	if err := InstallBinary(srcPath, destPath); err != nil {
		t.Fatalf("InstallBinary() error = %v", err)
	}

	// Verify new content
	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("Failed to read dest: %v", err)
	}
	if string(content) != "new binary" {
		t.Errorf("Expected 'new binary', got %s", string(content))
	}

	// Verify backup was cleaned up
	if _, err := os.Stat(destPath + ".bak"); !os.IsNotExist(err) {
		t.Error("Backup file should have been removed")
	}

	// Verify staged file was cleaned up
	if _, err := os.Stat(filepath.Join(tmpDir, ".amux-upgrade-new")); !os.IsNotExist(err) {
		t.Error("Staged file should have been removed")
	}
}

func TestInstallBinaryDoesNotClobberFixedStagingFile(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "new-amux")
	if err := os.WriteFile(srcPath, []byte("new binary"), 0o755); err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}
	destPath := filepath.Join(tmpDir, "amux")
	if err := os.WriteFile(destPath, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("Failed to create dest: %v", err)
	}
	sentinelPath := filepath.Join(tmpDir, ".amux-upgrade-new")
	if err := os.WriteFile(sentinelPath, []byte("sentinel"), 0o600); err != nil {
		t.Fatalf("Failed to create staging sentinel: %v", err)
	}

	if err := InstallBinary(srcPath, destPath); err != nil {
		t.Fatalf("InstallBinary() error = %v", err)
	}

	got, err := os.ReadFile(sentinelPath)
	if err != nil {
		t.Fatalf("Failed to read staging sentinel: %v", err)
	}
	if string(got) != "sentinel" {
		t.Fatalf("staging sentinel = %q, want sentinel", got)
	}
}

func TestInstallBinaryDoesNotClobberFixedBackupFile(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "new-amux")
	if err := os.WriteFile(srcPath, []byte("new binary"), 0o755); err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}
	destPath := filepath.Join(tmpDir, "amux")
	if err := os.WriteFile(destPath, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("Failed to create dest: %v", err)
	}
	sentinelPath := destPath + ".bak"
	if err := os.WriteFile(sentinelPath, []byte("sentinel"), 0o600); err != nil {
		t.Fatalf("Failed to create backup sentinel: %v", err)
	}

	if err := InstallBinary(srcPath, destPath); err != nil {
		t.Fatalf("InstallBinary() error = %v", err)
	}

	got, err := os.ReadFile(sentinelPath)
	if err != nil {
		t.Fatalf("Failed to read backup sentinel: %v", err)
	}
	if string(got) != "sentinel" {
		t.Fatalf("backup sentinel = %q, want sentinel", got)
	}
}

func TestInstallBinaryCrossDir(t *testing.T) {
	// Test that install works when source is in a different directory
	// This simulates the cross-filesystem scenario
	srcDir := t.TempDir()
	destDir := t.TempDir()

	srcPath := filepath.Join(srcDir, "new-amux")
	if err := os.WriteFile(srcPath, []byte("new binary content"), 0o755); err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	destPath := filepath.Join(destDir, "amux")
	if err := os.WriteFile(destPath, []byte("old binary content"), 0o755); err != nil {
		t.Fatalf("Failed to create dest: %v", err)
	}

	// Install from different directory
	if err := InstallBinary(srcPath, destPath); err != nil {
		t.Fatalf("InstallBinary() error = %v", err)
	}

	// Verify new content
	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("Failed to read dest: %v", err)
	}
	if string(content) != "new binary content" {
		t.Errorf("Expected 'new binary content', got %s", string(content))
	}
}

func TestInstallBinaryNonExecutableSourceInstallsExecutable(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "new-amux")
	if err := os.WriteFile(srcPath, []byte("new binary"), 0o600); err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}
	destPath := filepath.Join(tmpDir, "amux")
	if err := os.WriteFile(destPath, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("Failed to create dest: %v", err)
	}

	if err := InstallBinary(srcPath, destPath); err != nil {
		t.Fatalf("InstallBinary() error = %v", err)
	}

	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		t.Fatalf("Stat(src) error = %v", err)
	}
	if srcInfo.Mode()&0o111 != 0 {
		t.Fatalf("source mode = %03o, want no executable bits", srcInfo.Mode().Perm())
	}
	destInfo, err := os.Stat(destPath)
	if err != nil {
		t.Fatalf("Stat(dest) error = %v", err)
	}
	if destInfo.Mode()&0o111 == 0 {
		t.Fatalf("dest mode = %03o, want executable bits", destInfo.Mode().Perm())
	}
}

// TestInstallBinaryBackupCopyFailsPreservesTarget: the backup is a COPY — a
// failure leaves the live pathname holding the old binary, with the staged
// file cleaned up and no partial backup left behind.
func TestInstallBinaryBackupCopyFailsPreservesTarget(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "new-amux")
	if err := os.WriteFile(srcPath, []byte("new"), 0o755); err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}
	destPath := filepath.Join(tmpDir, "amux")
	if err := os.WriteFile(destPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("Failed to create dest: %v", err)
	}

	injected := errors.New("injected backup source failure")
	originalOpenSource := openCopySourceFile
	t.Cleanup(func() { openCopySourceFile = originalOpenSource })
	openCopySourceFile = func(name string) (io.ReadCloser, error) {
		if name == destPath {
			return nil, injected
		}
		return originalOpenSource(name)
	}

	err := InstallBinary(srcPath, destPath)
	if !errors.Is(err, injected) {
		t.Fatalf("InstallBinary() error = %v, want wrapped backup-copy cause", err)
	}
	if !strings.Contains(err.Error(), "backing up current binary") {
		t.Errorf("Expected error to mention backing up, got: %v", err)
	}

	// Live pathname never moved: still the old binary.
	content, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("Current binary should still exist: %v", readErr)
	}
	if string(content) != "old" {
		t.Errorf("Expected current binary to remain 'old', got %q", string(content))
	}

	// No staged or backup temp files remain.
	assertNoInstallTemps(t, tmpDir, filepath.Base(destPath))
}

// TestInstallBinaryRenameFailsPreservesTarget: the sole replacement rename
// fails — the old binary was never moved, so there is nothing to restore;
// the error wraps the cause and both temp files are cleaned up.
func TestInstallBinaryRenameFailsPreservesTarget(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "new-amux")
	if err := os.WriteFile(srcPath, []byte("new"), 0o755); err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}
	destPath := filepath.Join(tmpDir, "amux")
	if err := os.WriteFile(destPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("Failed to create dest: %v", err)
	}

	injected := errors.New("injected rename failure")
	t.Cleanup(func() { renameFile = os.Rename })
	var calls [][2]string
	renameFile = func(oldpath, newpath string) error {
		calls = append(calls, [2]string{oldpath, newpath})
		if newpath == destPath && isInstallStagedPath(tmpDir, oldpath) {
			return injected
		}
		return os.Rename(oldpath, newpath)
	}

	err := InstallBinary(srcPath, destPath)
	if !errors.Is(err, injected) {
		t.Fatalf("InstallBinary() error = %v, want wrapped rename cause", err)
	}

	content, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("Current binary should still exist: %v", readErr)
	}
	if string(content) != "old" {
		t.Errorf("Expected current binary to remain 'old', got %q", string(content))
	}
	// Exactly one rename was attempted — there is no restore path to exercise.
	if len(calls) != 1 {
		t.Fatalf("rename calls = %v, want exactly the staged→target attempt", calls)
	}
	assertNoInstallTemps(t, tmpDir, filepath.Base(destPath))
}

// assertNoInstallTemps fails when the install's staging or backup temp files
// still exist in dir.
func assertNoInstallTemps(t *testing.T, dir, base string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".amux-upgrade-new-") || strings.HasPrefix(e.Name(), "."+base+".bak-") {
			t.Errorf("leftover install temp file: %s", e.Name())
		}
	}
}

func isInstallStagedPath(dir, path string) bool {
	return filepath.Dir(path) == dir && strings.HasPrefix(filepath.Base(path), ".amux-upgrade-new-")
}
