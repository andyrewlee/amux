package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXGitShip_NoChangesButAheadSuggestsPush(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")

	repoDir := t.TempDir()
	if out, err := exec.Command("git", "-C", repoDir, "init", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "config", "user.email", "dx@example.com").CombinedOutput(); err != nil {
		t.Fatalf("git config email: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "config", "user.name", "DX Bot").CombinedOutput(); err != nil {
		t.Fatalf("git config name: %v\n%s", err, string(out))
	}

	remoteDir := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "--bare", remoteDir).CombinedOutput(); err != nil {
		t.Fatalf("git init bare: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "remote", "add", "origin", remoteDir).CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, string(out))
	}

	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if out, err := exec.Command("git", "-C", repoDir, "add", "README.md").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "commit", "-m", "initial").CombinedOutput(); err != nil {
		t.Fatalf("git commit initial: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "push", "-u", "origin", "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("git push initial: %v\n%s", err, string(out))
	}

	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("modify README: %v", err)
	}
	if out, err := exec.Command("git", "-C", repoDir, "add", "README.md").CombinedOutput(); err != nil {
		t.Fatalf("git add second: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "commit", "-m", "second").CombinedOutput(); err != nil {
		t.Fatalf("git commit second: %v\n%s", err, string(out))
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' "${FAKE_WORKSPACE_LIST_JSON:?missing FAKE_WORKSPACE_LIST_JSON}"
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	workspaceListJSON := `{"ok":true,"data":[{"id":"ws-1","name":"demo","repo":"` + repoDir + `","root":"` + repoDir + `"}],"error":null}`
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_WORKSPACE_LIST_JSON", workspaceListJSON)

	payload := runScriptJSON(t, scriptPath, env,
		"git", "ship",
		"--workspace", "ws-1",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "ready to push") {
		t.Fatalf("summary = %q, want push-ready guidance", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "git ship --workspace ws-1 --push") {
		t.Fatalf("suggested_command = %q, want push command", suggested)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawPush bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		if id == "push" {
			sawPush = true
			break
		}
	}
	if !sawPush {
		t.Fatalf("expected push quick action in %#v", quickActions)
	}
}

func TestOpenClawDXGitShip_PushWithoutOriginSuggestsRemoteInspection(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")

	repoDir := t.TempDir()
	if out, err := exec.Command("git", "-C", repoDir, "init", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "config", "user.email", "dx@example.com").CombinedOutput(); err != nil {
		t.Fatalf("git config email: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "config", "user.name", "DX Bot").CombinedOutput(); err != nil {
		t.Fatalf("git config name: %v\n%s", err, string(out))
	}
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if out, err := exec.Command("git", "-C", repoDir, "add", "README.md").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, string(out))
	}
	if out, err := exec.Command("git", "-C", repoDir, "commit", "-m", "initial").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, string(out))
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' "${FAKE_WORKSPACE_LIST_JSON:?missing FAKE_WORKSPACE_LIST_JSON}"
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	workspaceListJSON := `{"ok":true,"data":[{"id":"ws-1","name":"demo","repo":"` + repoDir + `","root":"` + repoDir + `"}],"error":null}`
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "FAKE_WORKSPACE_LIST_JSON", workspaceListJSON)

	payload := runScriptJSON(t, scriptPath, env,
		"git", "ship",
		"--workspace", "ws-1",
		"--push",
	)

	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	nextAction, _ := payload["next_action"].(string)
	if !strings.Contains(nextAction, "Configure an origin remote") {
		t.Fatalf("next_action = %q, want origin-remote guidance", nextAction)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "terminal run --workspace ws-1") || !strings.Contains(suggested, "git remote -v") {
		t.Fatalf("suggested_command = %q, want remote inspection command", suggested)
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
	var sawShowRemote bool
	var sawRetryPush bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		switch id {
		case "show_remote":
			sawShowRemote = true
		case "retry_push":
			sawRetryPush = true
		}
	}
	if !sawShowRemote || !sawRetryPush {
		t.Fatalf("expected show_remote and retry_push actions in %#v", quickActions)
	}
}
