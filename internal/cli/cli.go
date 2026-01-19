package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// RunCobra executes the sandbox-oriented Cobra CLI. It returns a process exit code.
func RunCobra(args []string) int {
	root := buildRootCommand()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		if exitErr, ok := err.(exitError); ok {
			return exitErr.code
		}
		fmt.Fprintln(os.Stderr, err)
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
	root.Version = "0.1.0"
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
	root.AddCommand(buildLogsCommand())

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
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List all amux sandboxes (alias for `sandbox ls`)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Delegate to sandbox ls
			sandboxCmd := buildSandboxLsCommand()
			return sandboxCmd.RunE(sandboxCmd, args)
		},
	}
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
