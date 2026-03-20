package cli

import (
	"strings"
	"testing"
)

func TestAssistantTurnScript_ChangedFilesSummaryWithoutPathCountsAsEditClaimOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-changed-files-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Changed files and tests passed.","agent_id":"agent-changed-files-clean","workspace_id":"ws-changed-files-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
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

func TestAssistantTurnScript_ReviewedChangedFilesSummaryStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-changed-files-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed changed files; no issues found.","agent_id":"agent-reviewed-changed-files-clean","workspace_id":"ws-reviewed-changed-files-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for review-only changed-files summary", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for review-only changed-files summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedChangedFilesAndMadeFixesCountsAsEditClaimOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-changed-files-made-fixes-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed changed files; made fixes.","agent_id":"agent-reviewed-changed-files-made-fixes-clean","workspace_id":"ws-reviewed-changed-files-made-fixes-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
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

func TestAssistantTurnScript_ChangedFilesNoneSummaryStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-changed-files-none-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Changed files: none.","agent_id":"agent-changed-files-none-clean","workspace_id":"ws-changed-files-none-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for changed-files-none summary", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for changed-files-none summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_NoChangedFilesSummaryStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-no-changed-files-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"No changed files.","agent_id":"agent-no-changed-files-clean","workspace_id":"ws-no-changed-files-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for no-changed-files summary", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for no-changed-files summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_NoModifiedFilesSummaryStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-no-modified-files-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"No modified files.","agent_id":"agent-no-modified-files-clean","workspace_id":"ws-no-modified-files-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for no-modified-files summary", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for no-modified-files summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedFileAndCreatedRemediationPlanStaysCompleted(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-plan", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and created a remediation plan.","agent_id":"agent-reviewed-plan","workspace_id":"ws-reviewed-plan","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the plan.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want review plan summary to stay completed", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review plan summary to omit review action", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ChangedFilesReviewedSummaryStaysCompleted(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-changed-files-reviewed", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Changed files reviewed: internal/foo.go.","agent_id":"agent-changed-files-reviewed","workspace_id":"ws-changed-files-reviewed","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want passive changed-files review summary to stay completed", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want passive changed-files review summary to omit review action", payload["quick_actions"])
	}
}
