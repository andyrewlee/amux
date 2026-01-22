package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyChecksum(t *testing.T) {
	// Create a temp file with known content
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	content := []byte("hello world\n")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// SHA256 of "hello world\n"
	expectedChecksum := "a948904f2f0f479b8f8564cbf12dac6b0c2e2dae5b8d8e6a0c3a5c9f0e1d2c3b"

	// First compute the actual checksum
	actualChecksum, err := ComputeChecksum(testFile)
	if err != nil {
		t.Fatalf("ComputeChecksum() error = %v", err)
	}

	// Now verify with the actual checksum (should pass)
	err = VerifyChecksum(testFile, actualChecksum)
	if err != nil {
		t.Errorf("VerifyChecksum() with correct checksum should pass, got error: %v", err)
	}

	// Verify with wrong checksum (should fail)
	err = VerifyChecksum(testFile, expectedChecksum)
	if err == nil {
		t.Error("VerifyChecksum() with wrong checksum should fail")
	}
}

func TestComputeChecksum(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Empty file
	if err := os.WriteFile(testFile, []byte{}, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	checksum, err := ComputeChecksum(testFile)
	if err != nil {
		t.Fatalf("ComputeChecksum() error = %v", err)
	}

	// SHA256 of empty file
	emptyChecksum := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if checksum != emptyChecksum {
		t.Errorf("ComputeChecksum() of empty file = %s, want %s", checksum, emptyChecksum)
	}
}

func TestVerifyChecksumFileNotFound(t *testing.T) {
	err := VerifyChecksum("/nonexistent/file", "abc123")
	if err == nil {
		t.Error("VerifyChecksum() should fail for nonexistent file")
	}
}

func TestComputeChecksumFileNotFound(t *testing.T) {
	_, err := ComputeChecksum("/nonexistent/file")
	if err == nil {
		t.Error("ComputeChecksum() should fail for nonexistent file")
	}
}
