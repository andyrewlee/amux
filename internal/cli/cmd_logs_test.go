package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdLogsTailRejectsNegativeLines(t *testing.T) {
	var w, wErr bytes.Buffer
	code := cmdLogs(&w, &wErr, GlobalFlags{}, []string{"tail", "--lines", "-1"}, "test-v1")

	if code != ExitUsage {
		t.Fatalf("expected ExitUsage for negative --lines, got %d", code)
	}
	if !strings.Contains(wErr.String(), "--lines must be >= 0") {
		t.Fatalf("expected validation error, got stderr: %q", wErr.String())
	}
}

func TestCmdLogsUsageJSON(t *testing.T) {
	var w, wErr bytes.Buffer
	code := cmdLogs(&w, &wErr, GlobalFlags{JSON: true}, []string{"unknown"}, "test-v1")
	if code != ExitUsage {
		t.Fatalf("expected ExitUsage, got %d", code)
	}
	if wErr.Len() != 0 {
		t.Fatalf("expected no stderr in JSON mode, got %q", wErr.String())
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
}

func TestCmdLogsTailEmptyFileReportsZeroLines(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	logDir := filepath.Join(home, ".amux", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	logFile := filepath.Join(logDir, "amux-2025-01-01.log")
	if err := os.WriteFile(logFile, []byte{}, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var w, wErr bytes.Buffer
	code := cmdLogs(&w, &wErr, GlobalFlags{JSON: true}, []string{"tail"}, "test-v1")
	if code != ExitOK {
		t.Fatalf("expected ExitOK, got %d; stderr: %s", code, wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", env.Data)
	}
	count, _ := data["count"].(float64)
	if count != 0 {
		t.Fatalf("expected count=0 for empty file, got %v", count)
	}
	lines, _ := data["lines"].([]any)
	if len(lines) != 0 {
		t.Fatalf("expected empty lines array, got %v", lines)
	}
}

func TestCmdLogsTailEmptyFileHumanNoOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	logDir := filepath.Join(home, ".amux", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	logFile := filepath.Join(logDir, "amux-2025-01-01.log")
	if err := os.WriteFile(logFile, []byte{}, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var w, wErr bytes.Buffer
	code := cmdLogs(&w, &wErr, GlobalFlags{JSON: false}, []string{"tail"}, "test-v1")
	if code != ExitOK {
		t.Fatalf("expected ExitOK, got %d; stderr: %s", code, wErr.String())
	}
	if w.Len() != 0 {
		t.Fatalf("expected no stdout for empty log, got %q", w.String())
	}
}
