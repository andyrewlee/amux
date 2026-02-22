package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var openClawDXContextByTest sync.Map

func envHasKey(env []string, key string) bool {
	for _, entry := range env {
		if strings.HasPrefix(entry, key+"=") {
			return true
		}
	}
	return false
}

func envLastValue(env []string, key string) (string, bool) {
	prefix := key + "="
	value := ""
	found := false
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			value = strings.TrimPrefix(entry, prefix)
			found = true
		}
	}
	return value, found
}

func envWithoutKey(env []string, key string) []string {
	prefix := key + "="
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func withIsolatedOpenClawDXContext(t *testing.T, env []string) []string {
	t.Helper()
	if envHasKey(env, "OPENCLAW_DX_CONTEXT_FILE") {
		lastValue, _ := envLastValue(env, "OPENCLAW_DX_CONTEXT_FILE")
		if lastValue != "" && lastValue != os.Getenv("OPENCLAW_DX_CONTEXT_FILE") {
			return env
		}
		env = envWithoutKey(env, "OPENCLAW_DX_CONTEXT_FILE")
	}
	if existing, ok := openClawDXContextByTest.Load(t.Name()); ok {
		if existingPath, ok := existing.(string); ok && existingPath != "" {
			return append(env, "OPENCLAW_DX_CONTEXT_FILE="+existingPath)
		}
	}
	path := filepath.Join(t.TempDir(), "openclaw-dx-context.json")
	openClawDXContextByTest.Store(t.Name(), path)
	t.Cleanup(func() {
		openClawDXContextByTest.Delete(t.Name())
	})
	return append(env, "OPENCLAW_DX_CONTEXT_FILE="+path)
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runScriptJSON(t *testing.T, scriptPath string, env []string, args ...string) map[string]any {
	t.Helper()
	cmd := exec.Command(scriptPath, args...)
	cmd.Env = withIsolatedOpenClawDXContext(t, env)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.Bytes()
	if err != nil {
		t.Fatalf("%s %v failed: %v\nstdout:\n%s\nstderr:\n%s", scriptPath, args, err, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}
	return payload
}

func runScriptJSONInDir(t *testing.T, scriptPath, dir string, env []string, args ...string) map[string]any {
	t.Helper()
	cmd := exec.Command(scriptPath, args...)
	cmd.Env = withIsolatedOpenClawDXContext(t, env)
	cmd.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.Bytes()
	if err != nil {
		t.Fatalf("%s %v failed: %v\nstdout:\n%s\nstderr:\n%s", scriptPath, args, err, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}
	return payload
}

func TestRunScriptJSON_AutoInjectsStableOpenClawDXContext(t *testing.T) {
	requireBinary(t, "bash")

	scriptPath := filepath.Join(t.TempDir(), "print-context.sh")
	writeExecutable(t, scriptPath, `#!/usr/bin/env bash
set -euo pipefail
printf '{"context":"%s"}' "${OPENCLAW_DX_CONTEXT_FILE:-}"
`)

	env := os.Environ()
	first := runScriptJSON(t, scriptPath, env)
	second := runScriptJSON(t, scriptPath, env)

	firstCtx, _ := first["context"].(string)
	secondCtx, _ := second["context"].(string)
	if firstCtx == "" {
		t.Fatalf("context path is empty")
	}
	if firstCtx != secondCtx {
		t.Fatalf("context path changed across calls: %q != %q", firstCtx, secondCtx)
	}
	if got := filepath.Base(firstCtx); got != "openclaw-dx-context.json" {
		t.Fatalf("context file basename = %q, want %q", got, "openclaw-dx-context.json")
	}
}

func TestRunScriptJSON_RespectsExplicitOpenClawDXContext(t *testing.T) {
	requireBinary(t, "bash")

	scriptPath := filepath.Join(t.TempDir(), "print-context.sh")
	writeExecutable(t, scriptPath, `#!/usr/bin/env bash
set -euo pipefail
printf '{"context":"%s"}' "${OPENCLAW_DX_CONTEXT_FILE:-}"
`)

	explicitPath := filepath.Join(t.TempDir(), "explicit-context.json")
	env := append(os.Environ(), "OPENCLAW_DX_CONTEXT_FILE="+explicitPath)

	payload := runScriptJSON(t, scriptPath, env)
	got, _ := payload["context"].(string)
	if got != explicitPath {
		t.Fatalf("context path = %q, want %q", got, explicitPath)
	}
}

func TestRunScriptJSON_ReplacesAmbientOpenClawDXContext(t *testing.T) {
	requireBinary(t, "bash")

	scriptPath := filepath.Join(t.TempDir(), "print-context.sh")
	writeExecutable(t, scriptPath, `#!/usr/bin/env bash
set -euo pipefail
printf '{"context":"%s"}' "${OPENCLAW_DX_CONTEXT_FILE:-}"
`)

	ambientPath := filepath.Join(t.TempDir(), "ambient-context.json")
	t.Setenv("OPENCLAW_DX_CONTEXT_FILE", ambientPath)
	env := os.Environ()

	payload := runScriptJSON(t, scriptPath, env)
	got, _ := payload["context"].(string)
	if got == "" {
		t.Fatalf("context path is empty")
	}
	if got == ambientPath {
		t.Fatalf("context path = %q, want isolated per-test context path", got)
	}
}

func TestRunScriptJSON_ExplicitContextWinsOverAmbientContext(t *testing.T) {
	requireBinary(t, "bash")

	scriptPath := filepath.Join(t.TempDir(), "print-context.sh")
	writeExecutable(t, scriptPath, `#!/usr/bin/env bash
set -euo pipefail
printf '{"context":"%s"}' "${OPENCLAW_DX_CONTEXT_FILE:-}"
`)

	ambientPath := filepath.Join(t.TempDir(), "ambient-context.json")
	explicitPath := filepath.Join(t.TempDir(), "explicit-context.json")
	t.Setenv("OPENCLAW_DX_CONTEXT_FILE", ambientPath)
	env := append(os.Environ(), "OPENCLAW_DX_CONTEXT_FILE="+explicitPath)

	payload := runScriptJSON(t, scriptPath, env)
	got, _ := payload["context"].(string)
	if got != explicitPath {
		t.Fatalf("context path = %q, want explicit override %q", got, explicitPath)
	}
}

func TestRunScriptJSONInDir_AutoInjectsStableOpenClawDXContext(t *testing.T) {
	requireBinary(t, "bash")

	workDir := t.TempDir()
	scriptPath := filepath.Join(t.TempDir(), "print-context.sh")
	writeExecutable(t, scriptPath, `#!/usr/bin/env bash
set -euo pipefail
printf '{"cwd":"%s","context":"%s"}' "$(pwd)" "${OPENCLAW_DX_CONTEXT_FILE:-}"
`)

	env := os.Environ()
	first := runScriptJSONInDir(t, scriptPath, workDir, env)
	second := runScriptJSONInDir(t, scriptPath, workDir, env)

	firstCtx, _ := first["context"].(string)
	secondCtx, _ := second["context"].(string)
	if firstCtx == "" {
		t.Fatalf("context path is empty")
	}
	if firstCtx != secondCtx {
		t.Fatalf("context path changed across calls: %q != %q", firstCtx, secondCtx)
	}
	cwd, _ := first["cwd"].(string)
	canonicalCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		canonicalCWD = cwd
	}
	canonicalWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		canonicalWorkDir = workDir
	}
	if canonicalCWD != canonicalWorkDir {
		t.Fatalf("cwd = %q (canonical %q), want %q (canonical %q)", cwd, canonicalCWD, workDir, canonicalWorkDir)
	}
}
