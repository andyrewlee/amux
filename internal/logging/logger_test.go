package logging

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func setupLogger(t *testing.T, level Level) (string, func()) {
	t.Helper()

	logDir := t.TempDir()
	if err := Initialize(logDir, level); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	logPath := GetLogPath()
	if logPath == "" {
		t.Fatalf("GetLogPath returned empty path")
	}

	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			_ = Close()
			defaultLogger = nil
		})
	}
	t.Cleanup(cleanup)

	return logPath, cleanup
}

func TestInitializeAndLogWrites(t *testing.T) {
	logPath, cleanup := setupLogger(t, LevelInfo)
	defer cleanup()

	Info("hello %s", "world")
	cleanup()

	if dir := filepath.Dir(logPath); dir == "" {
		t.Fatalf("expected log path to have a directory")
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "INFO: hello world") {
		t.Fatalf("expected log line to contain message, got: %q", content)
	}
}

func TestSetEnabledDisablesLogging(t *testing.T) {
	logPath, cleanup := setupLogger(t, LevelDebug)
	defer cleanup()

	SetEnabled(false)
	Info("should not write")
	cleanup()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if len(strings.TrimSpace(string(data))) != 0 {
		t.Fatalf("expected no log output when disabled, got: %q", string(data))
	}
}

func TestLevelFiltering(t *testing.T) {
	logPath, cleanup := setupLogger(t, LevelWarn)
	defer cleanup()

	Info("info message")
	Warn("warn message")
	cleanup()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "INFO: info message") {
		t.Fatalf("did not expect info log at warn level: %q", content)
	}
	if !strings.Contains(content, "WARN: warn message") {
		t.Fatalf("expected warn log, got: %q", content)
	}
}
