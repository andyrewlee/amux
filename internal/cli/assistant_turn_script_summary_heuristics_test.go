package cli

import (
	"strings"
	"testing"
)

func TestAssistantTurnScript_ModifiedFilesSummaryWithoutPathCountsAsEditClaimOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-modified-files-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Modified files and tests passed.","agent_id":"agent-modified-files-clean","workspace_id":"ws-modified-files-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action after clean-tree downgrade", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ModifiedFilesSummaryKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "go.mod", "module example.com/test\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-modified-files-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Modified files and tests passed.","agent_id":"agent-modified-files-dirty","workspace_id":"ws-modified-files-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for modified-files summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_UpdatedRecommendationsSummaryOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-updated-recommendations-report-only", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated recommendations.","agent_id":"agent-updated-recommendations-report-only","workspace_id":"ws-updated-recommendations-report-only","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the recommendations.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for report-only recommendation summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_UpdatedRolloutPlanSummaryOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-updated-rollout-plan-report-only", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated rollout plan.","agent_id":"agent-updated-rollout-plan-report-only","workspace_id":"ws-updated-rollout-plan-report-only","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the plan.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for report-only rollout plan summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_DomainMentionReportSummaryStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-domain-report-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Created findings for api.example.com.","agent_id":"agent-domain-report-clean","workspace_id":"ws-domain-report-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for domain mention", summary)
	}
}

func TestAssistantTurnScript_NumberedReviewedModifiedFileSummaryStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-numbered-reviewed-modified-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"1. Reviewed modified internal/foo.go; no issues found.","agent_id":"agent-numbered-reviewed-modified-clean","workspace_id":"ws-numbered-reviewed-modified-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for numbered review summary", summary)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for numbered review summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_FilesChangedZeroSummaryStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-files-changed-zero-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Files changed: 0","agent_id":"agent-files-changed-zero-clean","workspace_id":"ws-files-changed-zero-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for zero changed-files summary", summary)
	}
}

func TestAssistantTurnScript_ModifiedFilesZeroSummaryStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-modified-files-zero-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Modified files = 0","agent_id":"agent-modified-files-zero-clean","workspace_id":"ws-modified-files-zero-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for zero modified-files summary", summary)
	}
}

func TestAssistantTurnScript_VersionOnlyRecommendationSummaryStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-version-only-recommendation-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated recommendations for Go 1.23 upgrade.","agent_id":"agent-version-only-recommendation-clean","workspace_id":"ws-version-only-recommendation-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the recommendations.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for version-only summary", summary)
	}
}

func TestAssistantTurnScript_VersionOnlyRecommendationSummaryOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-version-only-recommendation-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated recommendations for Go 1.23 upgrade.","agent_id":"agent-version-only-recommendation-dirty","workspace_id":"ws-version-only-recommendation-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the recommendations.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for version-only recommendation summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ChangesHeadingSummaryCountsAsEditClaimOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-changes-heading-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Changes: internal/foo.go, README.md","agent_id":"agent-changes-heading-clean","workspace_id":"ws-changes-heading-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning for noun heading summary", summary)
	}
}

func TestAssistantTurnScript_PatchHeadingSummaryKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "internal/foo.go", "package internal\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-patch-heading-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Patch: internal/foo.go","agent_id":"agent-patch-heading-dirty","workspace_id":"ws-patch-heading-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for patch heading summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_FileListWithTrailingStatusCountsAsEditClaimOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-file-list-trailing-status-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"internal/foo.go, README.md; tests passed.","agent_id":"agent-file-list-trailing-status-clean","workspace_id":"ws-file-list-trailing-status-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning for file list with trailing status", summary)
	}
}

func TestAssistantTurnScript_ReviewedFileAndMadeChangesRequiredCountsAsEditClaimOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-made-required-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and made changes required by lint.","agent_id":"agent-reviewed-made-required-clean","workspace_id":"ws-reviewed-made-required-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning for reviewed-and-made-changes summary", summary)
	}
}

func TestAssistantTurnScript_ReviewedFileAndMadeChangesRequiredKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "internal/foo.go", "package internal\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-made-required-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and made changes required by lint.","agent_id":"agent-reviewed-made-required-dirty","workspace_id":"ws-reviewed-made-required-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for reviewed-and-made-changes summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_UpdatedGoModSentenceSummaryCountsAsEditClaimOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-gomod-sentence-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated go.mod.","agent_id":"agent-gomod-sentence-clean","workspace_id":"ws-gomod-sentence-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning for sentence-ending go.mod summary", summary)
	}
}

func TestAssistantTurnScript_UpdatedDockerComposeSentenceSummaryKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "docker-compose.yml", "services:\n  app:\n    image: example\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-docker-compose-sentence-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated docker-compose.yml.","agent_id":"agent-docker-compose-sentence-dirty","workspace_id":"ws-docker-compose-sentence-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for sentence-ending docker-compose summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_BareDomainFindingsSummaryStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-bare-domain-findings-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Created findings for example.com","agent_id":"agent-bare-domain-findings-clean","workspace_id":"ws-bare-domain-findings-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for bare domain findings summary", summary)
	}
}

func TestAssistantTurnScript_BareDomainFindingsSummaryOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-bare-domain-findings-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Created findings for example.com","agent_id":"agent-bare-domain-findings-dirty","workspace_id":"ws-bare-domain-findings-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for bare domain findings summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_VersionReleaseSummaryStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-version-release-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated recommendations for v1.2.3 release.","agent_id":"agent-version-release-clean","workspace_id":"ws-version-release-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the recommendations.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for version release summary", summary)
	}
}

func TestAssistantTurnScript_InternalHostnameFindingsSummaryOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-internal-hostname-findings-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Created findings for api.internal.","agent_id":"agent-internal-hostname-findings-dirty","workspace_id":"ws-internal-hostname-findings-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for internal hostname findings summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_DotlessDockerfilePathCountsAsEditClaimOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-dotless-dockerfile-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"internal/Dockerfile","agent_id":"agent-dotless-dockerfile-clean","workspace_id":"ws-dotless-dockerfile-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning for dotless Dockerfile path summary", summary)
	}
}

func TestAssistantTurnScript_DotlessDockerfilePathKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "internal/Dockerfile", "FROM alpine:3.20\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-dotless-dockerfile-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"internal/Dockerfile","agent_id":"agent-dotless-dockerfile-dirty","workspace_id":"ws-dotless-dockerfile-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for dotless Dockerfile path summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedAndAddedRecommendationsStaysCompletedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-added-recommendations-clean", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and added recommendations.","agent_id":"agent-reviewed-added-recommendations-clean","workspace_id":"ws-reviewed-added-recommendations-clean","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	summary, _ := payload["summary"].(string)
	if strings.Contains(summary, "Claimed file updates") {
		t.Fatalf("summary = %q, want no clean-tree edit warning for review-only recommendations summary", summary)
	}
}

func TestAssistantTurnScript_ReviewedAndImprovedPlanOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-improved-plan-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and improved plan.","agent_id":"agent-reviewed-improved-plan-dirty","workspace_id":"ws-reviewed-improved-plan-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for review-only improved-plan summary", payload["quick_actions"])
	}
}
