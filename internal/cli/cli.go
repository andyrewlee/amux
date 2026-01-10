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
		Use:           "amux",
		Short:         "Daytona-powered sandbox CLI for Claude Code, Codex, OpenCode, Amp, Gemini, and Droid",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.Version = "0.1.0"
	root.SetHelpCommand(&cobra.Command{Hidden: true})
	root.CompletionOptions.DisableDefaultCmd = true

	// Core commands
	root.AddCommand(buildSetupCommand())
	root.AddCommand(buildDoctorCommand())
	root.AddCommand(buildSnapshotCommand())
	root.AddCommand(buildAuthCommand())
	root.AddCommand(buildSandboxCommand())
	root.AddCommand(buildSettingsCommand())

	// Quick access commands
	root.AddCommand(buildStatusCommand())
	root.AddCommand(buildSSHCommand())
	root.AddCommand(buildExecCommand())

	// Agent aliases - shortcuts for `amux sandbox run <agent>`
	root.AddCommand(buildAgentAliasCommand("claude", "Run Claude Code in a sandbox"))
	root.AddCommand(buildAgentAliasCommand("codex", "Run Codex in a sandbox"))
	root.AddCommand(buildAgentAliasCommand("opencode", "Run OpenCode in a sandbox"))
	root.AddCommand(buildAgentAliasCommand("amp", "Run Amp in a sandbox"))
	root.AddCommand(buildAgentAliasCommand("gemini", "Run Gemini CLI in a sandbox"))
	root.AddCommand(buildAgentAliasCommand("droid", "Run Droid in a sandbox"))
	root.AddCommand(buildAgentAliasCommand("shell", "Run a shell in a sandbox"))

	return root
}
