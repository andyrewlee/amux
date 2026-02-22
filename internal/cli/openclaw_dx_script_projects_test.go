package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawDXScriptDoesNotUseBash4AssociativeArrays(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	body, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	if strings.Contains(string(body), "declare -A") {
		t.Fatalf("openclaw-dx.sh should avoid Bash 4 associative arrays for macOS Bash 3 compatibility")
	}
}

func TestOpenClawDXProjectAdd_CreatesWorkspace(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project add")
    printf '%s' '{"ok":true,"data":{"name":"demo","path":"/tmp/demo"},"error":null}'
    ;;
  "workspace create")
    printf '%s' '{"ok":true,"data":{"id":"ws-mobile","name":"mobile","repo":"/tmp/demo","root":"/tmp/ws-mobile","assistant":"codex"},"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"project", "add",
		"--path", "/tmp/demo",
		"--workspace", "mobile",
		"--assistant", "codex",
	)

	if got, _ := payload["command"].(string); got != "project.add" {
		t.Fatalf("command = %q, want %q", got, "project.add")
	}
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	workspace, ok := data["workspace"].(map[string]any)
	if !ok {
		t.Fatalf("workspace missing or wrong type: %T", data["workspace"])
	}
	if got, _ := workspace["id"].(string); got != "ws-mobile" {
		t.Fatalf("workspace.id = %q, want %q", got, "ws-mobile")
	}
	if got, _ := data["workspace_label"].(string); got != "ws-mobile (mobile) [project workspace]" {
		t.Fatalf("workspace_label = %q, want %q", got, "ws-mobile (mobile) [project workspace]")
	}
	channel, ok := payload["channel"].(map[string]any)
	if !ok {
		t.Fatalf("channel missing or wrong type: %T", payload["channel"])
	}
	message, _ := channel["message"].(string)
	if !strings.Contains(message, "Workspace: ws-mobile (mobile) [project workspace]") {
		t.Fatalf("channel.message = %q, want workspace label", message)
	}
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}
}

func TestOpenClawDXProjectAdd_WorkspaceConflictRecoversExistingWorkspace(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project add")
    printf '%s' '{"ok":true,"data":{"name":"demo","path":"/tmp/demo"},"error":null}'
    ;;
  "workspace create")
    printf '%s' '{"ok":false,"error":{"code":"create_failed","message":"worktree add with new branch failed: fatal: '\''/tmp/amux/demo/mobile'\'' already exists; fallback add existing branch failed: fatal: '\''/tmp/amux/demo/mobile'\'' already exists"}}'
    ;;
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-mobile","name":"mobile","repo":"/tmp/demo","root":"/tmp/amux/demo/mobile","assistant":"codex","scope":"project"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"project", "add",
		"--path", "/tmp/demo",
		"--workspace", "mobile",
		"--assistant", "codex",
	)

	if got, _ := payload["command"].(string); got != "project.add" {
		t.Fatalf("command = %q, want %q", got, "project.add")
	}
	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "Workspace already exists: ws-mobile") {
		t.Fatalf("summary = %q, want existing workspace guidance", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "start --workspace ws-mobile") {
		t.Fatalf("suggested_command = %q, want start existing workspace", suggested)
	}
}

func TestOpenClawDXProjectAdd_WorkspaceConflictWithoutExistingWorkspaceSuggestsNewName(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project add")
    printf '%s' '{"ok":true,"data":{"name":"demo","path":"/tmp/demo"},"error":null}'
    ;;
  "workspace create")
    printf '%s' '{"ok":false,"error":{"code":"create_failed","message":"worktree add with new branch failed: fatal: '\''/tmp/amux/demo/mobile'\'' already exists; fallback add existing branch failed: fatal: '\''/tmp/amux/demo/mobile'\'' already exists"}}'
    ;;
  "workspace list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"project", "add",
		"--path", "/tmp/demo",
		"--workspace", "mobile",
		"--assistant", "codex",
	)

	if got, _ := payload["command"].(string); got != "project.add" {
		t.Fatalf("command = %q, want %q", got, "project.add")
	}
	if got, _ := payload["status"].(string); got != "attention" {
		t.Fatalf("status = %q, want %q", got, "attention")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "workspace name is unavailable: mobile") {
		t.Fatalf("summary = %q, want workspace-name-conflict guidance", summary)
	}
	suggested, _ := payload["suggested_command"].(string)
	if !strings.Contains(suggested, "workspace create --name mobile-2 --project /tmp/demo") {
		t.Fatalf("suggested_command = %q, want create-with-new-name guidance", suggested)
	}
}

func TestOpenClawDXProjectAdd_WorkspaceConflictAutoRetriesWithAlternateName(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project add")
    printf '%s' '{"ok":true,"data":{"name":"demo","path":"/tmp/demo"},"error":null}'
    ;;
  "workspace create")
    if [[ "${3:-}" == "mobile" ]]; then
      printf '%s' '{"ok":false,"error":{"code":"create_failed","message":"worktree add with new branch failed: fatal: '\''/tmp/amux/demo/mobile'\'' already exists"}}'
      exit 0
    fi
    printf '%s' '{"ok":true,"data":{"id":"ws-mobile-2","name":"mobile-2","repo":"/tmp/demo","root":"/tmp/amux/demo/mobile-2","assistant":"codex"},"error":null}'
    ;;
  "workspace list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"project", "add",
		"--path", "/tmp/demo",
		"--workspace", "mobile",
		"--assistant", "codex",
	)

	if got, _ := payload["command"].(string); got != "project.add" {
		t.Fatalf("command = %q, want %q", got, "project.add")
	}
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	summary, _ := payload["summary"].(string)
	if !strings.Contains(summary, "fallback name") {
		t.Fatalf("summary = %q, want fallback-name wording", summary)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["workspace_name_requested"].(string); got != "mobile" {
		t.Fatalf("workspace_name_requested = %q, want %q", got, "mobile")
	}
	if got, _ := data["workspace_name_used"].(string); got != "mobile-2" {
		t.Fatalf("workspace_name_used = %q, want %q", got, "mobile-2")
	}
}

func TestOpenClawDXProjectAdd_InferPathFromGitRoot(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")
	requireBinary(t, "git")

	scriptPath, err := filepath.Abs(filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh"))
	if err != nil {
		t.Fatalf("resolve script path: %v", err)
	}
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	projectPathFile := filepath.Join(fakeBinDir, "project-path.txt")

	repoDir := t.TempDir()
	if out, err := exec.Command("git", "-C", repoDir, "init", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, string(out))
	}
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project add")
    project_path="${3:-}"
    printf '%s' "$project_path" > "${PROJECT_PATH_FILE:?missing PROJECT_PATH_FILE}"
    printf '{"ok":true,"data":{"name":"demo","path":"%s"},"error":null}' "$project_path"
    ;;
  "workspace create")
    printf '%s' '{"ok":true,"data":{"id":"ws-mobile","name":"mobile","repo":"/tmp/demo","root":"/tmp/ws-mobile","assistant":"codex"},"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "PROJECT_PATH_FILE", projectPathFile)

	payload := runScriptJSONInDir(t, scriptPath, repoDir, env,
		"project", "add",
		"--workspace", "mobile",
		"--assistant", "codex",
	)

	if got, _ := payload["command"].(string); got != "project.add" {
		t.Fatalf("command = %q, want %q", got, "project.add")
	}
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	calledProjectPathRaw, err := os.ReadFile(projectPathFile)
	if err != nil {
		t.Fatalf("read project path file: %v", err)
	}
	wantRepoDir := repoDir
	if resolvedRepoDir, err := filepath.EvalSymlinks(repoDir); err == nil && resolvedRepoDir != "" {
		wantRepoDir = resolvedRepoDir
	}
	if got := strings.TrimSpace(string(calledProjectPathRaw)); got != wantRepoDir {
		t.Fatalf("project add path = %q, want %q", got, wantRepoDir)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["path_source"].(string); got != "cwd_or_git_root" {
		t.Fatalf("path_source = %q, want %q", got, "cwd_or_git_root")
	}
}

func TestOpenClawDXProjectList_QueryFiltersProjects(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project list")
    printf '%s' '{"ok":true,"data":[{"name":"api","path":"/tmp/api"},{"name":"mobile","path":"/tmp/mobile"},{"name":"web","path":"/tmp/web"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))

	payload := runScriptJSON(t, scriptPath, env,
		"project", "list",
		"--query", "api",
	)

	if got, _ := payload["command"].(string); got != "project.list" {
		t.Fatalf("command = %q, want %q", got, "project.list")
	}
	if got, _ := payload["status"].(string); got != "ok" {
		t.Fatalf("status = %q, want %q", got, "ok")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["query"].(string); got != "api" {
		t.Fatalf("query = %q, want %q", got, "api")
	}
	if got, _ := data["count"].(float64); got != 1 {
		t.Fatalf("count = %v, want 1", got)
	}
	projects, ok := data["projects"].([]any)
	if !ok || len(projects) != 1 {
		t.Fatalf("projects = %#v, want len=1", data["projects"])
	}
	project, ok := projects[0].(map[string]any)
	if !ok {
		t.Fatalf("projects[0] wrong type: %T", projects[0])
	}
	if got, _ := project["name"].(string); got != "api" {
		t.Fatalf("project name = %q, want %q", got, "api")
	}
}
