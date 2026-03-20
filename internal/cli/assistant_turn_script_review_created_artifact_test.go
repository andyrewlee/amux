package cli

import (
	"strings"
	"testing"
)

func TestAssistantTurnScript_ReviewedCreatedTestsAndReportedFindingsDowngradesCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-review-created-tests-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go, created tests, and reported findings.","agent_id":"agent-review-created-tests-clean","workspace_id":"ws-review-created-tests-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning for created-tests review summary", summary)
	}
}

func TestAssistantTurnScript_ReviewedCreatedTestsAndReportedFindingsKeepsReviewActionWhenDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "internal/foo_test.go", "package internal\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-review-created-tests-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go, created tests, and reported findings.","agent_id":"agent-review-created-tests-dirty","workspace_id":"ws-review-created-tests-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for created-tests review summary", payload["quick_actions"])
	}
}
