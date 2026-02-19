package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCmdAgentRunUsageJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdAgentRun(&out, &errOut, GlobalFlags{JSON: true}, nil, "test-v1")
	if code != ExitUsage {
		t.Fatalf("cmdAgentRun() code = %d, want %d", code, ExitUsage)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
}

func TestCmdAgentRunRejectsInvalidWorkspaceID(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdAgentRun(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--workspace", "../../../tmp/evil", "--assistant", "claude"},
		"test-v1",
	)
	if code != ExitUsage {
		t.Fatalf("cmdAgentRun() code = %d, want %d", code, ExitUsage)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "invalid workspace id") {
		t.Fatalf("expected invalid workspace id message, got %#v", env.Error)
	}
}

func TestCmdAgentRunRejectsUnexpectedPositionalArguments(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdAgentRun(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--workspace", "0123456789abcdef", "--assistant", "claude", "stray-token"},
		"test-v1",
	)
	if code != ExitUsage {
		t.Fatalf("cmdAgentRun() code = %d, want %d", code, ExitUsage)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "unexpected arguments") {
		t.Fatalf("expected unexpected arguments message, got %#v", env.Error)
	}
}

func TestCmdAgentRunWaitRequiresPrompt(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdAgentRun(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--workspace", "0123456789abcdef", "--assistant", "claude", "--wait"},
		"test-v1",
	)
	if code != ExitUsage {
		t.Fatalf("cmdAgentRun() code = %d, want %d", code, ExitUsage)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "--wait requires --prompt") {
		t.Fatalf("unexpected message: %#v", env.Error)
	}
}

func TestCmdAgentRunRejectsWaitTimeoutNonPositive(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdAgentRun(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--workspace", "0123456789abcdef", "--assistant", "claude", "--wait-timeout", "0s"},
		"test-v1",
	)
	if code != ExitUsage {
		t.Fatalf("cmdAgentRun() code = %d, want %d", code, ExitUsage)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "--wait-timeout must be > 0") {
		t.Fatalf("unexpected message: %#v", env.Error)
	}
}

func TestCmdAgentRunRejectsIdleThresholdNonPositive(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdAgentRun(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--workspace", "0123456789abcdef", "--assistant", "claude", "--idle-threshold", "0s"},
		"test-v1",
	)
	if code != ExitUsage {
		t.Fatalf("cmdAgentRun() code = %d, want %d", code, ExitUsage)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "--idle-threshold must be > 0") {
		t.Fatalf("unexpected message: %#v", env.Error)
	}
}
