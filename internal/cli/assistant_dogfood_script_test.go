package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantDogfoodScript_MissingFlagValueFailsClearly(t *testing.T) {
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dogfood.sh")
	cmd := exec.Command(scriptPath, "--repo")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit for missing flag value")
	}
	text := string(out)
	if !strings.Contains(text, "missing value for --repo") {
		t.Fatalf("output = %q, want missing flag guidance", text)
	}
}

func TestAssistantDogfoodScript_ResolvesPrimaryWorkspaceFromWorkspaceCreate(t *testing.T) {
	requireBinary(t, "bash")
	requireBinary(t, "jq")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dogfood.sh")
	fakeBinDir := t.TempDir()
	writeExecutable(t, filepath.Join(fakeBinDir, "amux"), `#!/usr/bin/env bash
set -euo pipefail
printf '%s' '{"ok":true}'
`)
	writeExecutable(t, filepath.Join(fakeBinDir, "assistant"), `#!/usr/bin/env bash
set -euo pipefail
cmd="${1:-}"
shift || true
case "$cmd" in
  health)
    printf '%s' '{"ok":true}'
    ;;
  agent)
    local_mode="false"
    msg=""
    while [[ $# -gt 0 ]]; do
      case "$1" in
        --local)
          local_mode="true"
          shift
          ;;
        --message)
          msg="${2-}"
          shift 2
          ;;
        *)
          shift
          ;;
      esac
    done
    if [[ "$local_mode" == "true" ]]; then
      jq -cn --arg text "local ping ok" '{status:"ok",payloads:[{text:$text}]}'
    else
      jq -cn --arg text "$msg" '{status:"ok",result:{payloads:[{text:$text}]}}'
    fi
    ;;
  *)
    printf '%s' '{"ok":true}'
    ;;
esac
`)

	fakeDX := filepath.Join(fakeBinDir, "assistant-dx.sh")
	writeExecutable(t, fakeDX, `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${AMUX_DX_CALL_LOG:?missing AMUX_DX_CALL_LOG}"
cmd="${1:-}"
sub="${2:-}"
case "$cmd $sub" in
  "project add")
    jq -cn --arg path "${4:-${3:-}}" '{ok:true,command:"project.add",status:"ok",summary:"Project registered.",next_action:"Create workspace.",suggested_command:"",data:{name:"repo",path:$path},quick_actions:[]}'
    ;;
  "workspace create")
    name="${3:-}"
    ws_id="ws-primary"
    if [[ "$name" == *"parallel"* ]]; then
      ws_id="ws-secondary"
    fi
    jq -cn --arg ws "$ws_id" '{ok:true,command:"workspace.create",status:"ok",summary:"Workspace created.",next_action:"",suggested_command:"",data:{id:$ws,assistant:"codex"},quick_actions:[]}'
    ;;
  "status --workspace")
    jq -cn '{ok:true,command:"status",status:"attention",summary:"Implement pass reached bounded terminal state.",next_action:"Proceed to review.",suggested_command:"",data:{task:{overall_status:"partial"}},quick_actions:[]}'
    ;;
  "review --workspace")
    jq -cn '{ok:true,command:"review",status:"ok",summary:"Review completed.",next_action:"Continue.",suggested_command:"",data:{},quick_actions:[]}'
    ;;
  *)
    jq -cn '{ok:true,command:"noop",status:"ok",summary:"ok",next_action:"",suggested_command:"",data:{},quick_actions:[]}'
    ;;
esac
`)

	repoPath := t.TempDir()
	reportDir := t.TempDir()
	cmd := exec.Command(
		scriptPath,
		"--repo", repoPath,
		"--workspace", "dogfood-ws",
		"--assistant", "codex",
		"--report-dir", reportDir,
	)
	env := os.Environ()
	env = withEnv(env, "PATH", fakeBinDir+":"+os.Getenv("PATH"))
	env = withEnv(env, "AMUX_ASSISTANT_DOGFOOD_DX_SCRIPT", fakeDX)
	env = withEnv(env, "AMUX_DX_CALL_LOG", filepath.Join(fakeBinDir, "dx-calls.log"))
	env = withEnv(env, "AMUX_ASSISTANT_DOGFOOD_CHANNEL_EPHEMERAL_AGENT", "false")
	env = withEnv(env, "AMUX_ASSISTANT_DOGFOOD_CHANNEL_REQUIRE_PROOF", "false")
	env = withEnv(env, "AMUX_ASSISTANT_DOGFOOD_REQUIRE_CHANNEL_EXECUTION", "false")
	env = withEnv(env, "AMUX_ASSISTANT_DOGFOOD_REVIEW_GATE_TIMEOUT_SECONDS", "1")
	env = withEnv(env, "AMUX_ASSISTANT_DOGFOOD_REVIEW_GATE_POLL_SECONDS", "0")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("assistant-dogfood.sh failed: %v\noutput:\n%s", err, string(out))
	}

	summaryPath := filepath.Join(reportDir, "summary.txt")
	summaryRaw, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("read summary: %v\noutput:\n%s", err, string(out))
	}
	summary := string(summaryRaw)
	if !strings.Contains(summary, "workspace_primary=ws-primary") {
		t.Fatalf("summary missing resolved primary workspace id:\n%s", summary)
	}
	if strings.Contains(string(out), "failed to resolve ws1 id from project_add") {
		t.Fatalf("script still relied on project_add workspace id:\n%s", string(out))
	}

	callLogRaw, err := os.ReadFile(filepath.Join(fakeBinDir, "dx-calls.log"))
	if err != nil {
		t.Fatalf("read dx call log: %v", err)
	}
	callLog := string(callLogRaw)
	if strings.Contains(callLog, "workflow dual") {
		t.Fatalf("dogfood should not invoke removed workflow command, got:\n%s", callLog)
	}
	if !strings.Contains(callLog, "start --workspace ws-primary") || !strings.Contains(callLog, "--allow-new-run") {
		t.Fatalf("dogfood should run implement step with --allow-new-run, got:\n%s", callLog)
	}
	if !strings.Contains(callLog, "status --workspace ws-primary --assistant codex") {
		t.Fatalf("dogfood should gate review on workspace status checks, got:\n%s", callLog)
	}
	if !strings.Contains(callLog, "review --workspace ws-primary") {
		t.Fatalf("dogfood should run review step for primary workspace, got:\n%s", callLog)
	}
	for _, line := range strings.Split(callLog, "\n") {
		if strings.Contains(line, "review --workspace ws-primary") && strings.Contains(line, "--no-monitor") {
			t.Fatalf("dogfood review step should monitor to terminal status, got:\n%s", callLog)
		}
	}
}
