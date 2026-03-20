package cli

import (
	"strings"
	"testing"
)

func TestAssistantTurnScript_ImplementedRecommendationsStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-implemented-recommendations", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Implemented recommendations.","agent_id":"agent-implemented-recommendations","workspace_id":"ws-implemented-recommendations","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the recommendations.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for implemented-recommendations summary", summary)
	}
}

func TestAssistantTurnScript_AppliedRemediationOmitsReviewActionWhenDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-applied-remediation", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Applied remediation.","agent_id":"agent-applied-remediation","workspace_id":"ws-applied-remediation","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for applied-remediation summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_UpdatedReportStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-updated-report", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated report.","agent_id":"agent-updated-report","workspace_id":"ws-updated-report","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the report.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for updated-report summary", summary)
	}
}

func TestAssistantTurnScript_AddedSummaryStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-added-summary", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Added summary.","agent_id":"agent-added-summary","workspace_id":"ws-added-summary","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the summary.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for added-summary summary", summary)
	}
}

func TestAssistantTurnScript_UpdatedReportForFileOmitsReviewActionWhenDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-updated-report-for-file", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated report for internal/foo.go.","agent_id":"agent-updated-report-for-file","workspace_id":"ws-updated-report-for-file","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the report.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for updated-report-for-file summary", payload["quick_actions"])
	}
}
