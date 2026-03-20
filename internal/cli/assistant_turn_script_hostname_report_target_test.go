package cli

import (
	"strings"
	"testing"
)

func TestAssistantTurnScript_UpdatedDevHostnameRecommendationsStayReportOnlyOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-dev-host-report-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated api.dev recommendations.","agent_id":"agent-dev-host-report-clean","workspace_id":"ws-dev-host-report-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the recommendations.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for bare .dev hostname report", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for bare .dev hostname report", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_UpdatedLocalHostnameRecommendationsStayReportOnlyWhenDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-local-host-report-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated api.local recommendations.","agent_id":"agent-local-host-report-dirty","workspace_id":"ws-local-host-report-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the recommendations.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for bare .local hostname report", payload["quick_actions"])
	}
}
