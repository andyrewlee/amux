package pty

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/process"
)

// terminalCloseTimeout is how long Close waits for cmd.Wait after process
// termination before escalating and before giving up on a stuck waiter.
var terminalCloseTimeout = 5 * time.Second

var (
	terminalKillProcessGroup = process.KillProcessGroup
	terminalForceKillProcess = process.ForceKillProcess
	terminalWaitCommand      = func(cmd *exec.Cmd) error {
		return cmd.Wait()
	}
)

// Terminal wraps a PTY with an associated command
type Terminal struct {
	mu       sync.Mutex
	ptyFile  *os.File
	cmd      *exec.Cmd
	waitDone <-chan struct{}
	closed   bool
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

	term := &Terminal{
		ptyFile: ptmx,
		cmd:     cmd,
	}
	term.startWaitMonitor(cmd)
	return term, nil
}

func (t *Terminal) startWaitMonitor(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	waitDone := make(chan struct{})
	t.waitDone = waitDone
	waitCommand := terminalWaitCommand
	go func() {
		_ = waitCommand(cmd)
		close(waitDone)
	}()
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

// SetReadDeadline sets the deadline for future Read calls.
func (t *Terminal) SetReadDeadline(deadline time.Time) error {
	t.mu.Lock()
	closed := t.closed
	ptyFile := t.ptyFile
	t.mu.Unlock()

	if closed || ptyFile == nil {
		return io.ErrClosedPipe
	}

	return ptyFile.SetReadDeadline(deadline)
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
		// Log a control-byte classification, never the literal input: SendString
		// is the single funnel for all agent input, which can contain pasted
		// secrets (API keys, passwords) and prompt text.
		logging.Debug("SendString wrote %d bytes (%s)", n, controlByteHint(s))
	}
	return err
}

// controlByteHint summarizes the control framing of agent input for debug logs
// without recording any literal bytes.
func controlByteHint(s string) string {
	var hints []string
	if strings.Contains(s, "\x1b[200~") || strings.Contains(s, "\x1b[201~") {
		hints = append(hints, "paste")
	}
	if strings.ContainsRune(s, 0x0d) {
		hints = append(hints, "cr")
	}
	if strings.ContainsRune(s, 0x03) {
		hints = append(hints, "ctrl-c")
	}
	if len(hints) == 0 {
		return "text"
	}
	return strings.Join(hints, "+")
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
	waitDone := t.waitDone
	t.ptyFile = nil
	t.cmd = nil
	t.mu.Unlock()

	closeTimeout := terminalCloseTimeout
	killProcessGroup := terminalKillProcessGroup
	forceKillProcess := terminalForceKillProcess
	waitCommand := terminalWaitCommand

	if ptyFile != nil {
		_ = ptyFile.Close()
	}

	if cmd != nil {
		if waitComplete(waitDone) {
			return nil
		}

		proc := cmd.Process
		if waitDone == nil && proc != nil {
			waitDone = waitCommandAsync(cmd, waitCommand)
		}
		if proc != nil {
			leaderPID := proc.Pid
			_ = killProcessGroup(leaderPID, process.KillOptions{})
			// Wait with timeout, escalate to SIGKILL if needed.
			select {
			case <-waitDone:
				// Process exited cleanly.
			case <-time.After(closeTimeout):
				logging.Warn("agent did not exit within %s of SIGTERM; escalating to SIGKILL (pid %d)", closeTimeout, leaderPID)
				_ = forceKillProcess(leaderPID)
				select {
				case <-waitDone:
				case <-time.After(closeTimeout):
					logging.Error("agent did not exit within %s of SIGKILL; abandoning wait (pid %d)", closeTimeout, leaderPID)
				}
			}
		} else if waitDone != nil {
			<-waitDone
		} else {
			_ = waitCommand(cmd)
		}
	}

	return nil
}

func waitComplete(done <-chan struct{}) bool {
	if done == nil {
		return false
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func waitCommandAsync(cmd *exec.Cmd, waitCommand func(*exec.Cmd) error) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		_ = waitCommand(cmd)
		close(done)
	}()
	return done
}

// Running returns whether the terminal is still running
func (t *Terminal) Running() bool {
	t.mu.Lock()
	closed := t.closed
	cmd := t.cmd
	waitDone := t.waitDone
	t.mu.Unlock()

	if closed || cmd == nil {
		return false
	}

	if waitDone != nil {
		return !waitComplete(waitDone)
	}

	// Manually constructed Terminals in tests may not have a wait monitor.
	return cmd.ProcessState == nil
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
