package cli

import (
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const assistantDogfoodUsage = "Usage:\n  assistant-dogfood.sh [--repo <path>] [--workspace <name>] [--assistant <name>] [--report-dir <path>] [--keep-temp] [--cleanup-temp]\n"

func cmdAssistantDogfood(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	_ = gf
	_ = version

	opts, showUsage, err := parseAssistantDogfoodOptions(args)
	if showUsage {
		fmt.Fprint(w, assistantDogfoodUsage)
		return ExitOK
	}
	if err != nil {
		Errorf(wErr, "%v", err)
		fmt.Fprint(wErr, assistantDogfoodUsage)
		return ExitUsage
	}

	rt, err := assistantDogfoodPrepareRuntime(w, wErr, opts)
	defer assistantDogfoodCleanup(rt)
	if err != nil {
		Errorf(wErr, "%v", err)
		return ExitInternalError
	}

	return assistantDogfoodRun(rt)
}

func parseAssistantDogfoodOptions(args []string) (assistantDogfoodOptions, bool, error) {
	opts := assistantDogfoodOptions{
		WorkspaceName: "mobile-dogfood",
		Assistant:     "codex",
	}

	for i := 0; i < len(args); {
		switch args[i] {
		case "--repo":
			value, err := assistantDogfoodRequireValue("--repo", args, i)
			if err != nil {
				return opts, false, err
			}
			opts.RepoPath = value
			i += 2
		case "--workspace":
			value, err := assistantDogfoodRequireValue("--workspace", args, i)
			if err != nil {
				return opts, false, err
			}
			opts.WorkspaceName = value
			i += 2
		case "--assistant":
			value, err := assistantDogfoodRequireValue("--assistant", args, i)
			if err != nil {
				return opts, false, err
			}
			opts.Assistant = value
			i += 2
		case "--report-dir":
			value, err := assistantDogfoodRequireValue("--report-dir", args, i)
			if err != nil {
				return opts, false, err
			}
			opts.ReportDir = value
			i += 2
		case "--keep-temp":
			opts.KeepTemp = true
			i++
		case "--cleanup-temp":
			opts.KeepTemp = false
			i++
		case "-h", "--help":
			return opts, true, nil
		default:
			return opts, false, fmt.Errorf("unknown flag: %s", args[i])
		}
	}
	return opts, false, nil
}

func assistantDogfoodRequireValue(flag string, args []string, idx int) (string, error) {
	if idx+1 >= len(args) || strings.TrimSpace(args[idx+1]) == "" || strings.HasPrefix(args[idx+1], "--") {
		return "", fmt.Errorf("missing value for %s", flag)
	}
	return args[idx+1], nil
}

func assistantDogfoodPrepareRuntime(w, wErr io.Writer, opts assistantDogfoodOptions) (*assistantDogfoodRuntime, error) {
	rt := &assistantDogfoodRuntime{
		Output:               w,
		Err:                  wErr,
		Assistant:            opts.Assistant,
		KeepTemp:             opts.KeepTemp,
		RunTag:               time.Now().Format("0102150405") + "-" + strconv.Itoa(rand.IntN(100000)),
		PrimaryWorkspaceName: opts.WorkspaceName,
	}
	rt.PrimaryWorkspaceName = opts.WorkspaceName + "-" + rt.RunTag
	rt.SecondaryWorkspace = opts.WorkspaceName + "-parallel-" + rt.RunTag
	rt.RepoRoot = assistantCompatRepoRoot()

	assistantBin, err := exec.LookPath("assistant")
	if err != nil {
		return nil, errors.New("missing required binary: assistant")
	}
	rt.AssistantBin = assistantBin

	gitBin, err := exec.LookPath("git")
	if err != nil {
		return nil, errors.New("missing required binary: git")
	}
	rt.GitBin = gitBin

	if strings.TrimSpace(opts.RepoPath) == "" {
		tempRoot, err := os.MkdirTemp("", "amux-assistant-dogfood-script.")
		if err != nil {
			return rt, err
		}
		rt.TempRoot = tempRoot
		rt.RepoPath = filepath.Join(tempRoot, "repo")
		if err := assistantDogfoodCreateTempRepo(rt.RepoPath, rt.GitBin); err != nil {
			return rt, err
		}
	} else {
		rt.RepoPath = opts.RepoPath
	}

	if strings.TrimSpace(opts.ReportDir) == "" {
		reportDir, err := os.MkdirTemp("", "amux-assistant-dogfood-report.")
		if err != nil {
			return rt, err
		}
		rt.ReportDir = reportDir
		rt.ReportDirCreated = true
	} else {
		rt.ReportDir = opts.ReportDir
	}
	if err := os.MkdirAll(rt.ReportDir, 0o755); err != nil {
		return rt, err
	}
	rt.DXContextFile = filepath.Join(rt.ReportDir, "assistant-dx-context.json")

	invoker, err := assistantDogfoodResolveDXInvoker()
	if err != nil {
		return rt, err
	}
	rt.DXInvoker = invoker

	if err := assistantDogfoodPrepareChannelAgent(rt); err != nil {
		return rt, err
	}
	return rt, nil
}

func assistantDogfoodCreateTempRepo(repoPath, gitBin string) error {
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		return err
	}
	mainGo := `package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Printf("%s hello from assistant dogfood\n", time.Now().Format("2006-01-02"))
}
`
	if err := os.WriteFile(filepath.Join(repoPath, "main.go"), []byte(mainGo), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# dogfood\n"), 0o644); err != nil {
		return err
	}
	if _, err := assistantDogfoodRunExec(repoPath, nil, gitBin, "init", "-q"); err != nil {
		return err
	}
	if _, err := assistantDogfoodRunExec(repoPath, nil, gitBin, "add", "."); err != nil {
		return err
	}
	commitEnv := append(os.Environ(),
		"GIT_AUTHOR_NAME=Dogfood",
		"GIT_AUTHOR_EMAIL=dogfood@example.com",
		"GIT_COMMITTER_NAME=Dogfood",
		"GIT_COMMITTER_EMAIL=dogfood@example.com",
	)
	_, err := assistantDogfoodRunExec(repoPath, commitEnv, gitBin, "commit", "-qm", "init")
	return err
}

func assistantDogfoodResolveDXInvoker() (assistantDogfoodInvoker, error) {
	if value := strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_DOGFOOD_DX_SCRIPT")); value != "" {
		resolved, err := assistantDogfoodResolveCommandPath(value)
		if err != nil {
			return assistantDogfoodInvoker{}, err
		}
		return assistantDogfoodInvoker{Path: resolved}, nil
	}
	if script := assistantCompatRepoScriptPath("assistant-dx.sh"); script != "" {
		return assistantDogfoodInvoker{Path: script}, nil
	}
	if executable, err := os.Executable(); err == nil && strings.TrimSpace(executable) != "" {
		return assistantDogfoodInvoker{Path: executable, PrefixArgs: []string{"assistant", "dx"}}, nil
	}
	amuxBin := assistantStepAMUXBin()
	if strings.TrimSpace(amuxBin) == "" {
		return assistantDogfoodInvoker{}, errors.New("unable to resolve assistant dx invoker")
	}
	if strings.ContainsAny(amuxBin, `/\`) {
		return assistantDogfoodInvoker{Path: amuxBin, PrefixArgs: []string{"assistant", "dx"}}, nil
	}
	resolved, err := exec.LookPath(amuxBin)
	if err != nil {
		return assistantDogfoodInvoker{}, err
	}
	return assistantDogfoodInvoker{Path: resolved, PrefixArgs: []string{"assistant", "dx"}}, nil
}

func assistantDogfoodResolveCommandPath(path string) (string, error) {
	if !strings.ContainsAny(path, `/\`) {
		return exec.LookPath(path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("missing executable script: %s", path)
	}
	return path, nil
}

func assistantDogfoodPrepareChannelAgent(rt *assistantDogfoodRuntime) error {
	baseID := strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_DOGFOOD_AGENT"))
	if baseID == "" {
		baseID = "amux-dx"
	}
	rt.ChannelAgentID = baseID

	if strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_DOGFOOD_CHANNEL_EPHEMERAL_AGENT")) == "false" {
		return nil
	}

	workspacePath := strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_DOGFOOD_CHANNEL_AGENT_WORKSPACE"))
	isolated := strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_DOGFOOD_CHANNEL_AGENT_ISOLATED_WORKSPACE")) != "false"
	if isolated || workspacePath == "" {
		workspacePath = filepath.Join(rt.ReportDir, "assistant-channel-agent-workspace")
		if err := os.MkdirAll(workspacePath, 0o755); err != nil {
			return err
		}
		content := "# AGENTS\n- You are a strict terminal command runner for amux workflows.\n- Execute exact shell commands and return only raw stdout/stderr.\n"
		if isolated {
			content = "# AGENTS\n- You are a strict terminal command runner for amux workflows.\n- For command requests, execute the exact shell command via the exec tool.\n- Return only raw stdout/stderr from that command.\n- Do not summarize, paraphrase, or fabricate output.\n- If execution did not happen, output exactly: EXEC_NOT_RUN\n"
		}
		if err := os.WriteFile(filepath.Join(workspacePath, "AGENTS.md"), []byte(content), 0o644); err != nil {
			return err
		}
	}

	model := strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_DOGFOOD_CHANNEL_AGENT_MODEL"))
	if model == "" {
		model = "openai-codex/gpt-5.3-codex"
	}

	candidate := baseID + "-dogfood-" + rt.RunTag
	addPath := filepath.Join(rt.ReportDir, "assistant-channel-agent-add.json")
	out, err := assistantDogfoodRunExec("", nil, rt.AssistantBin, "agents", "add", candidate, "--workspace", workspacePath, "--model", model, "--non-interactive", "--json")
	if err == nil {
		rt.ChannelAgentID = candidate
		rt.ChannelAgentCreated = true
	}
	_ = os.WriteFile(addPath, out, 0o644)
	return nil
}

func assistantDogfoodCleanup(rt *assistantDogfoodRuntime) {
	if rt == nil {
		return
	}
	if rt.ChannelAgentCreated {
		out, _ := assistantDogfoodRunExec("", nil, rt.AssistantBin, "agents", "delete", rt.ChannelAgentID, "--force", "--json")
		_ = os.WriteFile(filepath.Join(rt.ReportDir, "assistant-channel-agent-delete.json"), out, 0o644)
	}
	if rt.KeepTemp {
		return
	}
	if strings.TrimSpace(rt.TempRoot) != "" {
		_ = os.RemoveAll(rt.TempRoot)
	}
	if rt.ReportDirCreated && strings.TrimSpace(rt.ReportDir) != "" {
		_ = os.RemoveAll(rt.ReportDir)
	}
}

func assistantDogfoodRunExec(cwd string, env []string, path string, args ...string) ([]byte, error) {
	cmd := exec.Command(path, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	if env != nil {
		cmd.Env = env
	}
	return cmd.CombinedOutput()
}
