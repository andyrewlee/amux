package e2e

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type cliEnvelope struct {
	OK    bool      `json:"ok"`
	Data  any       `json:"data"`
	Error *cliError `json:"error"`
}

type cliError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func TestCLIAgentSendAsyncQueueOrdering(t *testing.T) {
	skipIfNoTmux(t)

	home := t.TempDir()
	server := fmt.Sprintf("amux-e2e-cli-%d", time.Now().UnixNano())
	defer killTmuxServer(t, server)

	sessionName := "amux-e2e-order"
	logPath := filepath.Join(t.TempDir(), "ordered.log")
	createTmuxSessionWithCommand(t, server, sessionName, "cat >> "+shellQuote(logPath))

	_, first, _, _ := runAmuxJSON(t, home, server,
		"agent", "send", sessionName, "--text", "first", "--enter", "--async",
	)
	jobID1 := jsonStringField(t, first.Data, "job_id")

	time.Sleep(200 * time.Millisecond)

	_, second, _, _ := runAmuxJSON(t, home, server,
		"agent", "send", sessionName, "--text", "second", "--enter", "--async",
	)
	jobID2 := jsonStringField(t, second.Data, "job_id")

	code1, done1, _, _ := runAmuxJSON(t, home, server,
		"agent", "job", "wait", jobID1, "--timeout", "8s", "--interval", "50ms",
	)
	if code1 != 0 {
		t.Fatalf("wait job1 exit code = %d, want 0 (env=%+v)", code1, done1)
	}
	if got := jsonStringField(t, done1.Data, "status"); got != "completed" {
		t.Fatalf("job1 status = %q, want completed", got)
	}

	code2, done2, _, _ := runAmuxJSON(t, home, server,
		"agent", "job", "wait", jobID2, "--timeout", "8s", "--interval", "50ms",
	)
	if code2 != 0 {
		t.Fatalf("wait job2 exit code = %d, want 0 (env=%+v)", code2, done2)
	}
	if got := jsonStringField(t, done2.Data, "status"); got != "completed" {
		t.Fatalf("job2 status = %q, want completed", got)
	}

	lines := waitForLogLines(t, logPath, 2, 8*time.Second)
	if lines[0] != "first" || lines[1] != "second" {
		t.Fatalf("unexpected send order: %v", lines)
	}
}

func TestCLIAgentSendCancelRacePendingJob(t *testing.T) {
	skipIfNoTmux(t)

	home := t.TempDir()
	server := fmt.Sprintf("amux-e2e-cli-%d", time.Now().UnixNano())
	defer killTmuxServer(t, server)

	sessionName := "amux-e2e-cancel-race"
	logPath := filepath.Join(t.TempDir(), "cancel.log")
	createTmuxSessionWithCommand(t, server, sessionName, "cat >> "+shellQuote(logPath))

	lockFile := lockSendQueueFile(t, home, sessionName)
	defer func() {
		unlockSendQueueFile(t, lockFile)
	}()

	_, sendEnv, _, _ := runAmuxJSON(t, home, server,
		"agent", "send", sessionName, "--text", "blocked", "--enter", "--async",
	)
	jobID := jsonStringField(t, sendEnv.Data, "job_id")
	if status := jsonStringField(t, sendEnv.Data, "status"); status != "pending" {
		t.Fatalf("async send status = %q, want pending", status)
	}

	_, cancelEnv, _, _ := runAmuxJSON(t, home, server,
		"agent", "job", "cancel", jobID,
	)
	if canceled := jsonBoolField(t, cancelEnv.Data, "canceled"); !canceled {
		t.Fatalf("expected canceled=true, got false")
	}

	unlockSendQueueFile(t, lockFile)
	lockFile = nil

	code, waitEnv, _, _ := runAmuxJSON(t, home, server,
		"agent", "job", "wait", jobID, "--timeout", "8s", "--interval", "50ms",
	)
	if code != 0 {
		t.Fatalf("wait job exit code = %d, want 0 (env=%+v)", code, waitEnv)
	}
	if got := jsonStringField(t, waitEnv.Data, "status"); got != "canceled" {
		t.Fatalf("wait status = %q, want canceled", got)
	}

	time.Sleep(200 * time.Millisecond)
	content, _ := os.ReadFile(logPath)
	if strings.Contains(string(content), "blocked") {
		t.Fatalf("unexpected delivered text after cancel race: %q", string(content))
	}
}

func TestCLIAgentSendIdempotentErrorReplay(t *testing.T) {
	skipIfNoTmux(t)

	home := t.TempDir()
	server := fmt.Sprintf("amux-e2e-cli-%d", time.Now().UnixNano())
	defer killTmuxServer(t, server)

	sessionName := "amux-e2e-replay"
	logPath := filepath.Join(t.TempDir(), "replay.log")
	idemKey := "e2e-idem-send-not-found"

	firstCode, firstEnv, firstOut, _ := runAmuxJSON(t, home, server,
		"agent", "send", sessionName, "--text", "hello", "--enter", "--idempotency-key", idemKey,
	)
	if firstCode == 0 {
		t.Fatalf("expected non-zero exit for first missing-session send")
	}
	if firstEnv.Error == nil || firstEnv.Error.Code != "not_found" {
		t.Fatalf("expected not_found error, got %+v", firstEnv.Error)
	}

	createTmuxSessionWithCommand(t, server, sessionName, "cat >> "+shellQuote(logPath))

	secondCode, secondEnv, secondOut, _ := runAmuxJSON(t, home, server,
		"agent", "send", sessionName, "--text", "hello", "--enter", "--idempotency-key", idemKey,
	)
	if secondCode != firstCode {
		t.Fatalf("replay exit code = %d, want %d", secondCode, firstCode)
	}
	if secondEnv.Error == nil || secondEnv.Error.Code != "not_found" {
		t.Fatalf("expected replayed not_found error, got %+v", secondEnv.Error)
	}
	if secondOut != firstOut {
		t.Fatalf("expected exact replayed envelope\nfirst:\n%s\nsecond:\n%s", firstOut, secondOut)
	}

	time.Sleep(250 * time.Millisecond)
	content, _ := os.ReadFile(logPath)
	if strings.TrimSpace(string(content)) != "" {
		t.Fatalf("replayed error should not deliver text, got log: %q", string(content))
	}
}

func TestCLIAgentStopGracefulFallbackKillsIgnoredInterrupt(t *testing.T) {
	skipIfNoTmux(t)

	home := t.TempDir()
	server := fmt.Sprintf("amux-e2e-cli-%d", time.Now().UnixNano())
	defer killTmuxServer(t, server)

	sessionName := "amux-e2e-stop-fallback"
	createTmuxSessionWithCommand(
		t,
		server,
		sessionName,
		"trap '' INT; while :; do sleep 1; done",
	)

	code, env, _, _ := runAmuxJSON(t, home, server,
		"agent", "stop", sessionName, "--grace-period", "150ms",
	)
	if code != 0 {
		t.Fatalf("agent stop exit code = %d, want 0 (env=%+v)", code, env)
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got error=%+v", env.Error)
	}
	waitForSessionGone(t, server, sessionName, 5*time.Second)
}

func runAmuxJSON(t *testing.T, home, server string, args ...string) (int, cliEnvelope, string, string) {
	t.Helper()
	fullArgs := append([]string{"--json"}, args...)
	code, out, errOut := runAmux(t, home, server, fullArgs...)
	var env cliEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("decode json envelope: %v\nstdout:\n%s\nstderr:\n%s", err, out, errOut)
	}
	return code, env, out, errOut
}

func runAmux(t *testing.T, home, server string, args ...string) (int, string, string) {
	t.Helper()

	bin, cleanup, err := buildAmuxBinary()
	if err != nil {
		t.Fatalf("build amux binary: %v", err)
	}
	defer cleanup()

	cmd := exec.Command(bin, args...)
	cmd.Env = append(stripGitEnv(os.Environ()),
		"HOME="+home,
		"AMUX_TMUX_SERVER="+server,
		"AMUX_TMUX_CONFIG=/dev/null",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("run amux %v: %v", args, err)
		}
	}
	return exitCode, stdout.String(), stderr.String()
}

func createTmuxSessionWithCommand(t *testing.T, server, sessionName, command string) {
	t.Helper()
	cmd := exec.Command(
		"tmux", "-L", server, "-f", "/dev/null",
		"new-session", "-d", "-s", sessionName, "sh", "-lc", command,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("create tmux session %s: %v\n%s", sessionName, err, string(out))
	}
}

func waitForLogLines(t *testing.T, path string, count int, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(path)
		if err == nil {
			raw := strings.Split(strings.TrimSpace(string(content)), "\n")
			var lines []string
			for _, line := range raw {
				line = strings.TrimSpace(line)
				if line != "" {
					lines = append(lines, line)
				}
			}
			if len(lines) >= count {
				return lines
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %d lines in %s", count, path)
	return nil
}

func waitForSessionGone(t *testing.T, server, sessionName string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !tmuxSessionExists(server, sessionName) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("session %s still exists after %s", sessionName, timeout)
}

func tmuxSessionExists(server, sessionName string) bool {
	cmd := exec.Command("tmux", "-L", server, "-f", "/dev/null", "has-session", "-t", "="+sessionName)
	return cmd.Run() == nil
}

func lockSendQueueFile(t *testing.T, home, sessionName string) *os.File {
	t.Helper()
	sum := sha1.Sum([]byte(sessionName))
	lockPath := filepath.Join(home, ".amux", "cli-send-queue-"+hex.EncodeToString(sum[:8])+".lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("open lock file: %v", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		t.Fatalf("flock lock file: %v", err)
	}
	return file
}

func unlockSendQueueFile(t *testing.T, file *os.File) {
	t.Helper()
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}

func jsonStringField(t *testing.T, data any, key string) string {
	t.Helper()
	obj, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("expected object payload, got %T", data)
	}
	value, _ := obj[key].(string)
	return value
}

func jsonBoolField(t *testing.T, data any, key string) bool {
	t.Helper()
	obj, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("expected object payload, got %T", data)
	}
	value, _ := obj[key].(bool)
	return value
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
