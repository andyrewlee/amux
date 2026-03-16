package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCmdCtxErrResultUsesHumanOverrideInTextMode(t *testing.T) {
	var out, errOut bytes.Buffer
	ctx := &cmdCtx{
		w:       &out,
		wErr:    &errOut,
		gf:      GlobalFlags{},
		version: "test-v1",
		cmd:     "test.command",
	}

	code := ctx.errResult(ExitInternalError, "boom", "json message", nil, "human message")
	if code != ExitInternalError {
		t.Fatalf("errResult() code = %d, want %d", code, ExitInternalError)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no stdout output in text mode, got %q", out.String())
	}
	if got := errOut.String(); got != "Error: human message\n" {
		t.Fatalf("stderr = %q, want %q", got, "Error: human message\n")
	}
}

func TestCmdCtxErrResultKeepsJSONMessageWhenHumanOverrideProvided(t *testing.T) {
	var out, errOut bytes.Buffer
	ctx := &cmdCtx{
		w:       &out,
		wErr:    &errOut,
		gf:      GlobalFlags{JSON: true},
		version: "test-v1",
		cmd:     "test.command",
	}

	code := ctx.errResult(ExitInternalError, "boom", "json message", nil, "human message")
	if code != ExitInternalError {
		t.Fatalf("errResult() code = %d, want %d", code, ExitInternalError)
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
	if env.Error == nil || env.Error.Message != "json message" {
		t.Fatalf("expected JSON message to be preserved, got %#v", env.Error)
	}
}
