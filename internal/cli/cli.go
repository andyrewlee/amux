package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Run executes the AMUX CLI. It returns a process exit code.
func Run(args []string) int {
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
		Short: "Daytona-powered computer CLI for Claude Code, Codex, OpenCode, Amp, Gemini, and Droid",
		Long: `amux - Run AI coding agents in cloud computers

Quick start:
  amux claude              Run Claude Code in a cloud computer
  amux codex               Run Codex in a cloud computer
  amux shell               Run a shell in a cloud computer

Management:
  amux status              Check computer status
  amux ls                  List all computers
  amux rm [id]             Remove a computer
  amux ssh                 SSH into the computer

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
	root.AddCommand(buildComputerCommand())
	root.AddCommand(buildSettingsCommand())

	// Quick access commands
	root.AddCommand(buildStatusCommand())
	root.AddCommand(buildSSHCommand())
	root.AddCommand(buildExecCommand())

	// Documentation and help commands
	root.AddCommand(buildCompletionCommand())
	root.AddCommand(buildExplainCommand())
	root.AddCommand(buildLogsCommand())

	// Agent aliases - shortcuts for `amux computer run <agent>`
	root.AddCommand(buildAgentAliasCommand("claude", "Run Claude Code in a computer"))
	root.AddCommand(buildAgentAliasCommand("codex", "Run Codex in a computer"))
	root.AddCommand(buildAgentAliasCommand("opencode", "Run OpenCode in a computer"))
	root.AddCommand(buildAgentAliasCommand("amp", "Run Amp in a computer"))
	root.AddCommand(buildAgentAliasCommand("gemini", "Run Gemini CLI in a computer"))
	root.AddCommand(buildAgentAliasCommand("droid", "Run Droid in a computer"))
	root.AddCommand(buildAgentAliasCommand("shell", "Run a shell in a computer"))

	// Command aliases for convenience
	root.AddCommand(buildLsAlias())
	root.AddCommand(buildRmAlias())

	// Register shell completions for commands
	registerCompletions(root)

	return root
}

// buildLsAlias creates an alias for `amux computer ls`
func buildLsAlias() *cobra.Command {
	var provider string
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List all amux computers (alias for `computer ls`)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Delegate to computer ls
			computerCmd := buildComputerLsCommand()
			if provider != "" {
				_ = computerCmd.Flags().Set("provider", provider)
			}
			return computerCmd.RunE(computerCmd, args)
		},
	}
	cmd.Flags().StringVarP(&provider, "provider", "p", "", "Computer provider")
	return cmd
}

// buildRmAlias creates an alias for `amux computer rm`
func buildRmAlias() *cobra.Command {
	var project bool
	var provider string
	cmd := &cobra.Command{
		Use:   "rm [id]",
		Short: "Remove a computer (alias for `computer rm`)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Delegate to computer rm
			computerCmd := buildComputerRmCommand()
			if provider != "" {
				_ = computerCmd.Flags().Set("provider", provider)
			}
			if project {
				_ = computerCmd.Flags().Set("project", "true")
			}
			return computerCmd.RunE(computerCmd, args)
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "Remove computer for current project")
	cmd.Flags().StringVarP(&provider, "provider", "p", "", "Computer provider")
	return cmd
}
