package cli

import (
	"strings"
	"testing"
)

func TestAssistantTurnScript_CreatedPlanForWorkspaceFileStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-created-plan-target-file-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Created plan for internal/foo.go.","agent_id":"agent-created-plan-target-file-clean","workspace_id":"ws-created-plan-target-file-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the plan.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for targeted plan summary", summary)
	}
}

func TestAssistantTurnScript_BareHostPathFindingsSummaryOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-bare-host-path-findings-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Created findings for api.example.com/openapi.json.","agent_id":"agent-bare-host-path-findings-dirty","workspace_id":"ws-bare-host-path-findings-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for bare host/path findings summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_AppliedChangesSummaryBecomesPartialOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-applied-changes-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Applied the changes and tests passed.","agent_id":"agent-applied-changes-clean","workspace_id":"ws-applied-changes-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning for applied-changes summary", summary)
	}
}

func TestAssistantTurnScript_MadeRequiredFixesSummaryBecomesPartialOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-made-required-fixes-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Made the required fixes.","agent_id":"agent-made-required-fixes-clean","workspace_id":"ws-made-required-fixes-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning for made-required-fixes summary", summary)
	}
}

func TestAssistantTurnScript_ReviewedUpdatedCodeOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-updated-code-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed the updated code; no issues found.","agent_id":"agent-reviewed-updated-code-dirty","workspace_id":"ws-reviewed-updated-code-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for review-only updated-code summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedFindingsAndUpdatedFileCountsAsEditClaimOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-findings-updated-file-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed findings and updated internal/foo.go.","agent_id":"agent-reviewed-findings-updated-file-clean","workspace_id":"ws-reviewed-findings-updated-file-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning for mixed findings-and-file-edit summary", summary)
	}
}

func TestAssistantTurnScript_UpdatedImplementationPlanOmitsReviewAction(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-updated-implementation-plan", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated recommendations.","agent_id":"agent-updated-implementation-plan","workspace_id":"ws-updated-implementation-plan","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the updated implementation plan.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for implementation-plan follow-up", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ZeroModifiedFilesContextOmitsReviewAction(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-zero-modified-files-context", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated recommendations.","agent_id":"agent-zero-modified-files-context","workspace_id":"ws-zero-modified-files-context","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Files changed: 0. Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for zero-file review context", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedAndAddedPlanToWorkspaceFileCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-added-plan-readme", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and added plan to README.md.","agent_id":"agent-reviewed-added-plan-readme","workspace_id":"ws-reviewed-added-plan-readme","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning for file-backed added-plan summary", summary)
	}
}

func TestAssistantTurnScript_ReviewedAndImprovedNotesInWorkspaceFileKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "NOTES.md", "notes\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-improved-notes-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and improved notes in NOTES.md.","agent_id":"agent-reviewed-improved-notes-dirty","workspace_id":"ws-reviewed-improved-notes-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for file-backed improved-notes summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ImprovedRecommendationsStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-improved-recommendations-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Improved recommendations.","agent_id":"agent-improved-recommendations-clean","workspace_id":"ws-improved-recommendations-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the recommendations.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for improved-recommendations summary", summary)
	}
}

func TestAssistantTurnScript_AddedNotesOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-added-notes-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Added notes.","agent_id":"agent-added-notes-dirty","workspace_id":"ws-added-notes-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the notes.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for added-notes summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewRefactorPlanContextOmitsReviewAction(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-review-refactor-plan-context", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated recommendations.","agent_id":"agent-review-refactor-plan-context","workspace_id":"ws-review-refactor-plan-context","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Review refactor plan with the team.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for refactor-plan context", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedAndRevisedRecommendationsStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-revised-recommendations-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and revised recommendations.","agent_id":"agent-reviewed-revised-recommendations-clean","workspace_id":"ws-reviewed-revised-recommendations-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for revised-recommendations summary", summary)
	}
}

func TestAssistantTurnScript_ModifiedApproachAfterInvestigationStaysCompleted(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-modified-approach-after-investigation", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Modified approach after investigation.","agent_id":"agent-modified-approach-after-investigation","workspace_id":"ws-modified-approach-after-investigation","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for non-code approach summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewRewriteRecommendationsContextOmitsReviewAction(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-review-rewrite-recommendations-context", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated recommendations.","agent_id":"agent-review-rewrite-recommendations-context","workspace_id":"ws-review-rewrite-recommendations-context","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Review rewrite recommendations with the team.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for rewrite-recommendations context", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_FileListNoChangesRequiredStaysCompleted(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-file-list-no-changes-required", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"internal/foo.go: no changes required.","agent_id":"agent-file-list-no-changes-required","workspace_id":"ws-file-list-no-changes-required","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for no-op file-list summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_FileListNoIssuesFoundStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-file-list-no-issues-found", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"README.md; no issues found.","agent_id":"agent-file-list-no-issues-found","workspace_id":"ws-file-list-no-issues-found","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for no-op file-list summary", summary)
	}
}

func TestAssistantTurnScript_ChangesToFileWereRequiredStaysCompleted(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-changes-to-file-were-required", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Changes to internal/foo.go were required.","agent_id":"agent-changes-to-file-were-required","workspace_id":"ws-changes-to-file-were-required","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for required-change findings summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedAndTightenedRecommendationsStaysCompleted(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-tightened-recommendations", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed auth flow and tightened recommendations.","agent_id":"agent-reviewed-tightened-recommendations","workspace_id":"ws-reviewed-tightened-recommendations","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for tightened-recommendations summary", summary)
	}
}

func TestAssistantTurnScript_ReviewedAndStreamlinedPlanOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-streamlined-plan-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed API and streamlined plan.","agent_id":"agent-reviewed-streamlined-plan-dirty","workspace_id":"ws-reviewed-streamlined-plan-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for streamlined-plan summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_RefactoredRolloutPlanStaysCompleted(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-refactored-rollout-plan", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Refactored rollout plan.","agent_id":"agent-refactored-rollout-plan","workspace_id":"ws-refactored-rollout-plan","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the plan.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for refactored-rollout-plan summary", summary)
	}
}

func TestAssistantTurnScript_ReworkedRecommendationsOmitReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reworked-recommendations-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reworked recommendations.","agent_id":"agent-reworked-recommendations-dirty","workspace_id":"ws-reworked-recommendations-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for reworked-recommendations summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedAndRewroteRecommendationsStaysCompleted(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-rewrote-recommendations", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and rewrote recommendations.","agent_id":"agent-reviewed-rewrote-recommendations","workspace_id":"ws-reviewed-rewrote-recommendations","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for rewrote-recommendations summary", summary)
	}
}
