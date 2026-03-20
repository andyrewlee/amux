package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeWorkspaceFile(t *testing.T, root, rel, contents string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestAssistantTurnScript_AddedSummaryKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "NOTES.md", "status update\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-added-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Added NOTES.md with status update.","agent_id":"agent-added-dirty","workspace_id":"ws-added-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for added-summary edit", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ImprovedSummaryKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "internal/foo.go", "package foo\n")
	writeWorkspaceFile(t, workspaceRoot, "README.md", "notes\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-improved-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Improved internal/foo.go and README.md.","agent_id":"agent-improved-dirty","workspace_id":"ws-improved-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for improved-summary edit", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_FilesChangedSummaryKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "internal/foo.go", "package foo\n")
	writeWorkspaceFile(t, workspaceRoot, "README.md", "notes\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-files-changed-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Files changed: internal/foo.go, README.md","agent_id":"agent-files-changed-dirty","workspace_id":"ws-files-changed-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for files-changed summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_FilesPrefixSummaryKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "internal/foo.go", "package foo\n")
	writeWorkspaceFile(t, workspaceRoot, "README.md", "notes\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-files-prefix-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Files: internal/foo.go, README.md","agent_id":"agent-files-prefix-dirty","workspace_id":"ws-files-prefix-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for files-prefix summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_FileListWithTrailingStatusKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "internal/foo.go", "package foo\n")
	writeWorkspaceFile(t, workspaceRoot, "README.md", "notes\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-file-list-trailing-status-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"internal/foo.go, README.md; tests passed.","agent_id":"agent-file-list-trailing-status-dirty","workspace_id":"ws-file-list-trailing-status-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for file list with trailing status", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_PatchForFileSummaryKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "README.md", "notes\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-patch-for-file-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Patch for README.md.","agent_id":"agent-patch-for-file-dirty","workspace_id":"ws-patch-for-file-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for noun-form patch summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_NoFilesChangedSummaryKeepsCompletedAndOmitsReviewActionOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-no-files-changed", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go; no files changed.","agent_id":"agent-no-files-changed","workspace_id":"ws-no-files-changed","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for no-files-changed review summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_NoFilesChangedSummaryOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-no-files-changed-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go; no files changed.","agent_id":"agent-no-files-changed-dirty","workspace_id":"ws-no-files-changed-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for dirty no-files-changed review summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_NoChangedFilesSummaryOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-no-changed-files-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"No changed files.","agent_id":"agent-no-changed-files-dirty","workspace_id":"ws-no-changed-files-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for dirty no-changed-files summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_NoModifiedFilesSummaryOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-no-modified-files-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"No modified files.","agent_id":"agent-no-modified-files-dirty","workspace_id":"ws-no-modified-files-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for dirty no-modified-files summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedChangedFilesSummaryOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-changed-files-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed changed files; no issues found.","agent_id":"agent-reviewed-changed-files-dirty","workspace_id":"ws-reviewed-changed-files-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for dirty review-only changed-files summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewOfRefactorPathKeepsCompletedAndOmitsReviewAction(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-refactor-path-review", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed cmd/refactor.go; no changes required.","agent_id":"agent-refactor-path-review","workspace_id":"ws-refactor-path-review","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for review summary mentioning refactor.go", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewFoundRefactoringNeededKeepsCompletedAndOmitsReviewAction(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-review-refactoring-needed", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Review found refactoring needed in internal/foo.go.","agent_id":"agent-review-refactoring-needed","workspace_id":"ws-review-refactoring-needed","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for review findings summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedBreakingChangeFindingOmitsReviewAction(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-breaking-change-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed API schema; removed field foo is a breaking change.","agent_id":"agent-reviewed-breaking-change-dirty","workspace_id":"ws-reviewed-breaking-change-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for observed-change review finding", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedAndWroteFindingsKeepsCompletedAndOmitsReviewAction(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-wrote-findings", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and wrote findings.","agent_id":"agent-reviewed-wrote-findings","workspace_id":"ws-reviewed-wrote-findings","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for review summary that only wrote findings", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedAndWroteNotesOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-wrote-notes", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and wrote notes.","agent_id":"agent-reviewed-wrote-notes","workspace_id":"ws-reviewed-wrote-notes","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the notes.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for review summary that only wrote notes", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewedAndWroteFindingsToWorkspaceFileKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "NOTES.md", "review findings\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-reviewed-wrote-findings-file-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed internal/foo.go and wrote findings to NOTES.md.","agent_id":"agent-reviewed-wrote-findings-file-dirty","workspace_id":"ws-reviewed-wrote-findings-file-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the notes.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for review summary that wrote a workspace file", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ChangedFilesWereReviewedOmitsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "scratch.txt", "dirty\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-changed-files-were-reviewed", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Changed files were reviewed; no issues found.","agent_id":"agent-changed-files-were-reviewed","workspace_id":"ws-changed-files-were-reviewed","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Report findings.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for passive changed-files review summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_PatchedAfterReviewAndWroteFindingsStillCountsAsEditClaim(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)

	payload := runAssistantTurnCleanTreeCase(t, "ws-patched-after-review", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Patched internal/foo.go after review and wrote findings.","agent_id":"agent-patched-after-review","workspace_id":"ws-patched-after-review","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "partial" {
		t.Fatalf("overall_status = %q, want partial", got)
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
		t.Fatalf("summary = %q, want clean-tree edit warning", summary)
	}
}

func TestAssistantTurnScript_PatchedAfterReviewAndWroteFindingsKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "internal/foo.go", "package foo\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-patched-after-review-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Patched internal/foo.go after review and wrote findings.","agent_id":"agent-patched-after-review-dirty","workspace_id":"ws-patched-after-review-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for mixed patch-and-findings summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ChangedFilesSummaryWithoutPathKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "go.mod", "module example.com/test\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-changed-files-generic", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Changed files and tests passed.","agent_id":"agent-changed-files-generic","workspace_id":"ws-changed-files-generic","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for generic changed-files summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_UpdatedDockerComposeKeepsReviewActionWhenWorkspaceDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "docker-compose.yml", "services:\n  app:\n    image: example\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-docker-compose-dirty", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Updated docker-compose.yml","agent_id":"agent-docker-compose-dirty","workspace_id":"ws-docker-compose-dirty","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action for docker-compose.yml edit summary", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_GenericSummaryKeepsReviewActionWhenNextActionMentionsModifiedFiles(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "internal/foo.go", "package foo\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-generic-summary-modified-files", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Done.","agent_id":"agent-generic-summary-modified-files","workspace_id":"ws-generic-summary-modified-files","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Review modified files and summarize remaining risks.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action when next_action mentions modified files", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_GenericSummaryKeepsReviewActionWhenNextActionMentionsModifiedCode(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "internal/foo.go", "package foo\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-generic-summary-modified-code", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Done.","agent_id":"agent-generic-summary-modified-code","workspace_id":"ws-generic-summary-modified-code","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Review modified code before shipping.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action when next_action mentions modified code", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_GenericSummaryKeepsReviewActionWhenNextActionMentionsRefactor(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "internal/foo.go", "package foo\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-generic-summary-refactor", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Done.","agent_id":"agent-generic-summary-refactor","workspace_id":"ws-generic-summary-refactor","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Review refactor before shipping.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if !hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want review action when next_action mentions refactor", payload["quick_actions"])
	}
}

func TestAssistantTurnScript_ReviewUpdatedRecommendationsNextActionOmitsReviewAction(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	workspaceRoot := t.TempDir()
	initCleanGitRepo(t, workspaceRoot)
	writeWorkspaceFile(t, workspaceRoot, "internal/foo.go", "package foo\n")

	payload := runAssistantTurnCleanTreeCase(t, "ws-review-updated-recs", workspaceRoot, `{"ok":true,"mode":"run","status":"idle","summary":"Reviewed auth flow and updated recommendations.","agent_id":"agent-review-updated-recs","workspace_id":"ws-review-updated-recs","assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Review updated recommendations with the team.","suggested_command":""}`)
	if got, _ := payload["overall_status"].(string); got != "completed" {
		t.Fatalf("overall_status = %q, want completed", got)
	}
	if hasQuickActionID(payload, "review") {
		t.Fatalf("quick_actions = %#v, want no review action for review-only updated-recommendations context", payload["quick_actions"])
	}
}
