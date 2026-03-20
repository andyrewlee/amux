package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// RunCobraWithGlobals executes the Cobra CLI while honoring legacy global
// options that are pre-parsed by cmd/amux dispatch compatibility logic.
func RunCobraWithGlobals(args []string, gf GlobalFlags, version string) int {
	gf = mergedCobraResponseGlobals(args, gf)
	commandArgs := args
	if _, rest, err := ParseGlobalFlags(args); err == nil {
		commandArgs = rest
	}
	setResponseContext(gf.RequestID, commandFromArgs(commandArgs))
	defer clearResponseContext()
	restore, err := applyRunGlobals(gf)
	if err != nil {
		if cobraArgsWantJSON(args, gf) {
			details := map[string]any{"cwd": gf.Cwd}
			ReturnError(os.Stdout, "invalid_cwd", err.Error(), details, version)
		} else {
			Errorf(os.Stderr, "invalid --cwd: %v", err)
		}
		return ExitUsage
	}
	defer restore()
	return runCobra(args, version)
}

func mergedCobraResponseGlobals(args []string, gf GlobalFlags) GlobalFlags {
	parsed, _, err := ParseGlobalFlags(cobraPreflightArgs(args))
	if err != nil {
		return gf
	}
	if strings.TrimSpace(gf.RequestID) == "" {
		gf.RequestID = parsed.RequestID
	}
	if !gf.JSON {
		gf.JSON = parsed.JSON
	}
	return gf
}

func cobraArgsWantJSON(args []string, gf GlobalFlags) bool {
	if gf.JSON {
		return true
	}
	parsed, _, err := ParseGlobalFlags(cobraPreflightArgs(args))
	if err != nil {
		return false
	}
	return parsed.JSON
}

func cobraPreflightArgs(args []string) []string {
	for i, arg := range args {
		if arg == "--" {
			return append([]string(nil), args[:i]...)
		}
	}
	return args
}

// InsertFlagAfterCobraCommandPath inserts flag after the resolved Cobra command
// path so leaf-command flags remain visible to the intended subcommand.
func InsertFlagAfterCobraCommandPath(args []string, flag string) []string {
	insertAt := cobraCommandPathLength(args)
	withFlag := make([]string, 0, len(args)+1)
	withFlag = append(withFlag, args[:insertAt]...)
	withFlag = append(withFlag, flag)
	withFlag = append(withFlag, args[insertAt:]...)
	return withFlag
}

func cobraCommandPathLength(args []string) int {
	if len(args) == 0 {
		return 0
	}

	current := buildRootCommand()
	pathLen := 0
	for pathLen < len(args) {
		token := args[pathLen]
		if token == "--" || strings.HasPrefix(token, "-") {
			break
		}

		next := findCobraSubcommand(current, token)
		if next == nil {
			break
		}

		current = next
		pathLen++
	}

	if pathLen == 0 {
		return 1
	}
	return pathLen
}

func findCobraSubcommand(parent *cobra.Command, token string) *cobra.Command {
	for _, child := range parent.Commands() {
		if child.Name() == token || child.HasAlias(token) {
			return child
		}
	}
	return nil
}

func runCobra(args []string, version string) int {
	root := buildRootCommand()
	root.Version = version
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		if exitErr, ok := err.(exitError); ok {
			return exitErr.code
		}
		Errorf(os.Stderr, "%v", err)
		return 1
	}
	return 0
}

func buildRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "amux",
		Short: "Daytona-powered sandbox CLI for Claude Code, Codex, OpenCode, Amp, Gemini, and Droid",
		Long: `amux - Run AI coding agents in cloud sandboxes

Quick start:
  amux claude              Run Claude Code in a cloud sandbox
  amux codex               Run Codex in a cloud sandbox
  amux shell               Run a shell in a cloud sandbox

Management:
  amux status              Check sandbox status
  amux ls                  List all sandboxes
  amux rm [id]             Remove a sandbox
  amux ssh                 SSH into the sandbox

Setup:
  amux setup               Initial setup (validate credentials)
  amux doctor              Diagnose issues
  amux auth login          Configure Daytona API key`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetHelpCommand(&cobra.Command{Hidden: true})
	root.CompletionOptions.DisableDefaultCmd = true

	// Core commands
	root.AddCommand(buildSetupCommand())
	root.AddCommand(buildEnhancedDoctorCommand())
	root.AddCommand(buildSnapshotCommand())
	root.AddCommand(buildAuthCommand())
	root.AddCommand(buildSandboxCommand())
	root.AddCommand(buildSettingsCommand())

	// Quick access commands
	root.AddCommand(buildStatusCommand())
	root.AddCommand(buildSSHCommand())
	root.AddCommand(buildExecCommand())

	// Documentation and help commands
	root.AddCommand(buildCompletionCommand())
	root.AddCommand(buildExplainCommand())

	// Agent aliases - shortcuts for `amux sandbox run <agent>`
	root.AddCommand(buildAgentAliasCommand("claude", "Run Claude Code in a sandbox"))
	root.AddCommand(buildAgentAliasCommand("codex", "Run Codex in a sandbox"))
	root.AddCommand(buildAgentAliasCommand("opencode", "Run OpenCode in a sandbox"))
	root.AddCommand(buildAgentAliasCommand("amp", "Run Amp in a sandbox"))
	root.AddCommand(buildAgentAliasCommand("gemini", "Run Gemini CLI in a sandbox"))
	root.AddCommand(buildAgentAliasCommand("droid", "Run Droid in a sandbox"))
	root.AddCommand(buildAgentAliasCommand("shell", "Run a shell in a sandbox"))

	// Command aliases for convenience
	root.AddCommand(buildLsAlias())
	root.AddCommand(buildRmAlias())

	// Register shell completions for commands
	registerCompletions(root)

	return root
}

// buildLsAlias creates an alias for `amux sandbox ls`
func buildLsAlias() *cobra.Command {
	cmd := buildSandboxLsCommand()
	cmd.Use = "ls"
	cmd.Short = "List all amux sandboxes (alias for `sandbox ls`)"
	return cmd
}

// buildRmAlias creates an alias for `amux sandbox rm`
func buildRmAlias() *cobra.Command {
	var project bool
	cmd := &cobra.Command{
		Use:   "rm [id]",
		Short: "Remove a sandbox (alias for `sandbox rm`)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Delegate to sandbox rm
			sandboxCmd := buildSandboxRmCommand()
			if project {
				_ = sandboxCmd.Flags().Set("project", "true")
			}
			return sandboxCmd.RunE(sandboxCmd, args)
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "Remove sandbox for current project")
	return cmd
}
