package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCmdCapabilitiesJSON(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := cmdCapabilities(&out, &errOut, GlobalFlags{JSON: true}, nil, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdCapabilities() code = %d, want %d", code, ExitOK)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got error=%#v", env.Error)
	}

	payload, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", env.Data)
	}
	if got, _ := payload["schema_version"].(string); got != EnvelopeSchemaVersion {
		t.Fatalf("schema_version = %q, want %q", got, EnvelopeSchemaVersion)
	}
	features, ok := payload["features"].(map[string]any)
	if !ok {
		t.Fatalf("expected features object")
	}
	if got, _ := features["idempotency_key"].(bool); !got {
		t.Fatalf("expected idempotency_key capability")
	}
	if got, _ := features["send_jobs"].(bool); !got {
		t.Fatalf("expected send_jobs capability")
	}
	if got, _ := features["async_send"].(bool); !got {
		t.Fatalf("expected async_send capability")
	}
	if got, _ := features["job_wait"].(bool); !got {
		t.Fatalf("expected job_wait capability")
	}
	if got, _ := features["agent_wait"].(bool); !got {
		t.Fatalf("expected agent_wait capability")
	}
	if got, _ := features["wait_response_status"].(bool); !got {
		t.Fatalf("expected wait_response_status capability")
	}
	if got, _ := features["wait_response_delta"].(bool); !got {
		t.Fatalf("expected wait_response_delta capability")
	}
	if got, _ := features["wait_response_early_input"].(bool); !got {
		t.Fatalf("expected wait_response_early_input capability")
	}
	if got, _ := features["wait_response_needs_input"].(bool); !got {
		t.Fatalf("expected wait_response_needs_input capability")
	}
	if got, _ := features["wait_response_summary"].(bool); !got {
		t.Fatalf("expected wait_response_summary capability")
	}
	if got, _ := features["watch_heartbeat"].(bool); !got {
		t.Fatalf("expected watch_heartbeat capability")
	}
	if got, _ := features["watch_needs_input"].(bool); !got {
		t.Fatalf("expected watch_needs_input capability")
	}

	commands, ok := payload["commands"].([]any)
	if !ok {
		t.Fatalf("expected commands list")
	}
	containsCommand := func(values []any, want string) bool {
		for _, raw := range values {
			if got, _ := raw.(string); got == want {
				return true
			}
		}
		return false
	}
	if !containsCommand(commands, "agent job status") {
		t.Fatalf("expected agent job status command in capabilities")
	}
	if !containsCommand(commands, "agent job wait") {
		t.Fatalf("expected agent job wait command in capabilities")
	}
	if !containsCommand(commands, "doctor tmux") {
		t.Fatalf("expected doctor tmux command in capabilities")
	}
	if !containsCommand(commands, "task start") {
		t.Fatalf("expected task start command in capabilities")
	}
	if !containsCommand(commands, "task status") {
		t.Fatalf("expected task status command in capabilities")
	}

	mutating, ok := payload["mutating_commands"].([]any)
	if !ok {
		t.Fatalf("expected mutating commands list")
	}
	if !containsCommand(mutating, "task start") {
		t.Fatalf("expected task start command in mutating capabilities")
	}
	if containsCommand(mutating, "task status") {
		t.Fatalf("did not expect task status command in mutating capabilities")
	}
}
