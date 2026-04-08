package cli

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/sandbox"
)

type doctorLogsTrackingSandbox struct {
	*sandbox.MockRemoteSandbox
	execCalls            int
	execInteractiveCalls int
	execSequences        map[string][]sandbox.MockExecResult
	execHook             func(cmd string, call int)
}

func newDoctorLogsTrackingSandbox() *doctorLogsTrackingSandbox {
	return &doctorLogsTrackingSandbox{
		MockRemoteSandbox: sandbox.NewMockRemoteSandbox("sb-123"),
		execSequences:     make(map[string][]sandbox.MockExecResult),
	}
}

func (s *doctorLogsTrackingSandbox) SetExecSequence(cmd string, results ...sandbox.MockExecResult) {
	s.execSequences[cmd] = append([]sandbox.MockExecResult(nil), results...)
}

func (s *doctorLogsTrackingSandbox) Exec(ctx context.Context, cmd string, opts *sandbox.ExecOptions) (*sandbox.ExecResult, error) {
	s.execCalls++
	s.MockRemoteSandbox.ExecHistory = append(s.MockRemoteSandbox.ExecHistory, cmd)

	if seq := s.execSequences[cmd]; len(seq) > 0 {
		result := seq[0]
		s.execSequences[cmd] = seq[1:]
		if s.execHook != nil {
			s.execHook(cmd, s.execCalls)
		}
		if result.Error != nil {
			return nil, result.Error
		}
		return &sandbox.ExecResult{
			Stdout:   result.Output,
			ExitCode: result.ExitCode,
		}, nil
	}

	if s.execHook != nil {
		s.execHook(cmd, s.execCalls)
	}
	return &sandbox.ExecResult{Stdout: "", ExitCode: 0}, nil
}

func (s *doctorLogsTrackingSandbox) ExecInteractive(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer, opts *sandbox.ExecOptions) (int, error) {
	s.execInteractiveCalls++
	return s.MockRemoteSandbox.ExecInteractive(ctx, cmd, stdin, stdout, stderr, opts)
}

func setDoctorLogsFollowTestHooks(ctx context.Context, t *testing.T, cancel context.CancelFunc, interval time.Duration) {
	t.Helper()

	oldContext := doctorLogsFollowContext
	oldInterval := doctorLogsFollowInterval
	doctorLogsFollowContext = func() (context.Context, context.CancelFunc) {
		return ctx, cancel
	}
	doctorLogsFollowInterval = interval
	t.Cleanup(func() {
		doctorLogsFollowContext = oldContext
		doctorLogsFollowInterval = oldInterval
	})
}

func TestDoctorLogsCommandPropagatesProviderErrors(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	setCLIStdout(t)

	getErr := errors.New("network unavailable")
	provider := &resolveCurrentSandboxTestProvider{getErr: getErr}
	meta := &sandbox.SandboxMeta{SandboxID: "sb-123", Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildLogsCommand()
	cmd.SilenceUsage = true

	err := cmd.Execute()
	if !errors.Is(err, getErr) {
		t.Fatalf("logs command error = %v, want %v", err, getErr)
	}
}

func doctorLogsRange(start, end int) string {
	if end < start {
		return ""
	}
	var b strings.Builder
	for i := start; i <= end; i++ {
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\n')
	}
	return b.String()
}

func TestDoctorLogsCommandFollowUsesSnapshotPollingViaExec(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	ctx, cancel := context.WithCancel(context.Background())
	setDoctorLogsFollowTestHooks(ctx, t, cancel, time.Millisecond)

	sb := newDoctorLogsTrackingSandbox()
	journalCmd := doctorLogsJournalctlCmd(100, true)
	afterCursorCmd := doctorLogsJournalctlAfterCursorCmd("cursor-1")
	sb.SetExecSequence(journalCmd,
		sandbox.MockExecResult{Output: doctorLogsTimePrefix + "t0\n" + doctorLogsCursorPrefix + "cursor-1\nsnapshot logs\n", ExitCode: 0},
	)
	sb.SetExecSequence(afterCursorCmd,
		sandbox.MockExecResult{Output: doctorLogsTimePrefix + "t1\n" + doctorLogsCursorPrefix + "cursor-2\nfollowed logs\n", ExitCode: 0},
	)
	sb.execHook = func(cmd string, call int) {
		if cmd == afterCursorCmd && call == 2 {
			cancel()
		}
	}
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{SandboxID: "sb-123", Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildLogsCommand()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--follow"})

	err := cmd.Execute()
	exitErr, ok := err.(exitError)
	if !ok || exitErr.code != 130 {
		t.Fatalf("logs --follow error = %v, want exitError(130)", err)
	}
	if sb.execCalls != 2 {
		t.Fatalf("exec calls = %d, want 2", sb.execCalls)
	}
	if sb.execInteractiveCalls != 0 {
		t.Fatalf("interactive exec calls = %d, want 0", sb.execInteractiveCalls)
	}

	history := sb.GetExecHistory()
	if len(history) != 2 {
		t.Fatalf("exec history len = %d, want 2", len(history))
	}
	if history[0] != journalCmd || history[1] != afterCursorCmd {
		t.Fatalf("exec history = %q, want [%q %q]", history, journalCmd, afterCursorCmd)
	}
	if got := output.String(); got != "snapshot logs\nfollowed logs\n" {
		t.Fatalf("logs --follow output = %q, want %q", got, "snapshot logs\nfollowed logs\n")
	}
}

func TestDoctorLogsCommandFollowFallsBackToDmesgSnapshots(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	ctx, cancel := context.WithCancel(context.Background())
	setDoctorLogsFollowTestHooks(ctx, t, cancel, time.Millisecond)

	sb := newDoctorLogsTrackingSandbox()
	journalCmd := doctorLogsJournalctlCmd(100, true)
	dmesgCmd := doctorLogsDmesgFollowSnapshotCmd()
	sb.SetExecSequence(journalCmd,
		sandbox.MockExecResult{ExitCode: 127},
	)
	sb.SetExecSequence(dmesgCmd,
		sandbox.MockExecResult{Output: "snapshot logs\n", ExitCode: 0},
		sandbox.MockExecResult{Output: "snapshot logs\nfollowed kernel logs\n", ExitCode: 0},
	)
	sb.execHook = func(cmd string, call int) {
		if cmd == dmesgCmd && call == 3 {
			cancel()
		}
	}
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{SandboxID: "sb-123", Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildLogsCommand()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--follow"})

	err := cmd.Execute()
	exitErr, ok := err.(exitError)
	if !ok || exitErr.code != 130 {
		t.Fatalf("logs --follow error = %v, want exitError(130)", err)
	}
	if sb.execCalls != 3 {
		t.Fatalf("exec calls = %d, want 3", sb.execCalls)
	}
	if sb.execInteractiveCalls != 0 {
		t.Fatalf("interactive exec calls = %d, want 0", sb.execInteractiveCalls)
	}

	history := sb.GetExecHistory()
	if len(history) != 3 {
		t.Fatalf("exec history len = %d, want 3", len(history))
	}
	if history[0] != journalCmd || history[1] != dmesgCmd || history[2] != dmesgCmd {
		t.Fatalf("exec history = %q, want [%q %q %q]", history, journalCmd, dmesgCmd, dmesgCmd)
	}
	if strings.Contains(strings.Join(history, "\n"), "--since=") {
		t.Fatalf("exec history = %q, should not require dmesg --since", history)
	}
	if got := output.String(); got != "snapshot logs\nfollowed kernel logs\n" {
		t.Fatalf("logs --follow output = %q, want %q", got, "snapshot logs\nfollowed kernel logs\n")
	}
}

func TestDoctorLogsCommandFollowPropagatesJournalctlFailure(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	sb := newDoctorLogsTrackingSandbox()
	journalCmd := doctorLogsJournalctlCmd(100, true)
	sb.SetExecSequence(journalCmd, sandbox.MockExecResult{Output: "journal follow failed\n", ExitCode: 1})
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{SandboxID: "sb-123", Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildLogsCommand()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--follow"})

	err := cmd.Execute()
	exitErr, ok := err.(exitError)
	if !ok || exitErr.code != 1 {
		t.Fatalf("logs --follow error = %v, want exitError(1)", err)
	}
	if sb.execCalls != 1 {
		t.Fatalf("exec calls = %d, want 1", sb.execCalls)
	}
	if sb.execInteractiveCalls != 0 {
		t.Fatalf("interactive exec calls = %d, want 0", sb.execInteractiveCalls)
	}

	history := sb.GetExecHistory()
	if len(history) != 1 {
		t.Fatalf("exec history len = %d, want 1", len(history))
	}
	if history[0] != journalCmd {
		t.Fatalf("exec history = %q, want [%q]", history, journalCmd)
	}
	if got := output.String(); got != "journal follow failed\n" {
		t.Fatalf("logs --follow output = %q, want %q", got, "journal follow failed\n")
	}
}

func TestDoctorLogsCommandFollowFallsBackWhenJournalctlReportsNoJournalFiles(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	sb := newDoctorLogsTrackingSandbox()
	sb.SetExecSequence(doctorLogsJournalctlCmd(100, true), sandbox.MockExecResult{Output: "No journal files were found.\n", ExitCode: 1})
	sb.SetExecSequence(doctorLogsDmesgFollowSnapshotCmd(), sandbox.MockExecResult{Output: doctorLogsUnavailableMarker, ExitCode: 0})
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{SandboxID: "sb-123", Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildLogsCommand()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--follow"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("logs --follow error = %v", err)
	}
	if sb.execCalls != 2 {
		t.Fatalf("exec calls = %d, want 2", sb.execCalls)
	}
	if sb.execInteractiveCalls != 0 {
		t.Fatalf("interactive exec calls = %d, want 0", sb.execInteractiveCalls)
	}
	if got := output.String(); got != "No logs available\n" {
		t.Fatalf("logs --follow output = %q, want %q", got, "No logs available\n")
	}
}

func TestDoctorLogsCommandFollowPropagatesDmesgFailureAfterInitialSuccess(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	sb := newDoctorLogsTrackingSandbox()
	journalCmd := doctorLogsJournalctlCmd(100, true)
	dmesgCmd := doctorLogsDmesgFollowSnapshotCmd()
	sb.SetExecSequence(journalCmd,
		sandbox.MockExecResult{ExitCode: 127},
	)
	sb.SetExecSequence(dmesgCmd,
		sandbox.MockExecResult{Output: "snapshot logs\n", ExitCode: 0},
		sandbox.MockExecResult{ExitCode: 2},
	)
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{SandboxID: "sb-123", Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	ctx, cancel := context.WithCancel(context.Background())
	setDoctorLogsFollowTestHooks(ctx, t, cancel, time.Millisecond)

	cmd := buildLogsCommand()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--follow"})

	err := cmd.Execute()
	exitErr, ok := err.(exitError)
	if !ok || exitErr.code != 2 {
		t.Fatalf("logs --follow error = %v, want exitError(2)", err)
	}
	if sb.execCalls != 3 {
		t.Fatalf("exec calls = %d, want 3", sb.execCalls)
	}
	if got := output.String(); got != "snapshot logs\n" {
		t.Fatalf("logs --follow output = %q, want %q", got, "snapshot logs\n")
	}
}

func TestDoctorLogsCommandFollowPropagatesDmesgExecError(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	setCLIStdout(t)

	dmesgErr := errors.New("network failed")
	sb := newDoctorLogsTrackingSandbox()
	sb.SetExecSequence(doctorLogsJournalctlCmd(100, true), sandbox.MockExecResult{ExitCode: 127})
	sb.SetExecSequence(doctorLogsDmesgFollowSnapshotCmd(), sandbox.MockExecResult{Error: dmesgErr})
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{SandboxID: "sb-123", Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildLogsCommand()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--follow"})

	err := cmd.Execute()
	if !errors.Is(err, dmesgErr) {
		t.Fatalf("logs --follow error = %v, want %v", err, dmesgErr)
	}
	if sb.execCalls != 2 {
		t.Fatalf("exec calls = %d, want 2", sb.execCalls)
	}
	if sb.execInteractiveCalls != 0 {
		t.Fatalf("interactive exec calls = %d, want 0", sb.execInteractiveCalls)
	}
}
