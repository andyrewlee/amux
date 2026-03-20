package cli

import (
	"strings"
	"testing"
)

func TestAssistantTurnScript_AddedTestsCountsAsEditClaimOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-added-tests-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Added tests.","agent_id":"agent-added-tests-clean","workspace_id":"ws-added-tests-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_DeletedDeprecatedHandlersKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "internal/handlers/deprecated.go", "package handlers\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-deleted-deprecated-handlers-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Deleted deprecated handlers.","agent_id":"agent-deleted-deprecated-handlers-dirty","workspace_id":"ws-deleted-deprecated-handlers-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for deleted-handlers summary", payload["quick_actions"])
	}
}
