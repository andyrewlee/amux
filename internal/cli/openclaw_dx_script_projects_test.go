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
	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
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

func TestOpenClawDXProjectList_PaginatesAndAddsNavigationActions(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"name":"app-1","path":"/tmp/app-1"},{"name":"app-2","path":"/tmp/app-2"},{"name":"app-3","path":"/tmp/app-3"},{"name":"app-4","path":"/tmp/app-4"},{"name":"app-5","path":"/tmp/app-5"}],"error":null}'
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
		"--limit", "2",
		"--page", "2",
	)

	if got, _ := payload["command"].(string); got != "project.list" {
		t.Fatalf("command = %q, want %q", got, "project.list")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["page"].(float64); got != 2 {
		t.Fatalf("page = %v, want 2", got)
	}
	if got, _ := data["total_pages"].(float64); got != 3 {
		t.Fatalf("total_pages = %v, want 3", got)
	}
	if got, _ := data["has_prev"].(bool); !got {
		t.Fatalf("has_prev = %v, want true", got)
	}
	if got, _ := data["has_next"].(bool); !got {
		t.Fatalf("has_next = %v, want true", got)
	}
	projectsPage, ok := data["projects_page"].([]any)
	if !ok || len(projectsPage) != 2 {
		t.Fatalf("projects_page = %#v, want len=2", data["projects_page"])
	}
	firstPageProject, ok := projectsPage[0].(map[string]any)
	if !ok {
		t.Fatalf("projects_page[0] wrong type: %T", projectsPage[0])
	}
	if got, _ := firstPageProject["name"].(string); got != "app-3" {
		t.Fatalf("projects_page[0].name = %q, want app-3", got)
	}

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok {
		t.Fatalf("quick_actions missing or wrong type: %T", payload["quick_actions"])
	}
	var sawPrev bool
	var sawNext bool
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := action["id"].(string)
		command, _ := action["command"].(string)
		switch id {
		case "prev_page":
			sawPrev = strings.Contains(command, "--page 1")
		case "next_page":
			sawNext = strings.Contains(command, "--page 3")
		}
	}
	if !sawPrev {
		t.Fatalf("expected prev_page quick action targeting page 1: %#v", quickActions)
	}
	if !sawNext {
		t.Fatalf("expected next_page quick action targeting page 3: %#v", quickActions)
	}
}

func TestOpenClawDXProjectList_QuickActionCallbackDataIsOpenClawSafe(t *testing.T) {
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
    printf '%s' '{"ok":true,"data":[{"name":"demo","path":"/tmp/demo"}],"error":null}'
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
		"--limit", "1",
	)

	quickActions, ok := payload["quick_actions"].([]any)
	if !ok || len(quickActions) == 0 {
		t.Fatalf("quick_actions missing or empty: %#v", payload["quick_actions"])
	}

	seen := map[string]bool{}
	for _, raw := range quickActions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		callbackData, _ := action["callback_data"].(string)
		if !strings.HasPrefix(callbackData, "dx:") {
			t.Fatalf("callback_data = %q, want dx:* token", callbackData)
		}
		if len(callbackData) > 64 {
			t.Fatalf("callback_data len = %d, want <= 64 (%q)", len(callbackData), callbackData)
		}
		if seen[callbackData] {
			t.Fatalf("duplicate callback_data token: %q", callbackData)
		}
		seen[callbackData] = true
	}
}

func TestOpenClawDXProjectList_DataIncludesContextSnapshot(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{"project":{"path":"/tmp/demo","name":"demo"},"workspace":{"id":"ws-1","name":"mobile","repo":"/tmp/demo","assistant":"codex"},"agent":{"id":"agent-1","workspace_id":"ws-1","assistant":"codex"}}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
case "${1:-} ${2:-}" in
  "project list")
    printf '%s' '{"ok":true,"data":[{"name":"demo","path":"/tmp/demo"}],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "OPENCLAW_DX_CONTEXT_FILE", contextPath)

	payload := runScriptJSON(t, scriptPath, env, "project", "list")

	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	context, ok := data["context"].(map[string]any)
	if !ok {
		t.Fatalf("data.context missing or wrong type: %T", data["context"])
	}
	project, ok := context["project"].(map[string]any)
	if !ok {
		t.Fatalf("context.project missing or wrong type: %T", context["project"])
	}
	if got, _ := project["path"].(string); got != "/tmp/demo" {
		t.Fatalf("context.project.path = %q, want /tmp/demo", got)
	}
}

func TestOpenClawDXWorkspaceList_UsesContextProjectWhenProjectMissing(t *testing.T) {
	requireBinary(t, "jq")
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "openclaw-dx.sh")
	fakeBinDir := t.TempDir()
	fakeAmuxPath := filepath.Join(fakeBinDir, "amux")
	argsLog := filepath.Join(fakeBinDir, "amux-args.log")
	contextPath := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(contextPath, []byte(`{"project":{"path":"/tmp/demo","name":"demo"}}`), 0o644); err != nil {
		t.Fatalf("write context file: %v", err)
	}

	writeExecutable(t, fakeAmuxPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--json" ]]; then
  shift
fi
printf '%s\n' "$*" >> "${ARGS_LOG:?missing ARGS_LOG}"
case "${1:-} ${2:-}" in
  "workspace list")
    printf '%s' '{"ok":true,"data":[{"id":"ws-1","name":"mobile","repo":"/tmp/demo","root":"/tmp/ws-1","assistant":"codex"}],"error":null}'
    ;;
  "agent list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  "terminal list")
    printf '%s' '{"ok":true,"data":[],"error":null}'
    ;;
  *)
    printf '{"ok":false,"error":{"code":"unexpected","message":"unexpected args: %s"}}' "$*"
    ;;
esac
`)

	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "ARGS_LOG", argsLog)
	env = withEnv(env, "OPENCLAW_DX_CONTEXT_FILE", contextPath)

	payload := runScriptJSON(t, scriptPath, env,
		"workspace", "list",
		"--limit", "1",
	)

	if got, _ := payload["command"].(string); got != "workspace.list" {
		t.Fatalf("command = %q, want %q", got, "workspace.list")
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing or wrong type: %T", payload["data"])
	}
	if got, _ := data["project"].(string); got != "/tmp/demo" {
		t.Fatalf("project = %q, want /tmp/demo", got)
	}
	if got, _ := data["project_from_context"].(bool); !got {
		t.Fatalf("project_from_context = %v, want true", got)
	}

	argsRaw, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatalf("read args log: %v", err)
	}
	if !strings.Contains(string(argsRaw), "workspace list --repo /tmp/demo") {
		t.Fatalf("workspace list did not use context project, args:\n%s", string(argsRaw))
	}
}
