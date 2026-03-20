package cli

import (
	"strings"
	"testing"
)

func TestAssistantTurnScript_ReviewedUpdatedFilesStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-updated-files-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed updated files; no issues found.","agent_id":"agent-reviewed-updated-files-clean","workspace_id":"ws-reviewed-updated-files-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for review-only updated-files summary", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for review-only updated-files summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedUpdatedFilesOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-updated-files-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed updated files; no issues found.","agent_id":"agent-reviewed-updated-files-dirty","workspace_id":"ws-reviewed-updated-files-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for dirty review-only updated-files summary", payload["quick_actions"])
	}
}
