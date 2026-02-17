package pty

import (
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/process"
)

// terminalCloseTimeout is how long Close waits for cmd.Wait after SIGTERM/SIGKILL
// before escalating to a direct SIGKILL.
const terminalCloseTimeout = 5 * time.Second

// Terminal wraps a PTY with an associated command
type Terminal struct {
	mu      sync.Mutex
	ptyFile *os.File
	cmd     *exec.Cmd
	closed  bool
}

// New creates a new terminal with the given command.
func New(command, dir string, env []string) (*Terminal, error) {
	return NewWithSize(command, dir, env, 0, 0)
}

// NewWithSize creates a new terminal with an initial size, if provided.
func NewWithSize(command, dir string, env []string, rows, cols uint16) (*Terminal, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	// creack/pty sets Setsid=true; Setpgid here can cause EPERM on start.
	cmd.SysProcAttr = &syscall.SysProcAttr{}

	var (
		ptmx *os.File
		err  error
	)
	if rows > 0 && cols > 0 {
		ptmx, err = pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	} else {
		ptmx, err = pty.Start(cmd)
	}
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
	if t.closed {
		t.mu.Unlock()
		return nil
	}

	t.closed = true
	ptyFile := t.ptyFile
	cmd := t.cmd
	t.ptyFile = nil
	t.cmd = nil
	t.mu.Unlock()

	if ptyFile != nil {
		_ = ptyFile.Close()
	}

	if cmd != nil {
		proc := cmd.Process
		if proc != nil {
			leaderPID := proc.Pid
			_ = process.KillProcessGroup(leaderPID, process.KillOptions{})
			// Wait with timeout, escalate to SIGKILL if needed.
			done := make(chan struct{})
			go func() {
				_ = cmd.Wait()
				close(done)
			}()
			select {
			case <-done:
				// Process exited cleanly.
			case <-time.After(terminalCloseTimeout):
				_ = process.ForceKillProcess(leaderPID)
				<-done
			}
		} else {
			_ = cmd.Wait()
		}
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

// IsClosed returns whether the terminal has been closed
func (t *Terminal) IsClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

// File returns the underlying PTY file
func (t *Terminal) File() *os.File {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	return t.ptyFile
}
