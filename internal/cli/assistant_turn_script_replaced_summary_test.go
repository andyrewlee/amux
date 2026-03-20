package cli

import (
	"strings"
	"testing"
)

func TestAssistantTurnScript_ReplacedFileSummaryCountsAsEditClaimOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-replaced-file-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Replaced internal/foo.go with internal/bar.go.","agent_id":"agent-replaced-file-clean","workspace_id":"ws-replaced-file-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_ReplacedFileSummaryKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "internal/bar.go", "package internal\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-replaced-file-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Replaced internal/foo.go with internal/bar.go.","agent_id":"agent-replaced-file-dirty","workspace_id":"ws-replaced-file-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for replaced-file summary", payload["quick_actions"])
	}
}
