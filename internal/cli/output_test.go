package cli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestPrintJSON(t *testing.T) {
	var buf bytes.Buffer
	setResponseContext("req-1", "status")
	defer clearResponseContext()
	PrintJSON(&buf, map[string]string{"hello": "world"}, "test-v1")

	var env Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if !env.OK {
		t.Error("expected ok=true")
	}
	if env.Error != nil {
		t.Error("expected error=nil")
	}
	if env.Meta.AmuxVersion != "test-v1" {
		t.Errorf("expected version test-v1, got %s", env.Meta.AmuxVersion)
	}
	if env.Meta.GeneratedAt == "" {
		t.Error("expected generated_at to be set")
	}
	if env.SchemaVersion != EnvelopeSchemaVersion {
		t.Errorf("expected schema version %q, got %q", EnvelopeSchemaVersion, env.SchemaVersion)
	}
	if env.RequestID != "req-1" {
		t.Errorf("expected request_id req-1, got %q", env.RequestID)
	}
	if env.Command != "status" {
		t.Errorf("expected command status, got %q", env.Command)
	}
}

func TestReturnError(t *testing.T) {
	var buf bytes.Buffer
	ReturnError(&buf, "test_err", "something broke", nil, "test-v1")

	var env Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if env.OK {
		t.Error("expected ok=false")
	}
	if env.Error == nil {
		t.Fatal("expected error to be set")
	}
	if env.Error.Code != "test_err" {
		t.Errorf("expected code test_err, got %s", env.Error.Code)
	}
	if env.Error.Message != "something broke" {
		t.Errorf("expected message 'something broke', got %s", env.Error.Message)
	}
}

func TestReturnErrorWithDetails(t *testing.T) {
	var buf bytes.Buffer
	details := map[string]string{"field": "value"}
	ReturnError(&buf, "detail_err", "with details", details, "test-v1")

	var env Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if env.Error.Details == nil {
		t.Error("expected details to be set")
	}
}
