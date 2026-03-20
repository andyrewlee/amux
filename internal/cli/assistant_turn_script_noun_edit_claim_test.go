package cli

import (
	"strings"
	"testing"
)

func TestAssistantTurnScript_FixesInFileSummaryStillCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-fixes-in-file", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Fixes in internal/foo.go.","agent_id":"agent-fixes-in-file","workspace_id":"ws-fixes-in-file","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_ChangesMadeToFileStillCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-changes-made-to-file", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Changes made to internal/foo.go.","agent_id":"agent-changes-made-to-file","workspace_id":"ws-changes-made-to-file","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_FixesMadeInFileKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "README.md", "patched docs\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-fixes-made-in-file-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Fixes made in README.md.","agent_id":"agent-fixes-made-in-file-dirty","workspace_id":"ws-fixes-made-in-file-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for noun-form made-in edit summary", payload["quick_actions"])
	}
}
