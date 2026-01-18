package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
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
	if err := session.WaitForContains("Monitor", 5*time.Second); err != nil {
		t.Fatalf("monitor header not visible: %v", err)
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
		Width:  120,
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
	if err := session.SendString("\x00?"); err != nil {
		t.Fatalf("send prefix+help exit: %v", err)
	}
	if err := session.WaitForAbsent("Prefix Key (leader key)", 5*time.Second); err != nil {
		t.Fatalf("help overlay still visible: %v", err)
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
