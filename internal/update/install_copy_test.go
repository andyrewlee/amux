package update

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeSyncWriteCloser struct {
	closeErr error
}

func (f *fakeSyncWriteCloser) Write(p []byte) (int, error) {
	return len(p), nil
}

func (f *fakeSyncWriteCloser) Sync() error {
	return nil
}

func (f *fakeSyncWriteCloser) Close() error {
	return f.closeErr
}

func TestCopyFileReportsDestinationCloseError(t *testing.T) {
	originalOpenSource := openCopySourceFile
	originalOpenDest := openCopyDestFile
	t.Cleanup(func() {
		openCopySourceFile = originalOpenSource
		openCopyDestFile = originalOpenDest
	})

	injected := errors.New("injected close failure")
	openCopySourceFile = func(string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("new binary")), nil
	}
	openCopyDestFile = func(string, int, os.FileMode) (syncWriteCloser, error) {
		return &fakeSyncWriteCloser{closeErr: injected}, nil
	}

	err := copyFile("src", "dst")
	if !errors.Is(err, injected) {
		t.Fatalf("copyFile() error = %v, want injected close failure", err)
	}
}

func TestCopyFileRefusesExistingDestination(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src-amux")
	dstPath := filepath.Join(tmpDir, ".amux-upgrade-new-raced")
	if err := os.WriteFile(srcPath, []byte("new binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(src): %v", err)
	}
	if err := os.WriteFile(dstPath, []byte("sentinel"), 0o600); err != nil {
		t.Fatalf("WriteFile(dst): %v", err)
	}

	err := copyFile(srcPath, dstPath)
	if err == nil {
		t.Fatal("copyFile() should fail when destination already exists")
	}
	got, readErr := os.ReadFile(dstPath)
	if readErr != nil {
		t.Fatalf("ReadFile(dst): %v", readErr)
	}
	if string(got) != "sentinel" {
		t.Fatalf("destination content = %q, want sentinel", got)
	}
}

func TestCopyFileDoesNotFollowPrecreatedDestinationSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src-amux")
	victimPath := filepath.Join(tmpDir, "victim")
	dstPath := filepath.Join(tmpDir, ".amux-upgrade-new-raced")
	if err := os.WriteFile(srcPath, []byte("new binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(src): %v", err)
	}
	if err := os.WriteFile(victimPath, []byte("victim"), 0o600); err != nil {
		t.Fatalf("WriteFile(victim): %v", err)
	}
	if err := os.Symlink(victimPath, dstPath); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	err := copyFile(srcPath, dstPath)
	if err == nil {
		t.Fatal("copyFile() should fail when destination is a symlink")
	}
	got, readErr := os.ReadFile(victimPath)
	if readErr != nil {
		t.Fatalf("ReadFile(victim): %v", readErr)
	}
	if string(got) != "victim" {
		t.Fatalf("victim content = %q, want victim", got)
	}
}
