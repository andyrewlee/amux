package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCmdWorkspaceListByRelativeRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() { _ = os.Chdir(originalWD) }()

	// Use the current repo directory so "." resolves to a real path.
	if err := os.Chdir(originalWD); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	var w, wErr bytes.Buffer
	gf := GlobalFlags{JSON: true}
	code := cmdWorkspaceList(&w, &wErr, gf, []string{"--repo", "."}, "test-v1")
	if code != ExitOK {
		t.Fatalf("expected ExitOK, got %d; stderr: %s", code, wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true")
	}
}

func TestCmdWorkspaceListJSON(t *testing.T) {
	var w, wErr bytes.Buffer
	gf := GlobalFlags{JSON: true}
	code := cmdWorkspaceList(&w, &wErr, gf, nil, "test-v1")

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

	// Data should be an array (possibly empty)
	if env.Data == nil {
		t.Fatal("expected data to be set")
	}
}

func TestCmdWorkspaceListHuman(t *testing.T) {
	var w, wErr bytes.Buffer
	gf := GlobalFlags{JSON: false}
	code := cmdWorkspaceList(&w, &wErr, gf, nil, "test-v1")

	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, wErr.String())
	}
}

func TestCmdWorkspaceListJSONReturnsInternalErrorOnCorruptMetadata(t *testing.T) {
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

	var w, wErr bytes.Buffer
	code := cmdWorkspaceList(&w, &wErr, GlobalFlags{JSON: true}, nil, "test-v1")
	if code != ExitInternalError {
		t.Fatalf("expected exit %d, got %d", ExitInternalError, code)
	}
	if wErr.Len() != 0 {
		t.Fatalf("expected empty stderr in JSON mode, got %q", wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode JSON: %v\nraw: %s", err, w.String())
	}
	if env.OK {
		t.Fatal("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "list_failed" {
		t.Fatalf("expected list_failed error, got %#v", env.Error)
	}
}
