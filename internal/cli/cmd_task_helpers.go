package cli

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

const taskCaptureLines = 120

type taskAgentCandidate struct {
	SessionName   string
	AgentID       string
	Assistant     string
	CreatedAt     int64
	LastOutputAt  time.Time
	HasLastOutput bool
}

type taskAgentSnapshot struct {
	Summary    string
	LatestLine string
	NeedsInput bool
	InputHint  string
}

type taskStartLock struct {
	path string
}

var taskRunAgent = taskRunAgentViaCmdAgentRun

func taskRunAgentViaCmdAgentRun(
	_ *Services,
	wsID data.WorkspaceID,
	assistant,
	prompt string,
	waitTimeout,
	idleThreshold time.Duration,
	idempotencyKey,
	version string,
) (agentRunResult, error) {
	args := []string{
		"--workspace", string(wsID),
		"--assistant", assistant,
		"--prompt", prompt,
		"--wait",
		"--wait-timeout", waitTimeout.String(),
		"--idle-threshold", idleThreshold.String(),
	}
	if strings.TrimSpace(idempotencyKey) != "" {
		args = append(args, "--idempotency-key", strings.TrimSpace(idempotencyKey))
	}

	var out, errOut bytes.Buffer
	code := cmdAgentRun(&out, &errOut, GlobalFlags{JSON: true}, args, version)
	if code != ExitOK {
		env, parseErr := parseTaskEnvelope(out.Bytes())
		if parseErr == nil && env.Error != nil {
			return agentRunResult{}, fmt.Errorf("%s", env.Error.Message)
		}
		if strings.TrimSpace(errOut.String()) != "" {
			return agentRunResult{}, fmt.Errorf("%s", strings.TrimSpace(errOut.String()))
		}
		return agentRunResult{}, errors.New("agent run failed")
	}
	env, err := parseTaskEnvelope(out.Bytes())
	if err != nil {
		return agentRunResult{}, err
	}
	if !env.OK {
		if env.Error != nil {
			return agentRunResult{}, fmt.Errorf("%s", env.Error.Message)
		}
		return agentRunResult{}, errors.New("agent run failed")
	}

	var result agentRunResult
	encoded, err := json.Marshal(env.Data)
	if err != nil {
		return agentRunResult{}, err
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return agentRunResult{}, err
	}
	return result, nil
}

func parseTaskEnvelope(raw []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Envelope{}, err
	}
	return env, nil
}

func buildTaskNeedsConfirmationResult(
	workspaceID,
	assistant,
	prompt string,
	candidate taskAgentCandidate,
	snap taskAgentSnapshot,
) taskCommandResult {
	latest := strings.TrimSpace(snap.LatestLine)
	if latest == "" {
		latest = "(no visible output yet)"
	}
	continueCmd := fmt.Sprintf(
		"amux --json agent send --agent %s --text %s --enter --wait --wait-timeout 60s --idle-threshold 10s",
		quoteCommandValue(candidate.AgentID),
		quoteCommandValue("Provide a one-line status update and next action."),
	)
	startNewCmd := fmt.Sprintf(
		"amux --json task start --workspace %s --assistant %s --allow-new-run --prompt %s",
		quoteCommandValue(workspaceID),
		quoteCommandValue(assistant),
		quoteCommandValue(prompt),
	)
	statusCmd := fmt.Sprintf(
		"amux --json task status --workspace %s --assistant %s",
		quoteCommandValue(workspaceID),
		quoteCommandValue(assistant),
	)
	message := "Task not started automatically because another assistant tab is active."
	if strings.TrimSpace(candidate.SessionName) != "" {
		message += " Session: " + candidate.SessionName + "."
	}
	message += " Last agent line: " + latest
	return taskCommandResult{
		Mode:             "run",
		Status:           "needs_input",
		OverallStatus:    "needs_input",
		Summary:          message,
		AgentID:          candidate.AgentID,
		SessionName:      candidate.SessionName,
		WorkspaceID:      workspaceID,
		Assistant:        assistant,
		LatestLine:       latest,
		NeedsInput:       true,
		InputHint:        "Confirm: continue existing tab or explicitly start a new task tab.",
		NextAction:       "Continue existing tab, or start a new tab explicitly.",
		SuggestedCommand: continueCmd,
		Prompt:           prompt,
		QuickActions: []taskQuickAction{
			{ID: "continue_existing", Label: "Continue Existing", Command: continueCmd, Prompt: "Continue existing tab"},
			{ID: "start_new", Label: "Start New", Command: startNewCmd, Prompt: "Start a new tab"},
			{ID: "status", Label: "Status", Command: statusCmd, Prompt: "Check active task status"},
		},
	}
}

func buildTaskStartResult(workspaceID, assistant, prompt string, run agentRunResult) taskCommandResult {
	result := taskCommandResult{
		Mode:        "run",
		WorkspaceID: workspaceID,
		Assistant:   assistant,
		Prompt:      prompt,
		SessionName: run.SessionName,
		AgentID:     run.AgentID,
	}
	statusCmd := fmt.Sprintf(
		"amux --json task status --workspace %s --assistant %s",
		quoteCommandValue(workspaceID),
		quoteCommandValue(assistant),
	)
	result.SuggestedCommand = statusCmd

	if run.Response == nil {
		result.Status = "attention"
		result.OverallStatus = "in_progress"
		result.Summary = "Task started. Waiting for first visible output."
		result.NextAction = "Check task status shortly."
		result.QuickActions = []taskQuickAction{{ID: "status", Label: "Status", Command: statusCmd, Prompt: "Check active task status"}}
		return result
	}

	resp := run.Response
	result.LatestLine = strings.TrimSpace(resp.LatestLine)
	if result.LatestLine == "" {
		result.LatestLine = "(no visible output yet)"
	}
	result.NeedsInput = resp.NeedsInput
	result.InputHint = strings.TrimSpace(resp.InputHint)

	switch resp.Status {
	case "needs_input":
		result.Status = "needs_input"
		result.OverallStatus = "needs_input"
		result.Summary = nonEmpty(resp.Summary, "Task needs input.")
		result.NextAction = "Reply to the agent prompt to continue."
		followup := "Reply with the exact option needed, then continue and report status plus blockers."
		if strings.TrimSpace(result.InputHint) != "" {
			followup = result.InputHint
		}
		replyCmd := fmt.Sprintf(
			"amux --json agent send --agent %s --text %s --enter --wait --wait-timeout 60s --idle-threshold 10s",
			quoteCommandValue(run.AgentID),
			quoteCommandValue(followup),
		)
		result.SuggestedCommand = replyCmd
		result.QuickActions = []taskQuickAction{{ID: "reply", Label: "Reply", Command: replyCmd, Prompt: "Reply to agent prompt"}, {ID: "status", Label: "Status", Command: statusCmd, Prompt: "Check active task status"}}
	case "timed_out":
		result.Status = "attention"
		result.OverallStatus = "in_progress"
		result.Summary = "Task is still running."
		result.NextAction = "Keep the tab running and re-check status."
		result.QuickActions = []taskQuickAction{{ID: "status", Label: "Status", Command: statusCmd, Prompt: "Check active task status"}}
	case "session_exited":
		result.Status = "attention"
		result.OverallStatus = "session_exited"
		result.Summary = nonEmpty(resp.Summary, "Task session exited.")
		result.NextAction = "Start a fresh task run."
		result.SuggestedCommand = fmt.Sprintf("amux --json task start --workspace %s --assistant %s --prompt %s", quoteCommandValue(workspaceID), quoteCommandValue(assistant), quoteCommandValue(prompt))
		result.QuickActions = []taskQuickAction{{ID: "start", Label: "Start", Command: result.SuggestedCommand, Prompt: "Start a fresh task"}}
	default:
		result.Status = "idle"
		result.OverallStatus = "completed"
		result.Summary = nonEmpty(resp.Summary, "Task step completed.")
		result.NextAction = "Read output and continue or start a follow-up task."
		result.QuickActions = []taskQuickAction{{ID: "status", Label: "Status", Command: statusCmd, Prompt: "Check task status"}}
	}

	return result
}

func buildTaskStatusResult(workspaceID, assistant string, candidate taskAgentCandidate, snap taskAgentSnapshot) taskCommandResult {
	latest := strings.TrimSpace(snap.LatestLine)
	if latest == "" {
		latest = "(no visible output yet)"
	}
	statusCmd := fmt.Sprintf(
		"amux --json task status --workspace %s --assistant %s",
		quoteCommandValue(workspaceID),
		quoteCommandValue(assistant),
	)
	result := taskCommandResult{
		Mode:             "status",
		WorkspaceID:      workspaceID,
		Assistant:        assistant,
		SessionName:      candidate.SessionName,
		AgentID:          candidate.AgentID,
		LatestLine:       latest,
		SuggestedCommand: statusCmd,
	}
	if snap.NeedsInput {
		result.Status = "needs_input"
		result.OverallStatus = "needs_input"
		result.NeedsInput = true
		result.InputHint = snap.InputHint
		result.Summary = nonEmpty(snap.Summary, "Task needs input.")
		result.NextAction = "Reply to the prompt to continue."
		replyCmd := fmt.Sprintf(
			"amux --json agent send --agent %s --text %s --enter --wait --wait-timeout 60s --idle-threshold 10s",
			quoteCommandValue(candidate.AgentID),
			quoteCommandValue(nonEmpty(strings.TrimSpace(snap.InputHint), "Reply with the exact option needed, then continue and report status plus blockers.")),
		)
		result.SuggestedCommand = replyCmd
		result.QuickActions = []taskQuickAction{{ID: "reply", Label: "Reply", Command: replyCmd, Prompt: "Reply to active prompt"}, {ID: "status", Label: "Status", Command: statusCmd, Prompt: "Check task status"}}
		return result
	}
	if taskStatusLooksComplete(candidate, snap) {
		result.Status = "idle"
		result.OverallStatus = "completed"
		result.Summary = nonEmpty(strings.TrimSpace(snap.Summary), "Task appears complete.")
		result.NextAction = "Share results, or continue with a follow-up instruction."
		continueCmd := fmt.Sprintf(
			"amux --json agent send --agent %s --text %s --enter --wait --wait-timeout 60s --idle-threshold 10s",
			quoteCommandValue(candidate.AgentID),
			quoteCommandValue("Continue from current state and provide status plus next action."),
		)
		result.SuggestedCommand = continueCmd
		result.QuickActions = []taskQuickAction{
			{ID: "continue", Label: "Continue", Command: continueCmd, Prompt: "Send a follow-up instruction"},
			{ID: "status", Label: "Status", Command: statusCmd, Prompt: "Check task status"},
		}
		return result
	}
	result.Status = "attention"
	result.OverallStatus = "in_progress"
	result.Summary = nonEmpty(snap.Summary, "Task is still running.")
	result.NextAction = "Keep running and re-check status."
	result.QuickActions = []taskQuickAction{{ID: "status", Label: "Status", Command: statusCmd, Prompt: "Check task status"}}
	return result
}

func findLatestTaskAgentSnapshot(opts tmux.Options, workspaceID, assistant string) (*taskAgentCandidate, taskAgentSnapshot, error) {
	candidates, err := listTaskAgentCandidates(opts, workspaceID, assistant)
	if err != nil {
		return nil, taskAgentSnapshot{}, err
	}
	if len(candidates) == 0 {
		return nil, taskAgentSnapshot{}, nil
	}
	snap := captureTaskAgentSnapshot(candidates[0].SessionName, opts)
	return &candidates[0], snap, nil
}

func listTaskAgentCandidates(opts tmux.Options, workspaceID, assistant string) ([]taskAgentCandidate, error) {
	rows, err := tmuxSessionsWithTags(
		map[string]string{
			"@amux":           "1",
			"@amux_workspace": workspaceID,
			"@amux_type":      "agent",
		},
		[]string{"@amux_tab", "@amux_assistant", "@amux_created_at", tmux.TagLastOutputAt},
		opts,
	)
	if err != nil {
		return nil, err
	}
	out := make([]taskAgentCandidate, 0, len(rows))
	assistant = strings.ToLower(strings.TrimSpace(assistant))
	for _, row := range rows {
		tagAssistant := strings.ToLower(strings.TrimSpace(row.Tags["@amux_assistant"]))
		if assistant != "" && tagAssistant != "" && tagAssistant != assistant {
			continue
		}
		tabID := strings.TrimSpace(row.Tags["@amux_tab"])
		if tabID == "" {
			tabID = inferTabIDFromSessionName(row.Name, workspaceID)
		}
		agentID := formatAgentID(workspaceID, tabID)
		if strings.TrimSpace(agentID) == "" {
			agentID = workspaceID + ":" + row.Name
		}
		createdAt := int64(0)
		if raw := strings.TrimSpace(row.Tags["@amux_created_at"]); raw != "" {
			if ts, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil {
				createdAt = ts
			}
		}
		lastOutputAt, hasLastOutput := parseSessionTagTime(row.Tags[tmux.TagLastOutputAt])
		out = append(out, taskAgentCandidate{
			SessionName:   row.Name,
			AgentID:       agentID,
			Assistant:     nonEmpty(tagAssistant, assistant),
			CreatedAt:     createdAt,
			LastOutputAt:  lastOutputAt,
			HasLastOutput: hasLastOutput,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].SessionName > out[j].SessionName
	})
	return out, nil
}

func captureTaskAgentSnapshot(sessionName string, opts tmux.Options) taskAgentSnapshot {
	content, ok := captureAgentPaneWithRetry(sessionName, taskCaptureLines, opts)
	if !ok {
		return taskAgentSnapshot{
			Summary:    "(no visible output yet)",
			LatestLine: "(no visible output yet)",
		}
	}
	compact := strings.TrimSpace(compactAgentOutput(content))
	if compact == "" {
		compact = strings.TrimSpace(content)
	}
	latest := strings.TrimSpace(lastNonEmptyLine(compact))
	if latest == "" {
		latest = "(no visible output yet)"
	}
	needsInput, inputHint := detectNeedsInput(compact)
	if !needsInput {
		needsInput, inputHint = detectNeedsInput(content)
	}
	summary := summarizeWaitResponse("idle", latest, needsInput, inputHint)
	if strings.TrimSpace(summary) == "" {
		summary = latest
	}
	return taskAgentSnapshot{
		Summary:    summary,
		LatestLine: latest,
		NeedsInput: needsInput,
		InputHint:  strings.TrimSpace(inputHint),
	}
}

func inferTabIDFromSessionName(sessionName, workspaceID string) string {
	prefix := tmux.SessionName("amux", workspaceID) + "-"
	if strings.HasPrefix(sessionName, prefix) {
		return strings.TrimPrefix(sessionName, prefix)
	}
	return ""
}

func acquireTaskStartLock(home, workspaceID, assistant string, ttl time.Duration) (*taskStartLock, bool, error) {
	path := taskStartLockPath(home, workspaceID, assistant)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, false, err
	}
	if info, err := os.Stat(path); err == nil {
		if ttl <= 0 || time.Since(info.ModTime()) < ttl {
			return nil, false, nil
		}
		_ = os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	_, _ = f.WriteString(fmt.Sprintf("pid=%d ts=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339)))
	_ = f.Close()
	return &taskStartLock{path: path}, true, nil
}

func (l *taskStartLock) release() {
	if l == nil || strings.TrimSpace(l.path) == "" {
		return
	}
	_ = os.Remove(l.path)
}

func taskStartLockPath(home, workspaceID, assistant string) string {
	sum := sha1.Sum([]byte(workspaceID + "|" + assistant + "|task-start"))
	token := hex.EncodeToString(sum[:8])
	return filepath.Join(home, "locks", "task-start-"+token+".lock")
}

func quoteCommandValue(value string) string {
	return strconv.Quote(value)
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}
