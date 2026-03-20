package cli

import (
	"strings"
	"testing"
)

func TestAssistantTurnScript_GenericChangedSummaryCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-generic-changed-summary", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Changed default timeout to 5s.","agent_id":"agent-generic-changed-summary","workspace_id":"ws-generic-changed-summary","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning for generic changed summary", summary)
	}
}

func TestAssistantTurnScript_ReviewedRemovedFieldFromResponseOmitsReviewAction(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-removed-field-response", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed schema; removed field foo from response.","agent_id":"agent-reviewed-removed-field-response","workspace_id":"ws-reviewed-removed-field-response","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for observed removal finding", payload["quick_actions"])
	}
}
