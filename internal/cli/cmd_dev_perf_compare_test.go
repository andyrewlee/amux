package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdDevPerfCompareJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	baselineFile := filepath.Join(dir, "perf.env")
	if err := os.WriteFile(baselineFile, []byte(strings.Join([]string{
		"DARWIN_ARM64_CENTER_P95_MS=999",
		"DARWIN_ARM64_SIDEBAR_P95_MS=999",
		"DARWIN_ARM64_MONITOR_P95_MS=999",
		"LINUX_AMD64_CENTER_P95_MS=999",
		"LINUX_AMD64_SIDEBAR_P95_MS=999",
		"LINUX_AMD64_MONITOR_P95_MS=999",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("write baseline file: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdDevPerfCompare(&out, &errOut, GlobalFlags{JSON: true}, []string{
		"--baseline-file", baselineFile,
		"--frames", "1",
		"--scrollback-frames", "1",
		"--warmup", "0",
		"--width", "40",
		"--height", "20",
	}, "test")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr=%s\nstdout=%s", code, ExitOK, errOut.String(), out.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\nraw=%s", err, out.String())
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got false: %s", out.String())
	}
}

func TestCmdDevPerfCompareUsageError(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdDevPerfCompare(&out, &errOut, GlobalFlags{JSON: true}, []string{
		"--tolerance", "nope",
	}, "test")
	if code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
}
