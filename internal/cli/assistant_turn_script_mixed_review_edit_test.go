package cli

import (
	"strings"
	"testing"
)

func TestAssistantTurnScript_ReviewedRecommendationsThenFixedFileCountsAsEditClaimOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-recommendations-then-fixed-file", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed auth flow and updated recommendations, then fixed internal/foo.go.","agent_id":"agent-reviewed-recommendations-then-fixed-file","workspace_id":"ws-reviewed-recommendations-then-fixed-file","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_ReviewedUpdatedFilesThenFixedReadmeKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "README.md", "docs\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-updated-files-then-fixed-readme-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed updated files; no issues found and fixed README.md.","agent_id":"agent-reviewed-updated-files-then-fixed-readme-dirty","workspace_id":"ws-reviewed-updated-files-then-fixed-readme-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for mixed reviewed-updated-files summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_UpdatedFindingsThenFixedReadmeKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "README.md", "docs\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-updated-findings-then-fixed-readme-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated findings for internal/foo.go and fixed README.md.","agent_id":"agent-updated-findings-then-fixed-readme-dirty","workspace_id":"ws-updated-findings-then-fixed-readme-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for mixed targeted-report summary", payload["quick_actions"])
	}
}
