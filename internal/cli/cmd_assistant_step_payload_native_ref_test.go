package cli

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestBuildAssistantStepPayload_UsesExecutableNativeAssistantStepRef(t *testing.T) {
	oldStat := assistantCompatStat
	oldRepoLookup := assistantCompatRepoScriptPathFunc
	t.Cleanup(func() {
		assistantCompatStat = oldStat
		assistantCompatRepoScriptPathFunc = oldRepoLookup
	})

	assistantCompatStat = func(string) (os.FileInfo, error) {
		return nil, errors.New("missing")
	}
	assistantCompatRepoScriptPathFunc = func(string) string {
		return ""
	}

	t.Setenv("AMUX_ASSISTANT_STEP_CMD_REF", "")

	payload := buildAssistantStepPayload(
		assistantStepOptions{Mode: assistantStepModeSend, AgentID: "agent-1"},
		assistantStepUnderlying{
			AgentID:     "agent-1",
			WorkspaceID: "ws-1",
			Assistant:   "codex",
			Response: &waitResponseResult{
				Status: "timed_out",
			},
		},
		"amux",
	)

	if !strings.Contains(payload.SuggestedCommand, `amux assistant step send --agent agent-1`) {
		t.Fatalf("suggested_command = %q, want native assistant step command", payload.SuggestedCommand)
	}
	if strings.Contains(payload.SuggestedCommand, `'amux assistant step'`) {
		t.Fatalf("suggested_command = %q, should not quote native command ref as one token", payload.SuggestedCommand)
	}
}
