package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/shellutil"
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

func TestInstallBinaryBackupFails(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "new-amux")
	if err := os.WriteFile(srcPath, []byte("new"), 0o755); err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}
	destPath := filepath.Join(tmpDir, "amux")
	if err := os.WriteFile(destPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("Failed to create dest: %v", err)
	}

	injected := errors.New("injected backup failure")
	t.Cleanup(func() { renameFile = os.Rename })
	var backupPath string
	renameFile = func(oldpath, newpath string) error {
		// Fail the backup-create rename: current binary -> .bak
		if oldpath == destPath && isInstallBackupPath(tmpDir, filepath.Base(destPath), newpath) {
			backupPath = newpath
			return injected
		}
		return os.Rename(oldpath, newpath)
	}

	err := InstallBinary(srcPath, destPath)
	if err == nil {
		t.Fatal("InstallBinary() should have failed when backup rename fails")
	}
	if !strings.Contains(err.Error(), "backing up current binary") {
		t.Errorf("Expected error to mention backing up, got: %v", err)
	}
	if backupPath == "" {
		t.Fatal("expected test to observe generated backup path")
	}

	// Current binary still exists with original content
	content, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("Current binary should still exist: %v", readErr)
	}
	if string(content) != "old" {
		t.Errorf("Expected current binary to remain 'old', got %q", string(content))
	}

	// No backup file should exist
	if _, statErr := os.Stat(destPath + ".bak"); !os.IsNotExist(statErr) {
		t.Error("No fixed backup file should exist after backup rename failure")
	}
}

func TestInstallBinarySwapFailsRestoreSucceeds(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "new-amux")
	if err := os.WriteFile(srcPath, []byte("new"), 0o755); err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}
	destPath := filepath.Join(tmpDir, "amux")
	if err := os.WriteFile(destPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("Failed to create dest: %v", err)
	}

	injected := errors.New("injected swap failure")
	t.Cleanup(func() { renameFile = os.Rename })
	var stagedPath string
	renameFile = func(oldpath, newpath string) error {
		// Fail only the swap: staged -> current binary. Restore is allowed.
		if newpath == destPath && isInstallStagedPath(tmpDir, oldpath) {
			stagedPath = oldpath
			return injected
		}
		return os.Rename(oldpath, newpath)
	}

	err := InstallBinary(srcPath, destPath)
	if err == nil {
		t.Fatal("InstallBinary() should have failed when swap rename fails")
	}
	if !strings.Contains(err.Error(), "previous binary restored") {
		t.Errorf("Expected error to mention restore, got: %v", err)
	}

	// Target restored to original content
	content, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("Target should be restored: %v", readErr)
	}
	if string(content) != "old" {
		t.Errorf("Expected target restored to 'old', got %q", string(content))
	}

	// Staged file cleaned up by defer
	if stagedPath == "" {
		t.Fatal("expected test to observe generated staged path")
	}
	if _, statErr := os.Stat(stagedPath); !os.IsNotExist(statErr) {
		t.Error("Staged file should have been cleaned up")
	}
}

func TestInstallBinarySwapFailsRestoreFails(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "new-amux")
	if err := os.WriteFile(srcPath, []byte("new"), 0o755); err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}
	destPath := filepath.Join(tmpDir, "amux")
	if err := os.WriteFile(destPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("Failed to create dest: %v", err)
	}

	swapErr := errors.New("injected swap failure")
	restoreErr := errors.New("injected restore failure")
	t.Cleanup(func() { renameFile = os.Rename })
	var stagedPath string
	var backupPath string
	renameFile = func(oldpath, newpath string) error {
		// Fail the swap (staged -> current) and the restore (backup -> current).
		if newpath == destPath && isInstallStagedPath(tmpDir, oldpath) {
			stagedPath = oldpath
			return swapErr
		}
		if newpath == destPath && isInstallBackupPath(tmpDir, filepath.Base(destPath), oldpath) {
			backupPath = oldpath
			return restoreErr
		}
		return os.Rename(oldpath, newpath)
	}

	err := InstallBinary(srcPath, destPath)
	if err == nil {
		t.Fatal("InstallBinary() should have failed when both swap and restore fail")
	}
	if stagedPath == "" {
		t.Fatal("expected test to observe generated staged path")
	}
	if backupPath == "" {
		t.Fatal("expected test to observe generated backup path")
	}
	if !strings.Contains(err.Error(), backupPath) {
		t.Errorf("Expected error to name backup path %q, got: %v", backupPath, err)
	}
	wantHint := "mv " + shellutil.ShellQuote(backupPath) + " " + shellutil.ShellQuote(destPath)
	if !strings.Contains(err.Error(), wantHint) {
		t.Errorf("Expected error to include quoted manual recovery hint %q, got: %v", wantHint, err)
	}

	// Backup file still holds the original binary content
	content, readErr := os.ReadFile(backupPath)
	if readErr != nil {
		t.Fatalf("Backup file should still exist: %v", readErr)
	}
	if string(content) != "old" {
		t.Errorf("Expected backup to retain 'old', got %q", string(content))
	}
}

func isInstallStagedPath(dir, path string) bool {
	return filepath.Dir(path) == dir && strings.HasPrefix(filepath.Base(path), ".amux-upgrade-new-")
}

func isInstallBackupPath(dir, base, path string) bool {
	return filepath.Dir(path) == dir && strings.HasPrefix(filepath.Base(path), "."+base+".bak-")
}
