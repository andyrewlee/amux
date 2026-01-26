package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/vterm"
)

// pollInterval is the fallback polling interval for WaitFor* methods.
const pollInterval = 50 * time.Millisecond

type PTYSession struct {
	cmd     *exec.Cmd
	pty     *os.File
	term    *vterm.VTerm
	updates chan struct{}
	done    chan struct{}
	mu      sync.Mutex
}

type PTYOptions struct {
	Width  int
	Height int
	Setup  func(home string) error
	Env    []string
}

var (
	buildOnce sync.Once
	buildPath string
	buildErr  error
)

func StartPTYSession(opts PTYOptions) (*PTYSession, func(), error) {
	if opts.Width <= 0 {
		opts.Width = 120
	}
	if opts.Height <= 0 {
		opts.Height = 30
	}

	bin, cleanupBin, err := buildAmuxBinary()
	if err != nil {
		return nil, nil, err
	}

	root, err := repoRoot()
	if err != nil {
		cleanupBin()
		return nil, nil, err
	}

	home, err := os.MkdirTemp("", "amux-e2e-home-*")
	if err != nil {
		cleanupBin()
		return nil, nil, err
	}
	if opts.Setup != nil {
		if err := opts.Setup(home); err != nil {
			cleanupBin()
			_ = os.RemoveAll(home)
			return nil, nil, err
		}
	}

	cmd := exec.Command(bin)
	cmd.Dir = root
	// creack/pty sets Setsid=true; Setpgid here can cause EPERM on start (macOS/BSD).
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.Env = append(stripGitEnv(os.Environ()),
		"HOME="+home,
		"TERM=xterm-256color",
		"AMUX_PROFILE=0",
		"AMUX_PROFILE_INTERVAL_MS=0",
	)
	if len(opts.Env) > 0 {
		cmd.Env = append(cmd.Env, opts.Env...)
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(opts.Width),
		Rows: uint16(opts.Height),
	})
	if err != nil {
		cleanupBin()
		_ = os.RemoveAll(home)
		return nil, nil, err
	}

	session := &PTYSession{
		cmd:     cmd,
		pty:     ptmx,
		term:    vterm.New(opts.Width, opts.Height),
		updates: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}

	go session.readLoop()

	cleanup := func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			pgid := cmd.Process.Pid
			_ = process.KillProcessGroup(pgid, process.KillOptions{GracePeriod: 50 * time.Millisecond})
		}
		_, _ = cmd.Process.Wait()
		_ = os.RemoveAll(home)
		cleanupBin()
	}

	return session, cleanup, nil
}

func (s *PTYSession) readLoop() {
	defer close(s.done)
	buf := make([]byte, 4096)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			s.mu.Lock()
			s.term.Write(buf[:n])
			s.mu.Unlock()
			select {
			case s.updates <- struct{}{}:
			default:
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *PTYSession) SendBytes(data []byte) error {
	_, err := s.pty.Write(data)
	return err
}

func (s *PTYSession) SendString(text string) error {
	_, err := s.pty.Write([]byte(text))
	return err
}

func (s *PTYSession) ScreenASCII() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	screen := s.term.VisibleScreen()
	return CellsToASCII(screen)
}

func (s *PTYSession) WaitForContains(substr string, timeout time.Duration) error {
	// Immediate check - handles "already visible" case
	if stringsContains(s.ScreenASCII(), substr) {
		return nil
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	poll := time.NewTimer(pollInterval)
	defer poll.Stop()

	for {
		select {
		case <-s.updates:
			// Signal received - check immediately
			if stringsContains(s.ScreenASCII(), substr) {
				return nil
			}
		case <-poll.C:
			// Periodic check - safety net for missed signals
			if stringsContains(s.ScreenASCII(), substr) {
				return nil
			}
			poll.Reset(pollInterval)
		case <-deadline.C:
			return fmt.Errorf("timeout waiting for %q\n\nScreen:\n%s", substr, s.ScreenASCII())
		}
	}
}

func (s *PTYSession) WaitForAbsent(substr string, timeout time.Duration) error {
	// Immediate check - handles "already absent" case
	if !stringsContains(s.ScreenASCII(), substr) {
		return nil
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	poll := time.NewTimer(pollInterval)
	defer poll.Stop()

	for {
		select {
		case <-s.updates:
			// Signal received - check immediately
			if !stringsContains(s.ScreenASCII(), substr) {
				return nil
			}
		case <-poll.C:
			// Periodic check - safety net for missed signals
			if !stringsContains(s.ScreenASCII(), substr) {
				return nil
			}
			poll.Reset(pollInterval)
		case <-deadline.C:
			return fmt.Errorf("timeout waiting for %q to disappear\n\nScreen:\n%s", substr, s.ScreenASCII())
		}
	}
}

func buildAmuxBinary() (string, func(), error) {
	if path := os.Getenv("AMUX_E2E_BIN"); path != "" {
		return path, func() {}, nil
	}

	buildOnce.Do(func() {
		tmp, err := os.MkdirTemp("", "amux-e2e-bin-*")
		if err != nil {
			buildErr = err
			return
		}
		out := filepath.Join(tmp, "amux")
		root, err := repoRoot()
		if err != nil {
			buildErr = err
			return
		}
		cmd := exec.Command("go", "build", "-o", out, "./cmd/amux")
		cmd.Dir = root
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			buildErr = err
			return
		}
		buildPath = out
	})

	if buildErr != nil {
		return "", func() {}, buildErr
	}

	cleanup := func() {
		if buildPath == "" {
			return
		}
		if os.Getenv("AMUX_E2E_CLEANUP_BIN") == "" {
			return
		}
		_ = os.RemoveAll(filepath.Dir(buildPath))
	}
	return buildPath, cleanup, nil
}
