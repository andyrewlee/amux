package update

import (
	"errors"
	"io"
	"os"
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
