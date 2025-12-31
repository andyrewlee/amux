package pty

import (
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/creack/pty"
)

// Terminal wraps a PTY with an associated command
type Terminal struct {
	mu      sync.Mutex
	ptyFile *os.File
	cmd     *exec.Cmd
	closed  bool
}

// New creates a new terminal with the given command
func New(command string, dir string, env []string) (*Terminal, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	return &Terminal{
		ptyFile: ptmx,
		cmd:     cmd,
	}, nil
}

// SetSize sets the terminal size
func (t *Terminal) SetSize(rows, cols uint16) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed || t.ptyFile == nil {
		return nil
	}

	return pty.Setsize(t.ptyFile, &pty.Winsize{
		Rows: rows,
		Cols: cols,
	})
}

// Write sends input to the terminal
func (t *Terminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	closed := t.closed
	ptyFile := t.ptyFile
	t.mu.Unlock()

	if closed || ptyFile == nil {
		return 0, io.ErrClosedPipe
	}

	return ptyFile.Write(p)
}

// Read reads output from the terminal
// Note: This does NOT hold the mutex during the blocking read to avoid deadlock
func (t *Terminal) Read(p []byte) (int, error) {
	t.mu.Lock()
	closed := t.closed
	ptyFile := t.ptyFile
	t.mu.Unlock()

	if closed || ptyFile == nil {
		return 0, io.EOF
	}

	return ptyFile.Read(p)
}

// SendInterrupt sends Ctrl+C to the terminal
func (t *Terminal) SendInterrupt() error {
	_, err := t.Write([]byte{0x03})
	return err
}

// SendString sends a string to the terminal
func (t *Terminal) SendString(s string) error {
	n, err := t.Write([]byte(s))
	if err != nil {
		logging.Error("SendString failed: %v", err)
	} else {
		logging.Debug("SendString wrote %d bytes: %q", n, s)
	}
	return err
}

// Close closes the terminal
func (t *Terminal) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}

	t.closed = true

	if t.ptyFile != nil {
		t.ptyFile.Close()
	}

	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
		t.cmd.Wait()
	}

	return nil
}

// Running returns whether the terminal is still running
func (t *Terminal) Running() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed || t.cmd == nil {
		return false
	}

	// Check if process is still running
	return t.cmd.ProcessState == nil
}

// File returns the underlying PTY file
func (t *Terminal) File() *os.File {
	return t.ptyFile
}
