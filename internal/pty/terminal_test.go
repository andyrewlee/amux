package pty

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNew_EchoCommand(t *testing.T) {
	term, err := New("echo hello", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer term.Close()

	// Read output until we see "hello" or timeout
	buf := make([]byte, 1024)
	var output strings.Builder
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for output, got: %q", output.String())
		default:
		}
		n, err := term.Read(buf)
		if n > 0 {
			output.Write(buf[:n])
		}
		if strings.Contains(output.String(), "hello") {
			break
		}
		if err != nil {
			break
		}
	}

	if !strings.Contains(output.String(), "hello") {
		t.Errorf("expected output to contain 'hello', got %q", output.String())
	}
}

func TestNewWithSize(t *testing.T) {
	term, err := NewWithSize("echo sized", t.TempDir(), nil, 24, 80)
	if err != nil {
		t.Fatalf("NewWithSize failed: %v", err)
	}
	defer term.Close()

	// Verify it's running
	if !term.Running() {
		// The echo command may finish fast, so just check it was created
		if term.IsClosed() {
			t.Error("terminal should not be closed immediately after creation")
		}
	}
}

func TestNewWithSize_ZeroDimensions(t *testing.T) {
	// rows=0, cols=0 should fall through to pty.Start (no size)
	term, err := NewWithSize("echo zero", t.TempDir(), nil, 0, 0)
	if err != nil {
		t.Fatalf("NewWithSize with zero dimensions failed: %v", err)
	}
	defer term.Close()
}

func TestTerminal_Write(t *testing.T) {
	// Use cat which reads from stdin and echoes
	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer term.Close()

	n, err := term.Write([]byte("test input\n"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != 11 {
		t.Errorf("expected 11 bytes written, got %d", n)
	}
}

func TestTerminal_WriteAfterClose(t *testing.T) {
	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	term.Close()

	_, err = term.Write([]byte("data"))
	if err != io.ErrClosedPipe {
		t.Errorf("expected io.ErrClosedPipe after close, got %v", err)
	}
}

func TestTerminal_ReadAfterClose(t *testing.T) {
	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	term.Close()

	buf := make([]byte, 64)
	_, err = term.Read(buf)
	if err != io.EOF {
		t.Errorf("expected io.EOF after close, got %v", err)
	}
}

func TestTerminal_SendInterrupt(t *testing.T) {
	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer term.Close()

	err = term.SendInterrupt()
	if err != nil {
		t.Errorf("SendInterrupt failed: %v", err)
	}
}

func TestTerminal_SendString(t *testing.T) {
	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer term.Close()

	err = term.SendString("hello world")
	if err != nil {
		t.Errorf("SendString failed: %v", err)
	}
}

func TestTerminal_SetSize(t *testing.T) {
	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer term.Close()

	err = term.SetSize(40, 120)
	if err != nil {
		t.Errorf("SetSize failed: %v", err)
	}
}

func TestTerminal_SetSizeAfterClose(t *testing.T) {
	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	term.Close()

	// SetSize on a closed terminal should return nil (no-op)
	err = term.SetSize(40, 120)
	if err != nil {
		t.Errorf("SetSize on closed terminal should return nil, got %v", err)
	}
}

func TestTerminal_Running(t *testing.T) {
	term, err := New("sleep 10", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer term.Close()

	if !term.Running() {
		t.Error("expected terminal to be running")
	}
}

func TestTerminal_RunningAfterClose(t *testing.T) {
	term, err := New("sleep 10", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	term.Close()

	if term.Running() {
		t.Error("expected terminal not to be running after close")
	}
}

func TestTerminal_IsClosed(t *testing.T) {
	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if term.IsClosed() {
		t.Error("terminal should not be closed after creation")
	}

	term.Close()

	if !term.IsClosed() {
		t.Error("terminal should be closed after Close()")
	}
}

func TestTerminal_CloseIdempotent(t *testing.T) {
	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// Close multiple times should not panic or error
	if err := term.Close(); err != nil {
		t.Errorf("first Close failed: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Errorf("second Close failed: %v", err)
	}
}

func TestTerminal_File(t *testing.T) {
	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer term.Close()

	f := term.File()
	if f == nil {
		t.Error("File() should return non-nil for open terminal")
	}
}

func TestTerminal_FileAfterClose(t *testing.T) {
	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	term.Close()

	f := term.File()
	if f != nil {
		t.Error("File() should return nil for closed terminal")
	}
}

func TestTerminal_EnvPropagation(t *testing.T) {
	env := []string{"TEST_VAR=test_value_12345"}
	term, err := New("env", t.TempDir(), env)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer term.Close()

	buf := make([]byte, 4096)
	var output strings.Builder
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for env output, got: %q", output.String())
		default:
		}
		n, err := term.Read(buf)
		if n > 0 {
			output.Write(buf[:n])
		}
		if strings.Contains(output.String(), "TEST_VAR=test_value_12345") {
			return // success
		}
		if err != nil {
			break
		}
	}

	if !strings.Contains(output.String(), "TEST_VAR=test_value_12345") {
		t.Errorf("expected env var in output, got %q", output.String())
	}
}

func TestTerminal_ConcurrentWriteAndClose(t *testing.T) {
	term, err := New("cat", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	var wg sync.WaitGroup

	// Writer goroutine - writes until close
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_, err := term.Write([]byte("x"))
			if err != nil {
				return
			}
		}
	}()

	// Close after a short delay
	time.Sleep(10 * time.Millisecond)
	term.Close()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success - concurrent write and close did not panic
	case <-time.After(3 * time.Second):
		t.Error("concurrent write/close timed out")
	}
}

func TestTerminal_ConcurrentClose(t *testing.T) {
	term, err := New("sleep 10", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// Close from multiple goroutines should not panic
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			term.Close()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Error("concurrent close timed out")
	}
}

func TestNew_InvalidCommand(t *testing.T) {
	// Even an invalid command gets wrapped in sh -c, which still starts.
	// The process will exit quickly with an error, but New itself succeeds.
	term, err := New("nonexistent_command_xyz_12345", t.TempDir(), nil)
	if err != nil {
		// This is also acceptable - depends on how sh handles it
		return
	}
	defer term.Close()
}

func TestTerminal_ReadEOFAfterProcessExit(t *testing.T) {
	// "true" exits immediately; reading should eventually yield an error
	term, err := New("true", t.TempDir(), nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer term.Close()

	buf := make([]byte, 256)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for read error after process exit")
		default:
		}
		_, err := term.Read(buf)
		if err != nil {
			// Got an error (EIO or EOF) - expected after process exits
			return
		}
	}
}
