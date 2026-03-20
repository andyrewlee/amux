package cli

import (
	"strings"
	"testing"
)

func TestAssistantTurnScript_ZeroFilesChangedStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-zero-files-changed-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"0 files changed.","agent_id":"agent-zero-files-changed-clean","workspace_id":"ws-zero-files-changed-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for zero-files-changed summary", summary)
	}
}

func TestAssistantTurnScript_ZeroModifiedFilesOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-zero-modified-files-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"0 modified files.","agent_id":"agent-zero-modified-files-dirty","workspace_id":"ws-zero-modified-files-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for zero-modified-files summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_URLPathSummaryStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-url-path-summary-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Created findings for https://api.example.com/openapi.json.","agent_id":"agent-url-path-summary-clean","workspace_id":"ws-url-path-summary-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for URL path summary", summary)
	}
}

func TestAssistantTurnScript_URLPathSummaryOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-url-path-summary-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Created findings for https://api.example.com/openapi.json.","agent_id":"agent-url-path-summary-dirty","workspace_id":"ws-url-path-summary-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for URL path summary", payload["quick_actions"])
	}
}
