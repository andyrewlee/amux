package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/sandbox"
)

func TestDoctorLogsJournalctlFollowCmdCapturesBoundaryBeforeSnapshot(t *testing.T) {
	cmd := doctorLogsJournalctlCmd(100, true)

	journalctlIndex := strings.Index(cmd, `journalctl --no-pager --show-cursor -n 100 >"$snapshot_file" 2>/dev/null`)
	if journalctlIndex < 0 {
		t.Fatalf("journal follow command = %q, missing journalctl snapshot", cmd)
	}
	boundaryIndex := strings.Index(cmd, "boundary=$(date -u ")
	if boundaryIndex < 0 {
		t.Fatalf("journal follow command = %q, missing boundary capture", cmd)
	}
	if boundaryIndex > journalctlIndex {
		t.Fatalf("journal follow command = %q, boundary captured after snapshot", cmd)
	}
}

func TestDoctorLogsDmesgFollowSnapshotCmdTreatsUnreadableDmesgAsUnavailable(t *testing.T) {
	cmd := doctorLogsDmesgFollowSnapshotCmd()

	if !strings.Contains(cmd, "command -v dmesg >/dev/null 2>&1 || {") {
		t.Fatalf("dmesg follow command = %q, want quiet dmesg availability guard", cmd)
	}
	if !strings.Contains(cmd, `dmesg >"$snapshot_file" 2>/dev/null`) {
		t.Fatalf("dmesg follow command = %q, want quiet direct dmesg snapshot", cmd)
	}
	if !strings.Contains(cmd, doctorLogsUnavailableMarker) {
		t.Fatalf("dmesg follow command = %q, want unavailable marker handling", cmd)
	}
	if !strings.Contains(cmd, `if [ "$status" -eq 1 ]; then`) {
		t.Fatalf("dmesg follow command = %q, want unreadable dmesg handling", cmd)
	}
	if !strings.Contains(cmd, `exit "$status"`) {
		t.Fatalf("dmesg follow command = %q, want non-availability failures preserved", cmd)
	}
}

func TestDoctorLogsCommandFollowUsesRepeatedSnapshotCommand(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	setCLIStdout(t)

	ctx, cancel := context.WithCancel(context.Background())
	setDoctorLogsFollowTestHooks(ctx, t, cancel, time.Millisecond)

	sb := newDoctorLogsTrackingSandbox()
	journalCmd := doctorLogsJournalctlCmd(100, true)
	afterCursorCmd := doctorLogsJournalctlAfterCursorCmd("cursor-1")
	sb.SetExecSequence(journalCmd,
		sandbox.MockExecResult{Output: doctorLogsTimePrefix + "t0\n" + doctorLogsCursorPrefix + "cursor-1\n", ExitCode: 0},
	)
	sb.SetExecSequence(afterCursorCmd,
		sandbox.MockExecResult{Output: doctorLogsTimePrefix + "t1\n" + doctorLogsCursorPrefix + "cursor-2\nlate log\n", ExitCode: 0},
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

	history := sb.GetExecHistory()
	if len(history) != 2 {
		t.Fatalf("exec history len = %d, want 2", len(history))
	}
	if strings.Contains(strings.Join(history, "\n"), "exec journalctl") {
		t.Fatalf("exec history = %q, should not use an interactive journalctl handoff", history)
	}
}

func TestDoctorLogsCommandFollowPreservesRepeatedIdenticalLines(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	ctx, cancel := context.WithCancel(context.Background())
	setDoctorLogsFollowTestHooks(ctx, t, cancel, time.Millisecond)

	sb := newDoctorLogsTrackingSandbox()
	journalCmd := doctorLogsJournalctlCmd(1, true)
	afterCursorCmd := doctorLogsJournalctlAfterCursorCmd("cursor-1")
	sb.SetExecSequence(journalCmd,
		sandbox.MockExecResult{Output: doctorLogsTimePrefix + "t0\n" + doctorLogsCursorPrefix + "cursor-1\nheartbeat\n", ExitCode: 0},
	)
	sb.SetExecSequence(afterCursorCmd,
		sandbox.MockExecResult{Output: doctorLogsTimePrefix + "t1\n" + doctorLogsCursorPrefix + "cursor-2\nheartbeat\n", ExitCode: 0},
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
	cmd.SetArgs([]string{"--follow", "--lines=1"})

	err := cmd.Execute()
	exitErr, ok := err.(exitError)
	if !ok || exitErr.code != 130 {
		t.Fatalf("logs --follow error = %v, want exitError(130)", err)
	}
	if got := output.String(); got != "heartbeat\nheartbeat\n" {
		t.Fatalf("logs --follow output = %q, want %q", got, "heartbeat\nheartbeat\n")
	}
}

func TestDoctorLogsCommandFollowKeepsWholeInitialMultilineEvent(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	ctx, cancel := context.WithCancel(context.Background())
	setDoctorLogsFollowTestHooks(ctx, t, cancel, time.Millisecond)

	sb := newDoctorLogsTrackingSandbox()
	journalCmd := doctorLogsJournalctlCmd(1, true)
	sb.SetExecSequence(journalCmd,
		sandbox.MockExecResult{
			Output:   doctorLogsTimePrefix + "t0\n" + doctorLogsCursorPrefix + "cursor-1\nstack line 1\nstack line 2\n",
			ExitCode: 0,
		},
	)
	sb.execHook = func(cmd string, call int) {
		if cmd == journalCmd && call == 1 {
			cancel()
		}
	}
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{SandboxID: "sb-123", Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildLogsCommand()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--follow", "--lines=1"})

	err := cmd.Execute()
	exitErr, ok := err.(exitError)
	if !ok || exitErr.code != 130 {
		t.Fatalf("logs --follow error = %v, want exitError(130)", err)
	}
	if got := output.String(); got != "stack line 1\nstack line 2\n" {
		t.Fatalf("logs --follow output = %q, want %q", got, "stack line 1\nstack line 2\n")
	}
}

func TestDoctorLogsCommandFollowExpandsFirstNonEmptyBurstAfterEmptyBaseline(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	ctx, cancel := context.WithCancel(context.Background())
	setDoctorLogsFollowTestHooks(ctx, t, cancel, time.Millisecond)

	sb := newDoctorLogsTrackingSandbox()
	initialCmd := doctorLogsJournalctlCmd(100, true)
	sinceCmd := doctorLogsJournalctlSinceCmd("t0")
	sb.SetExecSequence(initialCmd,
		sandbox.MockExecResult{Output: doctorLogsTimePrefix + "t0\n" + doctorLogsCursorPrefix + "\n", ExitCode: 0},
	)
	sb.SetExecSequence(sinceCmd, sandbox.MockExecResult{Output: doctorLogsTimePrefix + "t1\n" + doctorLogsCursorPrefix + "cursor-2\n" + doctorLogsRange(1, 450), ExitCode: 0})
	sb.execHook = func(cmd string, call int) {
		if cmd == sinceCmd && call == 2 {
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

	history := sb.GetExecHistory()
	if len(history) != 2 {
		t.Fatalf("exec history len = %d, want 2", len(history))
	}
	if history[0] != initialCmd || history[1] != sinceCmd {
		t.Fatalf("exec history = %q, want [%q %q]", history, initialCmd, sinceCmd)
	}
	if got := output.String(); got != doctorLogsRange(1, 450) {
		t.Fatalf("logs --follow output len = %d, want %d", len(got), len(doctorLogsRange(1, 450)))
	}
}

func TestDoctorLogsCommandFollowSuppressesEmptyJournalBannerUntilRealLogsArrive(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	ctx, cancel := context.WithCancel(context.Background())
	setDoctorLogsFollowTestHooks(ctx, t, cancel, time.Millisecond)

	sb := newDoctorLogsTrackingSandbox()
	initialCmd := doctorLogsJournalctlCmd(100, true)
	sinceCmd := doctorLogsJournalctlSinceCmd("t0")
	sb.SetExecSequence(initialCmd,
		sandbox.MockExecResult{
			Output:   doctorLogsTimePrefix + "t0\n" + doctorLogsCursorPrefix + "\n-- No entries --\nNo journal files were found.\n",
			ExitCode: 0,
		},
	)
	sb.SetExecSequence(sinceCmd,
		sandbox.MockExecResult{
			Output:   doctorLogsTimePrefix + "t1\n" + doctorLogsCursorPrefix + "cursor-1\nreal log\n",
			ExitCode: 0,
		},
	)
	sb.execHook = func(cmd string, call int) {
		if cmd == sinceCmd && call == 2 {
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

	history := sb.GetExecHistory()
	if len(history) != 2 {
		t.Fatalf("exec history len = %d, want 2", len(history))
	}
	if history[0] != initialCmd || history[1] != sinceCmd {
		t.Fatalf("exec history = %q, want [%q %q]", history, initialCmd, sinceCmd)
	}
	if got := output.String(); got != "real log\n" {
		t.Fatalf("logs --follow output = %q, want %q", got, "real log\n")
	}
}

func TestDoctorLogsCommandFollowTrimsOverlapWhileWaitingForFirstCursor(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	ctx, cancel := context.WithCancel(context.Background())
	setDoctorLogsFollowTestHooks(ctx, t, cancel, time.Millisecond)

	sb := newDoctorLogsTrackingSandbox()
	initialCmd := doctorLogsJournalctlCmd(100, true)
	firstSinceCmd := doctorLogsJournalctlSinceCmd("t0")
	secondSinceCmd := doctorLogsJournalctlSinceCmd("t1")
	sb.SetExecSequence(initialCmd,
		sandbox.MockExecResult{Output: doctorLogsTimePrefix + "t0\n" + doctorLogsCursorPrefix + "\n", ExitCode: 0},
	)
	sb.SetExecSequence(firstSinceCmd,
		sandbox.MockExecResult{Output: doctorLogsTimePrefix + "t1\n" + doctorLogsCursorPrefix + "\nalpha\nbeta\n", ExitCode: 0},
	)
	sb.SetExecSequence(secondSinceCmd,
		sandbox.MockExecResult{Output: doctorLogsTimePrefix + "t2\n" + doctorLogsCursorPrefix + "cursor-2\nbeta\ngamma\n", ExitCode: 0},
	)
	sb.execHook = func(cmd string, call int) {
		if cmd == secondSinceCmd && call == 3 {
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

	history := sb.GetExecHistory()
	if len(history) != 3 {
		t.Fatalf("exec history len = %d, want 3", len(history))
	}
	if history[0] != initialCmd || history[1] != firstSinceCmd || history[2] != secondSinceCmd {
		t.Fatalf("exec history = %q, want [%q %q %q]", history, initialCmd, firstSinceCmd, secondSinceCmd)
	}
	if got := output.String(); got != "alpha\nbeta\ngamma\n" {
		t.Fatalf("logs --follow output = %q, want %q", got, "alpha\nbeta\ngamma\n")
	}
}

func TestDoctorLogsCommandFollowSkipsReplayedPreSnapshotWindowWithoutCursor(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	ctx, cancel := context.WithCancel(context.Background())
	setDoctorLogsFollowTestHooks(ctx, t, cancel, time.Millisecond)

	sb := newDoctorLogsTrackingSandbox()
	initialCmd := doctorLogsJournalctlCmd(2, true)
	sinceCmd := doctorLogsJournalctlSinceCmd("t0")
	sb.SetExecSequence(initialCmd,
		sandbox.MockExecResult{Output: doctorLogsTimePrefix + "t0\n" + doctorLogsCursorPrefix + "\ntail 1\ntail 2\n", ExitCode: 0},
	)
	sb.SetExecSequence(sinceCmd,
		sandbox.MockExecResult{
			Output:   doctorLogsTimePrefix + "t1\n" + doctorLogsCursorPrefix + "cursor-2\nolder 1\nolder 2\ntail 1\ntail 2\nnew 1\n",
			ExitCode: 0,
		},
	)
	sb.execHook = func(cmd string, call int) {
		if cmd == sinceCmd && call == 2 {
			cancel()
		}
	}
	provider := &resolveCurrentSandboxTestProvider{sb: sb}
	meta := &sandbox.SandboxMeta{SandboxID: "sb-123", Agent: sandbox.AgentClaude}
	setCLICommandDeps(t, provider, meta)

	cmd := buildLogsCommand()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--follow", "--lines=2"})

	err := cmd.Execute()
	exitErr, ok := err.(exitError)
	if !ok || exitErr.code != 130 {
		t.Fatalf("logs --follow error = %v, want exitError(130)", err)
	}

	history := sb.GetExecHistory()
	if len(history) != 2 {
		t.Fatalf("exec history len = %d, want 2", len(history))
	}
	if history[0] != initialCmd || history[1] != sinceCmd {
		t.Fatalf("exec history = %q, want [%q %q]", history, initialCmd, sinceCmd)
	}
	if got := output.String(); got != "tail 1\ntail 2\nnew 1\n" {
		t.Fatalf("logs --follow output = %q, want %q", got, "tail 1\ntail 2\nnew 1\n")
	}
}

func TestDoctorLogsCommandFollowDmesgTrimsBoundaryOverlap(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	output := setCLIStdout(t)

	ctx, cancel := context.WithCancel(context.Background())
	setDoctorLogsFollowTestHooks(ctx, t, cancel, time.Millisecond)

	sb := newDoctorLogsTrackingSandbox()
	journalCmd := doctorLogsJournalctlCmd(100, true)
	dmesgCmd := doctorLogsDmesgFollowSnapshotCmd()
	sb.SetExecSequence(journalCmd, sandbox.MockExecResult{ExitCode: 127})
	sb.SetExecSequence(dmesgCmd,
		sandbox.MockExecResult{Output: "snapshot logs\nboundary log\n", ExitCode: 0},
		sandbox.MockExecResult{Output: "snapshot logs\nboundary log\nnew log\n", ExitCode: 0},
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
	if got := output.String(); got != "snapshot logs\nboundary log\nnew log\n" {
		t.Fatalf("logs --follow output = %q, want %q", got, "snapshot logs\nboundary log\nnew log\n")
	}
	history := sb.GetExecHistory()
	if strings.Contains(strings.Join(history, "\n"), "--since=") {
		t.Fatalf("exec history = %q, should not require dmesg --since", history)
	}
}

func TestDoctorLogsCommandRejectsNonPositiveLines(t *testing.T) {
	cwd := t.TempDir()
	chdirForTest(t, cwd)
	setCLIStdout(t)

	cmd := buildLogsCommand()
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--follow", "--lines=-1"})

	err := cmd.Execute()
	if err == nil || err.Error() != "--lines must be > 0" {
		t.Fatalf("logs --follow error = %v, want %q", err, "--lines must be > 0")
	}
}
