package sandbox

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/andyrewlee/amux/internal/daytona"
)

func waitForSshAccessDaytona(ds *daytona.Sandbox, token string) (string, error) {
	deadline := time.Now().Add(sshReadyTimeout)
	for time.Now().Before(deadline) {
		validation, err := ds.ValidateSshAccess(token)
		if err == nil && validation.Valid {
			return validation.RunnerDomain, nil
		}
		time.Sleep(sshReadyInterval)
	}
	return "", errors.New("SSH access token not ready. Try again.")
}

type agentCommandSpec struct {
	Command       string
	CommandBlock  string
	RemoteCommand string
	DebugEnabled  bool
}

func buildAgentCommandSpec(s RemoteSandbox, cfg AgentConfig) (agentCommandSpec, error) {
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

	resolvedCommand := resolveAgentCommandPath(s, command)
	args := cfg.Args
	if args == nil {
		args = []string{}
	}
	homeDir := getHomeDir(s)

	wrapPref := envFirst("AMUX_TTY_WRAP")
	wrapTty := false
	if wrapPref == "1" {
		wrapTty = hasScript(s)
	} else if wrapPref == "0" {
		wrapTty = false
	} else {
		wrapTty = cfg.Agent == AgentClaude && hasScript(s)
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
	envExports := buildEnvExportsLocal(cfg.Env)
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
		`if [ -d "$HOME/.local/bin" ]; then export PATH="$HOME/.local/bin:$PATH"; fi`,
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
		fmt.Sprintf(`  for p in $HOME/.local/bin/%s $HOME/.amp/bin/%s $HOME/.factory/bin/%s /usr/local/share/nvm/versions/node/*/bin/%s /usr/local/share/nvm/current/bin/%s /usr/local/bin/%s /usr/bin/%s /home/daytona/.local/bin/%s; do`, command, command, command, command, command, command, command, command),
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
	recordPath := strings.TrimSpace(cfg.RecordPath)
	recordEnabled := recordPath != "" && hasScript(s)
	if recordPath != "" && !recordEnabled {
		fmt.Fprintln(os.Stderr, "Warning: recording requested but `script` is unavailable; proceeding without recording.")
	}
	if recordEnabled {
		execLines = append(execLines, fmt.Sprintf(`exec script -q -f %s -c "$AMUX_CMDLINE"`, quoteForShell(recordPath)))
	} else if wrapTty {
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

	return agentCommandSpec{
		Command:       command,
		CommandBlock:  commandBlock,
		RemoteCommand: remoteCommand,
		DebugEnabled:  debugEnabled,
	}, nil
}

// BuildAgentRemoteCommand returns the remote command string for an agent session.
func BuildAgentRemoteCommand(sb RemoteSandbox, cfg AgentConfig) (string, error) {
	spec, err := buildAgentCommandSpec(sb, cfg)
	if err != nil {
		return "", err
	}
	return spec.RemoteCommand, nil
}

// RunAgentInteractive runs the agent in an interactive SSH session (Daytona).
func (s *daytonaSandbox) RunAgentInteractive(cfg AgentConfig) (int, error) {
	spec, err := buildAgentCommandSpec(s, cfg)
	if err != nil {
		return 1, err
	}

	fmt.Printf("Starting %s in interactive mode...\n", cfg.Agent)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return 1, errors.New("interactive mode requires a TTY")
	}

	sshAccess, err := s.inner.CreateSshAccess(60)
	if err != nil {
		return 1, err
	}
	defer func() {
		_ = s.inner.RevokeSshAccess(sshAccess.Token)
	}()

	runnerDomain, err := waitForSshAccessDaytona(s.inner, sshAccess.Token)
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

	remoteCommand := spec.RemoteCommand

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

	if spec.DebugEnabled {
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
				spec.CommandBlock,
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
