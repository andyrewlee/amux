package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runScriptJSON(t *testing.T, scriptPath string, env []string, args ...string) map[string]any {
	t.Helper()
	cmd := exec.Command(scriptPath, args...)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v failed: %v", scriptPath, args, err)
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
	cmd.Env = env
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v failed: %v", scriptPath, args, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("decode json: %v\nraw: %s", err, string(out))
	}
	return payload
}
