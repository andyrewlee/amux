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
	foundAgentJobStatus := false
	for _, raw := range commands {
		if cmd, _ := raw.(string); cmd == "agent job status" {
			foundAgentJobStatus = true
			break
		}
	}
	if !foundAgentJobStatus {
		t.Fatalf("expected agent job status command in capabilities")
	}
	foundAgentJobWait := false
	for _, raw := range commands {
		if cmd, _ := raw.(string); cmd == "agent job wait" {
			foundAgentJobWait = true
			break
		}
	}
	if !foundAgentJobWait {
		t.Fatalf("expected agent job wait command in capabilities")
	}
}
