package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCmdTaskStart_ActiveAgentRequiresConfirmationByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wsID := createTaskTestWorkspace(t, home)

	origSessionsWithTags := tmuxSessionsWithTags
	origCapture := tmuxCapturePaneTail
	t.Cleanup(func() {
		tmuxSessionsWithTags = origSessionsWithTags
		tmuxCapturePaneTail = origCapture
	})

	tmuxSessionsWithTags = func(_ map[string]string, _ []string, _ tmux.Options) ([]tmux.SessionTagValues, error) {
		return []tmux.SessionTagValues{{
			Name: "amux-" + string(wsID) + "-t_existing",
			Tags: map[string]string{
				"@amux_tab":       "t_existing",
				"@amux_assistant": "codex",
			},
		}}, nil
	}
	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		return "Running task now", true
	}

	var out, errOut bytes.Buffer
	code := cmdTaskStart(&out, &errOut, GlobalFlags{JSON: true}, []string{
		"--workspace", string(wsID),
		"--assistant", "codex",
		"--prompt", "Refactor parser and run tests",
	}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdTaskStart() code = %d, stderr=%q out=%q", code, errOut.String(), out.String())
	}

	payload := decodeTaskResult(t, out.Bytes())
	if got := taskString(payload, "status"); got != "needs_input" {
		t.Fatalf("status = %q, want %q", got, "needs_input")
	}
	summary := taskString(payload, "summary")
	if !strings.Contains(summary, "another assistant tab is active") {
		t.Fatalf("summary = %q, expected active-tab confirmation", summary)
	}
	if !strings.Contains(summary, "Last agent line: Running task now") {
		t.Fatalf("summary = %q, expected last line hint", summary)
	}
}

func TestCmdTaskStart_AllowNewRunUsesTaskRunner(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wsID := createTaskTestWorkspace(t, home)

	origTaskRunAgent := taskRunAgent
	t.Cleanup(func() {
		taskRunAgent = origTaskRunAgent
	})
	taskRunAgent = func(_ *Services, wsID data.WorkspaceID, assistant, prompt string, waitTimeout, idleThreshold time.Duration, idempotencyKey, version string) (agentRunResult, error) {
		if wsID == "" || assistant == "" || prompt == "" || waitTimeout <= 0 || idleThreshold <= 0 || version == "" {
			t.Fatalf("unexpected runner inputs: ws=%q assistant=%q prompt=%q wait=%s idle=%s version=%q", wsID, assistant, prompt, waitTimeout, idleThreshold, version)
		}
		_ = idempotencyKey
		return agentRunResult{
			SessionName: "sess-new",
			AgentID:     string(wsID) + ":t_new",
			WorkspaceID: string(wsID),
			Assistant:   assistant,
			TabID:       "t_new",
			Response: &waitResponseResult{
				Status:     "timed_out",
				Summary:    "Timed out waiting for first output.",
				LatestLine: "(no output yet)",
				TimedOut:   true,
			},
		}, nil
	}

	var out, errOut bytes.Buffer
	code := cmdTaskStart(&out, &errOut, GlobalFlags{JSON: true}, []string{
		"--workspace", string(wsID),
		"--assistant", "codex",
		"--prompt", "Implement retry guardrails",
		"--allow-new-run",
	}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdTaskStart() code = %d, stderr=%q out=%q", code, errOut.String(), out.String())
	}

	payload := decodeTaskResult(t, out.Bytes())
	if got := taskString(payload, "status"); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	if got := taskString(payload, "overall_status"); got != "in_progress" {
		t.Fatalf("overall_status = %q, want %q", got, "in_progress")
	}
}

func TestCmdTaskStart_CompletedStaleAgentDoesNotRequireConfirmation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wsID := createTaskTestWorkspace(t, home)

	origSessionsWithTags := tmuxSessionsWithTags
	origCapture := tmuxCapturePaneTail
	origTaskRunAgent := taskRunAgent
	t.Cleanup(func() {
		tmuxSessionsWithTags = origSessionsWithTags
		tmuxCapturePaneTail = origCapture
		taskRunAgent = origTaskRunAgent
	})

	oldOutputTS := strconv.FormatInt(time.Now().Add(-2*time.Minute).Unix(), 10)
	tmuxSessionsWithTags = func(_ map[string]string, _ []string, _ tmux.Options) ([]tmux.SessionTagValues, error) {
		return []tmux.SessionTagValues{{
			Name: "amux-" + string(wsID) + "-t_done",
			Tags: map[string]string{
				"@amux_tab":          "t_done",
				"@amux_assistant":    "droid",
				"@amux_created_at":   strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10),
				tmux.TagLastOutputAt: oldOutputTS,
			},
		}}, nil
	}
	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		return "Review completed with findings.", true
	}
	taskRunAgent = func(_ *Services, wsID data.WorkspaceID, assistant, prompt string, waitTimeout, idleThreshold time.Duration, idempotencyKey, version string) (agentRunResult, error) {
		_ = waitTimeout
		_ = idleThreshold
		_ = idempotencyKey
		_ = version
		return agentRunResult{
			SessionName: "sess-next",
			AgentID:     string(wsID) + ":t_next",
			WorkspaceID: string(wsID),
			Assistant:   assistant,
			TabID:       "t_next",
			Response: &waitResponseResult{
				Status:     "idle",
				Summary:    "Started new run after stale completed tab.",
				LatestLine: "Started new run after stale completed tab.",
			},
		}, nil
	}

	var out, errOut bytes.Buffer
	code := cmdTaskStart(&out, &errOut, GlobalFlags{JSON: true}, []string{
		"--workspace", string(wsID),
		"--assistant", "droid",
		"--prompt", "Run a fresh review now",
	}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdTaskStart() code = %d, stderr=%q out=%q", code, errOut.String(), out.String())
	}

	payload := decodeTaskResult(t, out.Bytes())
	if got := taskString(payload, "status"); got == "needs_input" {
		t.Fatalf("status = %q, want not needs_input for stale completed tab", got)
	}
}

func TestCmdTaskStatus_ReportsNeedsInputFromActiveAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wsID := createTaskTestWorkspace(t, home)

	origSessionsWithTags := tmuxSessionsWithTags
	origCapture := tmuxCapturePaneTail
	t.Cleanup(func() {
		tmuxSessionsWithTags = origSessionsWithTags
		tmuxCapturePaneTail = origCapture
	})

	tmuxSessionsWithTags = func(_ map[string]string, _ []string, _ tmux.Options) ([]tmux.SessionTagValues, error) {
		return []tmux.SessionTagValues{{
			Name: "amux-" + string(wsID) + "-t_needinput",
			Tags: map[string]string{
				"@amux_tab":       "t_needinput",
				"@amux_assistant": "codex",
			},
		}}, nil
	}
	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		return "Would you like me to proceed with option 1?", true
	}

	var out, errOut bytes.Buffer
	code := cmdTaskStatus(&out, &errOut, GlobalFlags{JSON: true}, []string{
		"--workspace", string(wsID),
		"--assistant", "codex",
	}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdTaskStatus() code = %d, stderr=%q out=%q", code, errOut.String(), out.String())
	}

	payload := decodeTaskResult(t, out.Bytes())
	if got := taskString(payload, "status"); got != "needs_input" {
		t.Fatalf("status = %q, want %q", got, "needs_input")
	}
	suggested := taskString(payload, "suggested_command")
	if !strings.Contains(suggested, "agent send") {
		t.Fatalf("suggested_command = %q, expected reply command", suggested)
	}
}

func TestCmdTaskStatus_ReportsCompletedWhenOutputIsStable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wsID := createTaskTestWorkspace(t, home)

	origSessionsWithTags := tmuxSessionsWithTags
	origCapture := tmuxCapturePaneTail
	t.Cleanup(func() {
		tmuxSessionsWithTags = origSessionsWithTags
		tmuxCapturePaneTail = origCapture
	})

	oldOutputTS := strconv.FormatInt(time.Now().Add(-2*time.Minute).Unix(), 10)
	tmuxSessionsWithTags = func(_ map[string]string, _ []string, _ tmux.Options) ([]tmux.SessionTagValues, error) {
		return []tmux.SessionTagValues{{
			Name: "amux-" + string(wsID) + "-t_done",
			Tags: map[string]string{
				"@amux_tab":          "t_done",
				"@amux_assistant":    "droid",
				tmux.TagLastOutputAt: oldOutputTS,
			},
		}}, nil
	}
	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		return "Code review completed with findings and residual risks.", true
	}

	var out, errOut bytes.Buffer
	code := cmdTaskStatus(&out, &errOut, GlobalFlags{JSON: true}, []string{
		"--workspace", string(wsID),
		"--assistant", "droid",
	}, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdTaskStatus() code = %d, stderr=%q out=%q", code, errOut.String(), out.String())
	}

	payload := decodeTaskResult(t, out.Bytes())
	if got := taskString(payload, "status"); got != "idle" {
		t.Fatalf("status = %q, want %q", got, "idle")
	}
	if got := taskString(payload, "overall_status"); got != "completed" {
		t.Fatalf("overall_status = %q, want %q", got, "completed")
	}
}

func createTaskTestWorkspace(t *testing.T, home string) data.WorkspaceID {
	t.Helper()

	repo := filepath.Join(home, "repo")
	root := filepath.Join(home, "ws", "task")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo) error = %v", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(root) error = %v", err)
	}
	ws := data.NewWorkspace("task", "main", "", repo, root)
	store := data.NewWorkspaceStore(filepath.Join(home, ".amux", "workspaces-metadata"))
	if err := store.Save(ws); err != nil {
		t.Fatalf("store.Save() error = %v", err)
	}
	return ws.ID()
}

func decodeTaskResult(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("json.Unmarshal envelope: %v\nraw=%s", err, string(raw))
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got error=%#v raw=%s", env.Error, string(raw))
	}
	payload, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", env.Data)
	}
	return payload
}

func taskString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}
