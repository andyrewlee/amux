package cli

import (
	"strings"
	"testing"
)

func TestAssistantTurnScript_ReviewedAndUpdatedRecommendationsStaysCompleted(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-updated-recommendations", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and updated recommendations.","agent_id":"agent-reviewed-updated-recommendations","workspace_id":"ws-reviewed-updated-recommendations","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want review recommendation summary to stay completed", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for review-only recommendation update", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedAndUpdatedRecommendationsFileCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-updated-recommendations-file", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and updated recommendations.go.","agent_id":"agent-reviewed-updated-recommendations-file","workspace_id":"ws-reviewed-updated-recommendations-file","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_ReviewedModifiedFileOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-modified-file", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed modified internal/foo.go; no issues found.","agent_id":"agent-reviewed-modified-file","workspace_id":"ws-reviewed-modified-file","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for modified-file review summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedAndUpdatedRecommendationsFileKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "recommendations.go", "package recommendations\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-updated-recommendations-file-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and updated recommendations.go.","agent_id":"agent-reviewed-updated-recommendations-file-dirty","workspace_id":"ws-reviewed-updated-recommendations-file-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for recommendations.go edit summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_WroteWorkspaceFileCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-wrote-workspace-file", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Wrote README.md.","agent_id":"agent-wrote-workspace-file","workspace_id":"ws-wrote-workspace-file","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_WroteWorkspaceFileKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "README.md", "docs\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-wrote-workspace-file-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Wrote README.md.","agent_id":"agent-wrote-workspace-file-dirty","workspace_id":"ws-wrote-workspace-file-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for wrote-file summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_DocumentedFindingsInWorkspaceFileCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-documented-findings-file", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and documented findings in NOTES.md.","agent_id":"agent-documented-findings-file","workspace_id":"ws-documented-findings-file","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the notes.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_DocumentedFindingsInWorkspaceFileKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "NOTES.md", "review findings\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-documented-findings-file-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and documented findings in NOTES.md.","agent_id":"agent-documented-findings-file-dirty","workspace_id":"ws-documented-findings-file-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the notes.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for documented-findings file summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_SavedFindingsToWorkspaceFileCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-saved-findings-file", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Saved findings to docs/report.md.","agent_id":"agent-saved-findings-file","workspace_id":"ws-saved-findings-file","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the report.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_SavedFindingsToWorkspaceFileKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "docs/report.md", "findings\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-saved-findings-file-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Saved findings to docs/report.md.","agent_id":"agent-saved-findings-file-dirty","workspace_id":"ws-saved-findings-file-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the report.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for saved-findings file summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedAndUpdatedRemediationInWorkspaceFileCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-updated-remediation-file", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and updated remediation in NOTES.md.","agent_id":"agent-reviewed-updated-remediation-file","workspace_id":"ws-reviewed-updated-remediation-file","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the notes.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_ReviewedAndUpdatedRemediationInWorkspaceFileKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "NOTES.md", "review findings\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-updated-remediation-file-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and updated remediation in NOTES.md.","agent_id":"agent-reviewed-updated-remediation-file-dirty","workspace_id":"ws-reviewed-updated-remediation-file-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the notes.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for updated-remediation file summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedAndUpdatedSummaryInWorkspaceFileCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-updated-summary-file", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and updated summary in README.md.","agent_id":"agent-reviewed-updated-summary-file","workspace_id":"ws-reviewed-updated-summary-file","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the summary.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_ReviewedAndModifiedReportInWorkspaceFileKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "NOTES.md", "report\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-modified-report-file-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and modified report in NOTES.md.","agent_id":"agent-reviewed-modified-report-file-dirty","workspace_id":"ws-reviewed-modified-report-file-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the report.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for modified-report file summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_RequiredAndAppliedFileChangesCountAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-required-and-applied-file-changes", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Changes to README.md were required and applied.","agent_id":"agent-required-and-applied-file-changes","workspace_id":"ws-required-and-applied-file-changes","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_RequiredAndAppliedPatchKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "foo.go", "package main\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-required-and-applied-patch-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Patch for foo.go was required and applied.","agent_id":"agent-required-and-applied-patch-dirty","workspace_id":"ws-required-and-applied-patch-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for required-and-applied patch summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedAndUpdatedWorkspaceFileWithNoChangesElsewhereCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-updated-file-no-elsewhere", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and updated README.md; no changes required elsewhere.","agent_id":"agent-reviewed-updated-file-no-elsewhere","workspace_id":"ws-reviewed-updated-file-no-elsewhere","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_ReviewedAndUpdatedWorkspaceFileWithNoChangesElsewhereKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "README.md", "updated docs\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-updated-file-no-elsewhere-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and updated README.md; no changes required elsewhere.","agent_id":"agent-reviewed-updated-file-no-elsewhere-dirty","workspace_id":"ws-reviewed-updated-file-no-elsewhere-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for updated-file summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedModifiedFileAndSimplifiedAnotherFileCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-modified-and-simplified", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed modified internal/foo.go and simplified README.md; no issues found.","agent_id":"agent-reviewed-modified-and-simplified","workspace_id":"ws-reviewed-modified-and-simplified","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_ReviewedModifiedFileAndSimplifiedAnotherFileKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "README.md", "updated docs\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-modified-and-simplified-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed modified internal/foo.go and simplified README.md; no issues found.","agent_id":"agent-reviewed-modified-and-simplified-dirty","workspace_id":"ws-reviewed-modified-and-simplified-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for mixed review-edit summary", payload["quick_actions"])
	}
}
