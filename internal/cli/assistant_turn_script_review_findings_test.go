package cli

import (
	"strings"
	"testing"
)

func TestAssistantTurnScript_ReviewedBreakingChangeFindingStaysCompleted(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-breaking-change", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed API schema; removed field foo is a breaking change.","agent_id":"agent-reviewed-breaking-change","workspace_id":"ws-reviewed-breaking-change","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want observed-change review finding to stay completed", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for observed-change review finding", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedAndFixedIssueCountsAsEditClaimOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-fixed-issue", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed the API and fixed the issue.","agent_id":"agent-reviewed-fixed-issue","workspace_id":"ws-reviewed-fixed-issue","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_ReviewedAndRemovedCompatibilityRiskKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-removed-compatibility-risk-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed auth flow and removed the compatibility risk.","agent_id":"agent-reviewed-removed-compatibility-risk-dirty","workspace_id":"ws-reviewed-removed-compatibility-risk-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for post-review compatibility-risk fix", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_RemovedFieldBreakingChangeStaysCompletedWithoutReviewVerb(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-removed-field-breaking-change", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Removed field foo is a breaking change.","agent_id":"agent-removed-field-breaking-change","workspace_id":"ws-removed-field-breaking-change","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want non-review-verb observed-change finding to stay completed", summary)
	}
}

func TestAssistantTurnScript_RemovedFieldBreakingChangeOmitsReviewActionWithoutReviewVerb(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-removed-field-breaking-change-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Removed field foo is a breaking change.","agent_id":"agent-removed-field-breaking-change-dirty","workspace_id":"ws-removed-field-breaking-change-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for non-review-verb observed-change finding", payload["quick_actions"])
	}
}
