package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdWorkspaceRemoveUsageJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdWorkspaceRemove(&out, &errOut, GlobalFlags{JSON: true}, nil, "test-v1")
	if code != ExitUsage {
		t.Fatalf("cmdWorkspaceRemove() code = %d, want %d", code, ExitUsage)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
}

func TestCmdWorkspaceRemoveRejectsInvalidWorkspaceID(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdWorkspaceRemove(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"../../../tmp", "--yes"},
		"test-v1",
	)
	if code != ExitUsage {
		t.Fatalf("cmdWorkspaceRemove() code = %d, want %d", code, ExitUsage)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
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

func TestCmdWorkspaceRemoveCorruptedMetadataReturnsInternalError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wsID := "0123456789abcdef"
	metaDir := filepath.Join(home, ".amux", "workspaces-metadata", wsID)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "workspace.json"), []byte("{invalid"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var out, errOut bytes.Buffer
	code := cmdWorkspaceRemove(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{wsID, "--yes"},
		"test-v1",
	)
	if code != ExitInternalError {
		t.Fatalf("cmdWorkspaceRemove() code = %d, want %d", code, ExitInternalError)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "metadata_load_failed" {
		t.Fatalf("expected metadata_load_failed, got %#v", env.Error)
	}
}
