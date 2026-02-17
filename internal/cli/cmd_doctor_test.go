package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCmdDoctorUnexpectedArgsReturnsUsageError(t *testing.T) {
	var w bytes.Buffer
	var wErr bytes.Buffer
	code := cmdDoctor(&w, &wErr, GlobalFlags{JSON: true}, []string{"garbage"}, "test-v1")
	if code != ExitUsage {
		t.Fatalf("cmdDoctor() code = %d, want %d", code, ExitUsage)
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
