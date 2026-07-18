package e2e

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/andyrewlee/amux/internal/process"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/vterm"
)

// pollInterval is the fallback polling interval for WaitFor* methods.
const pollInterval = 50 * time.Millisecond

const (
	e2eBuildDirPrefix     = "amux-e2e-bin-"
	e2eBuildOwnerFilename = ".owner-pid"
	legacyE2EBuildMaxAge  = 24 * time.Hour
)

type PTYSession struct {
	cmd      *exec.Cmd
	pty      *os.File
	term     *vterm.VTerm
	updates  chan struct{}
	done     chan struct{}
	procDone chan struct{}
	mu       sync.Mutex
	waitMu   sync.Mutex
	waitErr  error
}

type PTYOptions struct {
	Width  int
	Height int
	Setup  func(home string) error
	Env    []string
	Home   string
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

	home := opts.Home
	ownHome := false
	if home == "" {
		var err error
		home, err = os.MkdirTemp("", "amux-e2e-home-*")
		if err != nil {
			cleanupBin()
			return nil, nil, err
		}
		ownHome = true
	}
	if opts.Setup != nil {
		if err := opts.Setup(home); err != nil {
			cleanupBin()
			if ownHome {
				_ = os.RemoveAll(home)
			}
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

	ptyRows, ptyCols, _ := appPty.WinsizeFromInts(opts.Height, opts.Width)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: ptyCols,
		Rows: ptyRows,
	})
	if err != nil {
		cleanupBin()
		if ownHome {
			_ = os.RemoveAll(home)
		}
		return nil, nil, err
	}

	session := &PTYSession{
		cmd:      cmd,
		pty:      ptmx,
		term:     vterm.New(opts.Width, opts.Height),
		updates:  make(chan struct{}, 1),
		done:     make(chan struct{}),
		procDone: make(chan struct{}),
	}

	go session.readLoop()
	go session.waitLoop()

	cleanup := func() {
		_ = ptmx.Close()
		proc := cmd.Process
		if proc != nil {
			leaderPID := proc.Pid
			_ = process.KillProcessGroup(leaderPID, process.KillOptions{GracePeriod: 50 * time.Millisecond})
		}
		select {
		case <-session.procDone:
		case <-time.After(250 * time.Millisecond):
		}
		if ownHome {
			_ = os.RemoveAll(home)
		}
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

func (s *PTYSession) waitLoop() {
	defer close(s.procDone)
	err := s.cmd.Wait()
	s.waitMu.Lock()
	s.waitErr = err
	s.waitMu.Unlock()
	_ = s.pty.Close()
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

func (s *PTYSession) WaitForExit(timeout time.Duration) error {
	// WaitForExit reports process termination. PTY drain/EOF may lag behind.
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-s.procDone:
		s.waitMu.Lock()
		defer s.waitMu.Unlock()
		if s.waitErr != nil {
			return fmt.Errorf("wait for session process: %w", s.waitErr)
		}
		return nil
	case <-timer.C:
		return errors.New("timeout waiting for session exit")
	}
}

func buildAmuxBinary() (string, func(), error) {
	if path := os.Getenv("AMUX_E2E_BIN"); path != "" {
		return path, func() {}, nil
	}

	buildOnce.Do(func() {
		tmp, err := os.MkdirTemp("", e2eBuildDirPrefix+"*")
		if err != nil {
			buildErr = err
			return
		}
		ownerPath := filepath.Join(tmp, e2eBuildOwnerFilename)
		if err := os.WriteFile(ownerPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			_ = os.RemoveAll(tmp)
			buildErr = fmt.Errorf("write e2e build owner: %w", err)
			return
		}
		out := filepath.Join(tmp, "amux")
		root, err := repoRoot()
		if err != nil {
			_ = os.RemoveAll(tmp)
			buildErr = err
			return
		}
		cmd := exec.Command("go", "build", "-o", out, "./cmd/amux")
		cmd.Dir = root
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			_ = os.RemoveAll(tmp)
			buildErr = err
			return
		}
		buildPath = out
	})

	if buildErr != nil {
		return "", func() {}, buildErr
	}

	// The binary is shared by every PTY session in this test process. Removing
	// it from an individual session cleanup makes subsequent tests reuse a path
	// that no longer exists, while never removing it leaks one directory per
	// `go test` invocation. TestMain performs the process-scoped cleanup.
	return buildPath, func() {}, nil
}

func cleanupBuiltAmuxBinary() error {
	if buildPath == "" {
		// An AMUX_E2E_BIN supplied by the caller is never owned by this test.
		return nil
	}
	dir := filepath.Dir(buildPath)
	if !strings.HasPrefix(filepath.Base(dir), e2eBuildDirPrefix) {
		return fmt.Errorf("refusing to remove unexpected build directory %q", dir)
	}
	return os.RemoveAll(dir)
}

func cleanupStaleBuiltAmuxBinaries(tempRoot string, now time.Time) error {
	return cleanupStaleBuiltAmuxBinariesWith(tempRoot, now, e2eBuildOwnerAlive)
}

func cleanupStaleBuiltAmuxBinariesWith(tempRoot string, now time.Time, ownerAlive func(int) bool) error {
	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), e2eBuildDirPrefix) {
			continue
		}
		dir := filepath.Join(tempRoot, entry.Name())
		useAgeGuard, liveOwner, ownerErr := classifyE2EBuildOwner(
			filepath.Join(dir, e2eBuildOwnerFilename),
			ownerAlive,
		)
		if ownerErr != nil {
			errs = append(errs, fmt.Errorf("read owner for %s: %w", entry.Name(), ownerErr))
			continue
		}
		if liveOwner {
			continue
		}
		if useAgeGuard {
			// Directories made by older tests have no owner marker. Retain fresh
			// ones so a concurrently starting legacy test cannot be disrupted.
			info, infoErr := entry.Info()
			if infoErr != nil {
				errs = append(errs, fmt.Errorf("stat legacy build %s: %w", entry.Name(), infoErr))
				continue
			}
			if info.ModTime().After(now) || now.Sub(info.ModTime()) < legacyE2EBuildMaxAge {
				continue
			}
		}
		if removeErr := os.RemoveAll(dir); removeErr != nil {
			errs = append(errs, fmt.Errorf("remove stale build %s: %w", entry.Name(), removeErr))
		}
	}
	return errors.Join(errs...)
}

func classifyE2EBuildOwner(ownerPath string, ownerAlive func(int) bool) (useAgeGuard, liveOwner bool, err error) {
	ownerData, err := os.ReadFile(ownerPath)
	if errors.Is(err, os.ErrNotExist) {
		return true, false, nil
	}
	if err != nil {
		return false, false, err
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(ownerData)))
	if parseErr != nil || pid <= 0 || ownerAlive == nil {
		// A concurrent writer can expose a short-lived partial marker. Treat
		// malformed markers like legacy directories and fail closed while they
		// are fresh. A missing liveness probe follows the same safe path.
		return true, false, nil
	}
	return false, ownerAlive(pid), nil
}

func e2eBuildOwnerAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
