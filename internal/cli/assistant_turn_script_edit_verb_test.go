package cli

import (
	"fmt"
	"strings"
	"testing"
)

func TestAssistantTurnScript_EditVerbsDetectedOnCleanTree(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	cases := []struct {
		verb        string
		wsID        string
		summaryText string
	}{
		{"Optimized", "ws-optimized", "Optimized internal/foo.go."},
		{"Adjusted", "ws-adjusted", "Adjusted internal/foo.go."},
		{"Resolved", "ws-resolved", "Resolved README.md issues."},
		{"Deleted file", "ws-deleted-file", "Deleted internal/foo.go."},
		{"Moved file", "ws-moved-file", "Moved internal/foo.go to internal/bar.go."},
		{"Changed file", "ws-changed-file", "Changed README.md."},
		{"Touched file", "ws-touched-file", "Touched README.md."},
		{"Added tests for", "ws-added-tests-for-file", "Added tests for internal/foo.go."},
		{"Created tests for", "ws-created-tests-for-file", "Created tests for internal/foo.go."},
		{"Updated Makefile", "ws-updated-makefile", "Updated Makefile."},
		{"Changed README", "ws-changed-readme", "Changed README"},
		{"Generated file", "ws-generated-file", "Generated internal/foo.go."},
		{"Added documentation to", "ws-added-docs-readme", "Added documentation to README.md."},
		{"Updated comments in", "ws-updated-comments-file", "Updated comments in internal/foo.go."},
	}

	for _, tc := range cases {
		t.Run(tc.verb, func(t *testing.T) {
			workspaceRoot := t.TempDir()
			initCleanGitRepo(t, workspaceRoot)

			agentID := "agent-" + strings.TrimPrefix(tc.wsID, "ws-")
			jsonPayload := fmt.Sprintf(`{"ok":true,"mode":"run","status":"idle","summary":%q,"agent_id":%q,"workspace_id":%q,"assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`, tc.summaryText, agentID, tc.wsID)

			payload := runAssistantTurnCleanTreeCase(t, tc.wsID, workspaceRoot, jsonPayload)
			if got, _ := payload["overall_status"].(string); got != "partial" {
				t.Fatalf("overall_status = %q, want partial", got)
			}
			summary, _ := payload["summary"].(string)
			if !strings.Contains(summary, "Claimed file updates, but no workspace changes were detected.") {
				t.Fatalf("summary = %q, want clean-tree edit warning", summary)
			}
		})
	}
}

func TestAssistantTurnScript_EditVerbsKeepReviewActionWhenDirty(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	cases := []struct {
		verb        string
		wsID        string
		summaryText string
		filePath    string
		fileContent string
	}{
		{"Simplified README.md", "ws-simplified-dirty", "Simplified README.md.", "README.md", "notes\n"},
		{"Deleted file", "ws-deleted-file-dirty", "Deleted internal/foo.go.", "internal/foo.go", "package internal\n"},
		{"Moved file", "ws-moved-file-dirty", "Moved internal/foo.go to internal/bar.go.", "internal/bar.go", "package internal\n"},
		{"Changed file", "ws-changed-file-dirty", "Changed README.md.", "README.md", "docs\n"},
		{"Polished file", "ws-polished-file-dirty", "Polished README.md.", "README.md", "docs\n"},
		{"Created tests for", "ws-created-tests-for-file-dirty", "Created tests for internal/foo.go.", "internal/foo_test.go", "package internal\n"},
		{"Wrote tests for", "ws-wrote-tests-for-file-dirty", "Wrote tests for internal/foo.go.", "internal/foo_test.go", "package internal\n"},
		{"Updated Dockerfile", "ws-updated-dockerfile-dirty", "Updated Dockerfile.", "Dockerfile", "FROM alpine:3.20\n"},
		{"Updated Makefile", "ws-updated-makefile-dirty", "Updated Makefile.", "Makefile", "test:\n\tgo test ./...\n"},
		{"Generated tests for", "ws-generated-tests-for-file-dirty", "Generated tests for internal/foo.go.", "internal/foo_test.go", "package internal\n"},
		{"Deleted section from README", "ws-deleted-section-readme-dirty", "Deleted obsolete section from README.md.", "README.md", "docs\n"},
		{"Modified parser", "ws-modified-parser-dirty", "Modified parser.", "internal/parser.go", "package internal\n"},
		{"Modified status output", "ws-modified-status-output-dirty", "Modified status output.", "internal/status/output.go", "package status\n"},
	}

	for _, tc := range cases {
		t.Run(tc.verb, func(t *testing.T) {
			workspaceRoot := t.TempDir()
			initCleanGitRepo(t, workspaceRoot)
			writeWorkspaceFile(t, workspaceRoot, tc.filePath, tc.fileContent)

			agentID := "agent-" + strings.TrimPrefix(tc.wsID, "ws-")
			jsonPayload := fmt.Sprintf(`{"ok":true,"mode":"run","status":"idle","summary":%q,"agent_id":%q,"workspace_id":%q,"assistant":"codex","response":{"substantive_output":true,"needs_input":false},"next_action":"Share the patch.","suggested_command":""}`, tc.summaryText, agentID, tc.wsID)

			payload := runAssistantTurnCleanTreeCase(t, tc.wsID, workspaceRoot, jsonPayload)
			if got, _ := payload["overall_status"].(string); got != "completed" {
				t.Fatalf("overall_status = %q, want completed", got)
			}
			if !hasQuickActionID(payload, "review") {
				t.Fatalf("quick_actions = %#v, want review action for %s edit", payload["quick_actions"], tc.verb)
			}
		})
	}
}
