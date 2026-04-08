package cli

import (
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/sandbox"
)

func TestDoctorLogsCommandFallsBackOnJournalctlCommandUnavailable(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	sb := sandbox.NewMockRemoteSandbox("sb-123")
	sb.SetExecResult(doctorLogsJournalctlCmd(100, false), "", 127)
	sb.SetExecResult(doctorLogsDmesgCmd(100, false), "fallback logs\n", 0)
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{SandboxID: "sb-123", Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildLogsCommand()
	cmd.SilenceUsage = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("logs command error = %v", err)
	}

	history := sb.GetExecHistory()
	if len(history) != 2 {
		t.Fatalf("exec history len = %d, want 2", len(history))
	}
	if history[0] != doctorLogsJournalctlCmd(100, false) {
		t.Fatalf("first log command = %q, want %q", history[0], doctorLogsJournalctlCmd(100, false))
	}
	if history[1] != doctorLogsDmesgCmd(100, false) {
		t.Fatalf("fallback log command = %q, want %q", history[1], doctorLogsDmesgCmd(100, false))
	}
	if !strings.Contains(history[1], "command -v dmesg >/dev/null 2>&1 || {") {
		t.Fatalf("fallback log command = %q, want quiet dmesg availability guard", history[1])
	}
	if !strings.Contains(history[1], doctorLogsUnavailableMarker) {
		t.Fatalf("fallback log command = %q, want unavailable marker handling", history[1])
	}
	if !strings.Contains(history[1], `dmesg >"$snapshot_file" 2>/dev/null`) {
		t.Fatalf("fallback log command = %q, want quiet direct dmesg snapshot", history[1])
	}
	if !strings.Contains(history[1], `status=$?`) || !strings.Contains(history[1], `exit "$status"`) {
		t.Fatalf("fallback log command = %q, want dmesg exit status propagation", history[1])
	}
	if !strings.Contains(history[1], `if [ "$status" -eq 1 ]; then`) {
		t.Fatalf("fallback log command = %q, want unreadable dmesg handling", history[1])
	}
	if strings.Contains(history[1], "dmesg | tail") {
		t.Fatalf("fallback log command = %q, should not use a masking pipeline", history[1])
	}
	if got := output.String(); got != "fallback logs\n" {
		t.Fatalf("logs output = %q, want %q", got, "fallback logs\n")
	}
}

func TestDoctorLogsCommandSnapshotFallbackTreatsUnreadableDmesgAsNoLogsAvailable(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	sb := sandbox.NewMockRemoteSandbox("sb-123")
	sb.SetExecResult(doctorLogsJournalctlCmd(100, false), "", 127)
	sb.SetExecResult(doctorLogsDmesgCmd(100, false), doctorLogsUnavailableMarker, 0)
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{SandboxID: "sb-123", Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildLogsCommand()
	cmd.SilenceUsage = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("logs command error = %v", err)
	}

	history := sb.GetExecHistory()
	if len(history) != 2 {
		t.Fatalf("exec history len = %d, want 2", len(history))
	}
	if !strings.Contains(history[1], `if [ "$status" -eq 1 ]; then`) {
		t.Fatalf("fallback log command = %q, want unreadable dmesg handling", history[1])
	}
	if got := output.String(); got != "No logs available\n" {
		t.Fatalf("logs output = %q, want %q", got, "No logs available\n")
	}
}

func TestDoctorLogsCommandSnapshotFallbackPropagatesNonAvailabilityDmesgFailure(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	sb := sandbox.NewMockRemoteSandbox("sb-123")
	sb.SetExecResult(doctorLogsJournalctlCmd(100, false), "", 127)
	sb.SetExecResult(doctorLogsDmesgCmd(100, false), "", 2)
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{SandboxID: "sb-123", Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildLogsCommand()
	cmd.SilenceUsage = true

	err := cmd.Execute()
	exitErr, ok := err.(exitError)
	if !ok || exitErr.code != 2 {
		t.Fatalf("logs command error = %v, want exitError(2)", err)
	}

	history := sb.GetExecHistory()
	if len(history) != 2 {
		t.Fatalf("exec history len = %d, want 2", len(history))
	}
	if got := output.String(); got != "" {
		t.Fatalf("logs output = %q, want empty output on hard dmesg failure", got)
	}
}

func TestDoctorLogsCommandPropagatesJournalctlFailureExitCode(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	sb := sandbox.NewMockRemoteSandbox("sb-123")
	sb.SetExecResult(doctorLogsJournalctlCmd(100, false), "journal failed\n", 1)
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{SandboxID: "sb-123", Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildLogsCommand()
	cmd.SilenceUsage = true

	err := cmd.Execute()
	exitErr, ok := err.(exitError)
	if !ok || exitErr.code != 1 {
		t.Fatalf("logs command error = %v, want exitError(1)", err)
	}

	history := sb.GetExecHistory()
	if len(history) != 1 {
		t.Fatalf("exec history len = %d, want 1", len(history))
	}
	if got := output.String(); got != "journal failed\n" {
		t.Fatalf("logs output = %q, want %q", got, "journal failed\n")
	}
}

func TestDoctorLogsCommandFallsBackToDmesgWhenJournalctlReportsNoJournalFiles(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	sb := sandbox.NewMockRemoteSandbox("sb-123")
	sb.SetExecResult(doctorLogsJournalctlCmd(100, false), "No journal files were found.\n", 1)
	sb.SetExecResult(doctorLogsDmesgCmd(100, false), "fallback logs\n", 0)
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{SandboxID: "sb-123", Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildLogsCommand()
	cmd.SilenceUsage = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("logs command error = %v", err)
	}

	history := sb.GetExecHistory()
	if len(history) != 2 {
		t.Fatalf("exec history len = %d, want 2", len(history))
	}
	if got := output.String(); got != "fallback logs\n" {
		t.Fatalf("logs output = %q, want %q", got, "fallback logs\n")
	}
}
