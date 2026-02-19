package cli

import (
	"fmt"
	"io"
)

type capabilityFeatures struct {
	JSONEnvelope           bool `json:"json_envelope"`
	RequestID              bool `json:"request_id"`
	AgentID                bool `json:"agent_id"`
	IdempotencyKey         bool `json:"idempotency_key"`
	SendJobs               bool `json:"send_jobs"`
	AsyncSend              bool `json:"async_send"`
	JobWait                bool `json:"job_wait"`
	AgentWait              bool `json:"agent_wait"`
	WaitResponseStatus     bool `json:"wait_response_status"`
	WaitResponseDelta      bool `json:"wait_response_delta"`
	WaitResponseEarlyInput bool `json:"wait_response_early_input"`
	WaitResponseNeedsInput bool `json:"wait_response_needs_input"`
	WaitResponseSummary    bool `json:"wait_response_summary"`
	WatchHeartbeat         bool `json:"watch_heartbeat"`
	WatchNeedsInput        bool `json:"watch_needs_input"`
}

type capabilitiesResult struct {
	SchemaVersion string             `json:"schema_version"`
	Commands      []string           `json:"commands"`
	Mutating      []string           `json:"mutating_commands"`
	GlobalFlags   []string           `json:"global_flags"`
	Features      capabilityFeatures `json:"features"`
}

func cmdCapabilities(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux capabilities [--json]"
	if len(args) > 0 {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}

	result := capabilitiesResult{
		SchemaVersion: EnvelopeSchemaVersion,
		Commands: []string{
			"status",
			"doctor",
			"capabilities",
			"logs tail",
			"workspace list",
			"workspace create",
			"workspace remove",
			"agent list",
			"agent capture",
			"agent run",
			"agent send",
			"agent stop",
			"agent watch",
			"agent job status",
			"agent job cancel",
			"agent job wait",
			"terminal list",
			"terminal run",
			"terminal logs",
			"project list",
			"project add",
			"project remove",
			"session list",
			"session prune",
			"version",
			"help",
		},
		Mutating: []string{
			"workspace create",
			"workspace remove",
			"agent run",
			"agent send",
			"agent stop",
			"agent job cancel",
			"terminal run",
			"project add",
			"project remove",
			"session prune",
		},
		GlobalFlags: []string{
			"--json",
			"--request-id",
			"--cwd",
			"--timeout",
			"--quiet",
			"--no-color",
		},
		Features: capabilityFeatures{
			JSONEnvelope:           true,
			RequestID:              true,
			AgentID:                true,
			IdempotencyKey:         true,
			SendJobs:               true,
			AsyncSend:              true,
			JobWait:                true,
			AgentWait:              true,
			WaitResponseStatus:     true,
			WaitResponseDelta:      true,
			WaitResponseEarlyInput: true,
			WaitResponseNeedsInput: true,
			WaitResponseSummary:    true,
			WatchHeartbeat:         true,
			WatchNeedsInput:        true,
		},
	}

	if gf.JSON {
		PrintJSON(w, result, version)
		return ExitOK
	}

	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "schema: %s\n", result.SchemaVersion)
		fmt.Fprintln(w, "commands:")
		for _, cmd := range result.Commands {
			fmt.Fprintf(w, "  - %s\n", cmd)
		}
		fmt.Fprintln(w, "mutating:")
		for _, cmd := range result.Mutating {
			fmt.Fprintf(w, "  - %s\n", cmd)
		}
	})
	return ExitOK
}
