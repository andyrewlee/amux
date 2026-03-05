package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestAuthStatusAllFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	previousStdout := cliStdout
	var output bytes.Buffer
	cliStdout = &output
	defer func() {
		cliStdout = previousStdout
	}()

	cmd := buildAuthCommand()
	cmd.SetArgs([]string{"status", "--all"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("auth status --all failed: %v", err)
	}

	got := output.String()
	if !strings.Contains(got, "amux auth status") {
		t.Fatalf("expected status header in output, got: %q", got)
	}
	if !strings.Contains(got, "Agent authentication") {
		t.Fatalf("expected --all details in output, got: %q", got)
	}
}
