package common

import (
	"os"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/logging"
)

// captureLogs points the package logger at a fresh temp file and returns a
// reader for whatever lands there. CopyToClipboardWithLog's only observable,
// deterministic behavior (independent of whether a real clipboard exists) is
// the log line it emits — or, on the empty-text guard, the log line it must
// NOT emit. Initialize reinstalls the package-level logger on every call, so
// each subtest gets an isolated log file.
//
// We exercise only the empty-text branch of CopyToClipboardWithLog here. Its
// non-empty branch and CopyToClipboard both shell out to pbcopy / the atotto
// clipboard library; those paths depend on a live system clipboard that is not
// reliably present in CI, and the package ships no fake/seam for the exec, so
// per the ticket guidance they are intentionally left uncovered rather than
// asserted against an environment-coupled side effect.
func captureLogs(t *testing.T) func() string {
	t.Helper()

	dir := t.TempDir()
	if err := logging.Initialize(dir, logging.LevelDebug); err != nil {
		t.Fatalf("logging.Initialize: %v", err)
	}
	t.Cleanup(func() { _ = logging.Close() })

	path := logging.GetLogPath()
	if path == "" {
		t.Fatalf("logging.GetLogPath returned empty path")
	}

	return func() string {
		t.Helper()
		// Writes are unbuffered (direct file Write), so no flush is required,
		// but Close is idempotent enough that reading first is safe.
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read log file: %v", err)
		}
		return string(data)
	}
}

// TestCopyToClipboardWithLog_EmptyTextIsSilentNoOp verifies the early-return
// guard: empty input must neither attempt a copy nor log anything. This is the
// one fully deterministic branch in the file — it never touches the clipboard.
func TestCopyToClipboardWithLog_EmptyTextIsSilentNoOp(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		label string
	}{
		{name: "empty text and empty label", text: "", label: ""},
		{name: "empty text with label", text: "", label: "selection"},
		{name: "empty text with multiword label", text: "", label: "tab output buffer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read := captureLogs(t)

			CopyToClipboardWithLog(tt.text, tt.label)

			if got := read(); got != "" {
				t.Errorf("empty-text copy wrote log output, want none; got %q", got)
			}
		})
	}
}

// TestCopyToClipboardWithLog_EmptyTextIgnoresLabel makes the no-op contract
// explicit: even a non-empty, attention-grabbing label must not cause the
// label to leak into the log when the text is empty.
func TestCopyToClipboardWithLog_EmptyTextIgnoresLabel(t *testing.T) {
	read := captureLogs(t)

	const label = "SHOULD-NOT-APPEAR"
	CopyToClipboardWithLog("", label)

	if got := read(); strings.Contains(got, label) {
		t.Errorf("label leaked into log on empty-text no-op: %q", got)
	}
}

// TestCopyToClipboardWithLog_EmptyTextNeverLogsAcrossLabels iterates a range of
// label boundary values (whitespace, very long, format-directive-like) to lock
// in that the empty-text guard short-circuits before any logging or copy work,
// regardless of what the caller passes as the label.
func TestCopyToClipboardWithLog_EmptyTextNeverLogsAcrossLabels(t *testing.T) {
	labels := []string{
		"",
		" ",
		"\t\n",
		"%s %d %v", // a printf-looking label must not be treated as a format string
		strings.Repeat("x", 4096),
	}

	for _, label := range labels {
		read := captureLogs(t)
		CopyToClipboardWithLog("", label)
		if got := read(); got != "" {
			t.Errorf("empty-text copy logged for label %q: %q", label, got)
		}
	}
}
