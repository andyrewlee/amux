package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initCleanGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "-q", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v\n%s", dir, err, string(out))
	}
}

func runAssistantTurnCleanTreeCase(t *testing.T, workspaceID, workspaceRoot, stepJSON string) map[string]any {
	t.Helper()

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-turn.sh")
	fakeDir := t.TempDir()
	fakeStepPath := filepath.Join(fakeDir, "fake-step.sh")
	fakeAmuxPath := filepath.Join(fakeDir, "amux")

	writeExecutable(t, fakeStepPath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "${FAKE_STEP_JSON:?missing FAKE_STEP_JSON}"
`)
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' "${FAKE_WORKSPACE_LIST_JSON:?missing FAKE_WORKSPACE_LIST_JSON}"
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "AMUX_ASSISTANT_TURN_STEP_SCRIPT", fakeStepPath)
	env = withEnv(env, "AMUX_BIN", fakeAmuxPath)
	env = withEnv(env, "FAKE_STEP_JSON", stepJSON)
	env = withEnv(env, "FAKE_WORKSPACE_LIST_JSON", `{"ok":true,"data":[{"id":"`+workspaceID+`","root":"`+workspaceRoot+`"}],"error":null}`)

	return runScriptJSON(t, scriptPath, env,
		"run",
		"--workspace", workspaceID,
		"--assistant", "codex",
		"--prompt", "Handle the requested work",
		"--max-steps", "1",
		"--turn-budget", "30",
		"--wait-timeout", "1s",
		"--idle-threshold", "1s",
	)
}

func hasQuickActionID(payload map[string]any, want string) bool {
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok {
		return false
	}
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if got, _ := action["action_id"].(string); got == want {
			return true
		}
	}
	return false
}

func TestAssistantTurnScript_ReviewStyleFileSummaryKeepsCompletedStatusOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-review", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/cli/cmd_task_snapshot.go and README.md; no changes required.","agent_id":"agent-review","workspace_id":"ws-review","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want review-style file references to stay completed", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review-only completion to omit review action", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewStyleNoChangesMadeKeepsCompletedStatusOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-review-made", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go; no changes made.","agent_id":"agent-review-made","workspace_id":"ws-review-made","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want review-style no-op summary to stay completed", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for no-op review summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewStyleNoFixesNeededKeepsCompletedStatusOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-review-fixes", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go; no fixes needed.","agent_id":"agent-review-fixes","workspace_id":"ws-review-fixes","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want review-style no-fixes summary to stay completed", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for no-fixes review summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_EditClaimOnCleanTreeBecomesPartial(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-edit", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated internal/cli/cmd_task_snapshot.go and README.md.","agent_id":"agent-edit","workspace_id":"ws-edit","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_UpdatedWithChangesRequiredStillCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-edit-required", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated internal/foo.go with changes required for auth flow.","agent_id":"agent-edit-required","workspace_id":"ws-edit-required","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_UpdatedWithNoChangesRequiredElsewhereStillCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-edit-no-elsewhere", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated internal/foo.go; no changes required elsewhere.","agent_id":"agent-edit-no-elsewhere","workspace_id":"ws-edit-no-elsewhere","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_ImprovedSummaryStillCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-improved", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Improved internal/foo.go and README.md.","agent_id":"agent-improved","workspace_id":"ws-improved","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_RewroteSummaryStillCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-rewrote", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Rewrote cmd/task.go.","agent_id":"agent-rewrote","workspace_id":"ws-rewrote","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_FileOnlySummaryStillCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-file-only", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"- NOTES.md","agent_id":"agent-file-only","workspace_id":"ws-file-only","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_MultiFileBareSummaryStillCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-multi-file", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"internal/foo.go, README.md","agent_id":"agent-multi-file","workspace_id":"ws-multi-file","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_NaturalLanguageFileListStillCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-natural-list", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"internal/foo.go and README.md","agent_id":"agent-natural-list","workspace_id":"ws-natural-list","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_FilesPrefixSummaryStillCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-files-prefix", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Files: internal/foo.go, README.md","agent_id":"agent-files-prefix","workspace_id":"ws-files-prefix","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_NumberedFileOnlySummaryStillCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-numbered-file", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"1. internal/foo.go","agent_id":"agent-numbered-file","workspace_id":"ws-numbered-file","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_AddedSummaryOnCleanTreeBecomesPartial(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-added", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Added NOTES.md with status update.","agent_id":"agent-added","workspace_id":"ws-added","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_SummaryOfFixesOnCleanTreeBecomesPartial(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-fixes", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Summary of fixes: internal/cli/cmd_task_snapshot.go and README.md.","agent_id":"agent-fixes","workspace_id":"ws-fixes","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_RefactorSummaryWithoutFileSignalsBecomesPartial(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-refactor", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Refactor applied and tests passed.","agent_id":"agent-refactor","workspace_id":"ws-refactor","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Review uncommitted changes.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action after clean-tree downgrade", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReportOnlySummaryOmitsReviewAction(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-report-only", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Created a remediation plan and reported findings.","agent_id":"agent-report-only","workspace_id":"ws-report-only","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the plan.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want report-only summary to stay completed", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want report-only summary to omit review action", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedAndWroteFindingsToWorkspaceFileCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-wrote-findings-file", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and wrote findings to NOTES.md.","agent_id":"agent-reviewed-wrote-findings-file","workspace_id":"ws-reviewed-wrote-findings-file","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the notes.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_ChangesRequiredSummaryKeepsCompletedWithoutReviewAction(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-required", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Changes required in auth flow.","agent_id":"agent-required","workspace_id":"ws-required","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Ask whether to apply the requested follow-up work.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want requested-work summary to omit review action", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_FixesRequiredSummaryKeepsCompletedWithoutReviewAction(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-fixes-required", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Fixes required in internal/foo.go.","agent_id":"agent-fixes-required","workspace_id":"ws-fixes-required","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Ask whether to apply the requested follow-up work.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want fixes-required summary to omit review action", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewFoundFixesRequiredKeepsCompletedWithoutReviewAction(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-review-found-fixes", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Review found fixes required in internal/foo.go.","agent_id":"agent-review-found-fixes","workspace_id":"ws-review-found-fixes","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want review findings summary to stay completed", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review findings summary to omit review action", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_SummaryOfChangesRequiredKeepsCompletedWithoutReviewAction(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-summary-required", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Summary of changes required for auth flow.","agent_id":"agent-summary-required","workspace_id":"ws-summary-required","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Ask whether to apply the requested follow-up work.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want summary-of-required-work to omit review action", payload["quick_actions"])
	}
}
