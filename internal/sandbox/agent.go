package sandbox

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/andyrewlee/amux/internal/daytona"
)

const (
	defaultSSHHost       = "ssh.app.daytona.io"
	sshReadyTimeout      = 15 * time.Second
	sshReadyInterval     = 1 * time.Second
	agentInstallBasePath = "/amux/.installed"
)

// AgentConfig configures interactive agent sessions.
type AgentConfig struct {
	Agent         Agent
	WorkspacePath string
	Args          []string
	Env           map[string]string
	RawMode       *bool
}

func getSSHHost() string {
	host := envFirst("AMUX_SSH_HOST", "DAYTONA_SSH_HOST")
	if host == "" {
		return defaultSSHHost
	}
	return host
}

func quoteForShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

var validEnvKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func isValidEnvKey(key string) bool {
	return validEnvKey.MatchString(key)
}

func buildEnvExports(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	lines := make([]string, 0, len(env))
	for key, value := range env {
		if !isValidEnvKey(key) {
			continue
		}
		lines = append(lines, fmt.Sprintf("export %s=%s", key, quoteForShell(value)))
	}
	return lines
}

func buildEnvAssignments(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}
	parts := make([]string, 0, len(env))
	for key, value := range env {
		if !isValidEnvKey(key) {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", key, quoteForShell(value)))
	}
	return strings.Join(parts, " ")
}

func redactExports(input string) string {
	re := regexp.MustCompile(`export [^\n]+`)
	return re.ReplaceAllString(input, "export <redacted>")
}

func getStdoutFromResponse(resp *daytona.ExecuteResponse) string {
	if resp == nil {
		return ""
	}
	if resp.Artifacts != nil && resp.Artifacts.Stdout != "" {
		return resp.Artifacts.Stdout
	}
	return resp.Result
}

func getNodeBinDir(sandbox *daytona.Sandbox) string {
	resp, err := sandbox.Process.ExecuteCommand("command -v node")
	if err == nil && resp.ExitCode == 0 {
		path := strings.TrimSpace(getStdoutFromResponse(resp))
		if path != "" {
			resp, err = sandbox.Process.ExecuteCommand(fmt.Sprintf("dirname %s", quoteForShell(path)))
			if err == nil && resp.ExitCode == 0 {
				dir := strings.TrimSpace(getStdoutFromResponse(resp))
				if dir != "" {
					return dir
				}
			}
		}
	}
	return ""
}

func getHomeDir(sandbox *daytona.Sandbox) string {
	resp, err := sandbox.Process.ExecuteCommand(`sh -lc "USER_NAME=$(id -un 2>/dev/null || echo daytona); HOME_DIR=$(getent passwd \"$USER_NAME\" 2>/dev/null | cut -d: -f6 || true); if [ -z \"$HOME_DIR\" ]; then HOME_DIR=/home/$USER_NAME; fi; printf \"%s\" \"$HOME_DIR\""`)
	if err == nil {
		stdout := strings.TrimSpace(getStdoutFromResponse(resp))
		if stdout != "" {
			return stdout
		}
	}
	return "/home/daytona"
}

func resolveAgentCommandPath(sandbox *daytona.Sandbox, command string) string {
	resp, err := sandbox.Process.ExecuteCommand(fmt.Sprintf("command -v %s", command))
	if err == nil && resp.ExitCode == 0 {
		path := strings.TrimSpace(getStdoutFromResponse(resp))
		if path != "" {
			return path
		}
	}
	if nodeBin := getNodeBinDir(sandbox); nodeBin != "" {
		candidate := fmt.Sprintf("%s/%s", nodeBin, command)
		resp, err = sandbox.Process.ExecuteCommand(fmt.Sprintf("test -x %s", quoteForShell(candidate)))
		if err == nil && resp.ExitCode == 0 {
			return candidate
		}
	}
	if command == "amp" {
		home := getHomeDir(sandbox)
		candidate := fmt.Sprintf("%s/.amp/bin/amp", home)
		resp, err = sandbox.Process.ExecuteCommand(fmt.Sprintf("test -x %s", quoteForShell(candidate)))
		if err == nil && resp.ExitCode == 0 {
			return candidate
		}
	}
	if command == "droid" {
		home := getHomeDir(sandbox)
		candidate := fmt.Sprintf("%s/.factory/bin/droid", home)
		resp, err = sandbox.Process.ExecuteCommand(fmt.Sprintf("test -x %s", quoteForShell(candidate)))
		if err == nil && resp.ExitCode == 0 {
			return candidate
		}
	}
	return command
}

func hasScript(sandbox *daytona.Sandbox) bool {
	resp, err := sandbox.Process.ExecuteCommand("command -v script")
	return err == nil && resp.ExitCode == 0 && strings.TrimSpace(getStdoutFromResponse(resp)) != ""
}

func agentInstallMarker(agent string) string {
	return fmt.Sprintf("%s/%s", agentInstallBasePath, agent)
}

func isAgentInstalled(sandbox *daytona.Sandbox, agent string) bool {
	resp, err := sandbox.Process.ExecuteCommand(fmt.Sprintf("test -f %s && echo exists", agentInstallMarker(agent)))
	return err == nil && strings.Contains(getStdoutFromResponse(resp), "exists")
}

func installClaude(sandbox *daytona.Sandbox) error {
	fmt.Println("Installing Claude Code...")
	resp, _ := sandbox.Process.ExecuteCommand("which claude")
	if resp != nil && resp.ExitCode == 0 {
		fmt.Println("Claude Code already installed")
	} else {
		resp, err := sandbox.Process.ExecuteCommand("npm install -g @anthropic-ai/claude-code")
		if err != nil || resp.ExitCode != 0 {
			return errors.New("failed to install Claude Code in sandbox")
		}
		fmt.Println("Claude Code installed")
	}
	_, _ = sandbox.Process.ExecuteCommand(fmt.Sprintf("mkdir -p %s && touch %s", agentInstallBasePath, agentInstallMarker("claude")))
	return nil
}

func installCodex(sandbox *daytona.Sandbox) error {
	fmt.Println("Installing Codex CLI...")
	resp, _ := sandbox.Process.ExecuteCommand("which codex")
	if resp != nil && resp.ExitCode == 0 {
		fmt.Println("Codex CLI already installed")
	} else {
		resp, err := sandbox.Process.ExecuteCommand("npm install -g @openai/codex")
		if err != nil || resp.ExitCode != 0 {
			return errors.New("failed to install Codex CLI in sandbox")
		}
		fmt.Println("Codex CLI installed")
	}
	_, _ = sandbox.Process.ExecuteCommand(fmt.Sprintf("mkdir -p %s && touch %s", agentInstallBasePath, agentInstallMarker("codex")))
	return nil
}

func installOpenCode(sandbox *daytona.Sandbox) error {
	fmt.Println("Installing OpenCode CLI...")
	resp, _ := sandbox.Process.ExecuteCommand("which opencode")
	if resp != nil && resp.ExitCode == 0 {
		fmt.Println("OpenCode CLI already installed")
	} else {
		resp, err := sandbox.Process.ExecuteCommand(`bash -lc "curl -fsSL https://opencode.ai/install | bash"`)
		if err != nil || resp.ExitCode != 0 {
			fmt.Println("OpenCode install script failed, trying npm...")
			resp, err = sandbox.Process.ExecuteCommand("npm install -g opencode-ai")
			if err != nil || resp.ExitCode != 0 {
				return errors.New("failed to install OpenCode CLI in sandbox")
			}
		}
		fmt.Println("OpenCode CLI installed")
	}
	_, _ = sandbox.Process.ExecuteCommand(fmt.Sprintf("mkdir -p %s && touch %s", agentInstallBasePath, agentInstallMarker("opencode")))
	return nil
}

func installAmp(sandbox *daytona.Sandbox) error {
	fmt.Println("Installing Amp CLI...")
	home := getHomeDir(sandbox)
	ampBin := fmt.Sprintf("%s/.amp/bin/amp", home)
	resp, _ := sandbox.Process.ExecuteCommand(fmt.Sprintf("sh -lc \"command -v amp >/dev/null 2>&1 || test -x %s\"", quoteForShell(ampBin)))
	if resp != nil && resp.ExitCode == 0 {
		fmt.Println("Amp CLI already installed")
	} else {
		resp, err := sandbox.Process.ExecuteCommand(`bash -lc "curl -fsSL https://ampcode.com/install.sh | bash"`)
		if err != nil || resp.ExitCode != 0 {
			fmt.Println("Amp install script failed, trying npm...")
			resp, err = sandbox.Process.ExecuteCommand("npm install -g @sourcegraph/amp")
			if err != nil || resp.ExitCode != 0 {
				return errors.New("failed to install Amp CLI in sandbox")
			}
		}
		fmt.Println("Amp CLI installed")
	}
	_, _ = sandbox.Process.ExecuteCommand(fmt.Sprintf("mkdir -p %s && touch %s", agentInstallBasePath, agentInstallMarker("amp")))
	return nil
}

func installGemini(sandbox *daytona.Sandbox) error {
	fmt.Println("Installing Gemini CLI...")
	resp, _ := sandbox.Process.ExecuteCommand("which gemini")
	if resp != nil && resp.ExitCode == 0 {
		fmt.Println("Gemini CLI already installed")
	} else {
		resp, err := sandbox.Process.ExecuteCommand("npm install -g @google/gemini-cli")
		if err != nil || resp.ExitCode != 0 {
			return errors.New("failed to install Gemini CLI in sandbox")
		}
		fmt.Println("Gemini CLI installed")
	}
	_, _ = sandbox.Process.ExecuteCommand(fmt.Sprintf("mkdir -p %s && touch %s", agentInstallBasePath, agentInstallMarker("gemini")))
	return nil
}

func installDroid(sandbox *daytona.Sandbox) error {
	fmt.Println("Installing Droid CLI...")
	resp, _ := sandbox.Process.ExecuteCommand("which droid")
	if resp != nil && resp.ExitCode == 0 {
		fmt.Println("Droid CLI already installed")
	} else {
		resp, err := sandbox.Process.ExecuteCommand(`bash -lc "curl -fsSL https://app.factory.ai/cli | sh"`)
		if err != nil || resp.ExitCode != 0 {
			return errors.New("failed to install Droid CLI in sandbox")
		}
		fmt.Println("Droid CLI installed")
	}
	_, _ = sandbox.Process.ExecuteCommand(fmt.Sprintf("mkdir -p %s && touch %s", agentInstallBasePath, agentInstallMarker("droid")))
	return nil
}

// EnsureAgentInstalled installs the requested agent if missing.
func EnsureAgentInstalled(sandbox *daytona.Sandbox, agent Agent) error {
	if agent == AgentShell {
		return nil
	}
	if isAgentInstalled(sandbox, agent.String()) {
		fmt.Printf("%s already installed\n", agent)
		return nil
	}
	switch agent {
	case AgentClaude:
		return installClaude(sandbox)
	case AgentCodex:
		return installCodex(sandbox)
	case AgentOpenCode:
		return installOpenCode(sandbox)
	case AgentAmp:
		return installAmp(sandbox)
	case AgentGemini:
		return installGemini(sandbox)
	case AgentDroid:
		return installDroid(sandbox)
	default:
		return nil
	}
}

func waitForSshAccess(sandbox *daytona.Sandbox, token string) (string, error) {
	deadline := time.Now().Add(sshReadyTimeout)
	for time.Now().Before(deadline) {
		validation, err := sandbox.ValidateSshAccess(token)
		if err == nil && validation.Valid {
			return validation.RunnerDomain, nil
		}
		time.Sleep(sshReadyInterval)
	}
	return "", errors.New("SSH access token not ready. Try again.")
}

// RunAgentInteractive runs the agent in an interactive SSH session.
func RunAgentInteractive(sandbox *daytona.Sandbox, cfg AgentConfig) (int, error) {
	command := "bash"
	switch cfg.Agent {
	case AgentClaude:
		command = "claude"
	case AgentCodex:
		command = "codex"
	case AgentOpenCode:
		command = "opencode"
	case AgentAmp:
		command = "amp"
	case AgentGemini:
		command = "gemini"
	case AgentDroid:
		command = "droid"
	case AgentShell:
		command = "bash"
	}

	resolvedCommand := resolveAgentCommandPath(sandbox, command)
	args := cfg.Args
	if args == nil {
		args = []string{}
	}
	homeDir := getHomeDir(sandbox)

	wrapPref := envFirst("AMUX_TTY_WRAP")
	wrapTty := false
	if wrapPref == "1" {
		wrapTty = hasScript(sandbox)
	} else if wrapPref == "0" {
		wrapTty = false
	} else {
		wrapTty = cfg.Agent == AgentClaude && hasScript(sandbox)
	}

	fmt.Printf("Starting %s in interactive mode...\n", cfg.Agent)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return 1, errors.New("interactive mode requires a TTY")
	}

	sshAccess, err := sandbox.CreateSshAccess(60)
	if err != nil {
		return 1, err
	}
	defer func() {
		_ = sandbox.RevokeSshAccess(sshAccess.Token)
	}()

	runnerDomain, err := waitForSshAccess(sandbox, sshAccess.Token)
	if err != nil {
		return 1, err
	}
	sshHost := runnerDomain
	if sshHost == "" {
		sshHost = getSSHHost()
	}
	target := fmt.Sprintf("%s@%s", sshAccess.Token, sshHost)

	rawShell := cfg.Agent == AgentShell && envDefaultTrue("AMUX_SHELL_RAW")
	useShellBootstrap := !rawShell && envIsOne("AMUX_SSH_SHELL")
	useRawMode := false
	if cfg.RawMode != nil {
		useRawMode = *cfg.RawMode
	} else {
		useRawMode = envIsOne("AMUX_SSH_RAW") || cfg.Agent == AgentCodex || cfg.Agent == AgentOpenCode || cfg.Agent == AgentAmp || cfg.Agent == AgentGemini || cfg.Agent == AgentDroid
	}

	safeWorkspace := quoteForShell(cfg.WorkspacePath)
	safeResolved := quoteForShell(resolvedCommand)
	safeArgs := ""
	if len(args) > 0 {
		quotedArgs := make([]string, 0, len(args))
		for _, arg := range args {
			quotedArgs = append(quotedArgs, quoteForShell(arg))
		}
		safeArgs = strings.Join(quotedArgs, " ")
	}
	shellInteractiveFlag := ""
	if cfg.Agent == AgentShell {
		shellInteractiveFlag = " -i"
	}

	exportHome := fmt.Sprintf("export HOME=%s", quoteForShell(homeDir))
	exportTerm := strings.Join([]string{
		`if [ -z "$TERM" ] || [ "$TERM" = "dumb" ]; then`,
		`  export TERM=xterm-256color`,
		`else`,
		`  infocmp "$TERM" >/dev/null 2>&1 || export TERM=xterm-256color`,
		`fi`,
	}, "\n")
	unsetCi := `if [ -n "$CI" ]; then unset CI; fi`
	envExports := buildEnvExports(cfg.Env)
	debugEnabled := envIsOne("AMUX_SSH_DEBUG")
	debugLines := []string{}
	if debugEnabled {
		debugLines = append(debugLines,
			`echo "AMUX_DEBUG: HOME=$HOME"`,
			`echo "AMUX_DEBUG: PATH=$PATH"`,
			`echo "AMUX_DEBUG: TERM=$TERM"`,
			`echo "AMUX_DEBUG: CI=$CI"`,
			`echo "AMUX_DEBUG: NODE_BIN=$NODE_BIN"`,
			`echo "AMUX_DEBUG: NODE_DIR=$NODE_DIR"`,
			`echo "AMUX_DEBUG: AMUX_RESOLVED=$AMUX_RESOLVED"`,
			`echo "AMUX_DEBUG: AMUX_CMD=$AMUX_CMD"`,
			`if [ -n "$ANTHROPIC_API_KEY" ]; then echo "AMUX_DEBUG: ANTHROPIC_API_KEY=SET"; else echo "AMUX_DEBUG: ANTHROPIC_API_KEY=UNSET"; fi`,
			`if [ -n "$CLAUDE_API_KEY" ]; then echo "AMUX_DEBUG: CLAUDE_API_KEY=SET"; else echo "AMUX_DEBUG: CLAUDE_API_KEY=UNSET"; fi`,
			`if [ -n "$ANTHROPIC_AUTH_TOKEN" ]; then echo "AMUX_DEBUG: ANTHROPIC_AUTH_TOKEN=SET"; else echo "AMUX_DEBUG: ANTHROPIC_AUTH_TOKEN=UNSET"; fi`,
			`if [ -n "$OPENAI_API_KEY" ]; then echo "AMUX_DEBUG: OPENAI_API_KEY=SET"; else echo "AMUX_DEBUG: OPENAI_API_KEY=UNSET"; fi`,
			`if [ -n "$GEMINI_API_KEY" ]; then echo "AMUX_DEBUG: GEMINI_API_KEY=SET"; else echo "AMUX_DEBUG: GEMINI_API_KEY=UNSET"; fi`,
			`if [ -n "$GOOGLE_API_KEY" ]; then echo "AMUX_DEBUG: GOOGLE_API_KEY=SET"; else echo "AMUX_DEBUG: GOOGLE_API_KEY=UNSET"; fi`,
			`if [ -n "$GOOGLE_APPLICATION_CREDENTIALS" ]; then echo "AMUX_DEBUG: GOOGLE_APPLICATION_CREDENTIALS=SET"; else echo "AMUX_DEBUG: GOOGLE_APPLICATION_CREDENTIALS=UNSET"; fi`,
			`if [ -n "$FACTORY_API_KEY" ]; then echo "AMUX_DEBUG: FACTORY_API_KEY=SET"; else echo "AMUX_DEBUG: FACTORY_API_KEY=UNSET"; fi`,
			fmt.Sprintf("command -v %s 2>/dev/null || true", command),
			`command -v node 2>/dev/null || true`,
			`command -v git 2>/dev/null || true`,
		)
	}

	innerCommand := []string{
		exportHome,
		exportTerm,
		unsetCi,
	}
	innerCommand = append(innerCommand, envExports...)
	innerCommand = append(innerCommand,
		`stty sane >/dev/null 2>&1 || true`,
		`if [ -d /usr/local/share/nvm/current/bin ]; then export PATH="/usr/local/share/nvm/current/bin:$PATH"; fi`,
		`if [ -d "$HOME/.amp/bin" ]; then export PATH="$HOME/.amp/bin:$PATH"; fi`,
		`if [ -d "$HOME/.factory/bin" ]; then export PATH="$HOME/.factory/bin:$PATH"; fi`,
		`if [ -d /usr/local/share/nvm/versions/node ]; then`,
		`  for p in /usr/local/share/nvm/versions/node/*/bin; do`,
		`    if [ -d "$p" ]; then export PATH="$p:$PATH"; fi`,
		`  done`,
		`fi`,
		fmt.Sprintf("cd %s", safeWorkspace),
		fmt.Sprintf("AMUX_RESOLVED=%s", safeResolved),
		`NODE_BIN=$(command -v node 2>/dev/null || true)`,
		`if [ -z "$NODE_BIN" ]; then`,
		`  for p in /usr/local/share/nvm/versions/node/*/bin/node /usr/local/share/nvm/current/bin/node /usr/local/bin/node /usr/bin/node; do`,
		`    if [ -x "$p" ]; then NODE_BIN="$p"; break; fi`,
		`  done`,
		`fi`,
		`if [ -n "$NODE_BIN" ]; then`,
		`  NODE_DIR=$(dirname "$NODE_BIN")`,
		`  export PATH="$NODE_DIR:$PATH"`,
		`fi`,
		`if command -v npm >/dev/null 2>&1; then`,
		`  NPM_PREFIX=$(npm config get prefix 2>/dev/null || true)`,
		`  if [ -n "$NPM_PREFIX" ] && [ -d "$NPM_PREFIX/bin" ]; then export PATH="$NPM_PREFIX/bin:$PATH"; fi`,
		`fi`,
		`AMUX_CMD=""`,
		`if [ -n "$AMUX_RESOLVED" ] && [ -x "$AMUX_RESOLVED" ]; then AMUX_CMD="$AMUX_RESOLVED"; fi`,
		fmt.Sprintf(`if [ -z "$AMUX_CMD" ]; then AMUX_CMD=$(command -v %s 2>/dev/null || true); fi`, command),
		fmt.Sprintf(`if [ -z "$AMUX_CMD" ] && [ -n "$NODE_DIR" ] && [ -x "$NODE_DIR/%s" ]; then AMUX_CMD="$NODE_DIR/%s"; fi`, command, command),
		`if [ -z "$AMUX_CMD" ]; then`,
		fmt.Sprintf(`  for p in $HOME/.amp/bin/%s $HOME/.factory/bin/%s /usr/local/share/nvm/versions/node/*/bin/%s /usr/local/share/nvm/current/bin/%s /usr/local/bin/%s /usr/bin/%s /home/daytona/.local/bin/%s; do`, command, command, command, command, command, command, command),
		`    if [ -x "$p" ]; then AMUX_CMD="$p"; break; fi`,
		`  done`,
		`fi`,
	)
	innerCommand = append(innerCommand, debugLines...)
	innerCommand = append(innerCommand, fmt.Sprintf("if [ -z \"$AMUX_CMD\" ]; then echo \"%s not found\" >&2; exit 127; fi", command))

	execLines := []string{
		fmt.Sprintf(`AMUX_CMDLINE="$AMUX_CMD%s%s"`, shellInteractiveFlag, func() string {
			if safeArgs != "" {
				return " " + safeArgs
			}
			return ""
		}()),
	}
	if wrapTty {
		execLines = append(execLines, `exec script -q -c "$AMUX_CMDLINE" /dev/null`)
	}
	execLines = append(execLines, fmt.Sprintf(`exec "$AMUX_CMD"%s%s`, shellInteractiveFlag, func() string {
		if safeArgs != "" {
			return " " + safeArgs
		}
		return ""
	}()))

	commandBlock := strings.Join(append(innerCommand, execLines...), "\n")
	remoteCommand := fmt.Sprintf("bash -lc %s", quoteForShell(commandBlock))

	sshArgs := []string{
		"-tt",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		target,
	}
	if !rawShell && !useShellBootstrap {
		sshArgs = append(sshArgs, remoteCommand)
	}

	if debugEnabled {
		sshArgs = append([]string{"-vvv"}, sshArgs...)
		fmt.Printf("SSH target: %s\n", target)
		if len(cfg.Env) > 0 {
			keys := make([]string, 0, len(cfg.Env))
			for key := range cfg.Env {
				keys = append(keys, key)
			}
			sortStrings(keys)
			fmt.Printf("SSH env keys: %s\n", strings.Join(keys, ", "))
		}
		if rawShell || useShellBootstrap {
			fmt.Printf("SSH command: ssh %s\n", target)
		} else {
			fmt.Printf("SSH command: %s\n", redactExports(remoteCommand))
		}
	}

	cmd := exec.Command("ssh", sshArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	var stdinPipe io.WriteCloser
	var pipeReader io.ReadCloser
	if useShellBootstrap {
		pr, pw := io.Pipe()
		pipeReader = pr
		stdinPipe = pw
		cmd.Stdin = pr
	} else {
		cmd.Stdin = os.Stdin
	}

	var restoreRaw func()
	if useRawMode {
		if term.IsTerminal(int(os.Stdin.Fd())) {
			state, err := term.MakeRaw(int(os.Stdin.Fd()))
			if err == nil {
				restoreRaw = func() { _ = term.Restore(int(os.Stdin.Fd()), state) }
			}
		}
	}

	if err := cmd.Start(); err != nil {
		if restoreRaw != nil {
			restoreRaw()
		}
		if pipeReader != nil {
			_ = pipeReader.Close()
		}
		if errors.Is(err, exec.ErrNotFound) {
			return 1, errors.New("ssh is required to run interactive sessions. Install OpenSSH and try again.")
		}
		return 1, err
	}

	if useShellBootstrap && stdinPipe != nil {
		go func() {
			wrappedScript := strings.Join([]string{
				"set +o history",
				"stty -echo",
				commandBlock,
			}, "\n")
			_, _ = io.WriteString(stdinPipe, wrappedScript+"\n")
			_, _ = io.Copy(stdinPipe, os.Stdin)
			_ = stdinPipe.Close()
		}()
	}

	err = cmd.Wait()
	if restoreRaw != nil {
		restoreRaw()
	}
	if pipeReader != nil {
		_ = pipeReader.Close()
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

// RunAgentCommand executes a non-interactive command for an agent.
func RunAgentCommand(sandbox *daytona.Sandbox, cfg AgentConfig, args []string) (int32, string, error) {
	command := "bash"
	switch cfg.Agent {
	case AgentClaude:
		command = "claude"
	case AgentCodex:
		command = "codex"
	case AgentOpenCode:
		command = "opencode"
	case AgentAmp:
		command = "amp"
	case AgentGemini:
		command = "gemini"
	case AgentDroid:
		command = "droid"
	}
	resolved := resolveAgentCommandPath(sandbox, command)
	allArgs := strings.Join(args, " ")
	cmdLine := resolved
	if allArgs != "" {
		cmdLine = fmt.Sprintf("%s %s", resolved, allArgs)
	}
	envAssignments := buildEnvAssignments(cfg.Env)
	if envAssignments != "" {
		cmdLine = fmt.Sprintf("%s %s", envAssignments, cmdLine)
	}
	resp, err := sandbox.Process.ExecuteCommand(cmdLine, daytona.ExecuteCommandOptions{Cwd: cfg.WorkspacePath})
	if err != nil {
		return 1, "", err
	}
	return resp.ExitCode, getStdoutFromResponse(resp), nil
}

func sortStrings(values []string) {
	for i := 0; i < len(values)-1; i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}
