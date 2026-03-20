package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
)

func assistantTurnInvokeStepScript(stepScriptPath string, args []string) (assistantStepPayload, error) {
	if strings.TrimSpace(stepScriptPath) == "" {
		return assistantTurnInvokeStepInternal(args)
	}
	cmd := exec.Command(stepScriptPath, args...)
	cmd.Env = append(os.Environ(), "AMUX_ASSISTANT_STEP_SKIP_PRESENT=true")
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return assistantStepPayload{
			OK:       false,
			Status:   "command_error",
			Summary:  "Step script exited without JSON output.",
			Response: assistantStepResponsePayload{},
			Channel:  assistantStepChannelPayload{},
			Delivery: assistantStepDeliveryPayload{},
			Mode:     firstArgOrEmpty(args),
			Recovery: assistantStepRecoveryPayload{},
		}, nil
	}

	var payload assistantStepPayload
	if json.Unmarshal(out, &payload) != nil {
		return assistantStepPayload{
			OK:       false,
			Status:   "command_error",
			Summary:  "Step script produced invalid JSON output.",
			Response: assistantStepResponsePayload{},
			Channel:  assistantStepChannelPayload{},
			Delivery: assistantStepDeliveryPayload{},
			Mode:     firstArgOrEmpty(args),
			Recovery: assistantStepRecoveryPayload{},
		}, nil
	}
	return payload, nil
}

func assistantTurnInvokeStepInternal(args []string) (assistantStepPayload, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cmdAssistantStep(&stdout, &stderr, GlobalFlags{}, args, "")
	out := stdout.Bytes()
	if code != ExitOK && len(out) == 0 {
		return assistantStepPayload{
			OK:       false,
			Status:   "command_error",
			Summary:  "Step command exited without JSON output.",
			Response: assistantStepResponsePayload{},
			Channel:  assistantStepChannelPayload{},
			Delivery: assistantStepDeliveryPayload{},
			Mode:     firstArgOrEmpty(args),
			Recovery: assistantStepRecoveryPayload{},
		}, nil
	}

	var payload assistantStepPayload
	if json.Unmarshal(out, &payload) != nil {
		return assistantStepPayload{
			OK:       false,
			Status:   "command_error",
			Summary:  "Step command produced invalid JSON output.",
			Response: assistantStepResponsePayload{},
			Channel:  assistantStepChannelPayload{},
			Delivery: assistantStepDeliveryPayload{},
			Mode:     firstArgOrEmpty(args),
			Recovery: assistantStepRecoveryPayload{},
		}, nil
	}
	return payload, nil
}

func firstArgOrEmpty(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
