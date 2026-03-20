package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

func assistantDogfoodRun(rt *assistantDogfoodRuntime) int {
	if _, err := fmt.Fprintf(
		rt.Output,
		"dogfood_start repo=%s report_dir=%s\n",
		shellQuoteCommandValue(rt.RepoPath),
		shellQuoteCommandValue(rt.ReportDir),
	); err != nil {
		Errorf(rt.Err, "failed to write dogfood start banner: %v", err)
		return ExitInternalError
	}
	if err := assistantDogfoodWriteHealthSnapshot(rt); err != nil {
		Errorf(rt.Err, "failed to capture assistant health: %v", err)
		return ExitInternalError
	}
	if _, err := assistantDogfoodRunDX(rt, "project_add", "project", "add", "--path", rt.RepoPath); err != nil {
		Errorf(rt.Err, "project add failed: %v", err)
		return ExitInternalError
	}

	workspace1, err := assistantDogfoodRunDX(
		rt,
		"workspace1_create",
		"workspace",
		"create",
		rt.PrimaryWorkspaceName,
		"--project",
		rt.RepoPath,
		"--assistant",
		rt.Assistant,
	)
	if err != nil {
		Errorf(rt.Err, "workspace1 create failed: %v", err)
		return ExitInternalError
	}
	ws1ID := assistantDogfoodWorkspaceID(workspace1.Payload)
	if strings.TrimSpace(ws1ID) == "" {
		Errorf(rt.Err, "failed to resolve ws1 id from workspace1_create")
		return ExitInternalError
	}

	if _, err := assistantDogfoodRunAssistantLocalPing(rt, "assistant_local_ping", ws1ID); err != nil {
		Errorf(rt.Err, "assistant local ping failed: %v", err)
		return ExitInternalError
	}
	channelCfg := assistantDogfoodChannelConfigFromEnv()
	channelStatusToken := fmt.Sprintf("ch-status-%s-%s", rt.RunTag, ws1ID)
	channelStatusCommand := "printf '%s\\n' " + shellQuoteCommandValue(channelStatusToken) + "; " +
		assistantDogfoodDXShellCommand(rt, "status", "--workspace", ws1ID)
	if _, err := assistantDogfoodRunAssistantChannelCommand(
		rt,
		"assistant_channel_status",
		"dogfood-channel-"+ws1ID+"-"+rt.RunTag,
		channelCfg.Channel,
		channelStatusCommand,
		channelStatusToken,
		true,
	); err != nil {
		Errorf(rt.Err, "assistant channel status failed: %v", err)
		return ExitInternalError
	}

	if _, err := assistantDogfoodRunDX(
		rt,
		"start_ws1",
		"start",
		"--workspace",
		ws1ID,
		"--assistant",
		rt.Assistant,
		"--prompt",
		"Update README with run instructions and add NOTES.md with one mobile DX tip.",
		"--max-steps",
		"2",
		"--turn-budget",
		"120",
		"--wait-timeout",
		"80s",
		"--idle-threshold",
		"10s",
	); err != nil {
		Errorf(rt.Err, "start ws1 failed: %v", err)
		return ExitInternalError
	}
	if _, err := assistantDogfoodRunDX(
		rt,
		"continue_ws1",
		"continue",
		"--workspace",
		ws1ID,
		"--auto-start",
		"--text",
		"Add one concise status line to NOTES.md and finish.",
		"--enter",
		"--max-steps",
		"1",
		"--turn-budget",
		"90",
		"--wait-timeout",
		"70s",
		"--idle-threshold",
		"10s",
	); err != nil {
		Errorf(rt.Err, "continue ws1 failed: %v", err)
		return ExitInternalError
	}

	workspace2, err := assistantDogfoodRunDX(
		rt,
		"workspace2_create",
		"workspace",
		"create",
		rt.SecondaryWorkspace,
		"--project",
		rt.RepoPath,
		"--assistant",
		rt.Assistant,
	)
	if err != nil {
		Errorf(rt.Err, "workspace2 create failed: %v", err)
		return ExitInternalError
	}
	ws2ID := assistantDogfoodWorkspaceID(workspace2.Payload)
	if strings.TrimSpace(ws2ID) != "" {
		if _, err := assistantDogfoodRunAssistantChannelCommand(
			rt,
			"assistant_channel_terminal_ws2",
			"dogfood-channel-ws2-"+ws2ID+"-"+rt.RunTag,
			channelCfg.Channel,
			assistantDogfoodDXShellCommand(
				rt,
				"terminal",
				"run",
				"--workspace",
				ws2ID,
				"--text",
				"echo channel-smoke > CHANNEL_SMOKE.txt",
				"--enter",
			),
			"",
			false,
		); err != nil {
			Errorf(rt.Err, "assistant channel ws2 terminal failed: %v", err)
			return ExitInternalError
		}
		if _, err := assistantDogfoodRunDX(
			rt,
			"start_ws2",
			"start",
			"--workspace",
			ws2ID,
			"--assistant",
			rt.Assistant,
			"--prompt",
			"Create TODO.md with three concise next steps for this repo.",
			"--max-steps",
			"1",
			"--turn-budget",
			"90",
			"--wait-timeout",
			"70s",
			"--idle-threshold",
			"10s",
		); err != nil {
			Errorf(rt.Err, "start ws2 failed: %v", err)
			return ExitInternalError
		}
	}

	if _, err := assistantDogfoodRunDX(rt, "terminal_run_ws1", "terminal", "run", "--workspace", ws1ID, "--text", "go run main.go", "--enter"); err != nil {
		Errorf(rt.Err, "terminal run failed: %v", err)
		return ExitInternalError
	}
	time.Sleep(time.Second)
	if _, err := assistantDogfoodRunDX(rt, "terminal_logs_ws1", "terminal", "logs", "--workspace", ws1ID, "--lines", "40"); err != nil {
		Errorf(rt.Err, "terminal logs failed: %v", err)
		return ExitInternalError
	}

	if _, err := assistantDogfoodRunDX(
		rt,
		"dual_impl_ws1",
		"start",
		"--workspace",
		ws1ID,
		"--assistant",
		rt.Assistant,
		"--prompt",
		"Append one concise mobile-coding tip to README.md and proceed even if there are unrelated uncommitted changes.",
		"--max-steps",
		"1",
		"--turn-budget",
		"100",
		"--wait-timeout",
		"70s",
		"--idle-threshold",
		"10s",
		"--allow-new-run",
	); err != nil {
		Errorf(rt.Err, "dual impl failed: %v", err)
		return ExitInternalError
	}
	if !assistantDogfoodWaitForWorkspaceReadyForReview(rt, ws1ID, rt.Assistant) {
		return ExitInternalError
	}
	if _, err := assistantDogfoodRunDX(
		rt,
		"dual_review_ws1",
		"review",
		"--workspace",
		ws1ID,
		"--assistant",
		rt.Assistant,
		"--prompt",
		"Review for clarity and correctness.",
		"--max-steps",
		"1",
		"--turn-budget",
		"100",
		"--wait-timeout",
		"70s",
		"--idle-threshold",
		"10s",
	); err != nil {
		Errorf(rt.Err, "dual review failed: %v", err)
		return ExitInternalError
	}

	steps := [][]string{
		{"git_ship_ws1", "git", "ship", "--workspace", ws1ID, "--message", "dogfood: scripted assistant pass"},
		{"status_ws1", "status", "--workspace", ws1ID, "--capture-agents", "8", "--capture-lines", "80"},
		{"alerts_project", "alerts", "--project", rt.RepoPath, "--capture-agents", "8", "--capture-lines", "80"},
	}
	if strings.TrimSpace(ws2ID) != "" {
		steps = append(steps, []string{"status_ws2", "status", "--workspace", ws2ID, "--capture-agents", "8", "--capture-lines", "80"})
	}
	for _, step := range steps {
		if _, err := assistantDogfoodRunDX(rt, step[0], step[1:]...); err != nil {
			Errorf(rt.Err, "%s failed: %v", step[0], err)
			return ExitInternalError
		}
	}

	summaryPath, channelUnverifiedCount, err := assistantDogfoodWriteSummary(rt, ws1ID, ws2ID)
	if err != nil {
		Errorf(rt.Err, "failed to write summary: %v", err)
		return ExitInternalError
	}
	if _, err := fmt.Fprintf(rt.Output, "dogfood_complete summary_file=%s\n", shellQuoteCommandValue(summaryPath)); err != nil {
		Errorf(rt.Err, "failed to write dogfood completion banner: %v", err)
		return ExitInternalError
	}
	summaryRaw, err := os.ReadFile(summaryPath)
	if err != nil {
		Errorf(rt.Err, "failed to read summary: %v", err)
		return ExitInternalError
	}
	if _, err := rt.Output.Write(summaryRaw); err != nil {
		Errorf(rt.Err, "failed to print summary: %v", err)
		return ExitInternalError
	}
	if assistantDogfoodEnvBool("AMUX_ASSISTANT_DOGFOOD_REQUIRE_CHANNEL_EXECUTION", true) && channelUnverifiedCount > 0 {
		if _, err := fmt.Fprintf(rt.Output, "dogfood_fail reason=channel_execution_unverified count=%d\n", channelUnverifiedCount); err != nil {
			Errorf(rt.Err, "failed to write dogfood failure banner: %v", err)
			return ExitInternalError
		}
		return ExitUsage
	}
	return ExitOK
}

func assistantDogfoodWaitForWorkspaceReadyForReview(
	rt *assistantDogfoodRuntime,
	workspace, assistant string,
) bool {
	timeoutSeconds := assistantDogfoodEnvInt("AMUX_ASSISTANT_DOGFOOD_REVIEW_GATE_TIMEOUT_SECONDS", 240)
	pollSeconds := assistantDogfoodEnvInt("AMUX_ASSISTANT_DOGFOOD_REVIEW_GATE_POLL_SECONDS", 5)
	start := time.Now()
	for attempt := 1; ; attempt++ {
		record, err := assistantDogfoodRunDX(
			rt,
			fmt.Sprintf("dual_gate_status_%d", attempt),
			"status",
			"--workspace",
			workspace,
			"--assistant",
			assistant,
		)
		if err != nil {
			Errorf(rt.Err, "review gate status failed: %v", err)
			return false
		}
		status := assistantDogfoodNestedString(record.Payload, "status")
		overall := assistantDogfoodNestedString(record.Payload, "data", "task", "overall_status")
		if status == "ok" || overall == "completed" || overall == "session_exited" ||
			overall == "partial" || overall == "partial_budget" || overall == "timed_out" {
			return true
		}
		if status == "needs_input" {
			if _, err := assistantDogfoodRunDX(
				rt,
				fmt.Sprintf("dual_gate_continue_%d", attempt),
				"continue",
				"--workspace",
				workspace,
				"--assistant",
				assistant,
				"--text",
				"Continue from current state and finish this run with a concise completion summary.",
				"--enter",
				"--wait-timeout",
				"70s",
				"--idle-threshold",
				"10s",
			); err != nil {
				Errorf(rt.Err, "review gate continue failed: %v", err)
				return false
			}
		}
		if timeoutSeconds >= 0 && assistantDogfoodElapsedSeconds(start) >= timeoutSeconds {
			Errorf(
				rt.Err,
				"timed out waiting for workspace %s implement run to reach terminal state before review (status=%s overall=%s)",
				workspace,
				status,
				overall,
			)
			return false
		}
		if pollSeconds > 0 {
			time.Sleep(time.Duration(pollSeconds) * time.Second)
		}
	}
}

func assistantDogfoodWriteHealthSnapshot(rt *assistantDogfoodRuntime) error {
	out, _ := assistantDogfoodRunExec("", nil, rt.AssistantBin, "health", "--json")
	return assistantDogfoodWriteTextFile(filepath.Join(rt.ReportDir, "assistant-health.raw"), string(out))
}

func assistantDogfoodWorkspaceID(payload map[string]any) string {
	return firstNonEmpty(
		assistantDogfoodNestedString(payload, "data", "id"),
		assistantDogfoodNestedString(payload, "data", "workspace", "id"),
		assistantDogfoodNestedString(payload, "data", "workspace_id"),
		assistantDogfoodNestedString(payload, "data", "context", "workspace", "id"),
	)
}

func assistantDogfoodWriteSummary(rt *assistantDogfoodRuntime, ws1ID, ws2ID string) (string, int, error) {
	statusFiles, err := filepath.Glob(filepath.Join(rt.ReportDir, "*.status"))
	if err != nil {
		return "", 0, err
	}
	slices.Sort(statusFiles)
	channelUnverifiedCount := 0
	lines := []string{
		"repo=" + rt.RepoPath,
		"report_dir=" + rt.ReportDir,
		"dx_context_file=" + rt.DXContextFile,
		"workspace_primary=" + ws1ID,
		"workspace_primary_name=" + rt.PrimaryWorkspaceName,
	}
	if strings.TrimSpace(ws2ID) != "" {
		lines = append(lines,
			"workspace_secondary="+ws2ID,
			"workspace_secondary_name="+rt.SecondaryWorkspace,
		)
	}
	lines = append(lines, "steps:")
	for _, path := range statusFiles {
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", 0, readErr
		}
		status := strings.TrimSpace(string(content))
		lines = append(lines, "  "+strings.TrimSuffix(filepath.Base(path), ".status")+": "+status)
		if strings.Contains(status, "channel output unverified: command execution proof missing") ||
			strings.Contains(status, "channel output missing execution markers") {
			channelUnverifiedCount++
		}
	}
	lines = append(lines, "channel_unverified_count="+strconv.Itoa(channelUnverifiedCount))
	summaryPath := filepath.Join(rt.ReportDir, "summary.txt")
	return summaryPath, channelUnverifiedCount, assistantDogfoodWriteTextFile(summaryPath, strings.Join(lines, "\n"))
}

func assistantDogfoodEnvInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
