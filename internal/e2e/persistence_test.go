package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

const (
	persistenceTimeout = 15 * time.Second
)

func TestTmuxPersistenceKeepsSessions(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	binDir := writeStubAssistant(t, home, "claude")
	server := fmt.Sprintf("amux-e2e-%d", time.Now().UnixNano())
	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null"}
	defer killTmuxServer(t, server)

	env := sessionEnv(binDir, server)
	session, cleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  env,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer cleanup()

	waitForUIContains(t, session, filepath.Base(repo), persistenceTimeout)
	activatePrimaryWorkspace(t, session)
	waitForUIContains(t, session, "[New agent]", persistenceTimeout)
	createAgentTab(t, session)
	waitForUIContains(t, session, "claude", persistenceTimeout)

	waitForSessionTypes(t, opts, map[string]bool{"agent": true, "terminal": true}, persistenceTimeout)

	quitApp(t, session)
	if err := session.WaitForExit(persistenceTimeout); err != nil {
		t.Fatalf("waiting for exit: %v", err)
	}

	waitForSessionTypes(t, opts, map[string]bool{"agent": true, "terminal": true}, persistenceTimeout)

	restart, restartCleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  env,
	})
	if err != nil {
		t.Fatalf("restart session: %v", err)
	}
	defer restartCleanup()

	waitForUIContains(t, restart, filepath.Base(repo), persistenceTimeout)
	activatePrimaryWorkspace(t, restart)
	waitForUIContains(t, restart, "claude", persistenceTimeout)
	quitApp(t, restart)
	if err := restart.WaitForExit(persistenceTimeout); err != nil {
		t.Fatalf("waiting for restart exit: %v", err)
	}
}

func activatePrimaryWorkspace(t *testing.T, session *PTYSession) {
	t.Helper()
	if err := session.SendString("j"); err != nil {
		t.Fatalf("send j: %v", err)
	}
	if err := session.SendString("\r"); err != nil {
		t.Fatalf("send enter: %v", err)
	}
}

func createAgentTab(t *testing.T, session *PTYSession) {
	t.Helper()
	sendPrefixCommand(t, session, "a")
	waitForUIContains(t, session, "New Agent", persistenceTimeout)
	if err := session.SendString("\r"); err != nil {
		t.Fatalf("select agent: %v", err)
	}
}

func quitApp(t *testing.T, session *PTYSession) {
	t.Helper()
	sendPrefixCommand(t, session, "q")
	waitForUIContains(t, session, "Quit AMUX", persistenceTimeout)
	if err := session.SendString("\r"); err != nil {
		t.Fatalf("confirm quit: %v", err)
	}
}

func sendPrefixCommand(t *testing.T, session *PTYSession, cmd string) {
	t.Helper()
	if err := session.SendBytes([]byte{0}); err != nil {
		t.Fatalf("send prefix: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := session.SendString(cmd); err != nil {
		t.Fatalf("send command %q: %v", cmd, err)
	}
}

func waitForSessionTypes(t *testing.T, opts tmux.Options, want map[string]bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	prefix := tmux.SessionName("amux") + "-"
	for time.Now().Before(deadline) {
		sessions, err := tmux.ListSessionsMatchingTags(map[string]string{"@amux": "1"}, opts)
		if err != nil {
			if hasSessionsWithPrefix(t, opts, prefix, len(want)) {
				return
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if len(sessions) == 0 {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		types := map[string]bool{}
		for _, session := range sessions {
			value, err := tmux.SessionTagValue(session, "@amux_type", opts)
			if err != nil {
				continue
			}
			types[strings.TrimSpace(value)] = true
		}
		ok := true
		for typ := range want {
			if !types[typ] {
				ok = false
				break
			}
		}
		if ok {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for tmux session types %v", want)
}

func waitForUIContains(t *testing.T, session *PTYSession, needle string, timeout time.Duration) {
	t.Helper()
	if err := session.WaitForContains(needle, timeout); err != nil {
		t.Fatalf("waiting for %q: %v", needle, err)
	}
}

func hasSessionsWithPrefix(t *testing.T, opts tmux.Options, prefix string, minCount int) bool {
	t.Helper()
	sessions, err := tmux.ListSessions(opts)
	if err != nil {
		return false
	}
	count := 0
	for _, name := range sessions {
		if strings.HasPrefix(name, prefix) {
			count++
		}
	}
	return count >= minCount
}

func writeRegistry(t *testing.T, home, repo string) {
	t.Helper()
	registryPath := filepath.Join(home, ".amux", "projects.json")
	registry := data.NewRegistry(registryPath)
	if err := registry.AddProject(repo); err != nil {
		t.Fatalf("add project: %v", err)
	}
}

// writeConfig creates an empty config file. The persistence parameter is ignored
// since sessions are always persisted now.
func writeConfig(t *testing.T, home string, _ bool) {
	t.Helper()
	configPath := filepath.Join(home, ".amux", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	payload := map[string]any{
		"ui": map[string]any{},
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func writeStubAssistant(t *testing.T, home, name string) string {
	t.Helper()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	scriptPath := filepath.Join(binDir, name)
	script := "#!/bin/sh\nsleep 1000\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub assistant: %v", err)
	}
	return binDir
}

func sessionEnv(binDir, server string) []string {
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/bin:/bin"
	}
	return []string{
		"PATH=" + binDir + string(os.PathListSeparator) + path,
		"AMUX_TMUX_SERVER=" + server,
		"AMUX_TMUX_CONFIG=/dev/null",
		"SHELL=/bin/sh",
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "amux@example.com")
	runGit(t, repo, "config", "user.name", "amux")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")
	return repo
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = stripGitEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
}

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func skipIfNoTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	ensureTmuxServer(t)
}

func ensureTmuxServer(t *testing.T) {
	t.Helper()
	server := fmt.Sprintf("amux-e2e-check-%d", time.Now().UnixNano())
	args := []string{"-L", server, "start-server"}
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("tmux server socket unavailable: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		killTmuxServer(t, server)
	})
	args = []string{"-L", server, "show-options", "-g"}
	cmd = exec.Command("tmux", args...)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Skipf("tmux server socket unreachable: %v\n%s", err, out)
	}
}

func killTmuxServer(t *testing.T, server string) {
	t.Helper()
	cmd := exec.Command("tmux", "-L", server, "kill-server")
	_ = cmd.Run()
}
