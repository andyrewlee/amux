package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func TestPTYMonitorToggle(t *testing.T) {
	if os.Getenv("AMUX_PTY_TESTS") == "0" {
		t.Skip("AMUX_PTY_TESTS=0")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	session, cleanup, err := StartPTYSession(PTYOptions{Width: 120, Height: 30})
	if err != nil {
		t.Fatalf("start PTY: %v", err)
	}
	defer cleanup()

	if err := session.WaitForContains("New project", 5*time.Second); err != nil {
		t.Fatalf("welcome not visible: %v", err)
	}

	if err := session.SendString("\x00m"); err != nil {
		t.Fatalf("send prefix+monitor: %v", err)
	}
	if err := session.WaitForContains("[Exit]", 5*time.Second); err != nil {
		t.Fatalf("monitor header not visible: %v", err)
	}

	if err := session.SendString("\r"); err != nil {
		t.Fatalf("send enter in monitor: %v", err)
	}
	if err := session.WaitForContains("[Exit]", 5*time.Second); err != nil {
		t.Fatalf("monitor exited after enter: %v", err)
	}

	if err := session.SendString("\x00m"); err != nil {
		t.Fatalf("send prefix+monitor exit: %v", err)
	}
	if err := session.WaitForContains("New project", 5*time.Second); err != nil {
		t.Fatalf("welcome not visible after exit: %v", err)
	}
}

func TestPTYActivateWorktreeAndHelp(t *testing.T) {
	if os.Getenv("AMUX_PTY_TESTS") == "0" {
		t.Skip("AMUX_PTY_TESTS=0")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	var repoName string
	setup := func(home string) error {
		repoPath, name, err := createGitRepo(home)
		if err != nil {
			return err
		}
		repoName = name
		reg := data.NewRegistry(filepath.Join(home, ".amux", "projects.json"))
		return reg.Save([]string{repoPath})
	}

	session, cleanup, err := StartPTYSession(PTYOptions{
		Width:  160,
		Height: 30,
		Setup:  setup,
	})
	if err != nil {
		t.Fatalf("start PTY: %v", err)
	}
	defer cleanup()

	if err := session.WaitForContains(repoName, 5*time.Second); err != nil {
		t.Fatalf("repo not visible in dashboard: %v", err)
	}

	if err := session.SendString("j"); err != nil {
		t.Fatalf("send down: %v", err)
	}
	if err := session.SendString("\r"); err != nil {
		t.Fatalf("send enter: %v", err)
	}
	if err := session.WaitForContains("Branch: main", 5*time.Second); err != nil {
		t.Fatalf("worktree info not visible: %v", err)
	}

	if err := session.SendString("\x00?"); err != nil {
		t.Fatalf("send prefix+help: %v", err)
	}
	if err := session.WaitForContains("Prefix Key (leader key)", 5*time.Second); err != nil {
		t.Fatalf("help overlay not visible: %v", err)
	}
	if err := session.SendString("\x1b"); err != nil { // Esc to close help
		t.Fatalf("send esc to close help: %v", err)
	}
	if err := session.WaitForAbsent("Prefix Key (leader key)", 5*time.Second); err != nil {
		t.Fatalf("help overlay still visible: %v", err)
	}
}

func TestPTYClickUntrackedFileOpensDiff(t *testing.T) {
	if os.Getenv("AMUX_PTY_TESTS") == "0" {
		t.Skip("AMUX_PTY_TESTS=0")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	setup := func(home string) error {
		repoPath, _, err := createGitRepo(home)
		if err != nil {
			return err
		}
		reg := data.NewRegistry(filepath.Join(home, ".amux", "projects.json"))
		return reg.Save([]string{repoPath})
	}

	session, cleanup, err := StartPTYSession(PTYOptions{
		Width:  160,
		Height: 30,
		Setup:  setup,
	})
	if err != nil {
		t.Fatalf("start PTY: %v", err)
	}
	defer cleanup()

	if err := session.WaitForContains("repo-main", 5*time.Second); err != nil {
		t.Fatalf("repo not visible in dashboard: %v", err)
	}
	if err := session.SendString("j"); err != nil {
		t.Fatalf("send down: %v", err)
	}
	if err := session.SendString("\r"); err != nil {
		t.Fatalf("send enter: %v", err)
	}
	if err := session.WaitForContains("Branch: main", 5*time.Second); err != nil {
		t.Fatalf("worktree info not visible: %v", err)
	}
	if err := session.SendString("\x00l"); err != nil {
		t.Fatalf("focus sidebar: %v", err)
	}
	if err := session.SendString("g"); err != nil {
		t.Fatalf("refresh status: %v", err)
	}
	if err := session.WaitForContains("AGENTS.md", 5*time.Second); err != nil {
		t.Fatalf("untracked file not visible: %v\nscreen:\n%s", err, session.ScreenASCII())
	}

	screen := session.ScreenASCII()
	x, y, ok := findText(screen, "AGENTS.md")
	if !ok {
		t.Fatalf("failed to locate AGENTS.md on screen")
	}

	if err := sendMouseClick(session, x, y); err != nil {
		t.Fatalf("send mouse click: %v", err)
	}
	if err := session.WaitForContains("Diff: AGENTS.md", 5*time.Second); err != nil {
		t.Fatalf("diff tab not visible: %v", err)
	}
}

func createGitRepo(home string) (string, string, error) {
	repoPath := filepath.Join(home, "repo-main")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		return "", "", err
	}
	if err := runGit(repoPath, "init", "-b", "main"); err != nil {
		return "", "", err
	}
	readme := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(readme, []byte("hello\n"), 0o644); err != nil {
		return "", "", err
	}
	if err := runGit(repoPath, "add", "."); err != nil {
		return "", "", err
	}
	if err := runGit(repoPath, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-m", "init"); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(filepath.Join(repoPath, "AGENTS.md"), []byte("test\n"), 0o644); err != nil {
		return "", "", err
	}
	return repoPath, filepath.Base(repoPath), nil
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = stripGitEnv(os.Environ())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func sendMouseClick(session *PTYSession, x, y int) error {
	// SGR mouse sequence uses 1-based coordinates.
	press := fmt.Sprintf("\x1b[<0;%d;%dM", x+1, y+1)
	release := fmt.Sprintf("\x1b[<0;%d;%dm", x+1, y+1)
	if err := session.SendString(press); err != nil {
		return err
	}
	return session.SendString(release)
}

func findText(screen, target string) (int, int, bool) {
	lines := strings.Split(screen, "\n")
	for y, line := range lines {
		if idx := strings.Index(line, target); idx >= 0 {
			return idx, y, true
		}
	}
	return 0, 0, false
}
