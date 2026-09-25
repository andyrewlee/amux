package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// Repo-supplied .amux/workspaces.json scripts are the security boundary: they
// must never execute until the user approves the exact content that prompted.
// These tests drive the real binary end to end — create a workspace, hit the
// trust prompt, and assert the filesystem outcome each way.

// writeRepoScriptConfig writes repo/.amux/workspaces.json. The setup script
// touches sentinel so the test can observe whether trusted execution ran.
// marker, when non-empty, leads the command so it shows in the trust dialog's
// truncated manifest — distinguishing a re-prompt for new content from the
// original prompt still being open.
func writeRepoScriptConfig(t *testing.T, repo, sentinel, marker string) {
	t.Helper()
	dir := filepath.Join(repo, ".amux")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir repo .amux: %v", err)
	}
	cmd := fmt.Sprintf("touch '%s'", sentinel)
	if marker != "" {
		cmd = fmt.Sprintf("echo %s; %s", marker, cmd)
	}
	payload, err := json.MarshalIndent(map[string]any{
		"setup-workspace": []string{cmd},
	}, "", "  ")
	if err != nil {
		t.Fatalf("marshal workspaces.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workspaces.json"), payload, 0o644); err != nil {
		t.Fatalf("write workspaces.json: %v", err)
	}
}

// startTrustSession builds the standard e2e fixture (repo + registry + stub
// assistant + dedicated tmux server) and returns a live session plus the
// tmux options the agent-wait helpers poll against.
func startTrustSession(t *testing.T, repo string) (*PTYSession, tmux.Options, func()) {
	t.Helper()
	home := t.TempDir()
	writeRegistry(t, home, repo)
	writeConfig(t, home, false)
	binDir := writeStubAssistant(t, home, "claude")
	server := fmt.Sprintf("amux-e2e-trust-%d", time.Now().UnixNano())
	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null"}
	t.Cleanup(func() { killTmuxServer(t, server) })

	session, cleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  sessionEnv(binDir, server),
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	return session, opts, cleanup
}

// approveTrustDialog moves the confirm cursor from the default "No" to "Yes"
// and presses Enter. It deliberately does NOT wait for the dialog to close:
// a changed-since-prompt config re-prompts under the same title, so the
// post-approval screen is the caller's observable. The atomic h+Enter write
// (same consumer for both bytes) replaces the old fixed settle sleep.
func approveTrustDialog(t *testing.T, session *PTYSession) {
	t.Helper()
	if err := session.SendString("h\r"); err != nil {
		t.Fatalf("approve trust dialog: %v", err)
	}
}

func TestRepoScriptTrustApprove(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	repo := initRepo(t)
	sentinel := filepath.Join(t.TempDir(), "setup-ran")
	writeRepoScriptConfig(t, repo, sentinel, "")

	session, opts, cleanup := startTrustSession(t, repo)
	defer cleanup()

	waitForUIContains(t, session, filepath.Base(repo), workspaceAgentTimeout)
	createWorkspaceWithAgent(t, session, "trustme", opts, workspaceAgentTimeout)

	// Setup ran into the trust gate: the prompt surfaces with the repo's
	// script content under review.
	waitForUIContains(t, session, "Trust Project Scripts", workspaceAgentTimeout)

	approveTrustDialog(t, session)

	waitForCond(t, func() bool {
		_, err := os.Stat(sentinel)
		return err == nil
	}, workspaceAgentTimeout, "setup sentinel %s to exist after trust approval", sentinel)
}

func TestRepoScriptTrustRefuse(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	repo := initRepo(t)
	sentinel := filepath.Join(t.TempDir(), "setup-ran")
	writeRepoScriptConfig(t, repo, sentinel, "")

	session, opts, cleanup := startTrustSession(t, repo)
	defer cleanup()

	waitForUIContains(t, session, filepath.Base(repo), workspaceAgentTimeout)
	createWorkspaceWithAgent(t, session, "trustme", opts, workspaceAgentTimeout)

	waitForUIContains(t, session, "Trust Project Scripts", workspaceAgentTimeout)

	// Enter on the default "No" refuses: dialog closes, nothing executes.
	if err := session.SendString("\r"); err != nil {
		t.Fatalf("refuse trust dialog: %v", err)
	}
	if err := session.WaitForAbsent("Trust Project Scripts", persistenceTimeout); err != nil {
		t.Fatalf("trust dialog should close after refusal\n\nScreen:\n%s", session.ScreenASCII())
	}

	// Refusal is terminal for this prompt — assert the sentinel never appears
	// over a generous window covering any latent retry.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sentinel); err == nil {
			t.Fatalf("setup sentinel %s exists despite trust refusal", sentinel)
		}
		time.Sleep(screenPollInterval)
	}
}

func TestRepoScriptTrustChangedSincePrompt(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	repo := initRepo(t)
	sentinelDir := t.TempDir()
	sentinelV1 := filepath.Join(sentinelDir, "setup-v1")
	sentinelV2 := filepath.Join(sentinelDir, "setup-v2")
	writeRepoScriptConfig(t, repo, sentinelV1, "")

	session, opts, cleanup := startTrustSession(t, repo)
	defer cleanup()

	waitForUIContains(t, session, filepath.Base(repo), workspaceAgentTimeout)
	createWorkspaceWithAgent(t, session, "trustme", opts, workspaceAgentTimeout)

	waitForUIContains(t, session, "Trust Project Scripts", workspaceAgentTimeout)

	// Edit the config between prompt and approval: the recorded hash no longer
	// matches, so the approval must be rejected (ErrScriptsChangedSincePrompt)
	// and the new content re-gated — neither version's script may run.
	writeRepoScriptConfig(t, repo, sentinelV2, "v2changed")

	approveTrustDialog(t, session)

	// The stale approval is rejected (ErrScriptsChangedSincePrompt): the dialog
	// closes then re-prompts against the NEW content. Don't WaitForAbsent — the
	// re-prompt lands faster than the screen poll can observe the gap. Instead
	// wait for the v2 marker in the re-rendered manifest, which only appears
	// once the dialog is showing the new content's prompt.
	waitForUIContains(t, session, "v2changed", workspaceAgentTimeout)

	if _, err := os.Stat(sentinelV1); err == nil {
		t.Fatalf("v1 sentinel %s exists — stale approval executed the script", sentinelV1)
	}
	if _, err := os.Stat(sentinelV2); err == nil {
		t.Fatalf("v2 sentinel %s exists — changed content ran without a fresh approval", sentinelV2)
	}
}
