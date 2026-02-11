package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCmdStatusJSON(t *testing.T) {
	var w, wErr bytes.Buffer
	gf := GlobalFlags{JSON: true}
	code := cmdStatus(&w, &wErr, gf, nil, "test-v1")

	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode JSON: %v\nraw: %s", err, w.String())
	}
	if !env.OK {
		t.Error("expected ok=true")
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be an object, got %T", env.Data)
	}
	if _, exists := data["version"]; !exists {
		t.Error("expected 'version' in data")
	}
	if _, exists := data["tmux_available"]; !exists {
		t.Error("expected 'tmux_available' in data")
	}
}

func TestCmdStatusHuman(t *testing.T) {
	var w, wErr bytes.Buffer
	gf := GlobalFlags{JSON: false}
	code := cmdStatus(&w, &wErr, gf, nil, "test-v1")

	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, wErr.String())
	}

	output := w.String()
	if output == "" {
		t.Error("expected non-empty human output")
	}
}

func TestCmdStatusUnexpectedArgsReturnsUsageError(t *testing.T) {
	var w bytes.Buffer
	var wErr bytes.Buffer
	code := cmdStatus(&w, &wErr, GlobalFlags{JSON: true}, []string{"garbage"}, "test-v1")
	if code != ExitUsage {
		t.Fatalf("cmdStatus() code = %d, want %d", code, ExitUsage)
	}
	if wErr.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "unexpected arguments") {
		t.Fatalf("unexpected usage_error message: %#v", env.Error)
	}
}
