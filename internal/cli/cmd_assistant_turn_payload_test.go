package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runGitWithEnv(t *testing.T, dir string, env map[string]string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func TestBuildAssistantTurnPayload_KeepsCompletedAfterCommittedEdits(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoRoot := t.TempDir()
	runGit(t, repoRoot, "init")
	runGit(t, repoRoot, "config", "user.email", "test@example.com")
	runGit(t, repoRoot, "config", "user.name", "amux-test")

	initialCommitTime := time.Now().Add(-2 * time.Hour)
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("before\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runGit(t, repoRoot, "add", "README.md")
	runGitWithEnv(t, repoRoot, map[string]string{
		"GIT_AUTHOR_DATE":    initialCommitTime.Format(time.RFC3339),
		"GIT_COMMITTER_DATE": initialCommitTime.Format(time.RFC3339),
	}, "commit", "-m", "init")

	turnStart := time.Now().Add(-1 * time.Hour)
	commitDuringTurn := time.Now().Add(-30 * time.Minute)
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("after\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	runGit(t, repoRoot, "add", "README.md")
	runGitWithEnv(t, repoRoot, map[string]string{
		"GIT_AUTHOR_DATE":    commitDuringTurn.Format(time.RFC3339),
		"GIT_COMMITTER_DATE": commitDuringTurn.Format(time.RFC3339),
	}, "commit", "-m", "turn commit")

	fakeAmuxPath := filepath.Join(t.TempDir(), "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "$*" in
  "workspace list --all"|"workspace list")
    printf '%s' "${FAKE_WORKSPACE_LIST_JSON:?missing FAKE_WORKSPACE_LIST_JSON}"
    ;;
  *)
    printf '%s' '{"ok":false,"error":{"code":"unexpected","message":"unexpected args"}}'
    exit 2
    ;;
esac
`)
	t.Setenv("FAKE_WORKSPACE_LIST_JSON", `{"ok":true,"data":[{"id":"ws-1","root":"`+repoRoot+`"}],"error":null}`)

	payload, err := buildAssistantTurnPayload(
		assistantTurnOptions{Mode: assistantStepModeRun},
		assistantTurnRuntime{
			MaxSteps:             1,
			TurnBudgetSeconds:    180,
			TimeoutStreakLimit:   2,
			ChunkChars:           4000,
			Verbosity:            "quiet",
			InlineButtonsEnabled: true,
			StepScriptCmdRef:     "assistant-step.sh",
			TurnScriptCmdRef:     "assistant-turn.sh",
			AMUXBin:              fakeAmuxPath,
		},
		assistantTurnState{
			StartTime:             turnStart,
			StepsUsed:             1,
			LastStatus:            "idle",
			LastSummary:           "Updated README.md and committed the change.",
			LastAgentID:           "agent-1",
			LastWorkspaceID:       "ws-1",
			LastAssistantOut:      "codex",
			LastSubstantiveOutput: true,
		},
	)
	if err != nil {
		t.Fatalf("buildAssistantTurnPayload() error = %v", err)
	}
	if payload.OverallStatus != "completed" {
		t.Fatalf("overall_status = %q, want completed", payload.OverallStatus)
	}
	if strings.Contains(payload.Summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, did not expect clean-tree downgrade after commit", payload.Summary)
	}
}
