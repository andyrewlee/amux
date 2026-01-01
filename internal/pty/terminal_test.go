package pty

import (
	"os"
	"testing"
	"time"
)

func TestTerminalReadWriteAndClose(t *testing.T) {
	term, err := New("sh -c 'cat'", os.TempDir(), nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	readCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 32)
		n, err := term.Read(buf)
		if err != nil {
			errCh <- err
			return
		}
		readCh <- buf[:n]
	}()

	if _, err := term.Write([]byte("hello\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	select {
	case <-readCh:
	case err := <-errCh:
		t.Fatalf("Read() error = %v", err)
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for terminal read")
	}

	if err := term.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !term.IsClosed() {
		t.Fatalf("expected terminal to be closed")
	}
}
