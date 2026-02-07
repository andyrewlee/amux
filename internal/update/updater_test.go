package update

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdaterCheckDevBuild(t *testing.T) {
	// Dev builds should skip update checks
	updater := NewUpdater("dev", "none", "unknown")
	result, err := updater.Check()
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if result.UpdateAvailable {
		t.Errorf("Dev build should not have updates available")
	}
}

func TestUpdaterCheckHomebrewBuild(t *testing.T) {
	original := homebrewBuild
	t.Cleanup(func() { homebrewBuild = original })
	homebrewBuild = "true"

	updater := NewUpdater("v0.0.1", "none", "unknown")
	result, err := updater.Check()
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if result.UpdateAvailable {
		t.Errorf("Homebrew build should not have updates available")
	}
}

func TestUpdaterUpgradeHomebrewBuild(t *testing.T) {
	original := homebrewBuild
	t.Cleanup(func() { homebrewBuild = original })
	homebrewBuild = "true"

	updater := NewUpdater("v0.0.10", "none", "unknown")
	err := updater.Upgrade(&Release{TagName: "v0.0.11"})
	if err == nil {
		t.Fatal("expected error for Homebrew build upgrade")
	}
	if !strings.Contains(err.Error(), "brew upgrade amux") {
		t.Fatalf("expected Homebrew upgrade hint, got: %v", err)
	}
}

func TestGetPlatformAssetName(t *testing.T) {
	// This tests the naming convention matches GoReleaser
	name := GetPlatformAssetName("v1.2.3")

	// Should not have "v" prefix in version part
	if name == "" {
		t.Error("GetPlatformAssetName returned empty string")
	}

	// Should end with .tar.gz
	if len(name) < 7 || name[len(name)-7:] != ".tar.gz" {
		t.Errorf("Expected .tar.gz extension, got %s", name)
	}

	// Should start with amux_1.2.3_ (no v prefix)
	if len(name) < 10 || name[:10] != "amux_1.2.3" {
		t.Errorf("Expected amux_1.2.3 prefix, got %s", name)
	}
}

func TestFindPlatformAsset(t *testing.T) {
	release := &Release{
		TagName: "v1.0.0",
		Assets: []Asset{
			{Name: "amux_1.0.0_darwin_amd64.tar.gz", BrowserDownloadURL: "https://example.com/darwin_amd64.tar.gz"},
			{Name: "amux_1.0.0_darwin_arm64.tar.gz", BrowserDownloadURL: "https://example.com/darwin_arm64.tar.gz"},
			{Name: "amux_1.0.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.com/linux_amd64.tar.gz"},
			{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums.txt"},
		},
	}

	asset := FindPlatformAsset(release)
	// We can't know which platform this runs on, but it should find something or nil
	// At minimum, verify it doesn't panic
	_ = asset
}

func TestParseChecksums(t *testing.T) {
	content := `abc123def456  amux_1.0.0_darwin_amd64.tar.gz
789xyz000111  amux_1.0.0_linux_amd64.tar.gz
checksum1234  checksums.txt`

	checksums := parseChecksums(content)

	if len(checksums) != 3 {
		t.Errorf("Expected 3 checksums, got %d", len(checksums))
	}

	if checksums["amux_1.0.0_darwin_amd64.tar.gz"] != "abc123def456" {
		t.Errorf("Wrong checksum for darwin_amd64")
	}

	if checksums["amux_1.0.0_linux_amd64.tar.gz"] != "789xyz000111" {
		t.Errorf("Wrong checksum for linux_amd64")
	}
}

func TestIsGoInstall(t *testing.T) {
	// Just verify it doesn't panic
	_ = IsGoInstall()
}

func TestCanWrite(t *testing.T) {
	// Test with a path we definitely can't write to
	canWrite := CanWrite("/this/path/definitely/does/not/exist/binary")
	if canWrite {
		t.Error("Should not be able to write to non-existent deep path")
	}
}

func TestExtractBinary(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test tar.gz archive with an amux binary
	archivePath := filepath.Join(tmpDir, "test.tar.gz")
	binaryContent := []byte("#!/bin/sh\necho hello\n")

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("Failed to create archive file: %v", err)
	}

	gzw := gzip.NewWriter(f)
	tw := tar.NewWriter(gzw)

	// Add the amux binary to the archive
	hdr := &tar.Header{
		Name: "amux",
		Mode: 0o755,
		Size: int64(len(binaryContent)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("Failed to write tar header: %v", err)
	}
	if _, err := tw.Write(binaryContent); err != nil {
		t.Fatalf("Failed to write tar content: %v", err)
	}

	tw.Close()
	gzw.Close()
	f.Close()

	// Extract the binary
	destDir := filepath.Join(tmpDir, "extracted")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("Failed to create dest dir: %v", err)
	}

	extractedPath, err := ExtractBinary(archivePath, destDir)
	if err != nil {
		t.Fatalf("ExtractBinary() error = %v", err)
	}

	// Verify the extracted file
	if extractedPath != filepath.Join(destDir, "amux") {
		t.Errorf("Expected path %s, got %s", filepath.Join(destDir, "amux"), extractedPath)
	}

	content, err := os.ReadFile(extractedPath)
	if err != nil {
		t.Fatalf("Failed to read extracted file: %v", err)
	}

	if string(content) != string(binaryContent) {
		t.Errorf("Extracted content mismatch")
	}
}

func TestExtractBinaryMissing(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an archive without an amux binary
	archivePath := filepath.Join(tmpDir, "test.tar.gz")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("Failed to create archive file: %v", err)
	}

	gzw := gzip.NewWriter(f)
	tw := tar.NewWriter(gzw)

	// Add a different file
	hdr := &tar.Header{
		Name: "other-file",
		Mode: 0o644,
		Size: 5,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("Failed to write tar header: %v", err)
	}
	if _, err := tw.Write([]byte("hello")); err != nil {
		t.Fatalf("Failed to write tar content: %v", err)
	}

	tw.Close()
	gzw.Close()
	f.Close()

	destDir := filepath.Join(tmpDir, "extracted")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("Failed to create dest dir: %v", err)
	}

	_, err = ExtractBinary(archivePath, destDir)
	if err == nil {
		t.Error("ExtractBinary() should fail when amux binary not found")
	}
}

func TestInstallBinary(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source binary
	srcPath := filepath.Join(tmpDir, "new-amux")
	if err := os.WriteFile(srcPath, []byte("new binary"), 0o755); err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	// Create destination binary
	destPath := filepath.Join(tmpDir, "amux")
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
