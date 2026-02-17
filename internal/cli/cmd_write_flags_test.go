package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestCmdWorkspaceCreateParsesTrailingProjectFlag(t *testing.T) {
	nonRepo := t.TempDir()
	var w, wErr bytes.Buffer
	code := cmdWorkspaceCreate(
		&w,
		&wErr,
		GlobalFlags{},
		[]string{"feature-x", "--project", nonRepo, "--assistant", "claude"},
		"test-v1",
	)

	if code != ExitUsage {
		t.Fatalf("expected ExitUsage for non-git repo, got %d", code)
	}
	if !strings.Contains(wErr.String(), "is not a git repository") {
		t.Fatalf("expected parsed --project validation error, got stderr: %q", wErr.String())
	}
}

func TestCmdWorkspaceRemoveParsesTrailingYesFlag(t *testing.T) {
	var w, wErr bytes.Buffer
	code := cmdWorkspaceRemove(
		&w,
		&wErr,
		GlobalFlags{},
		[]string{"missing-workspace", "--yes"},
		"test-v1",
	)

	if code == ExitUnsafeBlocked {
		t.Fatalf("expected --yes to be parsed, got confirmation block; stderr: %q", wErr.String())
	}
	if strings.Contains(wErr.String(), "pass --yes") {
		t.Fatalf("expected confirmation check to be bypassed, got stderr: %q", wErr.String())
	}
}

func TestCmdAgentSendParsesTrailingTextFlag(t *testing.T) {
	var w, wErr bytes.Buffer
	code := cmdAgentSend(
		&w,
		&wErr,
		GlobalFlags{},
		[]string{"missing-session", "--text", "hello"},
		"test-v1",
	)

	if code == ExitUsage {
		t.Fatalf("expected --text to be parsed, got usage; stderr: %q", wErr.String())
	}
}
